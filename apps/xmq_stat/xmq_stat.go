// This is a utility application that polls /stats for all the producers
// of the specified topic/channel and displays aggregate stats

package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"mlib.com/xmq/internal/app"
	"mlib.com/xmq/internal/clusterinfo"
	"mlib.com/xmq/internal/http_api"
	"mlib.com/xmq/internal/version"
)

var (
	showVersion        = flag.Bool("version", false, "print version")
	topic              = flag.String("topic", "", "XMQ topic")
	channel            = flag.String("channel", "", "XMQ channel")
	interval           = flag.Duration("interval", 2*time.Second, "duration of time between polling/printing output")
	httpConnectTimeout = flag.Duration("http-client-connect-timeout", 2*time.Second, "timeout for HTTP connect")
	httpRequestTimeout = flag.Duration("http-client-request-timeout", 5*time.Second, "timeout for HTTP request")
	countNum           = numValue{}
	xmqdHTTPAddrs      = app.StringArray{}
	lookupdHTTPAddrs   = app.StringArray{}
)

type numValue struct {
	isSet bool
	value int
}

func (nv *numValue) String() string { return "N" }

func (nv *numValue) Set(s string) error {
	value, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return err
	}
	nv.value = int(value)
	nv.isSet = true
	return nil
}

func init() {
	flag.Var(&xmqdHTTPAddrs, "xmqd-http-address", "xmqd HTTP address (may be given multiple times)")
	flag.Var(&lookupdHTTPAddrs, "lookupd-http-address", "lookupd HTTP address (may be given multiple times)")
	flag.Var(&countNum, "count", "number of reports")
}

func statLoop(interval time.Duration, connectTimeout time.Duration, requestTimeout time.Duration,
	topic string, channel string, xmqdTCPAddrs []string, lookupdHTTPAddrs []string) {
	ci := clusterinfo.New(http_api.NewClient(nil, connectTimeout, requestTimeout))
	var o *clusterinfo.ChannelStats
	for i := 0; !countNum.isSet || countNum.value >= i; i++ {
		var producers clusterinfo.Producers
		var err error

		if len(lookupdHTTPAddrs) != 0 {
			producers, err = ci.GetLookupdTopicProducers(topic, lookupdHTTPAddrs)
		} else {
			producers, err = ci.GetXMQDTopicProducers(topic, xmqdHTTPAddrs)
		}
		if err != nil {
			log.Fatalf("ERROR: failed to get topic producers - %s", err)
		}

		_, channelStats, err := ci.GetXMQDStats(producers, topic, channel, false)
		if err != nil {
			log.Fatalf("ERROR: failed to get xmqd stats - %s", err)
		}

		c, ok := channelStats[channel]
		if !ok {
			log.Fatalf("ERROR: failed to find channel(%s) in stats metadata for topic(%s)", channel, topic)
		}

		if i%25 == 0 {
			fmt.Printf("%s+%s+%s\n",
				"------rate------",
				"----------------depth----------------",
				"--------------metadata---------------")
			fmt.Printf("%7s %7s | %7s %7s %7s %5s %5s | %7s %7s %12s %7s\n",
				"ingress", "egress",
				"total", "mem", "disk", "inflt",
				"def", "req", "t-o", "msgs", "clients")
		}

		if o == nil {
			o = c
			time.Sleep(interval)
			continue
		}

		// TODO: paused
		fmt.Printf("%7d %7d | %7d %7d %7d %5d %5d | %7d %7d %12d %7d\n",
			int64(float64(c.MessageCount-o.MessageCount)/interval.Seconds()),
			int64(float64(c.MessageCount-o.MessageCount-(c.Depth-o.Depth))/interval.Seconds()),
			c.Depth,
			c.MemoryDepth,
			c.BackendDepth,
			c.InFlightCount,
			c.DeferredCount,
			c.RequeueCount,
			c.TimeoutCount,
			c.MessageCount,
			c.ClientCount)

		o = c
		time.Sleep(interval)
	}
	os.Exit(0)
}

func checkAddrs(addrs []string) error {
	for _, a := range addrs {
		if strings.HasPrefix(a, "http") {
			return errors.New("address should not contain scheme")
		}
	}
	return nil
}

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("xmq_stat v%s\n", version.Binary)
		return
	}

	if *topic == "" || *channel == "" {
		log.Fatal("--topic and --channel are required")
	}

	intvl := *interval
	if int64(intvl) <= 0 {
		log.Fatal("--interval should be positive")
	}

	connectTimeout := *httpConnectTimeout
	if int64(connectTimeout) <= 0 {
		log.Fatal("--http-client-connect-timeout should be positive")
	}

	requestTimeout := *httpRequestTimeout
	if int64(requestTimeout) <= 0 {
		log.Fatal("--http-client-request-timeout should be positive")
	}

	if countNum.isSet && countNum.value <= 0 {
		log.Fatal("--count should be positive")
	}

	if len(xmqdHTTPAddrs) == 0 && len(lookupdHTTPAddrs) == 0 {
		log.Fatal("--xmqd-http-address or --lookupd-http-address required")
	}
	if len(xmqdHTTPAddrs) > 0 && len(lookupdHTTPAddrs) > 0 {
		log.Fatal("use --xmqd-http-address or --lookupd-http-address not both")
	}

	if err := checkAddrs(xmqdHTTPAddrs); err != nil {
		log.Fatalf("--xmqd-http-address error - %s", err)
	}

	if err := checkAddrs(lookupdHTTPAddrs); err != nil {
		log.Fatalf("--lookupd-http-address error - %s", err)
	}

	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)

	go statLoop(intvl, connectTimeout, requestTimeout, *topic, *channel, xmqdHTTPAddrs, lookupdHTTPAddrs)

	<-termChan
}
