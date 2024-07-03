package xmqd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/golang/snappy"
	uuid "github.com/satori/go.uuid"
	"mlib.com/xmq/internal/http_api"
	"mlib.com/xmq/internal/test"
)

func TestStats(t *testing.T) {
	opts := NewOptions()

	tcpAddr, _, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	topicName := "test_stats" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqd.cluster.GetTopic(topicName)
	msg := NewMessage(topic.GenerateID(), []byte("test body"))
	topic.PutMessage(msg)

	accompanyTopicName := "accompany_test_stats" + strconv.Itoa(int(time.Now().Unix()))
	accompanyTopic := xmqd.cluster.GetTopic(accompanyTopicName)
	msg = NewMessage(accompanyTopic.GenerateID(), []byte("accompany test body"))
	accompanyTopic.PutMessage(msg)

	conn, err := mustConnectXMQD(tcpAddr)
	test.Nil(t, err)
	defer conn.Close()

	identify(t, conn, nil, frameTypeResponse)
	sub(t, conn, topicName, "ch")

	stats := xmqd.GetStats(topicName, "ch", true).Topics
	t.Logf("stats: %+v", stats)

	test.Equal(t, 1, len(stats))
	test.Equal(t, 1, len(stats[0].Channels))
	test.Equal(t, 1, len(stats[0].Channels[0].Clients))
	test.Equal(t, 1, stats[0].Channels[0].ClientCount)

	stats = xmqd.GetStats(topicName, "ch", false).Topics
	t.Logf("stats: %+v", stats)

	test.Equal(t, 1, len(stats))
	test.Equal(t, 1, len(stats[0].Channels))
	test.Equal(t, 0, len(stats[0].Channels[0].Clients))
	test.Equal(t, 1, stats[0].Channels[0].ClientCount)

	stats = xmqd.GetStats(topicName, "none_exist_channel", false).Topics
	t.Logf("stats: %+v", stats)

	test.Equal(t, 0, len(stats))

	stats = xmqd.GetStats("none_exist_topic", "none_exist_channel", false).Topics
	t.Logf("stats: %+v", stats)

	test.Equal(t, 0, len(stats))
}

func TestClientAttributes(t *testing.T) {
	userAgent := "Test User Agent"

	opts := NewOptions()

	opts.LogLevel = "debug"
	opts.SnappyEnabled = true
	tcpAddr, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	conn, err := mustConnectXMQD(tcpAddr)
	test.Nil(t, err)
	defer conn.Close()

	data := identify(t, conn, map[string]interface{}{
		"snappy":     true,
		"user_agent": userAgent,
	}, frameTypeResponse)
	resp := struct {
		Snappy    bool   `json:"snappy"`
		UserAgent string `json:"user_agent"`
	}{}
	err = json.Unmarshal(data, &resp)
	test.Nil(t, err)
	test.Equal(t, true, resp.Snappy)

	r := snappy.NewReader(conn)
	//lint:ignore SA1019 NewWriter is deprecated by NewBufferedWriter, but we don't want to buffer
	w := snappy.NewWriter(conn)
	readValidate(t, r, frameTypeResponse, "OK")

	topicName := "test_client_attributes" + strconv.Itoa(int(time.Now().Unix()))
	sub(t, readWriter{r, w}, topicName, "ch")

	var d struct {
		Topics []struct {
			Channels []struct {
				Clients []struct {
					UserAgent string `json:"user_agent"`
					Snappy    bool   `json:"snappy"`
				} `json:"clients"`
			} `json:"channels"`
		} `json:"topics"`
	}

	endpoint := fmt.Sprintf("http://%s/stats?format=json", httpAddr)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &d)
	test.Nil(t, err)

	test.Equal(t, userAgent, d.Topics[0].Channels[0].Clients[0].UserAgent)
	test.Equal(t, true, d.Topics[0].Channels[0].Clients[0].Snappy)
}

func TestStatsChannelLocking(t *testing.T) {
	opts := NewOptions()

	_, _, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	topicName := "test_channel_empty" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqd.cluster.GetTopic(topicName)
	channel := topic.GetChannel("channel")

	var wg sync.WaitGroup

	wg.Add(2)
	clientID := uuid.NewV4().String()
	go func() {
		for i := 0; i < 25; i++ {
			msg := NewMessage(topic.GenerateID(), []byte("test"))
			topic.PutMessage(msg)
			channel.StartInFlightTimeout(msg, clientID, opts.MsgTimeout)
		}
		wg.Done()
	}()

	go func() {
		for i := 0; i < 25; i++ {
			xmqd.GetStats("", "", true)
		}
		wg.Done()
	}()

	wg.Wait()

	stats := xmqd.GetStats(topicName, "channel", false).Topics
	t.Logf("stats: %+v", stats)

	test.Equal(t, 1, len(stats))
	test.Equal(t, 1, len(stats[0].Channels))
	test.Equal(t, 25, stats[0].Channels[0].InFlightCount)
}
