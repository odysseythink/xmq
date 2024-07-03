package xmqlookupd

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"sync/atomic"

	"github.com/julienschmidt/httprouter"
	"mlib.com/mlog"
	"mlib.com/xmq/internal/http_api"
	"mlib.com/xmq/internal/protocol"
	"mlib.com/xmq/internal/version"
	"mlib.com/xmq/pbapi"
)

type httpServer struct {
	xmqlookupd *XMQLookupd
	router     http.Handler
}

func newHTTPServer(l *XMQLookupd) *httpServer {

	router := httprouter.New()
	router.HandleMethodNotAllowed = true
	router.PanicHandler = http_api.LogPanicHandler()
	router.NotFound = http_api.LogNotFoundHandler()
	router.MethodNotAllowed = http_api.LogMethodNotAllowedHandler()
	s := &httpServer{
		xmqlookupd: l,
		router:     router,
	}

	router.Handle("GET", "/ping", http_api.Decorate(s.pingHandler, http_api.PlainText))
	router.Handle("GET", "/info", http_api.Decorate(s.doInfo, http_api.V1))

	// v1 negotiate
	router.Handle("GET", "/debug", http_api.Decorate(s.doDebug, http_api.V1))
	router.Handle("GET", "/lookup", http_api.Decorate(s.doLookup, http_api.V1))
	router.Handle("GET", "/topics", http_api.Decorate(s.doTopics, http_api.V1))
	router.Handle("GET", "/channels", http_api.Decorate(s.doChannels, http_api.V1))
	router.Handle("GET", "/nodes", http_api.Decorate(s.doNodes, http_api.V1))

	// only v1
	router.Handle("POST", "/topic/create", http_api.Decorate(s.doCreateTopic, http_api.V1))
	router.Handle("POST", "/topic/delete", http_api.Decorate(s.doDeleteTopic, http_api.V1))
	router.Handle("POST", "/channel/create", http_api.Decorate(s.doCreateChannel, http_api.V1))
	router.Handle("POST", "/channel/delete", http_api.Decorate(s.doDeleteChannel, http_api.V1))
	router.Handle("POST", "/topic/tombstone", http_api.Decorate(s.doTombstoneTopicProducer, http_api.V1))

	// debug
	router.HandlerFunc("GET", "/debug/pprof", pprof.Index)
	router.HandlerFunc("GET", "/debug/pprof/cmdline", pprof.Cmdline)
	router.HandlerFunc("GET", "/debug/pprof/symbol", pprof.Symbol)
	router.HandlerFunc("POST", "/debug/pprof/symbol", pprof.Symbol)
	router.HandlerFunc("GET", "/debug/pprof/profile", pprof.Profile)
	router.Handler("GET", "/debug/pprof/heap", pprof.Handler("heap"))
	router.Handler("GET", "/debug/pprof/goroutine", pprof.Handler("goroutine"))
	router.Handler("GET", "/debug/pprof/block", pprof.Handler("block"))
	router.Handler("GET", "/debug/pprof/threadcreate", pprof.Handler("threadcreate"))

	return s
}

func (s *httpServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	s.router.ServeHTTP(w, req)
}

func (s *httpServer) pingHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	return "OK", nil
}

func (s *httpServer) doInfo(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	return struct {
		Version string `json:"version"`
	}{
		Version: version.Binary,
	}, nil
}

func (s *httpServer) doTopics(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	topics := s.xmqlookupd.DB.FindRegistrations("topic", "*", "").Keys()
	return map[string]interface{}{
		"topics": topics,
	}, nil
}

func (s *httpServer) doChannels(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	reqParams, err := http_api.NewReqParams(req)
	if err != nil {
		return nil, http_api.Err{400, "INVALID_REQUEST"}
	}

	topicName, err := reqParams.Get("topic")
	if err != nil {
		return nil, http_api.Err{400, "MISSING_ARG_TOPIC"}
	}

	channels := s.xmqlookupd.DB.FindRegistrations("channel", topicName, "*").SubKeys()
	return map[string]interface{}{
		"channels": channels,
	}, nil
}

