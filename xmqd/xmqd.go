package xmqd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"mlib.com/mlog"
	"mlib.com/xmq/internal/clusterinfo"
	"mlib.com/xmq/internal/dirlock"
	"mlib.com/xmq/internal/http_api"
	"mlib.com/xmq/internal/protocol"
	"mlib.com/xmq/internal/statsd"
	"mlib.com/xmq/internal/util"
	"mlib.com/xmq/internal/version"
)

const (
	TLSNotRequired = iota
	TLSRequiredExceptHTTP
	TLSRequired
)

type errStore struct {
	err error
}

type raftNodeInfo struct {
	NodeID string
	Addr   string
}

type XMQD struct {
	// 64bit atomic vars need to be first for proper alignment on 32bit platforms
	// clientIDSequence int64

	sync.RWMutex
	ctx context.Context
	// ctxCancel cancels a context that main() is waiting on
	ctxCancel context.CancelFunc

	opts atomic.Value

	dl        *dirlock.DirLock
	isLoading int32
	isExiting int32
	errValue  atomic.Value
	startTime time.Time

	topicMap map[string]*Topic

	lookupPeers atomic.Value

	tcpServer       *tcpServer
	tcpListener     net.Listener
	httpListener    net.Listener
	httpsListener   net.Listener
	tlsConfig       *tls.Config
	clientTLSConfig *tls.Config

	poolSize int

	notifyChan           chan interface{}
	optsNotificationChan chan struct{}
	waitGroup            util.WaitGroupWrapper
	joinNotifyChan       chan *raftNodeInfo

	ci *clusterinfo.ClusterInfo

	cluster *XMQCluster
}

