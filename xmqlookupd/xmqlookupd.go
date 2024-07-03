package xmqlookupd

import (
	"fmt"
	"net"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"mlib.com/mlog"
	"mlib.com/xmq/internal/http_api"
	"mlib.com/xmq/internal/util"
	"mlib.com/xmq/internal/version"
	"mlib.com/xmq/pbapi"
)

type XMQLookupd struct {
	sync.RWMutex
	opts         *Options
	httpListener net.Listener
	waitGroup    util.WaitGroupWrapper
	DB           *RegistrationDB

	grpcTcpListener net.Listener
	grpcServer      *grpc.Server

	peers sync.Map
}

func New(opts *Options) (*XMQLookupd, error) {
	var err error

	l := &XMQLookupd{
		opts: opts,
		DB:   NewRegistrationDB(),
		// peers: make(map[string]*PeerInfo),
	}

	mlog.Infof(version.String("xmqlookupd"))

	l.httpListener, err = net.Listen("tcp", opts.HTTPAddress)
	if err != nil {
		return nil, fmt.Errorf("listen (%s) failed - %s", opts.HTTPAddress, err)
	}
	l.grpcTcpListener, err = net.Listen("tcp", opts.GrpcAddress)
	if err != nil {
		return nil, fmt.Errorf("listen (%s) failed - %s", opts.GrpcAddress, err)
	}
	l.grpcServer = grpc.NewServer()
	pbapi.RegisterXmqlookupdServer(l.grpcServer, l)
	reflection.Register(l.grpcServer)
	return l, nil
}

// Main starts an instance of xmqlookupd and returns an
// error if there was a problem starting up.
func (l *XMQLookupd) Main() error {
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
	l.waitGroup.Wrap(func() {
		if err := l.grpcServer.Serve(l.grpcTcpListener); err != nil {
			mlog.Errorf("faile serve(%s): %v", l.opts.GrpcAddress, err)
		}
	})
	httpServer := newHTTPServer(l)
	l.waitGroup.Wrap(func() {
		exitFunc(http_api.Serve(l.httpListener, httpServer, "HTTP"))
	})

	err := <-exitCh
	return err
}

func (l *XMQLookupd) RealHTTPAddr() *net.TCPAddr {
	return l.httpListener.Addr().(*net.TCPAddr)
}

func (l *XMQLookupd) RealGrpcAddr() *net.TCPAddr {
	return l.grpcTcpListener.Addr().(*net.TCPAddr)
}
func (l *XMQLookupd) Exit() {
	if l.grpcTcpListener != nil {
		l.grpcTcpListener.Close()
	}

	if l.httpListener != nil {
		l.httpListener.Close()
	}
	if l.grpcTcpListener != nil {
		l.grpcTcpListener.Close()
	}
	l.waitGroup.Wait()
}
