package xmqlookupd

import (
	"net"

	"mlib.com/xmq/internal/protocol"
)

type ClientV1 struct {
	net.Conn
	peerInfo *protocol.PeerInfo
}

func NewClientV1(conn net.Conn) *ClientV1 {
	return &ClientV1{
		Conn: conn,
	}
}

func (c *ClientV1) String() string {
	return c.RemoteAddr().String()
}
