package xmqd

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"mlib.com/go-xmq"
	"mlib.com/xmq/internal/http_api"
	"mlib.com/xmq/internal/test"
	"mlib.com/xmq/internal/version"
	"mlib.com/xmq/xmqlookupd"
)

type ErrMessage struct {
	Message string `json:"message"`
}

type InfoDoc struct {
	Version          string `json:"version"`
	BroadcastAddress string `json:"broadcast_address"`
	Hostname         string `json:"hostname"`
	HTTPPort         int    `json:"http_port"`
	TCPPort          int    `json:"tcp_port"`
	StartTime        int64  `json:"start_time"`
}

func TestHTTPpub(t *testing.T) {
	opts := NewOptions()
	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	topicName := "test_http_pub" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqd.cluster.GetTopic(topicName)

	buf := bytes.NewBuffer([]byte("test message"))
	url := fmt.Sprintf("http://%s/pub?topic=%s", httpAddr, topicName)
	resp, err := http.Post(url, "application/octet-stream", buf)
	test.Nil(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	test.Equal(t, "OK", string(body))

	time.Sleep(5 * time.Millisecond)

	test.Equal(t, int64(1), topic.Depth())
}

func TestHTTPpubEmpty(t *testing.T) {
	opts := NewOptions()

	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	topicName := "test_http_pub_empty" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqd.cluster.GetTopic(topicName)

	buf := bytes.NewBuffer([]byte(""))
	url := fmt.Sprintf("http://%s/pub?topic=%s", httpAddr, topicName)
	resp, err := http.Post(url, "application/octet-stream", buf)
	test.Nil(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	test.Equal(t, 400, resp.StatusCode)
	test.Equal(t, `{"message":"MSG_EMPTY"}`, string(body))

	time.Sleep(5 * time.Millisecond)

	test.Equal(t, int64(0), topic.Depth())
}

func TestHTTPmpub(t *testing.T) {
	opts := NewOptions()

	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	topicName := "test_http_mpub" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqd.cluster.GetTopic(topicName)

	msg := []byte("test message")
	msgs := make([][]byte, 4)
	for i := range msgs {
		msgs[i] = msg
	}
	buf := bytes.NewBuffer(bytes.Join(msgs, []byte("\n")))

	url := fmt.Sprintf("http://%s/mpub?topic=%s", httpAddr, topicName)
	resp, err := http.Post(url, "application/octet-stream", buf)
	test.Nil(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	test.Equal(t, "OK", string(body))

	time.Sleep(5 * time.Millisecond)

	test.Equal(t, int64(4), topic.Depth())
}

func TestHTTPmpubEmpty(t *testing.T) {
	opts := NewOptions()

	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	topicName := "test_http_mpub_empty" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqd.cluster.GetTopic(topicName)

	msg := []byte("test message")
	msgs := make([][]byte, 4)
	for i := range msgs {
		msgs[i] = msg
	}
	buf := bytes.NewBuffer(bytes.Join(msgs, []byte("\n")))
	_, err := buf.Write([]byte("\n"))
	test.Nil(t, err)

	url := fmt.Sprintf("http://%s/mpub?topic=%s", httpAddr, topicName)
	resp, err := http.Post(url, "application/octet-stream", buf)
	test.Nil(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	test.Equal(t, "OK", string(body))

	time.Sleep(5 * time.Millisecond)

	test.Equal(t, int64(4), topic.Depth())
}

func TestHTTPmpubBinary(t *testing.T) {
	opts := NewOptions()

	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	topicName := "test_http_mpub_bin" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqd.cluster.GetTopic(topicName)

	mpub := make([][]byte, 5)
	for i := range mpub {
		mpub[i] = make([]byte, 100)
	}
	cmd, _ := xmq.MultiPublish(topicName, mpub)
	buf := bytes.NewBuffer(cmd.Body)

	url := fmt.Sprintf("http://%s/mpub?topic=%s&binary=true", httpAddr, topicName)
	resp, err := http.Post(url, "application/octet-stream", buf)
	test.Nil(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	test.Equal(t, "OK", string(body))

	time.Sleep(5 * time.Millisecond)

	test.Equal(t, int64(5), topic.Depth())
}

func TestHTTPmpubForNonNormalizedBinaryParam(t *testing.T) {
	opts := NewOptions()

	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	topicName := "test_http_mpub_bin" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqd.cluster.GetTopic(topicName)

	mpub := make([][]byte, 5)
	for i := range mpub {
		mpub[i] = make([]byte, 100)
	}
	cmd, _ := xmq.MultiPublish(topicName, mpub)
	buf := bytes.NewBuffer(cmd.Body)

	url := fmt.Sprintf("http://%s/mpub?topic=%s&binary=non_normalized_binary_param", httpAddr, topicName)
	resp, err := http.Post(url, "application/octet-stream", buf)
	test.Nil(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	test.Equal(t, "OK", string(body))

	time.Sleep(5 * time.Millisecond)

	test.Equal(t, int64(5), topic.Depth())
}

func TestHTTPpubDefer(t *testing.T) {
	opts := NewOptions()

	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	topicName := "test_http_pub_defer" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqd.cluster.GetTopic(topicName)
	ch := topic.GetChannel("ch")

	buf := bytes.NewBuffer([]byte("test message"))
	url := fmt.Sprintf("http://%s/pub?topic=%s&defer=%d", httpAddr, topicName, 1000)
	resp, err := http.Post(url, "application/octet-stream", buf)
	test.Nil(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	test.Equal(t, "OK", string(body))

	time.Sleep(5 * time.Millisecond)

	ch.deferredMutex.Lock()
	numDef := len(ch.deferredMessages)
	ch.deferredMutex.Unlock()
	test.Equal(t, 1, numDef)
}

func TestHTTPSRequire(t *testing.T) {
	opts := NewOptions()

	opts.LogLevel = "debug"
	opts.TLSCert = "./test/certs/server.pem"
	opts.TLSKey = "./test/certs/server.key"
	opts.TLSClientAuthPolicy = "require"
	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	topicName := "test_http_pub_req" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqd.cluster.GetTopic(topicName)

	buf := bytes.NewBuffer([]byte("test message"))
	url := fmt.Sprintf("http://%s/pub?topic=%s", httpAddr, topicName)
	resp, _ := http.Post(url, "application/octet-stream", buf)
	test.Equal(t, 403, resp.StatusCode)

	httpsAddr := xmqd.httpsListener.Addr().(*net.TCPAddr)
	cert, err := tls.LoadX509KeyPair("./test/certs/cert.pem", "./test/certs/key.pem")
	test.Nil(t, err)
	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
		MinVersion:         0,
	}
	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}
	client := &http.Client{Transport: transport}

	buf = bytes.NewBuffer([]byte("test message"))
	url = fmt.Sprintf("https://%s/pub?topic=%s", httpsAddr, topicName)
	resp, err = client.Post(url, "application/octet-stream", buf)
	test.Nil(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	test.Equal(t, "OK", string(body))

	time.Sleep(5 * time.Millisecond)

	test.Equal(t, int64(1), topic.Depth())
}

func TestHTTPSRequireVerify(t *testing.T) {
	opts := NewOptions()

	opts.LogLevel = "debug"
	opts.TLSCert = "./test/certs/server.pem"
	opts.TLSKey = "./test/certs/server.key"
	opts.TLSRootCAFile = "./test/certs/ca.pem"
	opts.TLSClientAuthPolicy = "require-verify"
	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	httpsAddr := xmqd.httpsListener.Addr().(*net.TCPAddr)
	topicName := "test_http_pub_req_verf" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqd.cluster.GetTopic(topicName)

	// no cert
	buf := bytes.NewBuffer([]byte("test message"))
	url := fmt.Sprintf("http://%s/pub?topic=%s", httpAddr, topicName)
	resp, _ := http.Post(url, "application/octet-stream", buf)
	test.Equal(t, 403, resp.StatusCode)

	// unsigned cert
	cert, err := tls.LoadX509KeyPair("./test/certs/cert.pem", "./test/certs/key.pem")
	test.Nil(t, err)
	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}
	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}
	client := &http.Client{Transport: transport}

	buf = bytes.NewBuffer([]byte("test message"))
	url = fmt.Sprintf("https://%s/pub?topic=%s", httpsAddr, topicName)
	_, err = client.Post(url, "application/octet-stream", buf)
	test.NotNil(t, err)

	// signed cert
	cert, err = tls.LoadX509KeyPair("./test/certs/client.pem", "./test/certs/client.key")
	test.Nil(t, err)
	tlsConfig = &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}
	transport = &http.Transport{
		TLSClientConfig: tlsConfig,
	}
	client = &http.Client{Transport: transport}

	buf = bytes.NewBuffer([]byte("test message"))
	url = fmt.Sprintf("https://%s/pub?topic=%s", httpsAddr, topicName)
	resp, err = client.Post(url, "application/octet-stream", buf)
	test.Nil(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	test.Equal(t, "OK", string(body))

	time.Sleep(5 * time.Millisecond)

	test.Equal(t, int64(1), topic.Depth())
}

func TestTLSRequireVerifyExceptHTTP(t *testing.T) {
	opts := NewOptions()

	opts.LogLevel = "debug"
	opts.TLSCert = "./test/certs/server.pem"
	opts.TLSKey = "./test/certs/server.key"
	opts.TLSRootCAFile = "./test/certs/ca.pem"
	opts.TLSClientAuthPolicy = "require-verify"
	opts.TLSRequired = TLSRequiredExceptHTTP
	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	topicName := "test_http_req_verf_except_http" + strconv.Itoa(int(time.Now().Unix()))
	topic := xmqd.cluster.GetTopic(topicName)

	// no cert
	buf := bytes.NewBuffer([]byte("test message"))
	url := fmt.Sprintf("http://%s/pub?topic=%s", httpAddr, topicName)
	resp, err := http.Post(url, "application/octet-stream", buf)
	test.Nil(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	test.Equal(t, "OK", string(body))

	time.Sleep(5 * time.Millisecond)

	test.Equal(t, int64(1), topic.Depth())
}

func TestHTTPV1TopicChannel(t *testing.T) {
	opts := NewOptions()

	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	topicName := "test_http_topic_channel2" + strconv.Itoa(int(time.Now().Unix()))
	channelName := "ch2"

	url := fmt.Sprintf("http://%s/topic/create?topic=%s", httpAddr, topicName)
	resp, err := http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	test.Equal(t, "", string(body))
	test.Equal(t, "xmq; version=1.0", resp.Header.Get("X-XMQ-Content-Type"))

	url = fmt.Sprintf("http://%s/channel/create?topic=%s&channel=%s", httpAddr, topicName, channelName)
	resp, err = http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	test.Equal(t, "", string(body))
	test.Equal(t, "xmq; version=1.0", resp.Header.Get("X-XMQ-Content-Type"))

	topic, err := xmqd.cluster.GetExistingTopic(topicName)
	test.Nil(t, err)
	test.NotNil(t, topic)

	channel, err := topic.GetExistingChannel(channelName)
	test.Nil(t, err)
	test.NotNil(t, channel)

	em := ErrMessage{}

	url = fmt.Sprintf("http://%s/topic/pause", httpAddr)
	resp, err = http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 400, resp.StatusCode)
	test.Equal(t, "Bad Request", http.StatusText(resp.StatusCode))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "MISSING_ARG_TOPIC", em.Message)

	url = fmt.Sprintf("http://%s/topic/pause?topic=%s", httpAddr, topicName+"abc")
	resp, err = http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 404, resp.StatusCode)
	test.Equal(t, "Not Found", http.StatusText(resp.StatusCode))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "TOPIC_NOT_FOUND", em.Message)

	url = fmt.Sprintf("http://%s/topic/pause?topic=%s", httpAddr, topicName)
	resp, err = http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	test.Equal(t, "", string(body))
	test.Equal(t, "xmq; version=1.0", resp.Header.Get("X-XMQ-Content-Type"))

	test.Equal(t, true, topic.IsPaused())

	url = fmt.Sprintf("http://%s/topic/unpause?topic=%s", httpAddr, topicName)
	resp, err = http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	test.Equal(t, "", string(body))
	test.Equal(t, "xmq; version=1.0", resp.Header.Get("X-XMQ-Content-Type"))

	test.Equal(t, false, topic.IsPaused())

	url = fmt.Sprintf("http://%s/channel/pause?topic=%s&channel=%s", httpAddr, topicName, channelName)
	resp, err = http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	test.Equal(t, "", string(body))
	test.Equal(t, "xmq; version=1.0", resp.Header.Get("X-XMQ-Content-Type"))

	test.Equal(t, true, channel.IsPaused())

	url = fmt.Sprintf("http://%s/channel/unpause?topic=%s&channel=%s", httpAddr, topicName, channelName)
	resp, err = http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	test.Equal(t, "", string(body))
	test.Equal(t, "xmq; version=1.0", resp.Header.Get("X-XMQ-Content-Type"))

	test.Equal(t, false, channel.IsPaused())

	url = fmt.Sprintf("http://%s/channel/delete?topic=%s&channel=%s", httpAddr, topicName, channelName)
	resp, err = http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	test.Equal(t, "", string(body))
	test.Equal(t, "xmq; version=1.0", resp.Header.Get("X-XMQ-Content-Type"))

	_, err = topic.GetExistingChannel(channelName)
	test.NotNil(t, err)

	url = fmt.Sprintf("http://%s/topic/delete?topic=%s", httpAddr, topicName)
	resp, err = http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	test.Equal(t, "", string(body))
	test.Equal(t, "xmq; version=1.0", resp.Header.Get("X-XMQ-Content-Type"))

	_, err = xmqd.cluster.GetExistingTopic(topicName)
	test.NotNil(t, err)
}

func TestHTTPClientStats(t *testing.T) {
	topicName := "test_http_client_stats" + strconv.Itoa(int(time.Now().Unix()))

	opts := NewOptions()

	tcpAddr, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	conn, err := mustConnectXMQD(tcpAddr)
	test.Nil(t, err)
	defer conn.Close()

	identify(t, conn, nil, frameTypeResponse)
	sub(t, conn, topicName, "ch")

	var d struct {
		Topics []struct {
			Channels []struct {
				ClientCount int `json:"client_count"`
				Clients     []struct {
				} `json:"clients"`
			} `json:"channels"`
		} `json:"topics"`
		Memory *struct{} `json:"memory,omitempty"`
	}

	endpoint := fmt.Sprintf("http://%s/stats?format=json", httpAddr)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &d)
	test.Nil(t, err)

	test.Equal(t, 1, len(d.Topics[0].Channels[0].Clients))
	test.Equal(t, 1, d.Topics[0].Channels[0].ClientCount)
	test.NotNil(t, d.Memory)

	endpoint = fmt.Sprintf("http://%s/stats?format=json&include_clients=true", httpAddr)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &d)
	test.Nil(t, err)

	test.Equal(t, 1, len(d.Topics[0].Channels[0].Clients))
	test.Equal(t, 1, d.Topics[0].Channels[0].ClientCount)

	endpoint = fmt.Sprintf("http://%s/stats?format=json&include_clients=false", httpAddr)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &d)
	test.Nil(t, err)

	test.Equal(t, 0, len(d.Topics[0].Channels[0].Clients))
	test.Equal(t, 1, d.Topics[0].Channels[0].ClientCount)

	endpoint = fmt.Sprintf("http://%s/stats?format=json&include_mem=true", httpAddr)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &d)
	test.Nil(t, err)

	test.NotNil(t, d.Memory)

	d.Memory = nil
	endpoint = fmt.Sprintf("http://%s/stats?format=json&include_mem=false", httpAddr)
	err = http_api.NewClient(nil, ConnectTimeout, RequestTimeout).GETV1(endpoint, &d)
	test.Nil(t, err)

	test.Nil(t, d.Memory)
}

