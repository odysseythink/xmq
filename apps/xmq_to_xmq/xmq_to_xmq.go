// This is an XMQ client that reads the specified topic/channel
// and re-publishes the messages to destination xmqd via TCP

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/bitly/go-hostpool"
	"github.com/bitly/timer_metrics"
	"mlib.com/go-xmq"
	"mlib.com/xmq/internal/app"
	"mlib.com/xmq/internal/protocol"
	"mlib.com/xmq/internal/version"
)

const (
	ModeRoundRobin = iota
	ModeHostPool
)

var (
	showVersion = flag.Bool("version", false, "print version string")
	channel     = flag.String("channel", "xmq_to_xmq", "xmq channel")
	destTopic   = flag.String("destination-topic", "", "use this destination topic for all consumed topics (default is consumed topic name)")
	maxInFlight = flag.Int("max-in-flight", 200, "max number of messages to allow in flight")

	statusEvery = flag.Int("status-every", 250, "the # of requests between logging status (per destination), 0 disables")
	mode        = flag.String("mode", "hostpool", "the upstream request mode options: round-robin, hostpool (default), epsilon-greedy")

	xmqdTCPAddrs        = app.StringArray{}
	lookupdHTTPAddrs    = app.StringArray{}
	destXmqdTCPAddrs    = app.StringArray{}
	whitelistJSONFields = app.StringArray{}
	topics              = app.StringArray{}

	requireJSONField = flag.String("require-json-field", "", "for JSON messages: only pass messages that contain this field")
	requireJSONValue = flag.String("require-json-value", "", "for JSON messages: only pass messages in which the required field has this value")
)

func init() {
	flag.Var(&xmqdTCPAddrs, "xmqd-tcp-address", "xmqd TCP address (may be given multiple times)")
	flag.Var(&destXmqdTCPAddrs, "destination-xmqd-tcp-address", "destination xmqd TCP address (may be given multiple times)")
	flag.Var(&lookupdHTTPAddrs, "lookupd-http-address", "lookupd HTTP address (may be given multiple times)")
	flag.Var(&topics, "topic", "xmq topic (may be given multiple times)")
	flag.Var(&whitelistJSONFields, "whitelist-json-field", "for JSON messages: pass this field (may be given multiple times)")
}

type PublishHandler struct {
	// 64bit atomic vars need to be first for proper alignment on 32bit platforms
	counter uint64

	addresses app.StringArray
	producers map[string]*xmq.Producer
	mode      int
	hostPool  hostpool.HostPool
	respChan  chan *xmq.ProducerTransaction

	requireJSONValueParsed   bool
	requireJSONValueIsNumber bool
	requireJSONNumber        float64

	perAddressStatus map[string]*timer_metrics.TimerMetrics
	timermetrics     *timer_metrics.TimerMetrics
}

type TopicHandler struct {
	publishHandler   *PublishHandler
	destinationTopic string
}

func (ph *PublishHandler) responder() {
	var msg *xmq.Message
	var startTime time.Time
	var address string
	var hostPoolResponse hostpool.HostPoolResponse

	for t := range ph.respChan {
		switch ph.mode {
		case ModeRoundRobin:
			msg = t.Args[0].(*xmq.Message)
			startTime = t.Args[1].(time.Time)
			hostPoolResponse = nil
			address = t.Args[2].(string)
		case ModeHostPool:
			msg = t.Args[0].(*xmq.Message)
			startTime = t.Args[1].(time.Time)
			hostPoolResponse = t.Args[2].(hostpool.HostPoolResponse)
			address = hostPoolResponse.Host()
		}

		success := t.Error == nil

		if hostPoolResponse != nil {
			if !success {
				hostPoolResponse.Mark(errors.New("failed"))
			} else {
				hostPoolResponse.Mark(nil)
			}
		}

		if success {
			msg.Finish()
		} else {
			msg.Requeue(-1)
		}

		ph.perAddressStatus[address].Status(startTime)
		ph.timermetrics.Status(startTime)
	}
}

