package xmqlookupd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"mlib.com/xmq/internal/test"
	"mlib.com/xmq/internal/version"
	"mlib.com/xmq/xmqd"
)

type InfoDoc struct {
	Version string `json:"version"`
}

type ChannelsDoc struct {
	Channels []interface{} `json:"channels"`
}

type ErrMessage struct {
	Message string `json:"message"`
}

func bootstrapXMQCluster(t *testing.T) (string, []*xmqd.XMQD, *XMQLookupd) {
	xmqlookupdOpts := NewOptions()
	xmqlookupdOpts.GrpcAddress = "127.0.0.1:0"
	xmqlookupdOpts.HTTPAddress = "127.0.0.1:0"
	xmqlookupdOpts.BroadcastAddress = "127.0.0.1"
	xmqlookupd1, err := New(xmqlookupdOpts)
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
	tmpDir, err := os.MkdirTemp("", fmt.Sprintf("xmq-test-%d", time.Now().UnixNano()))
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

	time.Sleep(100 * time.Millisecond)

	return tmpDir, []*xmqd.XMQD{xmqd1}, xmqlookupd1
}

func makeTopic(xmqlookupd *XMQLookupd, topicName string) {
	key := Registration{"topic", topicName, ""}
	xmqlookupd.DB.AddRegistration(key)
}

func makeChannel(xmqlookupd *XMQLookupd, topicName string, channelName string) {
	key := Registration{"channel", topicName, channelName}
	xmqlookupd.DB.AddRegistration(key)
	makeTopic(xmqlookupd, topicName)
}

func TestPing(t *testing.T) {
	dataPath, xmqds, xmqlookupd1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupd1.Exit()

	client := http.Client{}
	url := fmt.Sprintf("http://%s/ping", xmqlookupd1.RealHTTPAddr())
	req, _ := http.NewRequest("GET", url, nil)
	resp, err := client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	test.Equal(t, []byte("OK"), body)
}

func TestInfo(t *testing.T) {
	dataPath, xmqds, xmqlookupd1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupd1.Exit()

	client := http.Client{}
	url := fmt.Sprintf("http://%s/info", xmqlookupd1.RealHTTPAddr())
	req, _ := http.NewRequest("GET", url, nil)
	resp, err := client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	info := InfoDoc{}
	err = json.Unmarshal(body, &info)
	test.Nil(t, err)
	test.Equal(t, version.Binary, info.Version)
}

func TestCreateTopic(t *testing.T) {
	dataPath, xmqds, xmqlookupd1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupd1.Exit()

	em := ErrMessage{}
	client := http.Client{}
	url := fmt.Sprintf("http://%s/topic/create", xmqlookupd1.RealHTTPAddr())

	req, _ := http.NewRequest("POST", url, nil)
	resp, err := client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 400, resp.StatusCode)
	test.Equal(t, "Bad Request", http.StatusText(resp.StatusCode))
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "MISSING_ARG_TOPIC", em.Message)

	topicName := "sampletopicA" + strconv.Itoa(int(time.Now().Unix())) + "$"
	url = fmt.Sprintf("http://%s/topic/create?topic=%s", xmqlookupd1.RealHTTPAddr(), topicName)

	req, _ = http.NewRequest("POST", url, nil)
	resp, err = client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 400, resp.StatusCode)
	test.Equal(t, "Bad Request", http.StatusText(resp.StatusCode))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "INVALID_ARG_TOPIC", em.Message)

	topicName = "sampletopicA" + strconv.Itoa(int(time.Now().Unix()))
	url = fmt.Sprintf("http://%s/topic/create?topic=%s", xmqlookupd1.RealHTTPAddr(), topicName)

	req, _ = http.NewRequest("POST", url, nil)
	resp, err = client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	test.Equal(t, []byte(""), body)
}

