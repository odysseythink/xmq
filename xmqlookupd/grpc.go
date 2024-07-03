package xmqlookupd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"demo.com/farm_manager/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"mlib.com/mlog"
	"mlib.com/xmq/internal/protocol"
	"mlib.com/xmq/internal/version"
	"mlib.com/xmq/pbapi"
)

func (l *XMQLookupd) Ping(ctx context.Context, in *pbapi.PingRequest) (*pbapi.PingReply, error) {
	peer, ok := peer.FromContext(ctx)
	if !ok {
		// 处理错误情况，无法从上下文中获取peer信息
		return nil, status.Error(codes.Internal, "无法获取客户端地址")
	}
	reply := &pbapi.PingReply{}
	defer func() {
		err := recover()
		if err != nil { //条件判断，是否存在异常
			//存在异常,抛出异常
			mlog.Infof("panic recover:%v\n%s", err, utils.GetCurrentGoroutineStack())
			reply.Err = "invalid arg"
			err = nil
			return
		}
	}()

	mlog.Infof("remote(%s) call Ping req=%#v", peer.Addr.String(), in)
	if in.Peer != nil && in.Peer.Id != "" {
		if val, ok := l.peers.Load(in.Peer.Id); ok {
			peerinfo := val.(*pbapi.PeerInfo)
			peerinfo.RaftState = in.Peer.RaftState
			l.peers.Store(in.Peer.Id, peerinfo)
		}
	}
	reply.Msg = "OK"
	return reply, nil
}

func (l *XMQLookupd) Identify(ctx context.Context, in *pbapi.IdentifyRequest) (*pbapi.IdentifyReply, error) {
	peer, ok := peer.FromContext(ctx)
	if !ok {
		// 处理错误情况，无法从上下文中获取peer信息
		return nil, status.Error(codes.Internal, "无法获取客户端地址")
	}
	reply := &pbapi.IdentifyReply{}
	defer func() {
		err := recover()
		if err != nil { //条件判断，是否存在异常
			//存在异常,抛出异常
			mlog.Infof("panic recover:%v\n%s", err, utils.GetCurrentGoroutineStack())
			reply.Err = "invalid arg"
			err = nil
			return
		}
	}()

	mlog.Infof("remote(%s) call Identify req=%#v", peer.Addr.String(), in)
	var err error

	// if client.peerInfo != nil {
	// 	return nil, protocol.NewFatalClientErr(err, "E_INVALID", "cannot IDENTIFY again")
	// }

	// var bodyLen int32
	// err = binary.Read(reader, binary.BigEndian, &bodyLen)
	// if err != nil {
	// 	return nil, protocol.NewFatalClientErr(err, "E_BAD_BODY", "IDENTIFY failed to read body size")
	// }

	// body := make([]byte, bodyLen)
	// _, err = io.ReadFull(reader, body)
	// if err != nil {
	// 	return nil, protocol.NewFatalClientErr(err, "E_BAD_BODY", "IDENTIFY failed to read body")
	// }

	// body is a json structure with producer information
	peerinfo := &pbapi.PeerInfo{
		Id:               in.Peer.Id,
		RemoteAddress:    in.Peer.RemoteAddress,
		Hostname:         in.Peer.Hostname,
		BroadcastAddress: in.Peer.BroadcastAddress,
		TcpPort:          in.Peer.TcpPort,
		HttpPort:         in.Peer.HttpPort,
		Version:          in.Peer.Version,
	}

	// err = json.Unmarshal(body, &peerInfo)
	// if err != nil {
	// 	return nil, protocol.NewFatalClientErr(err, "E_BAD_BODY", "IDENTIFY failed to decode JSON body")
	// }

	peerinfo.RemoteAddress = peer.Addr.String()

	// require all fields
	if peerinfo.Id == "" || peerinfo.BroadcastAddress == "" || peerinfo.TcpPort == 0 || peerinfo.HttpPort == 0 || peerinfo.Version == "" {
		mlog.Errorf("E_BAD_BODY:IDENTIFY missing fields")
		reply.Err = "E_BAD_BODY:IDENTIFY missing fields"
		// return nil, protocol.NewFatalClientErr(nil, "E_BAD_BODY", "IDENTIFY missing fields")
		return reply, nil
	}
	if _, ok := l.peers.Load(peerinfo.Id); ok {
		mlog.Errorf("E_INVALID:cannot IDENTIFY again")
		reply.Err = "E_INVALID:cannot IDENTIFY again"
		// return nil, protocol.NewFatalClientErr(err, "E_INVALID", "cannot IDENTIFY again")
		return reply, nil
	}

	atomic.StoreInt64(&peerinfo.LastUpdate, time.Now().UnixNano())

	mlog.Infof("IDENTIFY ID:%s Address:%s TCP:%d HTTP:%d Version:%s", peerinfo.Id,
		peerinfo.BroadcastAddress, peerinfo.TcpPort, peerinfo.HttpPort, peerinfo.Version)

	// client.peerInfo = &peerInfo
	l.peers.Store(peerinfo.Id, peerinfo)
	if l.DB.AddProducer(Registration{"client", "", ""}, &Producer{peerInfo: &protocol.PeerInfo{
		PeerInfo: pbapi.PeerInfo{
			Id:               peerinfo.Id,
			RemoteAddress:    peerinfo.RemoteAddress,
			Hostname:         peerinfo.Hostname,
			BroadcastAddress: peerinfo.BroadcastAddress,
			TcpPort:          peerinfo.TcpPort,
			HttpPort:         peerinfo.HttpPort,
			Version:          peerinfo.Version,
			LastUpdate:       peerinfo.LastUpdate,
		},
	}}) {
		mlog.Infof("DB: id(%s) REGISTER category:%s key:%s subkey:%s", peerinfo.Id, "client", "", "")
	}

	// build a response
	// data := make(map[string]interface{})
	reply.TcpPort = int32(l.RealGrpcAddr().Port)
	reply.HttpPort = int32(l.RealHTTPAddr().Port)
	reply.Version = version.Binary
	hostname, err := os.Hostname()
	if err != nil {
		mlog.Fatalf("ERROR: unable to get hostname %s", err)
	}
	reply.BroadcastAddress = l.opts.BroadcastAddress
	reply.Hostname = hostname

	return reply, nil
}