func (ph *PublishHandler) shouldPassMessage(js map[string]interface{}) (bool, bool) {
	pass := true
	backoff := false

	if *requireJSONField == "" {
		return pass, backoff
	}

	if *requireJSONValue != "" && !ph.requireJSONValueParsed {
		// cache conversion in case needed while filtering json
		var err error
		ph.requireJSONNumber, err = strconv.ParseFloat(*requireJSONValue, 64)
		ph.requireJSONValueIsNumber = (err == nil)
		ph.requireJSONValueParsed = true
	}

	v, ok := js[*requireJSONField]
	if !ok {
		pass = false
		if *requireJSONValue != "" {
			log.Printf("ERROR: missing field to check required value")
			backoff = true
		}
	} else if *requireJSONValue != "" {
		// if command-line argument can't convert to float, then it can't match a number
		// if it can, also integers (up to 2^53 or so) can be compared as float64
		if s, ok := v.(string); ok {
			if s != *requireJSONValue {
				pass = false
			}
		} else if ph.requireJSONValueIsNumber {
			f, ok := v.(float64)
			if !ok || f != ph.requireJSONNumber {
				pass = false
			}
		} else {
			// json value wasn't a plain string, and argument wasn't a number
			// give up on comparisons of other types
			pass = false
		}
	}

	return pass, backoff
}

func filterMessage(js map[string]interface{}, rawMsg []byte) ([]byte, error) {
	if len(whitelistJSONFields) == 0 {
		// no change
		return rawMsg, nil
	}

	newMsg := make(map[string]interface{}, len(whitelistJSONFields))

	for _, key := range whitelistJSONFields {
		value, ok := js[key]
		if ok {
			// avoid printing int as float (go 1.0)
			switch tvalue := value.(type) {
			case float64:
				ivalue := int64(tvalue)
				if float64(ivalue) == tvalue {
					newMsg[key] = ivalue
				} else {
					newMsg[key] = tvalue
				}
			default:
				newMsg[key] = value
			}
		}
	}

	newRawMsg, err := json.Marshal(newMsg)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal filtered message %v", newMsg)
	}
	return newRawMsg, nil
}

func (t *TopicHandler) HandleMessage(m *xmq.Message) error {
	return t.publishHandler.HandleMessage(m, t.destinationTopic)
}

func (ph *PublishHandler) HandleMessage(m *xmq.Message, destinationTopic string) error {
	var err error
	msgBody := m.Body

	if *requireJSONField != "" || len(whitelistJSONFields) > 0 {
		var js map[string]interface{}
		err = json.Unmarshal(msgBody, &js)
		if err != nil {
			log.Printf("ERROR: Unable to decode json: %s", msgBody)
			return nil
		}

		if pass, backoff := ph.shouldPassMessage(js); !pass {
			if backoff {
				return errors.New("backoff")
			}
			return nil
		}

		msgBody, err = filterMessage(js, msgBody)

		if err != nil {
			log.Printf("ERROR: filterMessage() failed: %s", err)
			return err
		}
	}

	startTime := time.Now()

	switch ph.mode {
	case ModeRoundRobin:
		counter := atomic.AddUint64(&ph.counter, 1)
		idx := counter % uint64(len(ph.addresses))
		addr := ph.addresses[idx]
		p := ph.producers[addr]
		err = p.PublishAsync(destinationTopic, msgBody, ph.respChan, m, startTime, addr)
	case ModeHostPool:
		hostPoolResponse := ph.hostPool.Get()
		p := ph.producers[hostPoolResponse.Host()]
		err = p.PublishAsync(destinationTopic, msgBody, ph.respChan, m, startTime, hostPoolResponse)
		if err != nil {
			hostPoolResponse.Mark(err)
		}
	}

	if err != nil {
		return err
	}
	m.DisableAutoResponse()
	return nil
}

