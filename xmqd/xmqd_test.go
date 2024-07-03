package xmqd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"
	"mlib.com/xmq/internal/http_api"
	"mlib.com/xmq/internal/test"
	"mlib.com/xmq/xmqlookupd"
)

const (
	ConnectTimeout = 2 * time.Second
	RequestTimeout = 5 * time.Second
)

func getMetadata(n *XMQD) (*Metadata, error) {
	fn := newMetadataFile(n.getOpts())
	data, err := os.ReadFile(fn)
	if err != nil {
		return nil, err
	}

	var m Metadata
	err = json.Unmarshal(data, &m)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func TestStartup(t *testing.T) {
	var msg *Message

	iterations := 300
	doneExitChan := make(chan int)

	opts := NewOptions()

	opts.MemQueueSize = 100
	opts.MaxBytesPerFile = 10240
	_, _, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)

	origDataPath := opts.DataPath

	topicName := "xmqd_test" + strconv.Itoa(int(time.Now().Unix()))

	exitChan := make(chan int)
	go func() {
		<-exitChan
		xmqd.Exit()
		doneExitChan <- 1
	}()

	// verify xmqd metadata shows no topics
	err := xmqd.PersistMetadata()
	test.Nil(t, err)
	atomic.StoreInt32(&xmqd.isLoading, 1)
	xmqd.cluster.GetTopic(topicName) // will not persist if `flagLoading`
	m, err := getMetadata(xmqd)
	test.Nil(t, err)
	test.Equal(t, 0, len(m.Topics))
	xmqd.DeleteExistingTopic(topicName)
	atomic.StoreInt32(&xmqd.isLoading, 0)

	body := make([]byte, 256)
	topic := xmqd.cluster.GetTopic(topicName)
	for i := 0; i < iterations; i++ {
		msg := NewMessage(topic.GenerateID(), body)
		topic.PutMessage(msg)
	}

	t.Logf("pulling from channel")
	channel1 := topic.GetChannel("ch1")

	t.Logf("read %d msgs", iterations/2)
	for i := 0; i < iterations/2; i++ {
		select {
		case msg = <-channel1.memoryMsgChan:
		case b := <-channel1.backend.ReadChan():
			msg, _ = decodeMessage(b)
		}
		t.Logf("read message %d", i+1)
		test.Equal(t, body, msg.Body)
	}

	for {
		if channel1.Depth() == int64(iterations/2) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// make sure metadata shows the topic
	m, err = getMetadata(xmqd)
	test.Nil(t, err)
	test.Equal(t, 1, len(m.Topics))
	test.Equal(t, topicName, m.Topics[0].Name)

	exitChan <- 1
	<-doneExitChan

	// start up a new xmqd w/ the same folder

	opts = NewOptions()

	opts.MemQueueSize = 100
	opts.MaxBytesPerFile = 10240
	opts.DataPath = origDataPath
	_, _, xmqd = mustStartXMQD(opts)

	go func() {
		<-exitChan
		xmqd.Exit()
		doneExitChan <- 1
	}()

	topic = xmqd.cluster.GetTopic(topicName)
	// should be empty; channel should have drained everything
	count := topic.Depth()
	test.Equal(t, int64(0), count)

	channel1 = topic.GetChannel("ch1")

	for {
		if channel1.Depth() == int64(iterations/2) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// read the other half of the messages
	for i := 0; i < iterations/2; i++ {
		select {
		case msg = <-channel1.memoryMsgChan:
		case b := <-channel1.backend.ReadChan():
			msg, _ = decodeMessage(b)
		}
		t.Logf("read message %d", i+1)
		test.Equal(t, body, msg.Body)
	}

	// verify we drained things
	test.Equal(t, 0, len(topic.memoryMsgChan))
	test.Equal(t, int64(0), topic.backend.Depth())

	exitChan <- 1
	<-doneExitChan
}

func TestEphemeralTopicsAndChannels(t *testing.T) {
	// ephemeral topics/channels are lazily removed after the last channel/client is removed
	opts := NewOptions()

	opts.MemQueueSize = 100
	_, _, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)

	topicName := "ephemeral_topic" + strconv.Itoa(int(time.Now().Unix())) + "#ephemeral"
	doneExitChan := make(chan int)

	exitChan := make(chan int)
	go func() {
		<-exitChan
		xmqd.Exit()
		doneExitChan <- 1
	}()

	body := []byte("an_ephemeral_message")
	topic := xmqd.cluster.GetTopic(topicName)
	ephemeralChannel := topic.GetChannel("ch1#ephemeral")
	clientID := uuid.NewV4().String()
	client := newClientV2(clientID, nil, xmqd)
	err := ephemeralChannel.AddClient(client.ID, client)
	test.Equal(t, err, nil)

	msg := NewMessage(topic.GenerateID(), body)
	topic.PutMessage(msg)
	msg = <-ephemeralChannel.memoryMsgChan
	test.Equal(t, body, msg.Body)

	ephemeralChannel.RemoveClient(client.ID)

	time.Sleep(100 * time.Millisecond)

	topic.Lock()
	numChannels := len(topic.Channels)
	topic.Unlock()
	test.Equal(t, 0, numChannels)

	xmqd.Lock()
	numTopics := len(xmqd.topicMap)
	xmqd.Unlock()
	test.Equal(t, 0, numTopics)

	exitChan <- 1
	<-doneExitChan
}

func TestPauseMetadata(t *testing.T) {
	opts := NewOptions()

	_, _, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	// avoid concurrency issue of async PersistMetadata() calls
	atomic.StoreInt32(&xmqd.isLoading, 1)
	topicName := "pause_metadata" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqd.cluster.GetTopic(topicName)
	channel := topic.GetChannel("ch")
	atomic.StoreInt32(&xmqd.isLoading, 0)
	xmqd.PersistMetadata()

	var isPaused = func(n *XMQD, topicIndex int, channelIndex int) bool {
		m, _ := getMetadata(n)
		return m.Topics[topicIndex].Channels[channelIndex].Paused
	}

	test.Equal(t, false, isPaused(xmqd, 0, 0))

	channel.Pause()
	test.Equal(t, false, isPaused(xmqd, 0, 0))

	xmqd.PersistMetadata()
	test.Equal(t, true, isPaused(xmqd, 0, 0))

	channel.UnPause()
	test.Equal(t, true, isPaused(xmqd, 0, 0))

	xmqd.PersistMetadata()
	test.Equal(t, false, isPaused(xmqd, 0, 0))
}

func mustStartXMQLookupd(opts *xmqlookupd.Options) (net.Addr, net.Addr, *xmqlookupd.XMQLookupd) {
	opts.GrpcAddress = "127.0.0.1:0"
	opts.HTTPAddress = "127.0.0.1:0"
	lookupd, err := xmqlookupd.New(opts)
	if err != nil {
		panic(err)
	}
	go func() {
		err := lookupd.Main()
		if err != nil {
			panic(err)
		}
	}()
	return lookupd.RealGrpcAddr(), lookupd.RealHTTPAddr(), lookupd
}

func TestReconfigure(t *testing.T) {
	lopts := xmqlookupd.NewOptions()

	lopts1 := *lopts
	_, _, lookupd1 := mustStartXMQLookupd(&lopts1)
	defer lookupd1.Exit()
	lopts2 := *lopts
	_, _, lookupd2 := mustStartXMQLookupd(&lopts2)
	defer lookupd2.Exit()
	lopts3 := *lopts
	_, _, lookupd3 := mustStartXMQLookupd(&lopts3)
	defer lookupd3.Exit()

	opts := NewOptions()

	_, _, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	newOpts := NewOptions()
	newOpts.XMQLookupdGrpcAddresses = []string{lookupd1.RealGrpcAddr().String()}
	xmqd.swapOpts(newOpts)
	xmqd.triggerOptsNotification()
	test.Equal(t, 1, len(xmqd.getOpts().XMQLookupdGrpcAddresses))

	var numLookupPeers int
	for i := 0; i < 100; i++ {
		numLookupPeers = len(xmqd.lookupPeers.Load().([]*lookupPeer))
		if numLookupPeers == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	test.Equal(t, 1, numLookupPeers)

	newOpts = NewOptions()
	newOpts.XMQLookupdGrpcAddresses = []string{lookupd2.RealGrpcAddr().String(), lookupd3.RealGrpcAddr().String()}
	xmqd.swapOpts(newOpts)
	xmqd.triggerOptsNotification()
	test.Equal(t, 2, len(xmqd.getOpts().XMQLookupdGrpcAddresses))

	for i := 0; i < 100; i++ {
		numLookupPeers = len(xmqd.lookupPeers.Load().([]*lookupPeer))
		if numLookupPeers == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	test.Equal(t, 2, numLookupPeers)

	var lookupPeers []string
	for _, lp := range xmqd.lookupPeers.Load().([]*lookupPeer) {
		lookupPeers = append(lookupPeers, lp.addr)
	}
	test.Equal(t, newOpts.XMQLookupdGrpcAddresses, lookupPeers)
}

func TestCluster(t *testing.T) {
	lopts := xmqlookupd.NewOptions()

	lopts.BroadcastAddress = "127.0.0.1"
	_, _, lookupd := mustStartXMQLookupd(lopts)

	opts := NewOptions()

	opts.XMQLookupdGrpcAddresses = []string{lookupd.RealGrpcAddr().String()}
	opts.BroadcastAddress = "127.0.0.1"
	_, _, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	topicName := "cluster_test" + strconv.Itoa(int(time.Now().Unix()))

	hostname, err := os.Hostname()
	test.Nil(t, err)

	url := fmt.Sprintf("http://%s/topic/create?topic=%s", xmqd.RealHTTPAddr(), topicName)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).POSTV1(url)
	test.Nil(t, err)

	url = fmt.Sprintf("http://%s/channel/create?topic=%s&channel=ch", xmqd.RealHTTPAddr(), topicName)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).POSTV1(url)
	test.Nil(t, err)

	// allow some time for xmqd to push info to xmqlookupd
	time.Sleep(350 * time.Millisecond)

	var d map[string][]struct {
		Hostname         string `json:"hostname"`
		BroadcastAddress string `json:"broadcast_address"`
		TCPPort          int    `json:"tcp_port"`
		Tombstoned       bool   `json:"tombstoned"`
	}

	endpoint := fmt.Sprintf("http://%s/debug", lookupd.RealHTTPAddr())
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &d)
	test.Nil(t, err)

	topicData := d["topic:"+topicName+":"]
	test.Equal(t, 1, len(topicData))

	test.Equal(t, hostname, topicData[0].Hostname)
	test.Equal(t, "127.0.0.1", topicData[0].BroadcastAddress)
	test.Equal(t, xmqd.RealTCPAddr().(*net.TCPAddr).Port, topicData[0].TCPPort)
	test.Equal(t, false, topicData[0].Tombstoned)

	channelData := d["channel:"+topicName+":ch"]
	test.Equal(t, 1, len(channelData))

	test.Equal(t, hostname, channelData[0].Hostname)
	test.Equal(t, "127.0.0.1", channelData[0].BroadcastAddress)
	test.Equal(t, xmqd.RealTCPAddr().(*net.TCPAddr).Port, channelData[0].TCPPort)
	test.Equal(t, false, channelData[0].Tombstoned)

	var lr struct {
		Producers []struct {
			Hostname         string `json:"hostname"`
			BroadcastAddress string `json:"broadcast_address"`
			TCPPort          int    `json:"tcp_port"`
		} `json:"producers"`
		Channels []string `json:"channels"`
	}

	endpoint = fmt.Sprintf("http://%s/lookup?topic=%s", lookupd.RealHTTPAddr(), topicName)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &lr)
	test.Nil(t, err)

	test.Equal(t, 1, len(lr.Producers))
	test.Equal(t, hostname, lr.Producers[0].Hostname)
	test.Equal(t, "127.0.0.1", lr.Producers[0].BroadcastAddress)
	test.Equal(t, xmqd.RealTCPAddr().(*net.TCPAddr).Port, lr.Producers[0].TCPPort)
	test.Equal(t, 1, len(lr.Channels))
	test.Equal(t, "ch", lr.Channels[0])

	url = fmt.Sprintf("http://%s/topic/delete?topic=%s", xmqd.RealHTTPAddr(), topicName)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).POSTV1(url)
	test.Nil(t, err)

	// allow some time for xmqd to push info to xmqlookupd
	time.Sleep(350 * time.Millisecond)

	endpoint = fmt.Sprintf("http://%s/lookup?topic=%s", lookupd.RealHTTPAddr(), topicName)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &lr)
	test.Nil(t, err)

	test.Equal(t, 0, len(lr.Producers))

	var dd map[string][]interface{}
	endpoint = fmt.Sprintf("http://%s/debug", lookupd.RealHTTPAddr())
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &dd)
	test.Nil(t, err)

	test.Equal(t, 0, len(dd["topic:"+topicName+":"]))
	test.Equal(t, 0, len(dd["channel:"+topicName+":ch"]))
}

