package xmqlookupd

import (
	"fmt"
	"net"
	"testing"
	"time"

	"mlib.com/go-xmq"
	"mlib.com/xmq/internal/clusterinfo"
	"mlib.com/xmq/internal/http_api"
	"mlib.com/xmq/internal/protocol"
	"mlib.com/xmq/internal/test"
)

const (
	ConnectTimeout = 2 * time.Second
	RequestTimeout = 5 * time.Second
	TCPPort        = 5000
	HTTPPort       = 5555
	HostAddr       = "ip.address"
	XMQDVersion    = "fake-version"
)

type ProducersDoc struct {
	Producers []interface{} `json:"producers"`
}

type TopicsDoc struct {
	Topics []interface{} `json:"topics"`
}

type LookupDoc struct {
	Channels  []interface{}        `json:"channels"`
	Producers []*protocol.PeerInfo `json:"producers"`
}

func mustStartLookupd(opts *Options) (*net.TCPAddr, *net.TCPAddr, *XMQLookupd) {
	opts.GrpcAddress = "127.0.0.1:0"
	opts.HTTPAddress = "127.0.0.1:0"
	xmqlookupd, err := New(opts)
	if err != nil {
		panic(err)
	}
	go func() {
		err := xmqlookupd.Main()
		if err != nil {
			panic(err)
		}
	}()
	return xmqlookupd.RealGrpcAddr(), xmqlookupd.RealHTTPAddr(), xmqlookupd
}

func mustConnectLookupd(t *testing.T, tcpAddr *net.TCPAddr) net.Conn {
	conn, err := net.DialTimeout("tcp", tcpAddr.String(), time.Second)
	if err != nil {
		t.Fatal("failed to connect to lookupd")
	}
	conn.Write(xmq.MagicV1)
	return conn
}

func identify(t *testing.T, conn net.Conn) {
	ci := make(map[string]interface{})
	ci["tcp_port"] = TCPPort
	ci["http_port"] = HTTPPort
	ci["broadcast_address"] = HostAddr
	ci["hostname"] = HostAddr
	ci["version"] = XMQDVersion
	cmd, _ := xmq.Identify(ci)
	_, err := cmd.WriteTo(conn)
	test.Nil(t, err)
	_, err = xmq.ReadResponse(conn)
	test.Nil(t, err)
}

func TestBasicLookupd(t *testing.T) {
	opts := NewOptions()

	tcpAddr, httpAddr, xmqlookupd := mustStartLookupd(opts)
	defer xmqlookupd.Exit()

	topics := xmqlookupd.DB.FindRegistrations("topic", "*", "*")
	test.Equal(t, 0, len(topics))

	topicName := "connectmsg"

	conn := mustConnectLookupd(t, tcpAddr)

	identify(t, conn)

	xmq.Register(topicName, "channel1").WriteTo(conn)
	v, err := xmq.ReadResponse(conn)
	test.Nil(t, err)
	test.Equal(t, []byte("OK"), v)

	pr := ProducersDoc{}
	endpoint := fmt.Sprintf("http://%s/nodes", httpAddr)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &pr)
	test.Nil(t, err)

	t.Logf("got %v", pr)
	test.Equal(t, 1, len(pr.Producers))

	topics = xmqlookupd.DB.FindRegistrations("topic", topicName, "")
	test.Equal(t, 1, len(topics))

	producers := xmqlookupd.DB.FindProducers("topic", topicName, "")
	test.Equal(t, 1, len(producers))
	producer := producers[0]

	test.Equal(t, HostAddr, producer.peerInfo.BroadcastAddress)
	test.Equal(t, HostAddr, producer.peerInfo.Hostname)
	test.Equal(t, TCPPort, producer.peerInfo.TcpPort)
	test.Equal(t, HTTPPort, producer.peerInfo.HttpPort)

	tr := TopicsDoc{}
	endpoint = fmt.Sprintf("http://%s/topics", httpAddr)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &tr)
	test.Nil(t, err)

	t.Logf("got %v", tr)
	test.Equal(t, 1, len(tr.Topics))

	lr := LookupDoc{}
	endpoint = fmt.Sprintf("http://%s/lookup?topic=%s", httpAddr, topicName)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &lr)
	test.Nil(t, err)

	t.Logf("got %v", lr)
	test.Equal(t, 1, len(lr.Channels))
	test.Equal(t, 1, len(lr.Producers))
	for _, p := range lr.Producers {
		test.Equal(t, TCPPort, p.TcpPort)
		test.Equal(t, HTTPPort, p.HttpPort)
		test.Equal(t, HostAddr, p.BroadcastAddress)
		test.Equal(t, XMQDVersion, p.Version)
	}

	conn.Close()
	time.Sleep(10 * time.Millisecond)

	// now there should be no producers, but still topic/channel entries
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &lr)
	test.Nil(t, err)

	test.Equal(t, 1, len(lr.Channels))
	test.Equal(t, 0, len(lr.Producers))
}