func (l *XMQLookupd) Unregister(ctx context.Context, in *pbapi.UnregisterRequest) (*pbapi.UnregisterReply, error) {
	peer, ok := peer.FromContext(ctx)
	if !ok {
		// 处理错误情况，无法从上下文中获取peer信息
		return nil, status.Error(codes.Internal, "无法获取客户端地址")
	}
	reply := &pbapi.UnregisterReply{}
	defer func() {
		err := recover()
		if err != nil { //条件判断，是否存在异常
			//存在异常,抛出异常
			mlog.Infof("panic recover:%v\n%s", err, utils.GetCurrentGoroutineStack())
			reply.Err = "invalid arg"
			err = nil
			return
		}
	}()

	mlog.Infof("remote(%s) call Unregister req=%#v", peer.Addr.String(), in)
	if in.Id == "" {
		mlog.Errorf("E_BAD_BODY:IDENTIFY missing fields")
		reply.Err = "E_BAD_BODY:IDENTIFY missing fields"
		// return nil, protocol.NewFatalClientErr(nil, "E_BAD_BODY", "IDENTIFY missing fields")
		return reply, nil
	}
	var peerInfo *pbapi.PeerInfo
	if val, ok := l.peers.Load(in.Id); !ok || val == nil {
		mlog.Errorf("E_INVALID:client must IDENTIFY")
		reply.Err = "E_INVALID:client must IDENTIFY"
		// return nil, protocol.NewFatalClientErr(err, "E_INVALID", "cannot IDENTIFY again")
		return reply, nil
	} else {
		if infoval, ok := val.(*pbapi.PeerInfo); !ok {
			mlog.Errorf("E_INVALID:client must IDENTIFY")
			reply.Err = "E_INVALID:client must IDENTIFY"
			// return nil, protocol.NewFatalClientErr(err, "E_INVALID", "cannot IDENTIFY again")
			return reply, nil
		} else {
			peerInfo = infoval
		}
	}

	// if client.peerInfo == nil {
	// 	return nil, protocol.NewFatalClientErr(nil, "E_INVALID", "client must IDENTIFY")
	// }
	if !protocol.IsValidTopicName(in.TopicName) {
		mlog.Errorf("E_BAD_TOPIC:Unregister topic name '%s' is not valid", in.TopicName)
		reply.Err = fmt.Sprintf("E_BAD_TOPIC:Unregister topic name '%s' is not valid", in.TopicName)
		return reply, nil
		// return "", "", protocol.NewFatalClientErr(nil, "E_BAD_TOPIC", fmt.Sprintf("%s topic name '%s' is not valid", command, topicName))
	}

	if in.ChannelName != "" && !protocol.IsValidChannelName(in.ChannelName) {
		mlog.Errorf("E_BAD_CHANNEL:Unregister channel name '%s' is not valid", in.ChannelName)
		reply.Err = fmt.Sprintf("E_BAD_CHANNEL:Unregister channel name '%s' is not valid", in.ChannelName)
		return reply, nil
		// return "", "", protocol.NewFatalClientErr(nil, "E_BAD_CHANNEL", fmt.Sprintf("%s channel name '%s' is not valid", command, channelName))
	}

	// topic, channel, err := getTopicChan("UNREGISTER", params)
	// if err != nil {
	// 	return nil, err
	// }

	if in.ChannelName != "" {
		key := Registration{"channel", in.TopicName, in.ChannelName}
		removed, left := l.DB.RemoveProducer(key, peerInfo.Id)
		if removed {
			mlog.Infof("DB: client(%s) UNREGISTER category:%s key:%s subkey:%s",
				peerInfo.Id, "channel", in.TopicName, in.ChannelName)
		}
		// for ephemeral channels, remove the channel as well if it has no producers
		if left == 0 && strings.HasSuffix(in.ChannelName, "#ephemeral") {
			l.DB.RemoveRegistration(key)
		}
	} else {
		// no channel was specified so this is a topic unregistration
		// remove all of the channel registrations...
		// normally this shouldn't happen which is why we print a warning message
		// if anything is actually removed
		registrations := l.DB.FindRegistrations("channel", in.TopicName, "*")
		for _, r := range registrations {
			removed, _ := l.DB.RemoveProducer(r, peerInfo.Id)
			if removed {
				mlog.Warningf("client(%s) unexpected UNREGISTER category:%s key:%s subkey:%s",
					peerInfo.Id, "channel", in.TopicName, r.SubKey)
			}
		}

		key := Registration{"topic", in.TopicName, ""}
		removed, left := l.DB.RemoveProducer(key, peerInfo.Id)
		if removed {
			mlog.Infof("DB: client(%s) UNREGISTER category:%s key:%s subkey:%s",
				peerInfo.Id, "topic", in.TopicName, "")
		}
		if left == 0 && strings.HasSuffix(in.TopicName, "#ephemeral") {
			l.DB.RemoveRegistration(key)
		}
	}
	reply.Msg = "OK"
	return reply, nil
}