func TestHTTPgetStatusJSON(t *testing.T) {
	testTime := time.Now()
	opts := NewOptions()

	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	xmqd.startTime = testTime
	expectedJSON := fmt.Sprintf(`{"version":"%v","health":"OK","start_time":%v,"topics":[],"memory":{`, version.Binary, testTime.Unix())

	url := fmt.Sprintf("http://%s/stats?format=json", httpAddr)
	resp, err := http.Get(url)
	test.Nil(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	test.Equal(t, 200, resp.StatusCode)
	test.Equal(t, true, strings.HasPrefix(string(body), expectedJSON))
}

func TestHTTPgetStatusText(t *testing.T) {
	testTime := time.Now()
	opts := NewOptions()

	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	xmqd.startTime = testTime

	url := fmt.Sprintf("http://%s/stats?format=text", httpAddr)
	resp, err := http.Get(url)
	test.Nil(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	test.Equal(t, 200, resp.StatusCode)
	test.NotNil(t, body)
}

func TestHTTPconfig(t *testing.T) {
	lopts := xmqlookupd.NewOptions()
	// lopts.Logger = test.NewTestLogger(t)

	lopts1 := *lopts
	_, _, lookupd1 := mustStartXMQLookupd(&lopts1)
	defer lookupd1.Exit()
	lopts2 := *lopts
	_, _, lookupd2 := mustStartXMQLookupd(&lopts2)
	defer lookupd2.Exit()

	opts := NewOptions()

	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	url := fmt.Sprintf("http://%s/config/xmqlookupd_grpc_addresses", httpAddr)
	resp, err := http.Get(url)
	test.Nil(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	test.Equal(t, 200, resp.StatusCode)
	test.Equal(t, "[]", string(body))

	client := http.Client{}
	addrs := fmt.Sprintf(`["%s","%s"]`, lookupd1.RealGrpcAddr().String(), lookupd2.RealGrpcAddr().String())
	url = fmt.Sprintf("http://%s/config/xmqlookupd_grpc_addresses", httpAddr)
	req, err := http.NewRequest("PUT", url, bytes.NewBuffer([]byte(addrs)))
	test.Nil(t, err)
	resp, err = client.Do(req)
	test.Nil(t, err)
	defer resp.Body.Close()
	body, _ = io.ReadAll(resp.Body)
	test.Equal(t, 200, resp.StatusCode)
	test.Equal(t, addrs, string(body))

	url = fmt.Sprintf("http://%s/config/log_level", httpAddr)
	req, err = http.NewRequest("PUT", url, bytes.NewBuffer([]byte(`fatal`)))
	test.Nil(t, err)
	resp, err = client.Do(req)
	test.Nil(t, err)
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	test.Equal(t, 200, resp.StatusCode)
	test.Equal(t, "fatal", xmqd.getOpts().LogLevel)

	url = fmt.Sprintf("http://%s/config/log_level", httpAddr)
	req, err = http.NewRequest("PUT", url, bytes.NewBuffer([]byte(`bad`)))
	test.Nil(t, err)
	resp, err = client.Do(req)
	test.Nil(t, err)
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	test.Equal(t, 400, resp.StatusCode)
}

func TestHTTPerrors(t *testing.T) {
	opts := NewOptions()

	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	url := fmt.Sprintf("http://%s/stats", httpAddr)
	resp, err := http.Post(url, "text/plain", nil)
	test.Nil(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	test.Equal(t, 405, resp.StatusCode)
	test.Equal(t, `{"message":"METHOD_NOT_ALLOWED"}`, string(body))

	url = fmt.Sprintf("http://%s/not_found", httpAddr)
	resp, err = http.Get(url)
	test.Nil(t, err)
	defer resp.Body.Close()
	body, _ = io.ReadAll(resp.Body)
	test.Equal(t, 404, resp.StatusCode)
	test.Equal(t, `{"message":"NOT_FOUND"}`, string(body))
}

func TestDeleteTopic(t *testing.T) {
	opts := NewOptions()

	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	em := ErrMessage{}

	url := fmt.Sprintf("http://%s/topic/delete", httpAddr)
	resp, err := http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 400, resp.StatusCode)
	test.Equal(t, "Bad Request", http.StatusText(resp.StatusCode))
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "MISSING_ARG_TOPIC", em.Message)

	topicName := "test_http_delete_topic" + strconv.Itoa(int(time.Now().Unix()))

	url = fmt.Sprintf("http://%s/topic/delete?topic=%s", httpAddr, topicName)
	resp, err = http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 404, resp.StatusCode)
	test.Equal(t, "Not Found", http.StatusText(resp.StatusCode))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "TOPIC_NOT_FOUND", em.Message)

	xmqd.cluster.GetTopic(topicName)

	resp, err = http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	test.Equal(t, []byte(""), body)
}