func TestDeleteTopic(t *testing.T) {
	dataPath, xmqds, xmqlookupd1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupd1.Exit()

	em := ErrMessage{}
	client := http.Client{}
	url := fmt.Sprintf("http://%s/topic/delete", xmqlookupd1.RealHTTPAddr())

	req, _ := http.NewRequest("POST", url, nil)
	resp, err := client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 400, resp.StatusCode)
	test.Equal(t, "Bad Request", http.StatusText(resp.StatusCode))
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "MISSING_ARG_TOPIC", em.Message)

	topicName := "sampletopicA" + strconv.Itoa(int(time.Now().Unix()))
	makeTopic(xmqlookupd1, topicName)

	url = fmt.Sprintf("http://%s/topic/delete?topic=%s", xmqlookupd1.RealHTTPAddr(), topicName)

	req, _ = http.NewRequest("POST", url, nil)
	resp, err = client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	test.Equal(t, []byte(""), body)

	topicName = "sampletopicB" + strconv.Itoa(int(time.Now().Unix()))
	channelName := "foobar" + strconv.Itoa(int(time.Now().Unix()))
	makeChannel(xmqlookupd1, topicName, channelName)

	url = fmt.Sprintf("http://%s/topic/delete?topic=%s", xmqlookupd1.RealHTTPAddr(), topicName)

	req, _ = http.NewRequest("POST", url, nil)
	resp, err = client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	test.Equal(t, []byte(""), body)
}

func TestGetChannels(t *testing.T) {
	dataPath, xmqds, xmqlookupd1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupd1.Exit()

	client := http.Client{}
	url := fmt.Sprintf("http://%s/channels", xmqlookupd1.RealHTTPAddr())

	em := ErrMessage{}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Add("Accept", "application/vnd.xmq; version=1.0")
	resp, err := client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 400, resp.StatusCode)
	test.Equal(t, "Bad Request", http.StatusText(resp.StatusCode))
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "MISSING_ARG_TOPIC", em.Message)

	ch := ChannelsDoc{}
	topicName := "sampletopicA" + strconv.Itoa(int(time.Now().Unix()))
	makeTopic(xmqlookupd1, topicName)

	url = fmt.Sprintf("http://%s/channels?topic=%s", xmqlookupd1.RealHTTPAddr(), topicName)

	req, _ = http.NewRequest("GET", url, nil)
	req.Header.Add("Accept", "application/vnd.xmq; version=1.0")
	resp, err = client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &ch)
	test.Nil(t, err)
	test.Equal(t, 0, len(ch.Channels))

	topicName = "sampletopicB" + strconv.Itoa(int(time.Now().Unix()))
	channelName := "foobar" + strconv.Itoa(int(time.Now().Unix()))
	makeChannel(xmqlookupd1, topicName, channelName)

	url = fmt.Sprintf("http://%s/channels?topic=%s", xmqlookupd1.RealHTTPAddr(), topicName)

	req, _ = http.NewRequest("GET", url, nil)
	req.Header.Add("Accept", "application/vnd.xmq; version=1.0")
	resp, err = client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &ch)
	test.Nil(t, err)
	test.Equal(t, 1, len(ch.Channels))
	test.Equal(t, channelName, ch.Channels[0])
}