func (s *httpServer) doLookup(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	reqParams, err := http_api.NewReqParams(req)
	if err != nil {
		return nil, http_api.Err{400, "INVALID_REQUEST"}
	}

	topicName, err := reqParams.Get("topic")
	if err != nil {
		return nil, http_api.Err{400, "MISSING_ARG_TOPIC"}
	}

	registration := s.xmqlookupd.DB.FindRegistrations("topic", topicName, "")
	if len(registration) == 0 {
		return nil, http_api.Err{404, "TOPIC_NOT_FOUND"}
	}

	channels := s.xmqlookupd.DB.FindRegistrations("channel", topicName, "*").SubKeys()
	producers := s.xmqlookupd.DB.FindProducers("topic", topicName, "")
	producers = producers.FilterByActive(s.xmqlookupd.opts.InactiveProducerTimeout,
		s.xmqlookupd.opts.TombstoneLifetime)
	return map[string]interface{}{
		"channels":  channels,
		"producers": producers.PeerInfo(),
	}, nil
}

func (s *httpServer) doCreateTopic(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	reqParams, err := http_api.NewReqParams(req)
	if err != nil {
		return nil, http_api.Err{400, "INVALID_REQUEST"}
	}

	topicName, err := reqParams.Get("topic")
	if err != nil {
		return nil, http_api.Err{400, "MISSING_ARG_TOPIC"}
	}

	if !protocol.IsValidTopicName(topicName) {
		return nil, http_api.Err{400, "INVALID_ARG_TOPIC"}
	}

	mlog.Infof("DB: adding topic(%s)", topicName)
	key := Registration{"topic", topicName, ""}
	s.xmqlookupd.DB.AddRegistration(key)

	return nil, nil
}

func (s *httpServer) doDeleteTopic(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	reqParams, err := http_api.NewReqParams(req)
	if err != nil {
		return nil, http_api.Err{400, "INVALID_REQUEST"}
	}

	topicName, err := reqParams.Get("topic")
	if err != nil {
		return nil, http_api.Err{400, "MISSING_ARG_TOPIC"}
	}

	registrations := s.xmqlookupd.DB.FindRegistrations("channel", topicName, "*")
	for _, registration := range registrations {
		mlog.Infof("DB: removing channel(%s) from topic(%s)", registration.SubKey, topicName)
		s.xmqlookupd.DB.RemoveRegistration(registration)
	}

	registrations = s.xmqlookupd.DB.FindRegistrations("topic", topicName, "")
	for _, registration := range registrations {
		mlog.Infof("DB: removing topic(%s)", topicName)
		s.xmqlookupd.DB.RemoveRegistration(registration)
	}

	return nil, nil
}

func (s *httpServer) doTombstoneTopicProducer(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	reqParams, err := http_api.NewReqParams(req)
	if err != nil {
		return nil, http_api.Err{400, "INVALID_REQUEST"}
	}

	topicName, err := reqParams.Get("topic")
	if err != nil {
		return nil, http_api.Err{400, "MISSING_ARG_TOPIC"}
	}

	node, err := reqParams.Get("node")
	if err != nil {
		return nil, http_api.Err{400, "MISSING_ARG_NODE"}
	}

	mlog.Infof("DB: setting tombstone for producer@%s of topic(%s)", node, topicName)
	producers := s.xmqlookupd.DB.FindProducers("topic", topicName, "")
	for _, p := range producers {
		thisNode := fmt.Sprintf("%s:%d", p.peerInfo.BroadcastAddress, p.peerInfo.HttpPort)
		if thisNode == node {
			p.Tombstone()
		}
	}

	return nil, nil
}

func (s *httpServer) doCreateChannel(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	reqParams, err := http_api.NewReqParams(req)
	if err != nil {
		return nil, http_api.Err{400, "INVALID_REQUEST"}
	}

	topicName, channelName, err := http_api.GetTopicChannelArgs(reqParams)
	if err != nil {
		return nil, http_api.Err{400, err.Error()}
	}

	mlog.Infof("DB: adding channel(%s) in topic(%s)", channelName, topicName)
	key := Registration{"channel", topicName, channelName}
	s.xmqlookupd.DB.AddRegistration(key)

	mlog.Infof("DB: adding topic(%s)", topicName)
	key = Registration{"topic", topicName, ""}
	s.xmqlookupd.DB.AddRegistration(key)

	return nil, nil
}