func TestSetHealth(t *testing.T) {
	opts := NewOptions()

	xmqd, err := New(opts)
	test.Nil(t, err)
	defer xmqd.Exit()

	test.Nil(t, xmqd.GetError())
	test.Equal(t, true, xmqd.IsHealthy())

	xmqd.SetHealth(nil)
	test.Nil(t, xmqd.GetError())
	test.Equal(t, true, xmqd.IsHealthy())

	xmqd.SetHealth(errors.New("health error"))
	test.NotNil(t, xmqd.GetError())
	test.Equal(t, "NOK - health error", xmqd.GetHealth())
	test.Equal(t, false, xmqd.IsHealthy())

	xmqd.SetHealth(nil)
	test.Nil(t, xmqd.GetError())
	test.Equal(t, "OK", xmqd.GetHealth())
	test.Equal(t, true, xmqd.IsHealthy())
}

func TestUnixSocketStartup(t *testing.T) {
	isSocket := func(path string) bool {
		fileInfo, err := os.Stat(path)
		if err != nil {
			return false
		}
		return fileInfo.Mode().Type() == fs.ModeSocket
	}

	opts := NewOptions()

	_, _, xmqd := mustUnixSocketStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	test.Equal(t, isSocket(opts.TCPAddress), true)
	test.Equal(t, isSocket(opts.HTTPAddress), true)
}
