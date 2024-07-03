package xmqd

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"mlib.com/go-xmq"
	"mlib.com/mlog"
	"mlib.com/xmq/pbapi"
)

// lookupPeer is a low-level type for connecting/reading/writing to xmqlookupd
//
// A lookupPeer instance is designed to connect lazily to xmqlookupd and reconnect
// gracefully (i.e. it is all handled by the library).  Clients can simply use the
// Command interface to perform a round-trip.
type lookupPeer struct {
	addr string
	// conn            net.Conn
	conn            *grpc.ClientConn
	client          pbapi.XmqlookupdClient
	state           int32
	connectCallback func(*lookupPeer)
	maxBodySize     int64
	Info            peerInfo
}

// peerInfo contains metadata for a lookupPeer instance (and is JSON marshalable)
type peerInfo struct {
	TCPPort          int    `json:"tcp_port"`
	HTTPPort         int    `json:"http_port"`
	Version          string `json:"version"`
	BroadcastAddress string `json:"broadcast_address"`
}

// newLookupPeer creates a new lookupPeer instance connecting to the supplied address.
//
// The supplied connectCallback will be called *every* time the instance connects.
func newLookupPeer(addr string, maxBodySize int64, connectCallback func(*lookupPeer)) *lookupPeer {
	return &lookupPeer{
		addr:            addr,
		state:           stateDisconnected,
		maxBodySize:     maxBodySize,
		connectCallback: connectCallback,
	}
}

// Connect will Dial the specified address, with timeouts
func (lp *lookupPeer) Connect() error {
	mlog.Infof("LOOKUP connecting to %s", lp.addr)
	// conn, err := net.DialTimeout("tcp", lp.addr, time.Second)
	// if err != nil {
	// 	return err
	// }
	// lp.conn = conn
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, lp.addr, grpc.WithInsecure())
	if err != nil {
		mlog.Errorf("grpc.DialContext(%s) failed:%v", lp.addr, err)
		return err
	}
	lp.conn = conn
	lp.client = pbapi.NewXmqlookupdClient(lp.conn)
	return nil
}

// String returns the specified address
func (lp *lookupPeer) String() string {
	return lp.addr
}

// Read implements the io.Reader interface, adding deadlines
func (lp *lookupPeer) Read(data []byte) (int, error) {
	// lp.conn.SetReadDeadline(time.Now().Add(time.Second))
	// return lp.conn.Read(data)
	return 0, nil
}

// Write implements the io.Writer interface, adding deadlines
func (lp *lookupPeer) Write(data []byte) (int, error) {
	// lp.conn.SetWriteDeadline(time.Now().Add(time.Second))
	// return lp.conn.Write(data)
	return 0, nil
}

// Close implements the io.Closer interface
func (lp *lookupPeer) Close() error {
	lp.state = stateDisconnected
	if lp.conn != nil {
		return lp.conn.Close()
	}
	return nil
}

func (lp *lookupPeer) startConnect() error {
	initialState := lp.state
	if lp.state != stateConnected {
		err := lp.Connect()
		if err != nil {
			return err
		}
		lp.state = stateConnected
		_, err = lp.Write(xmq.MagicV1)
		if err != nil {
			lp.Close()
			return err
		}
		if initialState == stateDisconnected {
			lp.connectCallback(lp)
		}
		if lp.state != stateConnected {
			return fmt.Errorf("lookupPeer connectCallback() failed")
		}
	}
	return nil
}

// Command performs a round-trip for the specified Command.
//
// It will lazily connect to xmqlookupd and gracefully handle
// reconnecting in the event of a failure.
//
// It returns the response from xmqlookupd as []byte
// func (lp *lookupPeer) Command(cmd *xmq.Command) ([]byte, error) {
// 	initialState := lp.state
// 	if lp.state != stateConnected {
// 		err := lp.Connect()
// 		if err != nil {
// 			return nil, err
// 		}
// 		lp.state = stateConnected
// 		_, err = lp.Write(xmq.MagicV1)
// 		if err != nil {
// 			lp.Close()
// 			return nil, err
// 		}
// 		if initialState == stateDisconnected {
// 			lp.connectCallback(lp)
// 		}
// 		if lp.state != stateConnected {
// 			return nil, fmt.Errorf("lookupPeer connectCallback() failed")
// 		}
// 	}
// 	if cmd == nil {
// 		return nil, nil
// 	}
// 	_, err := cmd.WriteTo(lp)
// 	if err != nil {
// 		lp.Close()
// 		return nil, err
// 	}
// 	resp, err := readResponseBounded(lp, lp.maxBodySize)
// 	if err != nil {
// 		lp.Close()
// 		return nil, err
// 	}
// 	return resp, nil
// }

// func readResponseBounded(r io.Reader, limit int64) ([]byte, error) {
// 	var msgSize int32

// 	// message size
// 	err := binary.Read(r, binary.BigEndian, &msgSize)
// 	if err != nil {
// 		return nil, err
// 	}

// 	if int64(msgSize) > limit {
// 		return nil, fmt.Errorf("response body size (%d) is greater than limit (%d)",
// 			msgSize, limit)
// 	}

// 	// message binary data
// 	buf := make([]byte, msgSize)
// 	_, err = io.ReadFull(r, buf)
// 	if err != nil {
// 		return nil, err
// 	}

// 	return buf, nil
// }