func TestCreateChannel(t *testing.T) {
	dataPath, xmqds, xmqlookupd1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupd1.Exit()

	em := ErrMessage{}
	client := http.Client{}
	url := fmt.Sprintf("http://%s/channel/create", xmqlookupd1.RealHTTPAddr())

	req, _ := http.NewRequest("POST", url, nil)
	resp, err := client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 400, resp.StatusCode)
	test.Equal(t, "Bad Request", http.StatusText(resp.StatusCode))
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "MISSING_ARG_TOPIC", em.Message)

	topicName := "sampletopicB" + strconv.Itoa(int(time.Now().Unix())) + "$"
	url = fmt.Sprintf("http://%s/channel/create?topic=%s", xmqlookupd1.RealHTTPAddr(), topicName)

	req, _ = http.NewRequest("POST", url, nil)
	resp, err = client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 400, resp.StatusCode)
	test.Equal(t, "Bad Request", http.StatusText(resp.StatusCode))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "INVALID_ARG_TOPIC", em.Message)

	topicName = "sampletopicB" + strconv.Itoa(int(time.Now().Unix()))
	url = fmt.Sprintf("http://%s/channel/create?topic=%s", xmqlookupd1.RealHTTPAddr(), topicName)

	req, _ = http.NewRequest("POST", url, nil)
	resp, err = client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 400, resp.StatusCode)
	test.Equal(t, "Bad Request", http.StatusText(resp.StatusCode))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "MISSING_ARG_CHANNEL", em.Message)

	channelName := "foobar" + strconv.Itoa(int(time.Now().Unix())) + "$"
	url = fmt.Sprintf("http://%s/channel/create?topic=%s&channel=%s", xmqlookupd1.RealHTTPAddr(), topicName, channelName)

	req, _ = http.NewRequest("POST", url, nil)
	resp, err = client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 400, resp.StatusCode)
	test.Equal(t, "Bad Request", http.StatusText(resp.StatusCode))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "INVALID_ARG_CHANNEL", em.Message)

	channelName = "foobar" + strconv.Itoa(int(time.Now().Unix()))
	url = fmt.Sprintf("http://%s/channel/create?topic=%s&channel=%s", xmqlookupd1.RealHTTPAddr(), topicName, channelName)

	req, _ = http.NewRequest("POST", url, nil)
	resp, err = client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	test.Equal(t, []byte(""), body)
}

func TestDeleteChannel(t *testing.T) {
	dataPath, xmqds, xmqlookupd1 := bootstrapXMQCluster(t)
	defer os.RemoveAll(dataPath)
	defer xmqds[0].Exit()
	defer xmqlookupd1.Exit()

	em := ErrMessage{}
	client := http.Client{}
	url := fmt.Sprintf("http://%s/channel/delete", xmqlookupd1.RealHTTPAddr())

	req, _ := http.NewRequest("POST", url, nil)
	resp, err := client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 400, resp.StatusCode)
	test.Equal(t, "Bad Request", http.StatusText(resp.StatusCode))
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "MISSING_ARG_TOPIC", em.Message)

	topicName := "sampletopicB" + strconv.Itoa(int(time.Now().Unix())) + "$"
	url = fmt.Sprintf("http://%s/channel/delete?topic=%s", xmqlookupd1.RealHTTPAddr(), topicName)

	req, _ = http.NewRequest("POST", url, nil)
	resp, err = client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 400, resp.StatusCode)
	test.Equal(t, "Bad Request", http.StatusText(resp.StatusCode))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "INVALID_ARG_TOPIC", em.Message)

	topicName = "sampletopicB" + strconv.Itoa(int(time.Now().Unix()))
	url = fmt.Sprintf("http://%s/channel/delete?topic=%s", xmqlookupd1.RealHTTPAddr(), topicName)

	req, _ = http.NewRequest("POST", url, nil)
	resp, err = client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 400, resp.StatusCode)
	test.Equal(t, "Bad Request", http.StatusText(resp.StatusCode))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "MISSING_ARG_CHANNEL", em.Message)

	channelName := "foobar" + strconv.Itoa(int(time.Now().Unix())) + "$"
	url = fmt.Sprintf("http://%s/channel/delete?topic=%s&channel=%s", xmqlookupd1.RealHTTPAddr(), topicName, channelName)

	req, _ = http.NewRequest("POST", url, nil)
	resp, err = client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 400, resp.StatusCode)
	test.Equal(t, "Bad Request", http.StatusText(resp.StatusCode))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "INVALID_ARG_CHANNEL", em.Message)

	channelName = "foobar" + strconv.Itoa(int(time.Now().Unix()))
	url = fmt.Sprintf("http://%s/channel/delete?topic=%s&channel=%s", xmqlookupd1.RealHTTPAddr(), topicName, channelName)

	req, _ = http.NewRequest("POST", url, nil)
	resp, err = client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 404, resp.StatusCode)
	test.Equal(t, "Not Found", http.StatusText(resp.StatusCode))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "CHANNEL_NOT_FOUND", em.Message)

	makeChannel(xmqlookupd1, topicName, channelName)

	req, _ = http.NewRequest("POST", url, nil)
	resp, err = client.Do(req)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	test.Equal(t, []byte(""), body)
}
