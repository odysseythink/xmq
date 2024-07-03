package xmqd

import (
	"io"
	"net"
	"sync"

	"github.com/hashicorp/raft"
	"mlib.com/mlog"
	"mlib.com/xmq/internal/protocol"
)

const (
	typeConsumer = iota
	typeProducer
)

type Client interface {
	Type() int
	Stats(string) ClientStats
}

type tcpServer struct {
	xmqd  *XMQD
	conns sync.Map
}

func (p *tcpServer) Handle(conn net.Conn) {
	mlog.Infof("TCP: new client(%s)", conn.RemoteAddr())
	if p.xmqd.cluster.r.State() != raft.Leader {
		protocol.SendFramedResponse(conn, frameTypeError, []byte("E_NOT_LEADER"))
		conn.Close()
	}
	// The client should initialize itself by sending a 4 byte sequence indicating
	// the version of the protocol that it intends to communicate, this will allow us
	// to gracefully upgrade the protocol away from text/line oriented to whatever...
	buf := make([]byte, 4)
	_, err := io.ReadFull(conn, buf)
	if err != nil {
		mlog.Errorf("failed to read protocol version - %s", err)
		conn.Close()
		return
	}
	protocolMagic := string(buf)

	mlog.Infof("CLIENT(%s): desired protocol magic '%s'",
		conn.RemoteAddr(), protocolMagic)

	var prot protocol.Protocol
	switch protocolMagic {
	case "  V2":
		prot = &protocolV2{xmqd: p.xmqd}
	default:
		protocol.SendFramedResponse(conn, frameTypeError, []byte("E_BAD_PROTOCOL"))
		conn.Close()
		mlog.Errorf("client(%s) bad protocol magic '%s'",
			conn.RemoteAddr(), protocolMagic)
		return
	}

	client := prot.NewClient(conn)
	p.conns.Store(conn.RemoteAddr(), client)

	err = prot.IOLoop(client)
	if err != nil {
		mlog.Errorf("client(%s) - %s", conn.RemoteAddr(), err)
	}

	p.conns.Delete(conn.RemoteAddr())
	client.Close()
}

func (p *tcpServer) Close() {
	p.conns.Range(func(k, v interface{}) bool {
		v.(protocol.Client).Close()
		return true
	})
}