func New(opts *Options) (*XMQD, error) {
	var err error

	dataPath := opts.DataPath
	if opts.DataPath == "" {
		cwd, _ := os.Getwd()
		dataPath = cwd
	}

	n := &XMQD{
		startTime:            time.Now(),
		topicMap:             make(map[string]*Topic),
		notifyChan:           make(chan interface{}),
		optsNotificationChan: make(chan struct{}, 1),
		dl:                   dirlock.New(dataPath),
		joinNotifyChan:       make(chan *raftNodeInfo, 20),
	}
	n.ctx, n.ctxCancel = context.WithCancel(context.Background())
	httpcli := http_api.NewClient(nil, opts.HTTPClientConnectTimeout, opts.HTTPClientRequestTimeout)
	n.ci = clusterinfo.New(httpcli)

	n.lookupPeers.Store([]*lookupPeer{})

	n.swapOpts(opts)
	n.errValue.Store(errStore{})

	err = n.dl.Lock()
	if err != nil {
		return nil, fmt.Errorf("failed to lock data-path: %v", err)
	}

	if opts.MaxDeflateLevel < 1 || opts.MaxDeflateLevel > 9 {
		return nil, errors.New("--max-deflate-level must be [1,9]")
	}

	// if opts.ID < 0 || opts.ID >= 1024 {
	// 	return nil, errors.New("--node-id must be [0,1024)")
	// }
	if opts.ID == "" {
		return nil, errors.New("--node-id can't be empty")
	}
	fmt.Println("--------------", opts.ClusterRaftAddresses)
	c, err := NewXMQCluster(n)
	if err != nil {
		return nil, fmt.Errorf("failed to build XMQCluster - %s", err)
	}
	n.cluster = c

	if opts.TLSClientAuthPolicy != "" && opts.TLSRequired == TLSNotRequired {
		opts.TLSRequired = TLSRequired
	}

	tlsConfig, err := buildTLSConfig(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to build TLS config - %s", err)
	}
	if tlsConfig == nil && opts.TLSRequired != TLSNotRequired {
		return nil, errors.New("cannot require TLS client connections without TLS key and cert")
	}
	n.tlsConfig = tlsConfig

	clientTLSConfig, err := buildClientTLSConfig(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to build client TLS config - %s", err)
	}
	n.clientTLSConfig = clientTLSConfig

	for _, v := range opts.E2EProcessingLatencyPercentiles {
		if v <= 0 || v > 1 {
			return nil, fmt.Errorf("invalid E2E processing latency percentile: %v", v)
		}
	}

	mlog.Infof(version.String("xmqd"))
	mlog.Infof("ID: %s", opts.ID)

	n.tcpServer = &tcpServer{xmqd: n}
	n.tcpListener, err = net.Listen(util.TypeOfAddr(opts.TCPAddress), opts.TCPAddress)
	if err != nil {
		return nil, fmt.Errorf("listen (%s) failed - %s", opts.TCPAddress, err)
	}
	if opts.HTTPAddress != "" {
		n.httpListener, err = net.Listen(util.TypeOfAddr(opts.HTTPAddress), opts.HTTPAddress)
		if err != nil {
			return nil, fmt.Errorf("listen (%s) failed - %s", opts.HTTPAddress, err)
		}
	}
	if n.tlsConfig != nil && opts.HTTPSAddress != "" {
		n.httpsListener, err = tls.Listen("tcp", opts.HTTPSAddress, n.tlsConfig)
		if err != nil {
			return nil, fmt.Errorf("listen (%s) failed - %s", opts.HTTPSAddress, err)
		}
	}
	if opts.BroadcastHTTPPort == 0 {
		tcpAddr, ok := n.RealHTTPAddr().(*net.TCPAddr)
		if ok {
			opts.BroadcastHTTPPort = tcpAddr.Port
		}
	}

	if opts.BroadcastTCPPort == 0 {
		tcpAddr, ok := n.RealTCPAddr().(*net.TCPAddr)
		if ok {
			opts.BroadcastTCPPort = tcpAddr.Port
		}
	}

	if opts.StatsdPrefix != "" {
		var port string = fmt.Sprint(opts.BroadcastHTTPPort)
		statsdHostKey := statsd.HostKey(net.JoinHostPort(opts.BroadcastAddress, port))
		prefixWithHost := strings.Replace(opts.StatsdPrefix, "%s", statsdHostKey, -1)
		if prefixWithHost[len(prefixWithHost)-1] != '.' {
			prefixWithHost += "."
		}
		opts.StatsdPrefix = prefixWithHost
	}

	return n, nil
}

func (n *XMQD) getOpts() *Options {
	return n.opts.Load().(*Options)
}

func (n *XMQD) swapOpts(opts *Options) {
	n.opts.Store(opts)
}

func (n *XMQD) triggerOptsNotification() {
	select {
	case n.optsNotificationChan <- struct{}{}:
	default:
	}
}

func (n *XMQD) RealTCPAddr() net.Addr {
	if n.tcpListener == nil {
		return &net.TCPAddr{}
	}
	return n.tcpListener.Addr()

}

func (n *XMQD) RealHTTPAddr() net.Addr {
	if n.httpListener == nil {
		return &net.TCPAddr{}
	}
	return n.httpListener.Addr()
}

func (n *XMQD) RealHTTPSAddr() *net.TCPAddr {
	if n.httpsListener == nil {
		return &net.TCPAddr{}
	}
	return n.httpsListener.Addr().(*net.TCPAddr)
}

func (n *XMQD) SetHealth(err error) {
	n.errValue.Store(errStore{err: err})
}

func (n *XMQD) IsHealthy() bool {
	return n.GetError() == nil
}

func (n *XMQD) GetError() error {
	errValue := n.errValue.Load()
	return errValue.(errStore).err
}

func (n *XMQD) GetHealth() string {
	err := n.GetError()
	if err != nil {
		return fmt.Sprintf("NOK - %s", err)
	}
	return "OK"
}

func (n *XMQD) GetStartTime() time.Time {
	return n.startTime
}