func (s *httpServer) doDeleteChannel(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	reqParams, err := http_api.NewReqParams(req)
	if err != nil {
		return nil, http_api.Err{400, "INVALID_REQUEST"}
	}

	topicName, channelName, err := http_api.GetTopicChannelArgs(reqParams)
	if err != nil {
		return nil, http_api.Err{400, err.Error()}
	}

	registrations := s.xmqlookupd.DB.FindRegistrations("channel", topicName, channelName)
	if len(registrations) == 0 {
		return nil, http_api.Err{404, "CHANNEL_NOT_FOUND"}
	}

	mlog.Infof("DB: removing channel(%s) from topic(%s)", channelName, topicName)
	for _, registration := range registrations {
		s.xmqlookupd.DB.RemoveRegistration(registration)
	}

	return nil, nil
}

type node struct {
	RemoteAddress    string   `json:"remote_address"`
	Hostname         string   `json:"hostname"`
	BroadcastAddress string   `json:"broadcast_address"`
	TCPPort          int      `json:"tcp_port"`
	HTTPPort         int      `json:"http_port"`
	Version          string   `json:"version"`
	Tombstones       []bool   `json:"tombstones"`
	Topics           []string `json:"topics"`
	NodeID           string   `json:"node_id"`
	RaftState        string   `json:"raft_state"`
}

func (xmqlookupd *XMQLookupd) getNodes() []*protocol.PeerInfo {
	producers := xmqlookupd.DB.FindProducers("client", "", "").FilterByActive(
		xmqlookupd.opts.InactiveProducerTimeout, 0)
	nodes := make([]*protocol.PeerInfo, len(producers))
	topicProducersMap := make(map[string]Producers)
	for i, p := range producers {
		topics := xmqlookupd.DB.LookupRegistrations(p.peerInfo.Id).Filter("topic", "*", "").Keys()

		// for each topic find the producer that matches this peer
		// to add tombstone information
		tombstones := make([]bool, len(topics))
		for j, t := range topics {
			if _, exists := topicProducersMap[t]; !exists {
				topicProducersMap[t] = xmqlookupd.DB.FindProducers("topic", t, "")
			}

			topicProducers := topicProducersMap[t]
			for _, tp := range topicProducers {
				if tp.peerInfo == p.peerInfo {
					tombstones[j] = tp.IsTombstoned(xmqlookupd.opts.TombstoneLifetime)
					break
				}
			}
		}
		if val, ok := xmqlookupd.peers.Load(p.peerInfo.Id); ok {
			peerinfo := val.(*pbapi.PeerInfo)
			p.peerInfo.RaftState = peerinfo.RaftState
		}
		nodes[i] = &protocol.PeerInfo{
			PeerInfo: pbapi.PeerInfo{
				Id:               p.peerInfo.Id,
				RemoteAddress:    p.peerInfo.RemoteAddress,
				Hostname:         p.peerInfo.Hostname,
				BroadcastAddress: p.peerInfo.BroadcastAddress,
				TcpPort:          p.peerInfo.TcpPort,
				HttpPort:         p.peerInfo.HttpPort,
				Version:          p.peerInfo.Version,
				LastUpdate:       p.peerInfo.LastUpdate,
				RaftState:        p.peerInfo.RaftState,
			},
			XmqdPeer: &protocol.XmqdPeerInfo{
				Tombstones: tombstones,
				Topics:     topics,
				// NodeID:      p.peerInfo.XmqdPeer.NodeID,
				// RaftAddress: p.peerInfo.XmqdPeer.RaftAddress,
			},
		}
	}
	return nodes
}

func (s *httpServer) doNodes(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	// dont filter out tombstoned nodes
	nodes := s.xmqlookupd.getNodes()

	return map[string]interface{}{
		"producers": nodes,
	}, nil
}

func (s *httpServer) doDebug(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	s.xmqlookupd.DB.RLock()
	defer s.xmqlookupd.DB.RUnlock()

	data := make(map[string][]map[string]interface{})
	for r, producers := range s.xmqlookupd.DB.registrationMap {
		key := r.Category + ":" + r.Key + ":" + r.SubKey
		for _, p := range producers {
			m := map[string]interface{}{
				"id":                p.peerInfo.Id,
				"hostname":          p.peerInfo.Hostname,
				"broadcast_address": p.peerInfo.BroadcastAddress,
				"tcp_port":          p.peerInfo.TcpPort,
				"http_port":         p.peerInfo.HttpPort,
				"version":           p.peerInfo.Version,
				"last_update":       atomic.LoadInt64(&p.peerInfo.LastUpdate),
				"tombstoned":        p.tombstoned,
				"tombstoned_at":     p.tombstonedAt.UnixNano(),
			}
			data[key] = append(data[key], m)
		}
	}

	return data, nil
}