func TestChannelUnregister(t *testing.T) {
	opts := NewOptions()

	tcpAddr, httpAddr, xmqlookupd := mustStartLookupd(opts)
	defer xmqlookupd.Exit()

	topics := xmqlookupd.DB.FindRegistrations("topic", "*", "*")
	test.Equal(t, 0, len(topics))

	topicName := "channel_unregister"

	conn := mustConnectLookupd(t, tcpAddr)
	defer conn.Close()

	identify(t, conn)

	xmq.Register(topicName, "ch1").WriteTo(conn)
	v, err := xmq.ReadResponse(conn)
	test.Nil(t, err)
	test.Equal(t, []byte("OK"), v)

	topics = xmqlookupd.DB.FindRegistrations("topic", topicName, "")
	test.Equal(t, 1, len(topics))

	channels := xmqlookupd.DB.FindRegistrations("channel", topicName, "*")
	test.Equal(t, 1, len(channels))

	xmq.UnRegister(topicName, "ch1").WriteTo(conn)
	v, err = xmq.ReadResponse(conn)
	test.Nil(t, err)
	test.Equal(t, []byte("OK"), v)

	topics = xmqlookupd.DB.FindRegistrations("topic", topicName, "")
	test.Equal(t, 1, len(topics))

	// we should still have mention of the topic even though there is no producer
	// (ie. we haven't *deleted* the channel, just unregistered as a producer)
	channels = xmqlookupd.DB.FindRegistrations("channel", topicName, "*")
	test.Equal(t, 1, len(channels))

	pr := ProducersDoc{}
	endpoint := fmt.Sprintf("http://%s/lookup?topic=%s", httpAddr, topicName)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &pr)
	test.Nil(t, err)
	t.Logf("got %v", pr)
	test.Equal(t, 1, len(pr.Producers))
}

func TestTombstoneRecover(t *testing.T) {
	opts := NewOptions()

	opts.TombstoneLifetime = 50 * time.Millisecond
	tcpAddr, httpAddr, xmqlookupd := mustStartLookupd(opts)
	defer xmqlookupd.Exit()

	topicName := "tombstone_recover"
	topicName2 := topicName + "2"

	conn := mustConnectLookupd(t, tcpAddr)
	defer conn.Close()

	identify(t, conn)

	xmq.Register(topicName, "channel1").WriteTo(conn)
	_, err := xmq.ReadResponse(conn)
	test.Nil(t, err)

	xmq.Register(topicName2, "channel2").WriteTo(conn)
	_, err = xmq.ReadResponse(conn)
	test.Nil(t, err)

	endpoint := fmt.Sprintf("http://%s/topic/tombstone?topic=%s&node=%s:%d",
		httpAddr, topicName, HostAddr, HTTPPort)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).POSTV1(endpoint)
	test.Nil(t, err)

	pr := ProducersDoc{}

	endpoint = fmt.Sprintf("http://%s/lookup?topic=%s", httpAddr, topicName)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &pr)
	test.Nil(t, err)
	test.Equal(t, 0, len(pr.Producers))

	endpoint = fmt.Sprintf("http://%s/lookup?topic=%s", httpAddr, topicName2)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &pr)
	test.Nil(t, err)
	test.Equal(t, 1, len(pr.Producers))

	time.Sleep(75 * time.Millisecond)

	endpoint = fmt.Sprintf("http://%s/lookup?topic=%s", httpAddr, topicName)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &pr)
	test.Nil(t, err)
	test.Equal(t, 1, len(pr.Producers))
}