func (n *XMQD) Main() error {
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

	n.waitGroup.Wrap(func() {
		exitFunc(protocol.TCPServer(n.tcpListener, n.tcpServer))
	})
	if n.httpListener != nil {
		httpServer := newHTTPServer(n, false, n.getOpts().TLSRequired == TLSRequired)
		n.waitGroup.Wrap(func() {
			exitFunc(http_api.Serve(n.httpListener, httpServer, "HTTP"))
		})
	}
	if n.httpsListener != nil {
		httpsServer := newHTTPServer(n, true, true)
		n.waitGroup.Wrap(func() {
			exitFunc(http_api.Serve(n.httpsListener, httpsServer, "HTTPS"))
		})
	}

	n.waitGroup.Wrap(n.queueScanLoop)
	n.waitGroup.Wrap(n.lookupLoop)
	n.waitGroup.Wrap(n.cluster.ioLoop)
	if n.getOpts().StatsdAddress != "" {
		n.waitGroup.Wrap(n.statsdLoop)
	}

	err := <-exitCh
	return err
}

// Metadata is the collection of persistent information about the current XMQD.
type Metadata struct {
	Topics  []TopicMetadata `json:"topics"`
	Version string          `json:"version"`
}

// TopicMetadata is the collection of persistent information about a topic.
type TopicMetadata struct {
	Name     string            `json:"name"`
	Paused   bool              `json:"paused"`
	Channels []ChannelMetadata `json:"channels"`
}

// ChannelMetadata is the collection of persistent information about a channel.
type ChannelMetadata struct {
	Name   string `json:"name"`
	Paused bool   `json:"paused"`
}

func newMetadataFile(opts *Options) string {
	return path.Join(opts.DataPath, "xmqd.dat")
}

func readOrEmpty(fn string) ([]byte, error) {
	data, err := os.ReadFile(fn)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read metadata from %s - %s", fn, err)
		}
	}
	return data, nil
}

