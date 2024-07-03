package xmqd

import (
	"context"
	"net"
	"os"
	"strconv"
	"time"

	"mlib.com/mlog"
	"mlib.com/xmq/internal/version"
	"mlib.com/xmq/pbapi"
)

func connectCallback(n *XMQD, hostname string) func(*lookupPeer) {
	return func(lp *lookupPeer) {
		// ci := make(map[string]interface{})
		// ci["version"] = version.Binary
		// ci["tcp_port"] = n.getOpts().BroadcastTCPPort
		// ci["http_port"] = n.getOpts().BroadcastHTTPPort
		// ci["hostname"] = hostname
		// ci["broadcast_address"] = n.getOpts().BroadcastAddress

		// cmd, err := xmq.Identify(ci)
		// if err != nil {
		// 	lp.Close()
		// 	return
		// }
		req := &pbapi.IdentifyRequest{
			Peer: &pbapi.PeerInfo{
				Id:               n.getOpts().ID,
				TcpPort:          int32(n.getOpts().BroadcastTCPPort),
				HttpPort:         int32(n.getOpts().BroadcastHTTPPort),
				Hostname:         hostname,
				BroadcastAddress: n.getOpts().BroadcastAddress,
				Version:          version.Binary,
			},
		}
		rsp, err := lp.client.Identify(context.Background(), req)
		// resp, err := lp.Command(cmd)
		if err != nil {
			mlog.Errorf("LOOKUPD(%#v): Identify failed:%v", lp, err)
			return
		} else if rsp.Err != "" { //bytes.Equal(resp, []byte("E_INVALID")) {
			mlog.Infof("LOOKUPD(%#v): lookupd returned %#v", lp, rsp)
			lp.Close()
			return
		}
		lp.Info.BroadcastAddress = rsp.BroadcastAddress
		lp.Info.HTTPPort = int(rsp.HttpPort)
		lp.Info.TCPPort = int(rsp.TcpPort)
		lp.Info.Version = rsp.Version
		// err = json.Unmarshal(resp, &lp.Info)
		// if err != nil {
		// 	mlog.Errorf("LOOKUPD(%s): parsing response - %s", lp, resp)
		// 	lp.Close()
		// 	return
		// }
		mlog.Infof("LOOKUPD(%s): peer info %+v", lp, lp.Info)
		if lp.Info.BroadcastAddress == "" {
			mlog.Errorf("LOOKUPD(%s): no broadcast address", lp)
		}

		// build all the commands first so we exit the lock(s) as fast as possible
		// var commands []*xmq.Command
		// n.RLock()
		// for _, topic := range n.topicMap {
		// 	topic.RLock()
		// 	if len(topic.channelMap) == 0 {
		// 		commands = append(commands, xmq.Register(topic.name, ""))
		// 	} else {
		// 		for _, channel := range topic.channelMap {
		// 			commands = append(commands, xmq.Register(channel.topicName, channel.name))
		// 		}
		// 	}
		// 	topic.RUnlock()
		// }
		// n.RUnlock()

		var commands []*pbapi.RegisterRequest
		n.RLock()
		for _, topic := range n.topicMap {
			topic.RLock()
			if len(topic.Channels) == 0 {
				commands = append(commands, &pbapi.RegisterRequest{
					Id:          n.getOpts().ID,
					TopicName:   topic.Name,
					ChannelName: "",
				})
			} else {
				for _, channel := range topic.Channels {
					commands = append(commands, &pbapi.RegisterRequest{
						Id:          n.getOpts().ID,
						TopicName:   topic.Name,
						ChannelName: channel.Name,
					})
				}
			}
			topic.RUnlock()
		}
		n.RUnlock()

		for _, cmd := range commands {
			mlog.Infof("LOOKUPD(%s): %s", lp, cmd)
			// _, err := lp.Command(cmd)
			// if err != nil {
			// 	mlog.Errorf("LOOKUPD(%s): %s - %s", lp, cmd, err)
			// 	return
			// }

			rsp, err := lp.client.Register(context.Background(), cmd)
			if err != nil {
				mlog.Errorf("LOOKUPD(%s): %s - %s", lp, cmd, err)
				return
			} else if rsp.Err != "" { //bytes.Equal(resp, []byte("E_INVALID")) {
				mlog.Infof("LOOKUPD(%#v): Register returned %#v", lp, rsp)
				return
			}
		}
	}
}

