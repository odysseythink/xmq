package xmqd

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"mlib.com/xmq/internal/test"
)

func TestGetTopic(t *testing.T) {
	opts := NewOptions()

	_, _, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	topic1 := xmqd.cluster.GetTopic("test")
	test.NotNil(t, topic1)
	test.Equal(t, "test", topic1.Name)

	topic2 := xmqd.cluster.GetTopic("test")
	test.Equal(t, topic1, topic2)

	topic3 := xmqd.cluster.GetTopic("test2")
	test.Equal(t, "test2", topic3.Name)
	test.NotEqual(t, topic2, topic3)
}

func TestGetChannel(t *testing.T) {
	opts := NewOptions()

	_, _, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	topic := xmqd.cluster.GetTopic("test")

	channel1 := topic.GetChannel("ch1")
	test.NotNil(t, channel1)
	test.Equal(t, "ch1", channel1.Name)

	channel2 := topic.GetChannel("ch2")
	test.NotNil(t, channel2)
	test.Equal(t, "ch2", channel2.Name)
}

type errorBackendQueue struct{}

func (d *errorBackendQueue) Put([]byte) error        { return errors.New("never gonna happen") }
func (d *errorBackendQueue) ReadChan() <-chan []byte { return nil }
func (d *errorBackendQueue) Close() error            { return nil }
func (d *errorBackendQueue) Delete() error           { return nil }
func (d *errorBackendQueue) Depth() int64            { return 0 }
func (d *errorBackendQueue) Empty() error            { return nil }

type errorRecoveredBackendQueue struct{ errorBackendQueue }

func (d *errorRecoveredBackendQueue) Put([]byte) error { return nil }

func TestHealth(t *testing.T) {
	opts := NewOptions()

	opts.MemQueueSize = 2
	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	topic := xmqd.cluster.GetTopic("test")
	topic.backend = &errorBackendQueue{}

	msg := NewMessage(topic.GenerateID(), make([]byte, 100))
	err := topic.PutMessage(msg)
	test.Nil(t, err)

	msg = NewMessage(topic.GenerateID(), make([]byte, 100))
	err = topic.PutMessages([]*Message{msg})
	test.Nil(t, err)

	msg = NewMessage(topic.GenerateID(), make([]byte, 100))
	err = topic.PutMessage(msg)
	test.NotNil(t, err)

	msg = NewMessage(topic.GenerateID(), make([]byte, 100))
	err = topic.PutMessages([]*Message{msg})
	test.NotNil(t, err)

	url := fmt.Sprintf("http://%s/ping", httpAddr)
	resp, err := http.Get(url)
	test.Nil(t, err)
	test.Equal(t, 500, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	test.Equal(t, "NOK - never gonna happen", string(body))

	topic.backend = &errorRecoveredBackendQueue{}

	msg = NewMessage(topic.GenerateID(), make([]byte, 100))
	err = topic.PutMessages([]*Message{msg})
	test.Nil(t, err)

	resp, err = http.Get(url)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	test.Equal(t, "OK", string(body))
}

func TestDeletes(t *testing.T) {
	opts := NewOptions()

	_, _, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	topic := xmqd.cluster.GetTopic("test")

	channel1 := topic.GetChannel("ch1")
	test.NotNil(t, channel1)

	err := topic.DeleteExistingChannel("ch1")
	test.Nil(t, err)
	test.Equal(t, 0, len(topic.Channels))

	channel2 := topic.GetChannel("ch2")
	test.NotNil(t, channel2)

	err = xmqd.DeleteExistingTopic("test")
	test.Nil(t, err)
	test.Equal(t, 0, len(topic.Channels))
	test.Equal(t, 0, len(xmqd.topicMap))
}

func TestDeleteLast(t *testing.T) {
	opts := NewOptions()

	_, _, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	topic := xmqd.cluster.GetTopic("test")

	channel1 := topic.GetChannel("ch1")
	test.NotNil(t, channel1)

	err := topic.DeleteExistingChannel("ch1")
	test.Nil(t, err)
	test.Equal(t, 0, len(topic.Channels))

	msg := NewMessage(topic.GenerateID(), []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	err = topic.PutMessage(msg)
	time.Sleep(100 * time.Millisecond)
	test.Nil(t, err)
	test.Equal(t, int64(1), topic.Depth())
}

func TestPause(t *testing.T) {
	opts := NewOptions()

	_, _, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	topicName := "test_topic_pause" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqd.cluster.GetTopic(topicName)
	err := topic.Pause()
	test.Nil(t, err)

	channel := topic.GetChannel("ch1")
	test.NotNil(t, channel)

	msg := NewMessage(topic.GenerateID(), []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	err = topic.PutMessage(msg)
	test.Nil(t, err)

	time.Sleep(15 * time.Millisecond)

	test.Equal(t, int64(1), topic.Depth())
	test.Equal(t, int64(0), channel.Depth())

	err = topic.UnPause()
	test.Nil(t, err)

	time.Sleep(15 * time.Millisecond)

	test.Equal(t, int64(0), topic.Depth())
	test.Equal(t, int64(1), channel.Depth())
}

func BenchmarkTopicPut(b *testing.B) {
	b.StopTimer()
	topicName := "bench_topic_put" + strconv.Itoa(b.N)
	opts := NewOptions()

	opts.MemQueueSize = int64(b.N)
	_, _, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()
	b.StartTimer()

	for i := 0; i <= b.N; i++ {
		topic := xmqd.cluster.GetTopic(topicName)
		msg := NewMessage(topic.GenerateID(), []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaa"))
		topic.PutMessage(msg)
	}
}

func BenchmarkTopicToChannelPut(b *testing.B) {
	b.StopTimer()
	topicName := "bench_topic_to_channel_put" + strconv.Itoa(b.N)
	channelName := "bench"
	opts := NewOptions()

	opts.MemQueueSize = int64(b.N)
	_, _, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()
	channel := xmqd.cluster.GetTopic(topicName).GetChannel(channelName)
	b.StartTimer()

	for i := 0; i <= b.N; i++ {
		topic := xmqd.cluster.GetTopic(topicName)
		msg := NewMessage(topic.GenerateID(), []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaa"))
		topic.PutMessage(msg)
	}

	for {
		if len(channel.memoryMsgChan) == b.N {
			break
		}
		runtime.Gosched()
	}
}