func TestEmptyTopic(t *testing.T) {
	opts := NewOptions()

	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	em := ErrMessage{}

	url := fmt.Sprintf("http://%s/topic/empty", httpAddr)
	resp, err := http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 400, resp.StatusCode)
	test.Equal(t, "Bad Request", http.StatusText(resp.StatusCode))
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "MISSING_ARG_TOPIC", em.Message)

	topicName := "test_http_empty_topic" + strconv.Itoa(int(time.Now().Unix()))

	url = fmt.Sprintf("http://%s/topic/empty?topic=%s", httpAddr, topicName+"$")
	resp, err = http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 400, resp.StatusCode)
	test.Equal(t, "Bad Request", http.StatusText(resp.StatusCode))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "INVALID_TOPIC", em.Message)

	url = fmt.Sprintf("http://%s/topic/empty?topic=%s", httpAddr, topicName)
	resp, err = http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 404, resp.StatusCode)
	test.Equal(t, "Not Found", http.StatusText(resp.StatusCode))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "TOPIC_NOT_FOUND", em.Message)

	xmqd.cluster.GetTopic(topicName)

	resp, err = http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	test.Equal(t, []byte(""), body)
}

func TestEmptyChannel(t *testing.T) {
	opts := NewOptions()

	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	em := ErrMessage{}

	url := fmt.Sprintf("http://%s/channel/empty", httpAddr)
	resp, err := http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 400, resp.StatusCode)
	test.Equal(t, "Bad Request", http.StatusText(resp.StatusCode))
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "MISSING_ARG_TOPIC", em.Message)

	topicName := "test_http_empty_channel" + strconv.Itoa(int(time.Now().Unix()))

	url = fmt.Sprintf("http://%s/channel/empty?topic=%s", httpAddr, topicName)
	resp, err = http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 400, resp.StatusCode)
	test.Equal(t, "Bad Request", http.StatusText(resp.StatusCode))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "MISSING_ARG_CHANNEL", em.Message)

	channelName := "ch"

	url = fmt.Sprintf("http://%s/channel/empty?topic=%s&channel=%s", httpAddr, topicName, channelName)
	resp, err = http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 404, resp.StatusCode)
	test.Equal(t, "Not Found", http.StatusText(resp.StatusCode))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "TOPIC_NOT_FOUND", em.Message)

	topic := xmqd.cluster.GetTopic(topicName)

	url = fmt.Sprintf("http://%s/channel/empty?topic=%s&channel=%s", httpAddr, topicName, channelName)
	resp, err = http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 404, resp.StatusCode)
	test.Equal(t, "Not Found", http.StatusText(resp.StatusCode))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &em)
	test.Nil(t, err)
	test.Equal(t, "CHANNEL_NOT_FOUND", em.Message)

	topic.GetChannel(channelName)

	resp, err = http.Post(url, "application/json", nil)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	test.Equal(t, []byte(""), body)
}