func TestTombstoneUnregister(t *testing.T) {
	opts := NewOptions()

	opts.TombstoneLifetime = 50 * time.Millisecond
	tcpAddr, httpAddr, xmqlookupd := mustStartLookupd(opts)
	defer xmqlookupd.Exit()

	topicName := "tombstone_unregister"

	conn := mustConnectLookupd(t, tcpAddr)
	defer conn.Close()

	identify(t, conn)

	xmq.Register(topicName, "channel1").WriteTo(conn)
	_, err := xmq.ReadResponse(conn)
	test.Nil(t, err)

	endpoint := fmt.Sprintf("http://%s/topic/tombstone?topic=%s&node=%s:%d",
		httpAddr, topicName, HostAddr, HTTPPort)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).POSTV1(endpoint)
	test.Nil(t, err)

	pr := ProducersDoc{}

	endpoint = fmt.Sprintf("http://%s/lookup?topic=%s", httpAddr, topicName)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &pr)
	test.Nil(t, err)
	test.Equal(t, 0, len(pr.Producers))

	xmq.UnRegister(topicName, "").WriteTo(conn)
	_, err = xmq.ReadResponse(conn)
	test.Nil(t, err)

	time.Sleep(55 * time.Millisecond)

	endpoint = fmt.Sprintf("http://%s/lookup?topic=%s", httpAddr, topicName)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &pr)
	test.Nil(t, err)
	test.Equal(t, 0, len(pr.Producers))
}

func TestInactiveNodes(t *testing.T) {
	opts := NewOptions()

	opts.InactiveProducerTimeout = 200 * time.Millisecond
	tcpAddr, httpAddr, xmqlookupd := mustStartLookupd(opts)
	defer xmqlookupd.Exit()

	lookupdHTTPAddrs := []string{httpAddr.String()}

	topicName := "inactive_nodes"

	conn := mustConnectLookupd(t, tcpAddr)
	defer conn.Close()

	identify(t, conn)

	xmq.Register(topicName, "channel1").WriteTo(conn)
	_, err := xmq.ReadResponse(conn)
	test.Nil(t, err)

	ci := clusterinfo.New(http_api.NewClient(nil, ConnectTimeout, RequestTimeout))

	producers, _ := ci.GetLookupdProducers(lookupdHTTPAddrs)
	test.Equal(t, 1, len(producers))
	test.Equal(t, 1, len(producers[0].Topics))
	test.Equal(t, topicName, producers[0].Topics[0].Topic)
	test.Equal(t, false, producers[0].Topics[0].Tombstoned)

	time.Sleep(250 * time.Millisecond)

	producers, _ = ci.GetLookupdProducers(lookupdHTTPAddrs)
	test.Equal(t, 0, len(producers))
}

func TestTombstonedNodes(t *testing.T) {
	opts := NewOptions()

	tcpAddr, httpAddr, xmqlookupd := mustStartLookupd(opts)
	defer xmqlookupd.Exit()

	lookupdHTTPAddrs := []string{httpAddr.String()}

	topicName := "inactive_nodes"

	conn := mustConnectLookupd(t, tcpAddr)
	defer conn.Close()

	identify(t, conn)

	xmq.Register(topicName, "channel1").WriteTo(conn)
	_, err := xmq.ReadResponse(conn)
	test.Nil(t, err)

	ci := clusterinfo.New(http_api.NewClient(nil, ConnectTimeout, RequestTimeout))

	producers, _ := ci.GetLookupdProducers(lookupdHTTPAddrs)
	test.Equal(t, 1, len(producers))
	test.Equal(t, 1, len(producers[0].Topics))
	test.Equal(t, topicName, producers[0].Topics[0].Topic)
	test.Equal(t, false, producers[0].Topics[0].Tombstoned)

	endpoint := fmt.Sprintf("http://%s/topic/tombstone?topic=%s&node=%s:%d",
		httpAddr, topicName, HostAddr, HTTPPort)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).POSTV1(endpoint)
	test.Nil(t, err)

	producers, _ = ci.GetLookupdProducers(lookupdHTTPAddrs)
	test.Equal(t, 1, len(producers))
	test.Equal(t, 1, len(producers[0].Topics))
	test.Equal(t, topicName, producers[0].Topics[0].Topic)
	test.Equal(t, true, producers[0].Topics[0].Tombstoned)
}
