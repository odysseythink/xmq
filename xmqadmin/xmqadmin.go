package xmqadmin

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"sync"
	"sync/atomic"

	"mlib.com/mlog"
	"mlib.com/xmq/internal/http_api"
	"mlib.com/xmq/internal/util"
	"mlib.com/xmq/internal/version"
)

type XMQAdmin struct {
	sync.RWMutex
	opts                atomic.Value
	httpListener        net.Listener
	waitGroup           util.WaitGroupWrapper
	notifications       chan *AdminAction
	graphiteURL         *url.URL
	httpClientTLSConfig *tls.Config
}

func New(opts *Options) (*XMQAdmin, error) {
	// if opts.Logger == nil {
	// 	opts.Logger = log.New(os.Stderr, opts.LogPrefix, log.Ldate|log.Ltime|log.Lmicroseconds)
	// }

	n := &XMQAdmin{
		notifications: make(chan *AdminAction),
	}
	n.swapOpts(opts)

	if len(opts.XMQDHTTPAddresses) == 0 && len(opts.XMQLookupdHTTPAddresses) == 0 {
		return nil, errors.New("--xmqd-http-address or --lookupd-http-address required")
	}

	if len(opts.XMQDHTTPAddresses) != 0 && len(opts.XMQLookupdHTTPAddresses) != 0 {
		return nil, errors.New("use --xmqd-http-address or --lookupd-http-address not both")
	}

	if opts.HTTPClientTLSCert != "" && opts.HTTPClientTLSKey == "" {
		return nil, errors.New("--http-client-tls-key must be specified with --http-client-tls-cert")
	}

	if opts.HTTPClientTLSKey != "" && opts.HTTPClientTLSCert == "" {
		return nil, errors.New("--http-client-tls-cert must be specified with --http-client-tls-key")
	}

	n.httpClientTLSConfig = &tls.Config{
		InsecureSkipVerify: opts.HTTPClientTLSInsecureSkipVerify,
	}
	if opts.HTTPClientTLSCert != "" && opts.HTTPClientTLSKey != "" {
		cert, err := tls.LoadX509KeyPair(opts.HTTPClientTLSCert, opts.HTTPClientTLSKey)
		if err != nil {
			return nil, fmt.Errorf("failed to LoadX509KeyPair %s, %s - %s",
				opts.HTTPClientTLSCert, opts.HTTPClientTLSKey, err)
		}
		n.httpClientTLSConfig.Certificates = []tls.Certificate{cert}
	}
	if opts.HTTPClientTLSRootCAFile != "" {
		tlsCertPool := x509.NewCertPool()
		caCertFile, err := os.ReadFile(opts.HTTPClientTLSRootCAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read TLS root CA file %s - %s",
				opts.HTTPClientTLSRootCAFile, err)
		}
		if !tlsCertPool.AppendCertsFromPEM(caCertFile) {
			return nil, fmt.Errorf("failed to AppendCertsFromPEM %s", opts.HTTPClientTLSRootCAFile)
		}
		n.httpClientTLSConfig.RootCAs = tlsCertPool
	}

	for _, address := range opts.XMQLookupdHTTPAddresses {
		_, err := net.ResolveTCPAddr("tcp", address)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve --lookupd-http-address (%s) - %s", address, err)
		}
	}

	for _, address := range opts.XMQDHTTPAddresses {
		_, err := net.ResolveTCPAddr("tcp", address)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve --xmqd-http-address (%s) - %s", address, err)
		}
	}

	if opts.ProxyGraphite {
		url, err := url.Parse(opts.GraphiteURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse --graphite-url (%s) - %s", opts.GraphiteURL, err)
		}
		n.graphiteURL = url
	}

	if opts.AllowConfigFromCIDR != "" {
		_, _, err := net.ParseCIDR(opts.AllowConfigFromCIDR)
		if err != nil {
			return nil, fmt.Errorf("failed to parse --allow-config-from-cidr (%s) - %s", opts.AllowConfigFromCIDR, err)
		}
	}

	opts.BasePath = normalizeBasePath(opts.BasePath)

	mlog.Infof(version.String("xmqadmin"))

	var err error
	n.httpListener, err = net.Listen("tcp", n.getOpts().HTTPAddress)
	if err != nil {
		return nil, fmt.Errorf("listen (%s) failed - %s", n.getOpts().HTTPAddress, err)
	}

	return n, nil
}

func normalizeBasePath(p string) string {
	if len(p) == 0 {
		return "/"
	}
	// add leading slash
	if p[0] != '/' {
		p = "/" + p
	}
	return path.Clean(p)
}

func (n *XMQAdmin) getOpts() *Options {
	return n.opts.Load().(*Options)
}

func (n *XMQAdmin) swapOpts(opts *Options) {
	n.opts.Store(opts)
}

func (n *XMQAdmin) RealHTTPAddr() *net.TCPAddr {
	return n.httpListener.Addr().(*net.TCPAddr)
}

func (n *XMQAdmin) handleAdminActions() {
	for action := range n.notifications {
		content, err := json.Marshal(action)
		if err != nil {
			mlog.Errorf("failed to serialize admin action - %s", err)
		}
		httpclient := &http.Client{
			Transport: http_api.NewDeadlineTransport(n.getOpts().HTTPClientConnectTimeout, n.getOpts().HTTPClientRequestTimeout),
		}
		mlog.Infof("POSTing notification to %s", n.getOpts().NotificationHTTPEndpoint)
		resp, err := httpclient.Post(n.getOpts().NotificationHTTPEndpoint,
			"application/json", bytes.NewBuffer(content))
		if err != nil {
			mlog.Errorf("failed to POST notification - %s", err)
		}
		resp.Body.Close()
	}
}

func (n *XMQAdmin) Main() error {
	exitCh := make(chan error)
	var once sync.Once
	exitFunc := func(err error) {
		once.Do(func() {
			if err != nil {
				mlog.Fatalf("%s", err)
			}
			exitCh <- err
		})
	}

	httpServer := NewHTTPServer(n)
	n.waitGroup.Wrap(func() {
		exitFunc(http_api.Serve(n.httpListener, http_api.CompressHandler(httpServer), "HTTP"))
	})
	n.waitGroup.Wrap(n.handleAdminActions)

	err := <-exitCh
	return err
}

func (n *XMQAdmin) Exit() {
	if n.httpListener != nil {
		n.httpListener.Close()
	}
	close(n.notifications)
	n.waitGroup.Wait()
}
