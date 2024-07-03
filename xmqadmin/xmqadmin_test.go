package xmqadmin

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"

	"mlib.com/xmq/internal/test"
	"mlib.com/xmq/xmqd"
)

func TestNeitherXMQDAndXMQLookup(t *testing.T) {
	opts := NewOptions()
	// opts.Logger = lg.NilLogger{}
	opts.HTTPAddress = "127.0.0.1:0"
	_, err := New(opts)
	test.NotNil(t, err)
	test.Equal(t, "--xmqd-http-address or --lookupd-http-address required", fmt.Sprintf("%s", err))
}

func TestBothXMQDAndXMQLookup(t *testing.T) {
	opts := NewOptions()
	// opts.Logger = lg.NilLogger{}
	opts.HTTPAddress = "127.0.0.1:0"
	opts.XMQLookupdHTTPAddresses = []string{"127.0.0.1:4161"}
	opts.XMQDHTTPAddresses = []string{"127.0.0.1:4151"}
	_, err := New(opts)
	test.NotNil(t, err)
	test.Equal(t, "use --xmqd-http-address or --lookupd-http-address not both", fmt.Sprintf("%s", err))
}

func TestTLSHTTPClient(t *testing.T) {
	// lgr := test.NewTestLogger(t)

	xmqdOpts := xmqd.NewOptions()
	xmqdOpts.TLSCert = "./test/server.pem"
	xmqdOpts.TLSKey = "./test/server.key"
	xmqdOpts.TLSRootCAFile = "./test/ca.pem"
	xmqdOpts.TLSClientAuthPolicy = "require-verify"
	// xmqdOpts.Logger = lgr
	_, xmqdHTTPAddr, xmqd := mustStartXMQD(xmqdOpts)
	defer os.RemoveAll(xmqdOpts.DataPath)
	defer xmqd.Exit()

	opts := NewOptions()
	opts.HTTPAddress = "127.0.0.1:0"
	opts.XMQDHTTPAddresses = []string{xmqdHTTPAddr.String()}
	opts.HTTPClientTLSRootCAFile = "./test/ca.pem"
	opts.HTTPClientTLSCert = "./test/client.pem"
	opts.HTTPClientTLSKey = "./test/client.key"
	// opts.Logger = lgr
	xmqadmin, err := New(opts)
	test.Nil(t, err)
	go func() {
		err := xmqadmin.Main()
		if err != nil {
			panic(err)
		}
	}()
	defer xmqadmin.Exit()

	httpAddr := xmqadmin.RealHTTPAddr()
	u := url.URL{
		Scheme: "http",
		Host:   httpAddr.String(),
		Path:   "/api/nodes/" + xmqdHTTPAddr.String(),
	}

	resp, err := http.Get(u.String())
	test.Nil(t, err)
	defer resp.Body.Close()

	test.Equal(t, resp.StatusCode < 500, true)
}

func mustStartXMQD(opts *xmqd.Options) (net.Addr, net.Addr, *xmqd.XMQD) {
	opts.TCPAddress = "127.0.0.1:0"
	opts.HTTPAddress = "127.0.0.1:0"
	opts.HTTPSAddress = "127.0.0.1:0"
	if opts.DataPath == "" {
		tmpDir, err := os.MkdirTemp("", "xmq-test-")
		if err != nil {
			panic(err)
		}
		opts.DataPath = tmpDir
	}
	xmqd, err := xmqd.New(opts)
	if err != nil {
		panic(err)
	}
	go func() {
		err := xmqd.Main()
		if err != nil {
			panic(err)
		}
	}()
	return xmqd.RealTCPAddr(), xmqd.RealHTTPAddr(), xmqd
}