func main() {
	var selectedMode int

	cCfg := xmq.NewConfig()
	pCfg := xmq.NewConfig()

	flag.Var(&xmq.ConfigFlag{cCfg}, "consumer-opt", "option to passthrough to xmq.Consumer (may be given multiple times")
	flag.Var(&xmq.ConfigFlag{pCfg}, "producer-opt", "option to passthrough to xmq.Producer (may be given multiple times")

	flag.Parse()

	if *showVersion {
		fmt.Printf("xmq_to_xmq v%s\n", version.Binary)
		return
	}

	if len(topics) == 0 || *channel == "" {
		log.Fatal("--topic and --channel are required")
	}

	for _, topic := range topics {
		if !protocol.IsValidTopicName(topic) {
			log.Fatal("--topic is invalid")
		}
	}

	if *destTopic != "" && !protocol.IsValidTopicName(*destTopic) {
		log.Fatal("--destination-topic is invalid")
	}

	if !protocol.IsValidChannelName(*channel) {
		log.Fatal("--channel is invalid")
	}

	if len(xmqdTCPAddrs) == 0 && len(lookupdHTTPAddrs) == 0 {
		log.Fatal("--xmqd-tcp-address or --lookupd-http-address required")
	}
	if len(xmqdTCPAddrs) > 0 && len(lookupdHTTPAddrs) > 0 {
		log.Fatal("use --xmqd-tcp-address or --lookupd-http-address not both")
	}

	if len(destXmqdTCPAddrs) == 0 {
		log.Fatal("--destination-xmqd-tcp-address required")
	}

	switch *mode {
	case "round-robin":
		selectedMode = ModeRoundRobin
	case "hostpool", "epsilon-greedy":
		selectedMode = ModeHostPool
	}

	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	defaultUA := fmt.Sprintf("xmq_to_xmq/%s go-xmq/%s", version.Binary, xmq.VERSION)

	cCfg.UserAgent = defaultUA
	cCfg.MaxInFlight = *maxInFlight
	pCfg.UserAgent = defaultUA

	producers := make(map[string]*xmq.Producer)
	for _, addr := range destXmqdTCPAddrs {
		producer, err := xmq.NewProducer(addr, pCfg)
		if err != nil {
			log.Fatalf("failed creating producer %s", err)
		}
		producers[addr] = producer
	}

	perAddressStatus := make(map[string]*timer_metrics.TimerMetrics)
	if len(destXmqdTCPAddrs) == 1 {
		// disable since there is only one address
		perAddressStatus[destXmqdTCPAddrs[0]] = timer_metrics.NewTimerMetrics(0, "")
	} else {
		for _, a := range destXmqdTCPAddrs {
			perAddressStatus[a] = timer_metrics.NewTimerMetrics(*statusEvery,
				fmt.Sprintf("[%s]:", a))
		}
	}

	hostPool := hostpool.New(destXmqdTCPAddrs)
	if *mode == "epsilon-greedy" {
		hostPool = hostpool.NewEpsilonGreedy(destXmqdTCPAddrs, 0, &hostpool.LinearEpsilonValueCalculator{})
	}

	var consumerList []*xmq.Consumer

	publisher := &PublishHandler{
		addresses:        destXmqdTCPAddrs,
		producers:        producers,
		mode:             selectedMode,
		hostPool:         hostPool,
		respChan:         make(chan *xmq.ProducerTransaction, len(destXmqdTCPAddrs)),
		perAddressStatus: perAddressStatus,
		timermetrics:     timer_metrics.NewTimerMetrics(*statusEvery, "[aggregate]:"),
	}

	for _, topic := range topics {
		consumer, err := xmq.NewConsumer(topic, *channel, cCfg)
		consumerList = append(consumerList, consumer)
		if err != nil {
			log.Fatal(err)
		}

		publishTopic := topic
		if *destTopic != "" {
			publishTopic = *destTopic
		}
		topicHandler := &TopicHandler{
			publishHandler:   publisher,
			destinationTopic: publishTopic,
		}
		consumer.AddConcurrentHandlers(topicHandler, len(destXmqdTCPAddrs))
	}
	for i := 0; i < len(destXmqdTCPAddrs); i++ {
		go publisher.responder()
	}

	for _, consumer := range consumerList {
		err := consumer.ConnectToXMQDs(xmqdTCPAddrs)
		if err != nil {
			log.Fatal(err)
		}
	}

	for _, consumer := range consumerList {
		err := consumer.ConnectToXMQLookupds(lookupdHTTPAddrs)
		if err != nil {
			log.Fatal(err)
		}
	}

	<-termChan // wait for signal

	for _, consumer := range consumerList {
		consumer.Stop()
	}
	for _, consumer := range consumerList {
		<-consumer.StopChan
	}
}