func TestInfo(t *testing.T) {
	opts := NewOptions()

	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	defer xmqd.Exit()

	info := InfoDoc{}

	url := fmt.Sprintf("http://%s/info", httpAddr)
	resp, err := http.Get(url)
	test.Nil(t, err)
	test.Equal(t, 200, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("%s", body)
	err = json.Unmarshal(body, &info)
	test.Nil(t, err)
	test.Equal(t, version.Binary, info.Version)
}

func BenchmarkHTTPpub(b *testing.B) {
	var wg sync.WaitGroup
	b.StopTimer()
	opts := NewOptions()
	// opts.Logger = test.NewTestLogger(b)
	opts.MemQueueSize = int64(b.N)
	_, httpAddr, xmqd := mustStartXMQD(opts)
	defer os.RemoveAll(opts.DataPath)
	msg := make([]byte, 256)
	topicName := "bench_http_pub" + strconv.Itoa(int(time.Now().Unix()))
	url := fmt.Sprintf("http://%s/pub?topic=%s", httpAddr, topicName)
	client := &http.Client{}
	b.SetBytes(int64(len(msg)))
	b.StartTimer()

	for j := 0; j < runtime.GOMAXPROCS(0); j++ {
		wg.Add(1)
		go func() {
			num := b.N / runtime.GOMAXPROCS(0)
			for i := 0; i < num; i++ {
				buf := bytes.NewBuffer(msg)
				req, _ := http.NewRequest("POST", url, buf)
				resp, err := client.Do(req)
				if err != nil {
					panic(err.Error())
				}
				body, _ := io.ReadAll(resp.Body)
				if !bytes.Equal(body, []byte("OK")) {
					panic("bad response")
				}
				resp.Body.Close()
			}
			wg.Done()
		}()
	}

	wg.Wait()

	b.StopTimer()
	xmqd.Exit()
}