func writeSyncFile(fn string, data []byte) error {
	f, err := os.OpenFile(fn, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	f.Close()
	return err
}

func (n *XMQD) LoadMetadata() error {
	atomic.StoreInt32(&n.isLoading, 1)
	defer atomic.StoreInt32(&n.isLoading, 0)

	fn := newMetadataFile(n.getOpts())

	data, err := readOrEmpty(fn)
	if err != nil {
		return err
	}
	if data == nil {
		return nil // fresh start
	}

	var m Metadata
	err = json.Unmarshal(data, &m)
	if err != nil {
		return fmt.Errorf("failed to parse metadata in %s - %s", fn, err)
	}

	for _, t := range m.Topics {
		if !protocol.IsValidTopicName(t.Name) {
			mlog.Warningf("skipping creation of invalid topic %s", t.Name)
			continue
		}
		topic := n.cluster.GetTopic(t.Name)
		if topic == nil {
			mlog.Warningf("can't get topic %s", t.Name)
			continue
		}
		if t.Paused {
			topic.Pause()
		}
		for _, c := range t.Channels {
			if !protocol.IsValidChannelName(c.Name) {
				mlog.Warningf("skipping creation of invalid channel %s", c.Name)
				continue
			}
			channel := topic.GetChannel(c.Name)
			if c.Paused {
				channel.Pause()
			}
		}
		topic.Start()
	}
	return nil
}

// GetMetadata retrieves the current topic and channel set of the XMQ daemon. If
// the ephemeral flag is set, ephemeral topics are also returned even though these
// are not saved to disk.
func (n *XMQD) GetMetadata(ephemeral bool) *Metadata {
	meta := &Metadata{
		Version: version.Binary,
	}
	n.cluster.topics.Range(func(key, val interface{}) bool {
		//for _, topic := range n.topicMap {
		topic := val.(*Topic)
		if topic.Ephemeral && !ephemeral {
			return true
		}
		topicData := TopicMetadata{
			Name:   topic.Name,
			Paused: topic.IsPaused(),
		}
		topic.RLock()
		for _, channel := range topic.Channels {
			if channel.Ephemeral {
				continue
			}
			topicData.Channels = append(topicData.Channels, ChannelMetadata{
				Name:   channel.Name,
				Paused: channel.IsPaused(),
			})
		}
		topic.RUnlock()
		meta.Topics = append(meta.Topics, topicData)
		return true
	})
	return meta
}

func (n *XMQD) PersistMetadata() error {
	// persist metadata about what topics/channels we have, across restarts
	fileName := newMetadataFile(n.getOpts())

	mlog.Infof("XMQ: persisting topic/channel metadata to %s", fileName)

	data, err := json.Marshal(n.GetMetadata(false))
	if err != nil {
		return err
	}
	tmpFileName := fmt.Sprintf("%s.%d.tmp", fileName, rand.Int())

	err = writeSyncFile(tmpFileName, data)
	if err != nil {
		return err
	}
	err = os.Rename(tmpFileName, fileName)
	if err != nil {
		return err
	}
	// technically should fsync DataPath here

	return nil
}

func (n *XMQD) Exit() {
	if !atomic.CompareAndSwapInt32(&n.isExiting, 0, 1) {
		// avoid double call
		return
	}
	if n.tcpListener != nil {
		n.tcpListener.Close()
	}

	if n.tcpServer != nil {
		n.tcpServer.Close()
	}

	if n.httpListener != nil {
		n.httpListener.Close()
	}

	if n.httpsListener != nil {
		n.httpsListener.Close()
	}

	if n.cluster != nil {
		n.cluster.Close()
	}

	n.Lock()
	err := n.PersistMetadata()
	if err != nil {
		mlog.Errorf("failed to persist metadata - %s", err)
	}
	mlog.Infof("XMQ: closing topics")
	n.cluster.topics.Range(func(key, val interface{}) bool {
		topic := val.(*Topic)
		topic.Close()
		return true
	})
	// for _, topic := range n.topicMap {
	// 	topic.Close()
	// }
	n.Unlock()

	mlog.Infof("XMQ: stopping subsystems")
	n.ctxCancel()
	n.waitGroup.Wait()
	n.dl.Unlock()
	mlog.Infof("XMQ: bye")
}

// GetTopic performs a thread safe operation
// to return a pointer to a Topic object (potentially new)
// func (n *XMQD) GetTopic(topicName string) *Topic {
// 	mlog.Debugf("---topicName=%s", topicName)
// 	t, ok := n.cluster.topics.Load(topicName)
// 	if ok {
// 		return t.(*Topic)
// 	}
// 	err := n.cluster.NewTopic(topicName)
// 	if err != nil {
// 		return nil
// 	}

// 	t, ok = n.cluster.topics.Load(topicName)
// 	if !ok {
// 		mlog.Errorf("creat new topic(%s) failed", topicName)
// 		return nil
// 	}

// 	if atomic.LoadInt32(&n.isLoading) == 1 {
// 		return t.(*Topic)
// 	}

// 	// if using lookupd, make a blocking call to get channels and immediately create them
// 	// to ensure that all channels receive published messages
// 	lookupdHTTPAddrs := n.lookupdHTTPAddrs()
// 	if len(lookupdHTTPAddrs) > 0 {
// 		channelNames, err := n.ci.GetLookupdTopicChannels(topicName, lookupdHTTPAddrs)
// 		if err != nil {
// 			mlog.Warningf("failed to query xmqlookupd for channels to pre-create for topic %s - %s", topicName, err)
// 		}
// 		for _, channelName := range channelNames {
// 			if strings.HasSuffix(channelName, "#ephemeral") {
// 				continue // do not create ephemeral channel with no consumer client
// 			}
// 			t.(*Topic).GetChannel(channelName)
// 		}
// 	} else if len(n.getOpts().XMQLookupdGrpcAddresses) > 0 {
// 		mlog.Errorf("no available xmqlookupd to query for channels to pre-create for topic %s", topicName)
// 	}

// 	// now that all channels are added, start topic messagePump
// 	t.(*Topic).Start()
// 	return t.(*Topic)
// }

// GetExistingTopic gets a topic only if it exists
// func (n *XMQD) GetExistingTopic(topicName string) (*Topic, error) {
// 	n.RLock()
// 	defer n.RUnlock()
// 	t, ok := n.cluster.topics.Load(topicName)
// 	// topic, ok := n.topicMap[topicName]
// 	if !ok {
// 		return nil, errors.New("topic does not exist")
// 	}
// 	return t.(*Topic), nil
// }

// DeleteExistingTopic removes a topic only if it exists
func (n *XMQD) DeleteExistingTopic(topicName string) error {
	return n.cluster.DeleteTopic(topicName)
}

func (n *XMQD) Notify(v interface{}, persist bool) {
	// since the in-memory metadata is incomplete,
	// should not persist metadata while loading it.
	// xmqd will call `PersistMetadata` it after loading
	loading := atomic.LoadInt32(&n.isLoading) == 1
	n.waitGroup.Wrap(func() {
		// by selecting on ctx.Done() we guarantee that
		// we do not block exit, see issue #123
		select {
		case <-n.ctx.Done():
		case n.notifyChan <- v:
			if loading || !persist {
				return
			}
			n.Lock()
			err := n.PersistMetadata()
			if err != nil {
				mlog.Errorf("failed to persist metadata - %s", err)
			}
			n.Unlock()
		}
	})
}

// channels returns a flat slice of all channels in all topics
func (n *XMQD) channels() []*Channel {
	var channels []*Channel
	n.RLock()
	n.cluster.topics.Range(func(key, val interface{}) bool {
		topic := val.(*Topic)
		for _, channel := range topic.Channels {
			channels = append(channels, channel)
		}
		return true
	})

	n.RUnlock()
	return channels
}

// resizePool adjusts the size of the pool of queueScanWorker goroutines
//
//	1 <= pool <= min(num * 0.25, QueueScanWorkerPoolMax)
func (n *XMQD) resizePool(num int, workCh chan *Channel, responseCh chan bool, closeCh chan int) {
	idealPoolSize := int(float64(num) * 0.25)
	if idealPoolSize < 1 {
		idealPoolSize = 1
	} else if idealPoolSize > n.getOpts().QueueScanWorkerPoolMax {
		idealPoolSize = n.getOpts().QueueScanWorkerPoolMax
	}
	for {
		if idealPoolSize == n.poolSize {
			break
		} else if idealPoolSize < n.poolSize {
			// contract
			closeCh <- 1
			n.poolSize--
		} else {
			// expand
			n.waitGroup.Wrap(func() {
				n.queueScanWorker(workCh, responseCh, closeCh)
			})
			n.poolSize++
		}
	}
}

// queueScanWorker receives work (in the form of a channel) from queueScanLoop
// and processes the deferred and in-flight queues
func (n *XMQD) queueScanWorker(workCh chan *Channel, responseCh chan bool, closeCh chan int) {
	for {
		select {
		case c := <-workCh:
			now := time.Now().UnixNano()
			dirty := false
			if c.processInFlightQueue(now) {
				dirty = true
			}
			if c.processDeferredQueue(now) {
				dirty = true
			}
			responseCh <- dirty
		case <-closeCh:
			return
		}
	}
}

// queueScanLoop runs in a single goroutine to process in-flight and deferred
// priority queues. It manages a pool of queueScanWorker (configurable max of
// QueueScanWorkerPoolMax (default: 4)) that process channels concurrently.
//
// It copies Redis's probabilistic expiration algorithm: it wakes up every
// QueueScanInterval (default: 100ms) to select a random QueueScanSelectionCount
// (default: 20) channels from a locally cached list (refreshed every
// QueueScanRefreshInterval (default: 5s)).
//
// If either of the queues had work to do the channel is considered "dirty".
//
// If QueueScanDirtyPercent (default: 25%) of the selected channels were dirty,
// the loop continues without sleep.
func (n *XMQD) queueScanLoop() {
	workCh := make(chan *Channel, n.getOpts().QueueScanSelectionCount)
	responseCh := make(chan bool, n.getOpts().QueueScanSelectionCount)
	closeCh := make(chan int)

	workTicker := time.NewTicker(n.getOpts().QueueScanInterval)
	refreshTicker := time.NewTicker(n.getOpts().QueueScanRefreshInterval)

	channels := n.channels()
	n.resizePool(len(channels), workCh, responseCh, closeCh)

	for {
		select {
		case <-workTicker.C:
			if len(channels) == 0 {
				continue
			}
		case <-refreshTicker.C:
			channels = n.channels()
			n.resizePool(len(channels), workCh, responseCh, closeCh)
			continue
		case <-n.ctx.Done():
			goto exit
		}

		num := n.getOpts().QueueScanSelectionCount
		if num > len(channels) {
			num = len(channels)
		}

	loop:
		for _, i := range util.UniqRands(num, len(channels)) {
			workCh <- channels[i]
		}

		numDirty := 0
		for i := 0; i < num; i++ {
			if <-responseCh {
				numDirty++
			}
		}

		if float64(numDirty)/float64(num) > n.getOpts().QueueScanDirtyPercent {
			goto loop
		}
	}

exit:
	mlog.Infof("QUEUESCAN: closing")
	close(closeCh)
	workTicker.Stop()
	refreshTicker.Stop()
}

func buildTLSConfig(opts *Options) (*tls.Config, error) {
	var tlsConfig *tls.Config

	if opts.TLSCert == "" && opts.TLSKey == "" {
		return nil, nil
	}

	tlsClientAuthPolicy := tls.VerifyClientCertIfGiven

	cert, err := tls.LoadX509KeyPair(opts.TLSCert, opts.TLSKey)
	if err != nil {
		return nil, err
	}
	switch opts.TLSClientAuthPolicy {
	case "require":
		tlsClientAuthPolicy = tls.RequireAnyClientCert
	case "require-verify":
		tlsClientAuthPolicy = tls.RequireAndVerifyClientCert
	default:
		tlsClientAuthPolicy = tls.NoClientCert
	}

	tlsConfig = &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tlsClientAuthPolicy,
		MinVersion:   opts.TLSMinVersion,
		MaxVersion:   tls.VersionTLS12, // enable TLS_FALLBACK_SCSV prior to Go 1.5: https://go-review.googlesource.com/#/c/1776/
	}

	if opts.TLSRootCAFile != "" {
		tlsCertPool := x509.NewCertPool()
		caCertFile, err := os.ReadFile(opts.TLSRootCAFile)
		if err != nil {
			return nil, err
		}
		if !tlsCertPool.AppendCertsFromPEM(caCertFile) {
			return nil, errors.New("failed to append certificate to pool")
		}
		tlsConfig.ClientCAs = tlsCertPool
	}

	return tlsConfig, nil
}

func buildClientTLSConfig(opts *Options) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion: opts.TLSMinVersion,
	}

	if opts.TLSRootCAFile != "" {
		tlsCertPool := x509.NewCertPool()
		caCertFile, err := os.ReadFile(opts.TLSRootCAFile)
		if err != nil {
			return nil, err
		}
		if !tlsCertPool.AppendCertsFromPEM(caCertFile) {
			return nil, errors.New("failed to append certificate to pool")
		}
		tlsConfig.RootCAs = tlsCertPool
	}

	return tlsConfig, nil
}

func (n *XMQD) IsAuthEnabled() bool {
	return len(n.getOpts().AuthHTTPAddresses) != 0
}

// Context returns a context that will be canceled when xmqd initiates the shutdown
func (n *XMQD) Context() context.Context {
	return n.ctx
}