func (l *XMQLookupd) Register(ctx context.Context, in *pbapi.RegisterRequest) (*pbapi.RegisterReply, error) {
	peer, ok := peer.FromContext(ctx)
	if !ok {
		// 处理错误情况，无法从上下文中获取peer信息
		return nil, status.Error(codes.Internal, "无法获取客户端地址")
	}
	reply := &pbapi.RegisterReply{}
	defer func() {
		err := recover()
		if err != nil { //条件判断，是否存在异常
			//存在异常,抛出异常
			mlog.Infof("panic recover:%v\n%s", err, utils.GetCurrentGoroutineStack())
			reply.Err = "invalid arg"
			err = nil
			return
		}
	}()

	mlog.Infof("remote(%s) call Register req=%#v", peer.Addr.String(), in)

	mlog.Infof("remote(%s) call Unregister req=%#v", peer.Addr.String(), in)
	if in.Id == "" {
		mlog.Errorf("E_BAD_BODY:IDENTIFY missing fields")
		reply.Err = "E_BAD_BODY:IDENTIFY missing fields"
		// return nil, protocol.NewFatalClientErr(nil, "E_BAD_BODY", "IDENTIFY missing fields")
		return reply, nil
	}
	var peerinfo *pbapi.PeerInfo
	if val, ok := l.peers.Load(in.Id); !ok || val == nil {
		mlog.Errorf("E_INVALID:client must IDENTIFY")
		reply.Err = "E_INVALID:client must IDENTIFY"
		// return nil, protocol.NewFatalClientErr(err, "E_INVALID", "cannot IDENTIFY again")
		return reply, nil
	} else {
		if infoval, ok := val.(*pbapi.PeerInfo); !ok {
			mlog.Errorf("E_INVALID:client must IDENTIFY")
			reply.Err = "E_INVALID:client must IDENTIFY"
			// return nil, protocol.NewFatalClientErr(err, "E_INVALID", "cannot IDENTIFY again")
			return reply, nil
		} else {
			peerinfo = infoval
		}
	}

	// func (p *LookupProtocolV1) REGISTER(client *ClientV1, reader *bufio.Reader, params []string) ([]byte, error) {
	// if client.peerInfo == nil {
	// 	return nil, protocol.NewFatalClientErr(nil, "E_INVALID", "client must IDENTIFY")
	// }

	// topic, channel, err := getTopicChan("REGISTER", params)
	// if err != nil {
	// 	return nil, err
	// }
	if !protocol.IsValidTopicName(in.TopicName) {
		mlog.Errorf("E_BAD_TOPIC:Unregister topic name '%s' is not valid", in.TopicName)
		reply.Err = fmt.Sprintf("E_BAD_TOPIC:Unregister topic name '%s' is not valid", in.TopicName)
		return reply, nil
		// return "", "", protocol.NewFatalClientErr(nil, "E_BAD_TOPIC", fmt.Sprintf("%s topic name '%s' is not valid", command, topicName))
	}

	if in.ChannelName != "" && !protocol.IsValidChannelName(in.ChannelName) {
		mlog.Errorf("E_BAD_CHANNEL:Unregister channel name '%s' is not valid", in.ChannelName)
		reply.Err = fmt.Sprintf("E_BAD_CHANNEL:Unregister channel name '%s' is not valid", in.ChannelName)
		return reply, nil
		// return "", "", protocol.NewFatalClientErr(nil, "E_BAD_CHANNEL", fmt.Sprintf("%s channel name '%s' is not valid", command, channelName))
	}

	if in.ChannelName != "" {
		key := Registration{"channel", in.TopicName, in.ChannelName}
		if l.DB.AddProducer(key, &Producer{peerInfo: &protocol.PeerInfo{
			PeerInfo: pbapi.PeerInfo{
				Id:               peerinfo.Id,
				RemoteAddress:    peerinfo.RemoteAddress,
				Hostname:         peerinfo.Hostname,
				BroadcastAddress: peerinfo.BroadcastAddress,
				TcpPort:          peerinfo.TcpPort,
				HttpPort:         peerinfo.HttpPort,
				Version:          peerinfo.Version,
				LastUpdate:       peerinfo.LastUpdate,
			},
		}}) {
			mlog.Infof("DB: client(%s) REGISTER category:%s key:%s subkey:%s",
				peerinfo.Id, "channel", in.TopicName, in.ChannelName)
		}
	}
	key := Registration{"topic", in.TopicName, ""}
	if l.DB.AddProducer(key, &Producer{peerInfo: &protocol.PeerInfo{
		PeerInfo: pbapi.PeerInfo{
			Id:               peerinfo.Id,
			RemoteAddress:    peerinfo.RemoteAddress,
			Hostname:         peerinfo.Hostname,
			BroadcastAddress: peerinfo.BroadcastAddress,
			TcpPort:          peerinfo.TcpPort,
			HttpPort:         peerinfo.HttpPort,
			Version:          peerinfo.Version,
			LastUpdate:       peerinfo.LastUpdate,
		},
	}}) {
		mlog.Infof("DB: client(%s) REGISTER category:%s key:%s subkey:%s",
			peerinfo.Id, "topic", in.TopicName, "")
	}
	reply.Msg = "OK"
	return reply, nil
}
