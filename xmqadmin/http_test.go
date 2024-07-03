package xmqadmin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"mlib.com/xmq/internal/clusterinfo"
	"mlib.com/xmq/internal/test"
	"mlib.com/xmq/internal/version"
	"mlib.com/xmq/xmqd"
	"mlib.com/xmq/xmqlookupd"
)

type TopicsDoc struct {
	Topics []interface{} `json:"topics"`
}

type TopicStatsDoc struct {
	*clusterinfo.TopicStats
	Message string `json:"message"`
}

type NodesDoc struct {
	Nodes   clusterinfo.Producers `json:"nodes"`
	Message string                `json:"message"`
}

type NodeStatsDoc struct {
	Node          string                    `json:"node"`
	TopicStats    []*clusterinfo.TopicStats `json:"topics"`
	TotalMessages int64                     `json:"total_messages"`
	TotalClients  int64                     `json:"total_clients"`
	Message       string                    `json:"message"`
}

type ChannelStatsDoc struct {
	*clusterinfo.ChannelStats
	Message string `json:"message"`
}

func mustStartXMQLookupd(opts *xmqlookupd.Options) (*net.TCPAddr, *net.TCPAddr, *xmqlookupd.XMQLookupd) {
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

func bootstrapXMQCluster(t *testing.T) (string, []*xmqd.XMQD, []*xmqlookupd.XMQLookupd, *XMQAdmin) {
	return bootstrapXMQClusterWithAuth(t, false)
}

func bootstrapXMQClusterWithAuth(t *testing.T, withAuth bool) (string, []*xmqd.XMQD, []*xmqlookupd.XMQLookupd, *XMQAdmin) {
	// lgr := test.NewTestLogger(t)

	xmqlookupdOpts := xmqlookupd.NewOptions()
	xmqlookupdOpts.GrpcAddress = "127.0.0.1:0"
	xmqlookupdOpts.HTTPAddress = "127.0.0.1:0"
	xmqlookupdOpts.BroadcastAddress = "127.0.0.1"
	// xmqlookupdOpts.Logger = lgr
	xmqlookupd1, err := xmqlookupd.New(xmqlookupdOpts)
	if err != nil {
		panic(err)
	}
	go func() {
		err := xmqlookupd1.Main()
		if err != nil {
			panic(err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	xmqdOpts := xmqd.NewOptions()
	xmqdOpts.TCPAddress = "127.0.0.1:0"
	xmqdOpts.HTTPAddress = "127.0.0.1:0"
	xmqdOpts.BroadcastAddress = "127.0.0.1"
	xmqdOpts.XMQLookupdGrpcAddresses = []string{xmqlookupd1.RealGrpcAddr().String()}
	// xmqdOpts.Logger = lgr
	tmpDir, err := os.MkdirTemp("", "xmq-test-")
	if err != nil {
		panic(err)
	}
	xmqdOpts.DataPath = tmpDir
	xmqd1, err := xmqd.New(xmqdOpts)
	if err != nil {
		panic(err)
	}
	go func() {
		err := xmqd1.Main()
		if err != nil {
			panic(err)
		}
	}()

	xmqadminOpts := NewOptions()
	xmqadminOpts.HTTPAddress = "127.0.0.1:0"
	xmqadminOpts.XMQLookupdHTTPAddresses = []string{xmqlookupd1.RealHTTPAddr().String()}
	// xmqadminOpts.Logger = lgr
	if withAuth {
		xmqadminOpts.AdminUsers = []string{"matt"}
	}
	xmqadmin1, err := New(xmqadminOpts)
	if err != nil {
		panic(err)
	}
	go func() {
		err := xmqadmin1.Main()
		if err != nil {
			panic(err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	return tmpDir, []*xmqd.XMQD{xmqd1}, []*xmqlookupd.XMQLookupd{xmqlookupd1}, xmqadmin1
}

func TestPing(t *testing.T) {
	dataPath, xmqds, xmqlookupds, xmqadmin1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupds[0].Exit()
	defer xmqadmin1.Exit()

	client := http.Client{}
	url := fmt.Sprintf("http://%s/ping", xmqadmin1.RealHTTPAddr())
	req, _ := http.NewRequest("GET", url, nil)
	resp, err := client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	test.Equal(t, []byte("OK"), body)
}

func TestHTTPTopicsGET(t *testing.T) {
	dataPath, xmqds, xmqlookupds, xmqadmin1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupds[0].Exit()
	defer xmqadmin1.Exit()

	topicName := "test_topics_get" + strconv.Itoa(int(time.Now().Unix()))
	xmqds[0].GetTopic(topicName)
	time.Sleep(100 * time.Millisecond)

	client := http.Client{}
	url := fmt.Sprintf("http://%s/api/topics", xmqadmin1.RealHTTPAddr())
	req, _ := http.NewRequest("GET", url, nil)
	resp, err := client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	tr := TopicsDoc{}
	err = json.Unmarshal(body, &tr)
	test.Nil(t, err)
	test.Equal(t, 1, len(tr.Topics))
	test.Equal(t, topicName, tr.Topics[0])
}

func TestHTTPTopicGET(t *testing.T) {
	dataPath, xmqds, xmqlookupds, xmqadmin1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupds[0].Exit()
	defer xmqadmin1.Exit()

	topicName := "test_topic_get" + strconv.Itoa(int(time.Now().Unix()))
	xmqds[0].GetTopic(topicName)
	time.Sleep(100 * time.Millisecond)

	client := http.Client{}
	url := fmt.Sprintf("http://%s/api/topics/%s", xmqadmin1.RealHTTPAddr(), topicName)
	req, _ := http.NewRequest("GET", url, nil)
	resp, err := client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	ts := TopicStatsDoc{}
	err = json.Unmarshal(body, &ts)
	test.Nil(t, err)
	test.Equal(t, topicName, ts.TopicName)
	test.Equal(t, 0, int(ts.Depth))
	test.Equal(t, 0, int(ts.MemoryDepth))
	test.Equal(t, 0, int(ts.BackendDepth))
	test.Equal(t, 0, int(ts.MessageCount))
	test.Equal(t, false, ts.Paused)
}

func TestHTTPNodesGET(t *testing.T) {
	dataPath, xmqds, xmqlookupds, xmqadmin1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupds[0].Exit()
	defer xmqadmin1.Exit()

	time.Sleep(100 * time.Millisecond)

	client := http.Client{}
	url := fmt.Sprintf("http://%s/api/nodes", xmqadmin1.RealHTTPAddr())
	req, _ := http.NewRequest("GET", url, nil)
	resp, err := client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	hostname, _ := os.Hostname()

	t.Logf("%s", body)
	ns := NodesDoc{}
	err = json.Unmarshal(body, &ns)
	test.Nil(t, err)
	test.Equal(t, 1, len(ns.Nodes))
	testNode := ns.Nodes[0]
	test.Equal(t, hostname, testNode.Hostname)
	test.Equal(t, "127.0.0.1", testNode.BroadcastAddress)
	test.Equal(t, xmqds[0].RealTCPAddr().(*net.TCPAddr).Port, testNode.TCPPort)
	test.Equal(t, xmqds[0].RealHTTPAddr().(*net.TCPAddr).Port, testNode.HTTPPort)
	test.Equal(t, version.Binary, testNode.Version)
	test.Equal(t, 0, len(testNode.Topics))
}

func TestHTTPChannelGET(t *testing.T) {
	dataPath, xmqds, xmqlookupds, xmqadmin1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupds[0].Exit()
	defer xmqadmin1.Exit()

	topicName := "test_channel_get" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqds[0].GetTopic(topicName)
	topic.GetChannel("ch")
	time.Sleep(100 * time.Millisecond)

	client := http.Client{}
	url := fmt.Sprintf("http://%s/api/topics/%s/ch", xmqadmin1.RealHTTPAddr(), topicName)
	req, _ := http.NewRequest("GET", url, nil)
	resp, err := client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	cs := ChannelStatsDoc{}
	err = json.Unmarshal(body, &cs)
	test.Nil(t, err)
	test.Equal(t, topicName, cs.TopicName)
	test.Equal(t, "ch", cs.ChannelName)
	test.Equal(t, 0, int(cs.Depth))
	test.Equal(t, 0, int(cs.MemoryDepth))
	test.Equal(t, 0, int(cs.BackendDepth))
	test.Equal(t, 0, int(cs.MessageCount))
	test.Equal(t, false, cs.Paused)
	test.Equal(t, 0, int(cs.InFlightCount))
	test.Equal(t, 0, int(cs.DeferredCount))
	test.Equal(t, 0, int(cs.RequeueCount))
	test.Equal(t, 0, int(cs.TimeoutCount))
	test.Equal(t, 0, len(cs.Clients))
}

func TestHTTPNodesSingleGET(t *testing.T) {
	dataPath, xmqds, xmqlookupds, xmqadmin1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupds[0].Exit()
	defer xmqadmin1.Exit()

	topicName := "test_nodes_single_get" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqds[0].GetTopic(topicName)
	topic.GetChannel("ch")
	time.Sleep(100 * time.Millisecond)

	client := http.Client{}
	url := fmt.Sprintf("http://%s/api/nodes/%s", xmqadmin1.RealHTTPAddr(),
		xmqds[0].RealHTTPAddr().String())
	req, _ := http.NewRequest("GET", url, nil)
	resp, err := client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	ns := NodeStatsDoc{}
	err = json.Unmarshal(body, &ns)
	test.Nil(t, err)
	test.Equal(t, xmqds[0].RealHTTPAddr().String(), ns.Node)
	test.Equal(t, 1, len(ns.TopicStats))
	testTopic := ns.TopicStats[0]
	test.Equal(t, topicName, testTopic.TopicName)
	test.Equal(t, 0, int(testTopic.Depth))
	test.Equal(t, 0, int(testTopic.MemoryDepth))
	test.Equal(t, 0, int(testTopic.BackendDepth))
	test.Equal(t, 0, int(testTopic.MessageCount))
	test.Equal(t, false, testTopic.Paused)
}

func TestHTTPCreateTopicPOST(t *testing.T) {
	dataPath, xmqds, xmqlookupds, xmqadmin1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupds[0].Exit()
	defer xmqadmin1.Exit()

	time.Sleep(100 * time.Millisecond)

	topicName := "test_create_topic_post" + strconv.Itoa(int(time.Now().Unix()))

	client := http.Client{}
	url := fmt.Sprintf("http://%s/api/topics", xmqadmin1.RealHTTPAddr())
	body, _ := json.Marshal(map[string]interface{}{
		"topic": topicName,
	})
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	resp, err := client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	resp.Body.Close()
}

func TestHTTPCreateTopicChannelPOST(t *testing.T) {
	dataPath, xmqds, xmqlookupds, xmqadmin1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupds[0].Exit()
	defer xmqadmin1.Exit()

	time.Sleep(100 * time.Millisecond)

	topicName := "test_create_topic_channel_post" + strconv.Itoa(int(time.Now().Unix()))

	client := http.Client{}
	url := fmt.Sprintf("http://%s/api/topics", xmqadmin1.RealHTTPAddr())
	body, _ := json.Marshal(map[string]interface{}{
		"topic":   topicName,
		"channel": "ch",
	})
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	resp, err := client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	resp.Body.Close()
}

func TestHTTPTombstoneTopicNodePOST(t *testing.T) {
	dataPath, xmqds, xmqlookupds, xmqadmin1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupds[0].Exit()
	defer xmqadmin1.Exit()

	topicName := "test_tombstone_topic_node_post" + strconv.Itoa(int(time.Now().Unix()))
	xmqds[0].GetTopic(topicName)
	time.Sleep(100 * time.Millisecond)

	client := http.Client{}
	url := fmt.Sprintf("http://%s/api/nodes/%s", xmqadmin1.RealHTTPAddr(), xmqds[0].RealHTTPAddr())
	body, _ := json.Marshal(map[string]interface{}{
		"topic": topicName,
	})
	req, _ := http.NewRequest("DELETE", url, bytes.NewBuffer(body))
	resp, err := client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	resp.Body.Close()
}

func TestHTTPDeleteTopicPOST(t *testing.T) {
	dataPath, xmqds, xmqlookupds, xmqadmin1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupds[0].Exit()
	defer xmqadmin1.Exit()

	topicName := "test_delete_topic_post" + strconv.Itoa(int(time.Now().Unix()))
	xmqds[0].GetTopic(topicName)
	time.Sleep(100 * time.Millisecond)

	client := http.Client{}
	url := fmt.Sprintf("http://%s/api/topics/%s", xmqadmin1.RealHTTPAddr(), topicName)
	req, _ := http.NewRequest("DELETE", url, nil)
	resp, err := client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	resp.Body.Close()
}

func TestHTTPDeleteChannelPOST(t *testing.T) {
	dataPath, xmqds, xmqlookupds, xmqadmin1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupds[0].Exit()
	defer xmqadmin1.Exit()

	topicName := "test_delete_channel_post" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqds[0].GetTopic(topicName)
	topic.GetChannel("ch")
	time.Sleep(100 * time.Millisecond)

	client := http.Client{}
	url := fmt.Sprintf("http://%s/api/topics/%s/ch", xmqadmin1.RealHTTPAddr(), topicName)
	req, _ := http.NewRequest("DELETE", url, nil)
	resp, err := client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	resp.Body.Close()
}

func TestHTTPPauseTopicPOST(t *testing.T) {
	dataPath, xmqds, xmqlookupds, xmqadmin1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupds[0].Exit()
	defer xmqadmin1.Exit()

	topicName := "test_pause_topic_post" + strconv.Itoa(int(time.Now().Unix()))
	xmqds[0].GetTopic(topicName)
	time.Sleep(100 * time.Millisecond)

	client := http.Client{}
	url := fmt.Sprintf("http://%s/api/topics/%s", xmqadmin1.RealHTTPAddr(), topicName)
	body, _ := json.Marshal(map[string]interface{}{
		"action": "pause",
	})
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	resp, err := client.Do(req)
	test.Nil(t, err)
	_, _ = io.ReadAll(resp.Body)
	test.Equal(t, 200, resp.StatusCode)
	resp.Body.Close()

	url = fmt.Sprintf("http://%s/api/topics/%s", xmqadmin1.RealHTTPAddr(), topicName)
	body, _ = json.Marshal(map[string]interface{}{
		"action": "unpause",
	})
	req, _ = http.NewRequest("POST", url, bytes.NewBuffer(body))
	resp, err = client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	resp.Body.Close()
}

func TestHTTPPauseChannelPOST(t *testing.T) {
	dataPath, xmqds, xmqlookupds, xmqadmin1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupds[0].Exit()
	defer xmqadmin1.Exit()

	topicName := "test_pause_channel_post" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqds[0].GetTopic(topicName)
	topic.GetChannel("ch")
	time.Sleep(100 * time.Millisecond)

	client := http.Client{}
	url := fmt.Sprintf("http://%s/api/topics/%s/ch", xmqadmin1.RealHTTPAddr(), topicName)
	body, _ := json.Marshal(map[string]interface{}{
		"action": "pause",
	})
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	resp, err := client.Do(req)
	test.Nil(t, err)
	_, _ = io.ReadAll(resp.Body)
	test.Equal(t, 200, resp.StatusCode)
	resp.Body.Close()

	url = fmt.Sprintf("http://%s/api/topics/%s/ch", xmqadmin1.RealHTTPAddr(), topicName)
	body, _ = json.Marshal(map[string]interface{}{
		"action": "unpause",
	})
	req, _ = http.NewRequest("POST", url, bytes.NewBuffer(body))
	resp, err = client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	resp.Body.Close()
}

func TestHTTPEmptyTopicPOST(t *testing.T) {
	dataPath, xmqds, xmqlookupds, xmqadmin1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupds[0].Exit()
	defer xmqadmin1.Exit()

	topicName := "test_empty_topic_post" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqds[0].GetTopic(topicName)
	topic.PutMessage(xmqd.NewMessage(xmqd.MessageID{}, []byte("1234")))
	test.Equal(t, int64(1), topic.Depth())
	time.Sleep(100 * time.Millisecond)

	client := http.Client{}
	url := fmt.Sprintf("http://%s/api/topics/%s", xmqadmin1.RealHTTPAddr(), topicName)
	body, _ := json.Marshal(map[string]interface{}{
		"action": "empty",
	})
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	resp, err := client.Do(req)
	test.Nil(t, err)
	_, _ = io.ReadAll(resp.Body)
	test.Equal(t, 200, resp.StatusCode)
	resp.Body.Close()

	test.Equal(t, int64(0), topic.Depth())
}

func TestHTTPEmptyChannelPOST(t *testing.T) {
	dataPath, xmqds, xmqlookupds, xmqadmin1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupds[0].Exit()
	defer xmqadmin1.Exit()

	topicName := "test_empty_channel_post" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqds[0].GetTopic(topicName)
	channel := topic.GetChannel("ch")
	channel.PutMessage(xmqd.NewMessage(xmqd.MessageID{}, []byte("1234")))

	time.Sleep(100 * time.Millisecond)
	test.Equal(t, int64(1), channel.Depth())

	client := http.Client{}
	url := fmt.Sprintf("http://%s/api/topics/%s/ch", xmqadmin1.RealHTTPAddr(), topicName)
	body, _ := json.Marshal(map[string]interface{}{
		"action": "empty",
	})
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	resp, err := client.Do(req)
	test.Nil(t, err)
	_, _ = io.ReadAll(resp.Body)
	test.Equal(t, 200, resp.StatusCode)
	resp.Body.Close()

	test.Equal(t, int64(0), channel.Depth())
}

func TestHTTPconfig(t *testing.T) {
	dataPath, xmqds, xmqlookupds, xmqadmin1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupds[0].Exit()
	defer xmqadmin1.Exit()

	lopts := xmqlookupd.NewOptions()
	// lopts.Logger = test.NewTestLogger(t)

	lopts1 := *lopts
	_, _, lookupd1 := mustStartXMQLookupd(&lopts1)
	defer lookupd1.Exit()
	lopts2 := *lopts
	_, _, lookupd2 := mustStartXMQLookupd(&lopts2)
	defer lookupd2.Exit()

	url := fmt.Sprintf("http://%s/config/xmqlookupd_http_addresses", xmqadmin1.RealHTTPAddr())
	resp, err := http.Get(url)
	test.Nil(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	test.Equal(t, 200, resp.StatusCode)
	origaddrs := fmt.Sprintf(`["%s"]`, xmqlookupds[0].RealHTTPAddr().String())
	test.Equal(t, origaddrs, string(body))

	client := http.Client{}
	addrs := fmt.Sprintf(`["%s","%s"]`, lookupd1.RealHTTPAddr().String(), lookupd2.RealHTTPAddr().String())
	url = fmt.Sprintf("http://%s/config/xmqlookupd_http_addresses", xmqadmin1.RealHTTPAddr())
	req, err := http.NewRequest("PUT", url, bytes.NewBuffer([]byte(addrs)))
	test.Nil(t, err)
	resp, err = client.Do(req)
	test.Nil(t, err)
	defer resp.Body.Close()
	body, _ = io.ReadAll(resp.Body)
	test.Equal(t, 200, resp.StatusCode)
	test.Equal(t, addrs, string(body))

	url = fmt.Sprintf("http://%s/config/log_level", xmqadmin1.RealHTTPAddr())
	req, err = http.NewRequest("PUT", url, bytes.NewBuffer([]byte(`fatal`)))
	test.Nil(t, err)
	resp, err = client.Do(req)
	test.Nil(t, err)
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	test.Equal(t, 200, resp.StatusCode)
	test.Equal(t, "fatal", xmqadmin1.getOpts().LogLevel)

	url = fmt.Sprintf("http://%s/config/log_level", xmqadmin1.RealHTTPAddr())
	req, err = http.NewRequest("PUT", url, bytes.NewBuffer([]byte(`bad`)))
	test.Nil(t, err)
	resp, err = client.Do(req)
	test.Nil(t, err)
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	test.Equal(t, 400, resp.StatusCode)
}

func TestHTTPconfigCIDR(t *testing.T) {
	opts := NewOptions()
	opts.HTTPAddress = "127.0.0.1:0"
	opts.XMQLookupdHTTPAddresses = []string{"127.0.0.1:4161"}
	// opts.Logger = test.NewTestLogger(t)
	opts.AllowConfigFromCIDR = "10.0.0.0/8"
	xmqadmin, err := New(opts)
	test.Nil(t, err)
	go func() {
		err := xmqadmin.Main()
		if err != nil {
			panic(err)
		}
	}()
	defer xmqadmin.Exit()

	time.Sleep(100 * time.Millisecond)

	url := fmt.Sprintf("http://%s/config/xmqlookupd_http_addresses", xmqadmin.RealHTTPAddr())
	resp, err := http.Get(url)
	test.Nil(t, err)
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	test.Equal(t, 403, resp.StatusCode)
}