func (n *XMQD) lookupLoop() {
	var lookupPeers []*lookupPeer
	var lookupAddrs []string
	connect := true

	hostname, err := os.Hostname()
	if err != nil {
		mlog.Fatalf("failed to get hostname - %s", err)
		os.Exit(1)
	}

	// for announcements, lookupd determines the host automatically
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if connect {
			for _, host := range n.getOpts().XMQLookupdGrpcAddresses {
				if in(host, lookupAddrs) {
					continue
				}
				mlog.Infof("LOOKUP(%s): adding peer", host)
				lookupPeer := newLookupPeer(host, n.getOpts().MaxBodySize,
					connectCallback(n, hostname))
				lookupPeer.startConnect() // start the connection
				lookupPeers = append(lookupPeers, lookupPeer)
				lookupAddrs = append(lookupAddrs, host)
			}
			n.lookupPeers.Store(lookupPeers)
			connect = false
		}

		select {
		case <-ticker.C:
			// send a heartbeat and read a response (read detects closed conns)
			for _, lookupPeer := range lookupPeers {
				mlog.Debugf("LOOKUPD(%s): sending heartbeat", lookupPeer)
				if err := lookupPeer.startConnect(); err != nil {
					mlog.Errorf("lookupPeer.startConnect failed:%v", err)
				} else {
					cmd := &pbapi.PingRequest{
						Peer: &pbapi.PeerInfo{
							Id:        n.getOpts().ID,
							RaftState: n.cluster.r.State().String(),
						},
					}
					_, err := lookupPeer.client.Ping(context.Background(), cmd)
					if err != nil {
						mlog.Errorf("LOOKUPD(%s): %s - %s", lookupPeer, cmd, err)
					}
				}
			}
		case val := <-n.notifyChan:
			var registerReq *pbapi.RegisterRequest
			var unregisterReq *pbapi.UnregisterRequest
			var branch string

			switch val := val.(type) {
			case *Channel:
				// notify all xmqlookupds that a new channel exists, or that it's removed
				branch = "channel"
				channel := val
				if channel.Exiting() {
					unregisterReq = &pbapi.UnregisterRequest{
						Id:          n.getOpts().ID,
						TopicName:   channel.TopicName,
						ChannelName: channel.Name,
					} //.UnRegister(channel.topicName, channel.name)
				} else {
					registerReq = &pbapi.RegisterRequest{
						Id:          n.getOpts().ID,
						TopicName:   channel.TopicName,
						ChannelName: channel.Name,
					} //xmq.Register(channel.topicName, channel.name)
				}
			case *Topic:
				// notify all xmqlookupds that a new topic exists, or that it's removed
				branch = "topic"
				topic := val
				if topic.Exiting() {
					unregisterReq = &pbapi.UnregisterRequest{
						Id:          n.getOpts().ID,
						TopicName:   topic.Name,
						ChannelName: "",
					} //xmq.UnRegister(topic.name, "")
				} else {
					registerReq = &pbapi.RegisterRequest{
						Id:          n.getOpts().ID,
						TopicName:   topic.Name,
						ChannelName: "",
					} //xmq.Register(topic.name, "")
				}
			}

			for _, lookupPeer := range lookupPeers {
				if registerReq != nil {
					mlog.Infof("LOOKUPD(%s) register: %s %s", lookupPeer, branch, registerReq)
					rsp, err := lookupPeer.client.Register(context.Background(), registerReq) //Command(cmd)
					if err != nil || rsp.Err != "" {
						mlog.Errorf("LOOKUPD(%s)  register(%#v) failed: %s - %#v", lookupPeer, registerReq, err, rsp)
					}
				}
				if unregisterReq != nil {
					mlog.Infof("LOOKUPD(%s) unregister: %s %s", lookupPeer, branch, unregisterReq)
					rsp, err := lookupPeer.client.Unregister(context.Background(), unregisterReq) //Command(cmd)
					if err != nil || rsp.Err != "" {
						mlog.Errorf("LOOKUPD(%s)  unregister(%#v) failed: %s - %#v", lookupPeer, unregisterReq, err, rsp)
					}
				}
			}
		case <-n.optsNotificationChan:
			var tmpPeers []*lookupPeer
			var tmpAddrs []string
			for _, lp := range lookupPeers {
				if in(lp.addr, n.getOpts().XMQLookupdGrpcAddresses) {
					tmpPeers = append(tmpPeers, lp)
					tmpAddrs = append(tmpAddrs, lp.addr)
					continue
				}
				mlog.Infof("LOOKUP(%s): removing peer", lp)
				lp.Close()
			}
			lookupPeers = tmpPeers
			lookupAddrs = tmpAddrs
			connect = true
		case <-n.ctx.Done():
			goto exit
		}
	}

exit:
	mlog.Infof("LOOKUP: closing")
}

func in(s string, lst []string) bool {
	for _, v := range lst {
		if s == v {
			return true
		}
	}
	return false
}

func (n *XMQD) lookupdHTTPAddrs() []string {
	var lookupHTTPAddrs []string
	lookupPeers := n.lookupPeers.Load()
	if lookupPeers == nil {
		return nil
	}
	for _, lp := range lookupPeers.([]*lookupPeer) {
		if len(lp.Info.BroadcastAddress) <= 0 {
			continue
		}
		addr := net.JoinHostPort(lp.Info.BroadcastAddress, strconv.Itoa(lp.Info.HTTPPort))
		lookupHTTPAddrs = append(lookupHTTPAddrs, addr)
	}
	return lookupHTTPAddrs
}
