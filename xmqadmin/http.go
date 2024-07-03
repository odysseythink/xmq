package xmqadmin

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"reflect"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"mlib.com/mlog"
	"mlib.com/xmq/internal/clusterinfo"
	"mlib.com/xmq/internal/http_api"
	"mlib.com/xmq/internal/protocol"
	"mlib.com/xmq/internal/version"
)

func maybeWarnMsg(msgs []string) string {
	if len(msgs) > 0 {
		return "WARNING: " + strings.Join(msgs, "; ")
	}
	return ""
}

// this is similar to httputil.NewSingleHostReverseProxy except it passes along basic auth
func NewSingleHostReverseProxy(target *url.URL, connectTimeout time.Duration, requestTimeout time.Duration) *httputil.ReverseProxy {
	director := func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		if target.User != nil {
			passwd, _ := target.User.Password()
			req.SetBasicAuth(target.User.Username(), passwd)
		}
	}
	return &httputil.ReverseProxy{
		Director:  director,
		Transport: http_api.NewDeadlineTransport(connectTimeout, requestTimeout),
	}
}

type httpServer struct {
	xmqadmin     *XMQAdmin
	router       http.Handler
	client       *http_api.Client
	ci           *clusterinfo.ClusterInfo
	basePath     string
	devStaticDir string
}

func NewHTTPServer(xmqadmin *XMQAdmin) *httpServer {
	client := http_api.NewClient(xmqadmin.httpClientTLSConfig, xmqadmin.getOpts().HTTPClientConnectTimeout,
		xmqadmin.getOpts().HTTPClientRequestTimeout)

	router := httprouter.New()
	router.HandleMethodNotAllowed = true
	router.PanicHandler = http_api.LogPanicHandler()
	router.NotFound = http_api.LogNotFoundHandler()
	router.MethodNotAllowed = http_api.LogMethodNotAllowedHandler()

	s := &httpServer{
		xmqadmin: xmqadmin,
		router:   router,
		client:   client,
		ci:       clusterinfo.New(client),

		basePath:     xmqadmin.getOpts().BasePath,
		devStaticDir: xmqadmin.getOpts().DevStaticDir,
	}

	bp := func(p string) string {
		return path.Join(s.basePath, p)
	}

	router.Handle("GET", bp("/"), http_api.Decorate(s.indexHandler))
	router.Handle("GET", bp("/ping"), http_api.Decorate(s.pingHandler, http_api.PlainText))

	router.Handle("GET", bp("/topics"), http_api.Decorate(s.indexHandler))
	router.Handle("GET", bp("/topics/:topic"), http_api.Decorate(s.indexHandler))
	router.Handle("GET", bp("/topics/:topic/:channel"), http_api.Decorate(s.indexHandler))
	router.Handle("GET", bp("/nodes"), http_api.Decorate(s.indexHandler))
	router.Handle("GET", bp("/nodes/:node"), http_api.Decorate(s.indexHandler))
	router.Handle("GET", bp("/counter"), http_api.Decorate(s.indexHandler))
	router.Handle("GET", bp("/lookup"), http_api.Decorate(s.indexHandler))

	router.Handle("GET", bp("/static/:asset"), http_api.Decorate(s.staticAssetHandler, http_api.PlainText))
	router.Handle("GET", bp("/fonts/:asset"), http_api.Decorate(s.staticAssetHandler, http_api.PlainText))
	if s.xmqadmin.getOpts().ProxyGraphite {
		proxy := NewSingleHostReverseProxy(xmqadmin.graphiteURL, xmqadmin.getOpts().HTTPClientConnectTimeout,
			xmqadmin.getOpts().HTTPClientRequestTimeout)
		router.Handler("GET", bp("/render"), proxy)
	}

	// v1 endpoints
	router.Handle("GET", bp("/api/topics"), http_api.Decorate(s.topicsHandler, http_api.V1))
	router.Handle("GET", bp("/api/topics/:topic"), http_api.Decorate(s.topicHandler, http_api.V1))
	router.Handle("GET", bp("/api/topics/:topic/:channel"), http_api.Decorate(s.channelHandler, http_api.V1))
	router.Handle("GET", bp("/api/nodes"), http_api.Decorate(s.nodesHandler, http_api.V1))
	router.Handle("GET", bp("/api/nodes/:node"), http_api.Decorate(s.nodeHandler, http_api.V1))
	router.Handle("POST", bp("/api/topics"), http_api.Decorate(s.createTopicChannelHandler, http_api.V1))
	router.Handle("POST", bp("/api/topics/:topic"), http_api.Decorate(s.topicActionHandler, http_api.V1))
	router.Handle("POST", bp("/api/topics/:topic/:channel"), http_api.Decorate(s.channelActionHandler, http_api.V1))
	router.Handle("DELETE", bp("/api/nodes/:node"), http_api.Decorate(s.tombstoneNodeForTopicHandler, http_api.V1))
	router.Handle("DELETE", bp("/api/topics/:topic"), http_api.Decorate(s.deleteTopicHandler, http_api.V1))
	router.Handle("DELETE", bp("/api/topics/:topic/:channel"), http_api.Decorate(s.deleteChannelHandler, http_api.V1))
	router.Handle("GET", bp("/api/counter"), http_api.Decorate(s.counterHandler, http_api.V1))
	router.Handle("GET", bp("/api/graphite"), http_api.Decorate(s.graphiteHandler, http_api.V1))
	router.Handle("GET", bp("/config/:opt"), http_api.Decorate(s.doConfig, http_api.V1))
	router.Handle("PUT", bp("/config/:opt"), http_api.Decorate(s.doConfig, http_api.V1))

	return s
}

func (s *httpServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	s.router.ServeHTTP(w, req)
}

func (s *httpServer) pingHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	return "OK", nil
}

func (s *httpServer) indexHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	asset, _ := staticAsset("index.html")
	t, _ := template.New("index").Funcs(template.FuncMap{
		"basePath": func(p string) string {
			return path.Join(s.basePath, p)
		},
	}).Parse(string(asset))

	w.Header().Set("Content-Type", "text/html")
	t.Execute(w, struct {
		Version             string
		ProxyGraphite       bool
		GraphEnabled        bool
		GraphiteURL         string
		StatsdInterval      int
		StatsdCounterFormat string
		StatsdGaugeFormat   string
		StatsdPrefix        string
		XMQLookupd          []string
		IsAdmin             bool
	}{
		Version:             version.Binary,
		ProxyGraphite:       s.xmqadmin.getOpts().ProxyGraphite,
		GraphEnabled:        s.xmqadmin.getOpts().GraphiteURL != "",
		GraphiteURL:         s.xmqadmin.getOpts().GraphiteURL,
		StatsdInterval:      int(s.xmqadmin.getOpts().StatsdInterval / time.Second),
		StatsdCounterFormat: s.xmqadmin.getOpts().StatsdCounterFormat,
		StatsdGaugeFormat:   s.xmqadmin.getOpts().StatsdGaugeFormat,
		StatsdPrefix:        s.xmqadmin.getOpts().StatsdPrefix,
		XMQLookupd:          s.xmqadmin.getOpts().XMQLookupdHTTPAddresses,
		IsAdmin:             s.isAuthorizedAdminRequest(req),
	})

	return nil, nil
}

func (s *httpServer) staticAssetHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	assetName := ps.ByName("asset")

	var (
		asset []byte
		err   error
	)
	if s.devStaticDir != "" {
		mlog.Debugf("using dev dir %q for static asset %q", s.devStaticDir, assetName)
		fsPath := path.Join(s.devStaticDir, assetName)
		asset, err = os.ReadFile(fsPath)
	} else {
		asset, err = staticAsset(assetName)
	}
	if err != nil {
		return nil, http_api.Err{404, "NOT_FOUND"}
	}

	ext := path.Ext(assetName)
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		switch ext {
		case ".map":
			ct = "application/json"
		case ".svg":
			ct = "image/svg+xml"
		case ".woff":
			ct = "application/font-woff"
		case ".ttf":
			ct = "application/font-sfnt"
		case ".eot":
			ct = "application/vnd.ms-fontobject"
		case ".woff2":
			ct = "application/font-woff2"
		}
	}
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}

	return string(asset), nil
}

func (s *httpServer) topicsHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	var messages []string

	reqParams, err := http_api.NewReqParams(req)
	if err != nil {
		return nil, http_api.Err{400, err.Error()}
	}

	var topics []string
	if len(s.xmqadmin.getOpts().XMQLookupdHTTPAddresses) != 0 {
		topics, err = s.ci.GetLookupdTopics(s.xmqadmin.getOpts().XMQLookupdHTTPAddresses)
	} else {
		topics, err = s.ci.GetXMQDTopics(s.xmqadmin.getOpts().XMQDHTTPAddresses)
	}
	if err != nil {
		pe, ok := err.(clusterinfo.PartialErr)
		if !ok {
			mlog.Errorf("failed to get topics - %s", err)
			return nil, http_api.Err{502, fmt.Sprintf("UPSTREAM_ERROR: %s", err)}
		}
		mlog.Warningf("%s", err)
		messages = append(messages, pe.Error())
	}

	inactive, _ := reqParams.Get("inactive")
	if inactive == "true" {
		topicChannelMap := make(map[string][]string)
		if len(s.xmqadmin.getOpts().XMQLookupdHTTPAddresses) == 0 {
			goto respond
		}
		for _, topicName := range topics {
			producers, _ := s.ci.GetLookupdTopicProducers(
				topicName, s.xmqadmin.getOpts().XMQLookupdHTTPAddresses)
			if len(producers) == 0 {
				topicChannels, _ := s.ci.GetLookupdTopicChannels(
					topicName, s.xmqadmin.getOpts().XMQLookupdHTTPAddresses)
				topicChannelMap[topicName] = topicChannels
			}
		}
	respond:
		return struct {
			Topics  map[string][]string `json:"topics"`
			Message string              `json:"message"`
		}{topicChannelMap, maybeWarnMsg(messages)}, nil
	}

	return struct {
		Topics  []string `json:"topics"`
		Message string   `json:"message"`
	}{topics, maybeWarnMsg(messages)}, nil
}

func (s *httpServer) topicHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	var messages []string

	topicName := ps.ByName("topic")

	producers, err := s.ci.GetTopicProducers(topicName,
		s.xmqadmin.getOpts().XMQLookupdHTTPAddresses,
		s.xmqadmin.getOpts().XMQDHTTPAddresses)
	if err != nil {
		pe, ok := err.(clusterinfo.PartialErr)
		if !ok {
			mlog.Errorf("failed to get topic producers - %s", err)
			return nil, http_api.Err{502, fmt.Sprintf("UPSTREAM_ERROR: %s", err)}
		}
		mlog.Warningf("%s", err)
		messages = append(messages, pe.Error())
	}
	topicStats, _, err := s.ci.GetXMQDStats(producers, topicName, "", false)
	if err != nil {
		pe, ok := err.(clusterinfo.PartialErr)
		if !ok {
			mlog.Errorf("failed to get topic metadata - %s", err)
			return nil, http_api.Err{502, fmt.Sprintf("UPSTREAM_ERROR: %s", err)}
		}
		mlog.Warningf("%s", err)
		messages = append(messages, pe.Error())
	}

	allNodesTopicStats := &clusterinfo.TopicStats{TopicName: topicName}
	for _, t := range topicStats {
		allNodesTopicStats.Add(t)
	}

	return struct {
		*clusterinfo.TopicStats
		Message string `json:"message"`
	}{allNodesTopicStats, maybeWarnMsg(messages)}, nil
}

func (s *httpServer) channelHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	var messages []string

	topicName := ps.ByName("topic")
	channelName := ps.ByName("channel")

	producers, err := s.ci.GetTopicProducers(topicName,
		s.xmqadmin.getOpts().XMQLookupdHTTPAddresses,
		s.xmqadmin.getOpts().XMQDHTTPAddresses)
	if err != nil {
		pe, ok := err.(clusterinfo.PartialErr)
		if !ok {
			mlog.Errorf("failed to get topic producers - %s", err)
			return nil, http_api.Err{502, fmt.Sprintf("UPSTREAM_ERROR: %s", err)}
		}
		mlog.Warningf("%s", err)
		messages = append(messages, pe.Error())
	}
	_, channelStats, err := s.ci.GetXMQDStats(producers, topicName, channelName, true)
	if err != nil {
		pe, ok := err.(clusterinfo.PartialErr)
		if !ok {
			mlog.Errorf("failed to get channel metadata - %s", err)
			return nil, http_api.Err{502, fmt.Sprintf("UPSTREAM_ERROR: %s", err)}
		}
		mlog.Warningf("%s", err)
		messages = append(messages, pe.Error())
	}

	return struct {
		*clusterinfo.ChannelStats
		Message string `json:"message"`
	}{channelStats[channelName], maybeWarnMsg(messages)}, nil
}

func (s *httpServer) nodesHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	var messages []string

	producers, err := s.ci.GetProducers(s.xmqadmin.getOpts().XMQLookupdHTTPAddresses, s.xmqadmin.getOpts().XMQDHTTPAddresses)
	if err != nil {
		pe, ok := err.(clusterinfo.PartialErr)
		if !ok {
			mlog.Errorf("failed to get nodes - %s", err)
			return nil, http_api.Err{502, fmt.Sprintf("UPSTREAM_ERROR: %s", err)}
		}
		mlog.Warningf("%s", err)
		messages = append(messages, pe.Error())
	}

	return struct {
		Nodes   clusterinfo.Producers `json:"nodes"`
		Message string                `json:"message"`
	}{producers, maybeWarnMsg(messages)}, nil
}

func (s *httpServer) nodeHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	var messages []string

	node := ps.ByName("node")

	producers, err := s.ci.GetProducers(s.xmqadmin.getOpts().XMQLookupdHTTPAddresses, s.xmqadmin.getOpts().XMQDHTTPAddresses)
	if err != nil {
		pe, ok := err.(clusterinfo.PartialErr)
		if !ok {
			mlog.Errorf("failed to get producers - %s", err)
			return nil, http_api.Err{502, fmt.Sprintf("UPSTREAM_ERROR: %s", err)}
		}
		mlog.Warningf("%s", err)
		messages = append(messages, pe.Error())
	}

	producer := producers.Search(node)
	if producer == nil {
		return nil, http_api.Err{404, "NODE_NOT_FOUND"}
	}

	topicStats, _, err := s.ci.GetXMQDStats(clusterinfo.Producers{producer}, "", "", true)
	if err != nil {
		mlog.Errorf("failed to get xmqd stats - %s", err)
		return nil, http_api.Err{502, fmt.Sprintf("UPSTREAM_ERROR: %s", err)}
	}

	var totalClients int64
	var totalMessages int64
	for _, ts := range topicStats {
		for _, cs := range ts.Channels {
			totalClients += int64(len(cs.Clients))
		}
		totalMessages += ts.MessageCount
	}

	return struct {
		Node          string                    `json:"node"`
		TopicStats    []*clusterinfo.TopicStats `json:"topics"`
		TotalMessages int64                     `json:"total_messages"`
		TotalClients  int64                     `json:"total_clients"`
		Message       string                    `json:"message"`
	}{
		Node:          node,
		TopicStats:    topicStats,
		TotalMessages: totalMessages,
		TotalClients:  totalClients,
		Message:       maybeWarnMsg(messages),
	}, nil
}

func (s *httpServer) tombstoneNodeForTopicHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	var messages []string

	if !s.isAuthorizedAdminRequest(req) {
		return nil, http_api.Err{403, "FORBIDDEN"}
	}

	node := ps.ByName("node")

	var body struct {
		Topic string `json:"topic"`
	}
	err := json.NewDecoder(req.Body).Decode(&body)
	if err != nil {
		return nil, http_api.Err{400, "INVALID_BODY"}
	}

	if !protocol.IsValidTopicName(body.Topic) {
		return nil, http_api.Err{400, "INVALID_TOPIC"}
	}

	err = s.ci.TombstoneNodeForTopic(body.Topic, node,
		s.xmqadmin.getOpts().XMQLookupdHTTPAddresses)
	if err != nil {
		pe, ok := err.(clusterinfo.PartialErr)
		if !ok {
			mlog.Errorf("failed to tombstone node for topic - %s", err)
			return nil, http_api.Err{502, fmt.Sprintf("UPSTREAM_ERROR: %s", err)}
		}
		mlog.Warningf("%s", err)
		messages = append(messages, pe.Error())
	}

	s.notifyAdminAction("tombstone_topic_producer", body.Topic, "", node, req)

	return struct {
		Message string `json:"message"`
	}{maybeWarnMsg(messages)}, nil
}

func (s *httpServer) createTopicChannelHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	var messages []string

	var body struct {
		Topic   string `json:"topic"`
		Channel string `json:"channel"`
	}

	if !s.isAuthorizedAdminRequest(req) {
		return nil, http_api.Err{403, "FORBIDDEN"}
	}

	err := json.NewDecoder(req.Body).Decode(&body)
	if err != nil {
		return nil, http_api.Err{400, err.Error()}
	}

	if !protocol.IsValidTopicName(body.Topic) {
		return nil, http_api.Err{400, "INVALID_TOPIC"}
	}

	if len(body.Channel) > 0 && !protocol.IsValidChannelName(body.Channel) {
		return nil, http_api.Err{400, "INVALID_CHANNEL"}
	}

	err = s.ci.CreateTopicChannel(body.Topic, body.Channel,
		s.xmqadmin.getOpts().XMQLookupdHTTPAddresses)
	if err != nil {
		pe, ok := err.(clusterinfo.PartialErr)
		if !ok {
			mlog.Errorf("failed to create topic/channel - %s", err)
			return nil, http_api.Err{502, fmt.Sprintf("UPSTREAM_ERROR: %s", err)}
		}
		mlog.Warningf("%s", err)
		messages = append(messages, pe.Error())
	}

	s.notifyAdminAction("create_topic", body.Topic, "", "", req)
	if len(body.Channel) > 0 {
		s.notifyAdminAction("create_channel", body.Topic, body.Channel, "", req)
	}

	return struct {
		Message string `json:"message"`
	}{maybeWarnMsg(messages)}, nil
}

func (s *httpServer) deleteTopicHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	var messages []string

	if !s.isAuthorizedAdminRequest(req) {
		return nil, http_api.Err{403, "FORBIDDEN"}
	}

	topicName := ps.ByName("topic")

	err := s.ci.DeleteTopic(topicName,
		s.xmqadmin.getOpts().XMQLookupdHTTPAddresses,
		s.xmqadmin.getOpts().XMQDHTTPAddresses)
	if err != nil {
		pe, ok := err.(clusterinfo.PartialErr)
		if !ok {
			mlog.Errorf("failed to delete topic - %s", err)
			return nil, http_api.Err{502, fmt.Sprintf("UPSTREAM_ERROR: %s", err)}
		}
		mlog.Warningf("%s", err)
		messages = append(messages, pe.Error())
	}

	s.notifyAdminAction("delete_topic", topicName, "", "", req)

	return struct {
		Message string `json:"message"`
	}{maybeWarnMsg(messages)}, nil
}

func (s *httpServer) deleteChannelHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	var messages []string

	if !s.isAuthorizedAdminRequest(req) {
		return nil, http_api.Err{403, "FORBIDDEN"}
	}

	topicName := ps.ByName("topic")
	channelName := ps.ByName("channel")

	err := s.ci.DeleteChannel(topicName, channelName,
		s.xmqadmin.getOpts().XMQLookupdHTTPAddresses,
		s.xmqadmin.getOpts().XMQDHTTPAddresses)
	if err != nil {
		pe, ok := err.(clusterinfo.PartialErr)
		if !ok {
			mlog.Errorf("failed to delete channel - %s", err)
			return nil, http_api.Err{502, fmt.Sprintf("UPSTREAM_ERROR: %s", err)}
		}
		mlog.Warningf("%s", err)
		messages = append(messages, pe.Error())
	}

	s.notifyAdminAction("delete_channel", topicName, channelName, "", req)

	return struct {
		Message string `json:"message"`
	}{maybeWarnMsg(messages)}, nil
}

func (s *httpServer) topicActionHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	topicName := ps.ByName("topic")
	return s.topicChannelAction(req, topicName, "")
}

func (s *httpServer) channelActionHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	topicName := ps.ByName("topic")
	channelName := ps.ByName("channel")
	return s.topicChannelAction(req, topicName, channelName)
}

func (s *httpServer) topicChannelAction(req *http.Request, topicName string, channelName string) (interface{}, error) {
	var messages []string

	var body struct {
		Action string `json:"action"`
	}

	if !s.isAuthorizedAdminRequest(req) {
		return nil, http_api.Err{403, "FORBIDDEN"}
	}

	err := json.NewDecoder(req.Body).Decode(&body)
	if err != nil {
		return nil, http_api.Err{400, err.Error()}
	}

	switch body.Action {
	case "pause":
		if channelName != "" {
			err = s.ci.PauseChannel(topicName, channelName,
				s.xmqadmin.getOpts().XMQLookupdHTTPAddresses,
				s.xmqadmin.getOpts().XMQDHTTPAddresses)

			s.notifyAdminAction("pause_channel", topicName, channelName, "", req)
		} else {
			err = s.ci.PauseTopic(topicName,
				s.xmqadmin.getOpts().XMQLookupdHTTPAddresses,
				s.xmqadmin.getOpts().XMQDHTTPAddresses)

			s.notifyAdminAction("pause_topic", topicName, "", "", req)
		}
	case "unpause":
		if channelName != "" {
			err = s.ci.UnPauseChannel(topicName, channelName,
				s.xmqadmin.getOpts().XMQLookupdHTTPAddresses,
				s.xmqadmin.getOpts().XMQDHTTPAddresses)

			s.notifyAdminAction("unpause_channel", topicName, channelName, "", req)
		} else {
			err = s.ci.UnPauseTopic(topicName,
				s.xmqadmin.getOpts().XMQLookupdHTTPAddresses,
				s.xmqadmin.getOpts().XMQDHTTPAddresses)

			s.notifyAdminAction("unpause_topic", topicName, "", "", req)
		}
	case "empty":
		if channelName != "" {
			err = s.ci.EmptyChannel(topicName, channelName,
				s.xmqadmin.getOpts().XMQLookupdHTTPAddresses,
				s.xmqadmin.getOpts().XMQDHTTPAddresses)

			s.notifyAdminAction("empty_channel", topicName, channelName, "", req)
		} else {
			err = s.ci.EmptyTopic(topicName,
				s.xmqadmin.getOpts().XMQLookupdHTTPAddresses,
				s.xmqadmin.getOpts().XMQDHTTPAddresses)

			s.notifyAdminAction("empty_topic", topicName, "", "", req)
		}
	default:
		return nil, http_api.Err{400, "INVALID_ACTION"}
	}

	if err != nil {
		pe, ok := err.(clusterinfo.PartialErr)
		if !ok {
			mlog.Errorf("failed to %s topic/channel - %s", body.Action, err)
			return nil, http_api.Err{502, fmt.Sprintf("UPSTREAM_ERROR: %s", err)}
		}
		mlog.Warningf("%s", err)
		messages = append(messages, pe.Error())
	}

	return struct {
		Message string `json:"message"`
	}{maybeWarnMsg(messages)}, nil
}

type counterStats struct {
	Node         string `json:"node"`
	TopicName    string `json:"topic_name"`
	ChannelName  string `json:"channel_name"`
	MessageCount int64  `json:"message_count"`
}

func (s *httpServer) counterHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	var messages []string
	stats := make(map[string]*counterStats)

	producers, err := s.ci.GetProducers(s.xmqadmin.getOpts().XMQLookupdHTTPAddresses, s.xmqadmin.getOpts().XMQDHTTPAddresses)
	if err != nil {
		pe, ok := err.(clusterinfo.PartialErr)
		if !ok {
			mlog.Errorf("failed to get counter producer list - %s", err)
			return nil, http_api.Err{502, fmt.Sprintf("UPSTREAM_ERROR: %s", err)}
		}
		mlog.Warningf("%s", err)
		messages = append(messages, pe.Error())
	}
	_, channelStats, err := s.ci.GetXMQDStats(producers, "", "", false)
	if err != nil {
		pe, ok := err.(clusterinfo.PartialErr)
		if !ok {
			mlog.Errorf("failed to get xmqd stats - %s", err)
			return nil, http_api.Err{502, fmt.Sprintf("UPSTREAM_ERROR: %s", err)}
		}
		mlog.Warningf("%s", err)
		messages = append(messages, pe.Error())
	}

	for _, channelStats := range channelStats {
		for _, hostChannelStats := range channelStats.NodeStats {
			key := fmt.Sprintf("%s:%s:%s", channelStats.TopicName, channelStats.ChannelName, hostChannelStats.Node)
			s, ok := stats[key]
			if !ok {
				s = &counterStats{
					Node:        hostChannelStats.Node,
					TopicName:   channelStats.TopicName,
					ChannelName: channelStats.ChannelName,
				}
				stats[key] = s
			}
			s.MessageCount += hostChannelStats.MessageCount
		}
	}

	return struct {
		Stats   map[string]*counterStats `json:"stats"`
		Message string                   `json:"message"`
	}{stats, maybeWarnMsg(messages)}, nil
}

func (s *httpServer) graphiteHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	reqParams, err := http_api.NewReqParams(req)
	if err != nil {
		return nil, http_api.Err{400, "INVALID_REQUEST"}
	}

	metric, err := reqParams.Get("metric")
	if err != nil || metric != "rate" {
		return nil, http_api.Err{400, "INVALID_ARG_METRIC"}
	}

	target, err := reqParams.Get("target")
	if err != nil {
		return nil, http_api.Err{400, "INVALID_ARG_TARGET"}
	}

	params := url.Values{}
	params.Set("from", fmt.Sprintf("-%dsec", s.xmqadmin.getOpts().StatsdInterval*2/time.Second))
	params.Set("until", fmt.Sprintf("-%dsec", s.xmqadmin.getOpts().StatsdInterval/time.Second))
	params.Set("format", "json")
	params.Set("target", target)
	query := fmt.Sprintf("/render?%s", params.Encode())
	url := s.xmqadmin.getOpts().GraphiteURL + query

	mlog.Infof("GRAPHITE: %s", url)

	var response []struct {
		Target     string       `json:"target"`
		DataPoints [][]*float64 `json:"datapoints"`
	}
	err = s.client.GETV1(url, &response)
	if err != nil {
		mlog.Errorf("graphite request failed - %s", err)
		return nil, http_api.Err{500, "INTERNAL_ERROR"}
	}

	var rateStr string
	rate := *response[0].DataPoints[0][0]
	if rate < 0 {
		rateStr = "N/A"
	} else {
		rateDivisor := s.xmqadmin.getOpts().StatsdInterval / time.Second
		rateStr = fmt.Sprintf("%.2f", rate/float64(rateDivisor))
	}
	return struct {
		Rate string `json:"rate"`
	}{rateStr}, nil
}

func (s *httpServer) doConfig(w http.ResponseWriter, req *http.Request, ps httprouter.Params) (interface{}, error) {
	opt := ps.ByName("opt")

	allowConfigFromCIDR := s.xmqadmin.getOpts().AllowConfigFromCIDR
	if allowConfigFromCIDR != "" {
		_, ipnet, _ := net.ParseCIDR(allowConfigFromCIDR)
		addr, _, err := net.SplitHostPort(req.RemoteAddr)
		if err != nil {
			mlog.Errorf("failed to parse RemoteAddr %s", req.RemoteAddr)
			return nil, http_api.Err{400, "INVALID_REMOTE_ADDR"}
		}
		ip := net.ParseIP(addr)
		if ip == nil {
			mlog.Errorf("failed to parse RemoteAddr %s", req.RemoteAddr)
			return nil, http_api.Err{400, "INVALID_REMOTE_ADDR"}
		}
		if !ipnet.Contains(ip) {
			return nil, http_api.Err{403, "FORBIDDEN"}
		}
	}

	if req.Method == "PUT" {
		// add 1 so that it's greater than our max when we test for it
		// (LimitReader returns a "fake" EOF)
		readMax := int64(1024*1024 + 1)
		body, err := io.ReadAll(io.LimitReader(req.Body, readMax))
		if err != nil {
			return nil, http_api.Err{500, "INTERNAL_ERROR"}
		}
		if int64(len(body)) == readMax || len(body) == 0 {
			return nil, http_api.Err{413, "INVALID_VALUE"}
		}

		opts := *s.xmqadmin.getOpts()
		switch opt {
		case "xmqlookupd_http_addresses":
			err := json.Unmarshal(body, &opts.XMQLookupdHTTPAddresses)
			if err != nil {
				return nil, http_api.Err{400, "INVALID_VALUE"}
			}
		case "log_level":
			logLevelStr := strings.ToLower(string(body))
			switch logLevelStr {
			case "debug":
				mlog.SetLogLevel(0)
			case "info":
				mlog.SetLogLevel(1)
			case "warning":
				mlog.SetLogLevel(2)
			case "error":
				mlog.SetLogLevel(3)
			case "fatal":
				mlog.SetLogLevel(4)
			default:
				mlog.Errorf("unsurport log_level(%s)", logLevelStr)
				return nil, http_api.Err{400, "INVALID_VALUE"}
			}
			if err != nil {
				return nil, http_api.Err{400, "INVALID_VALUE"}
			}
			opts.LogLevel = logLevelStr
		default:
			return nil, http_api.Err{400, "INVALID_OPTION"}
		}
		s.xmqadmin.swapOpts(&opts)
	}

	v, ok := getOptByCfgName(s.xmqadmin.getOpts(), opt)
	if !ok {
		return nil, http_api.Err{400, "INVALID_OPTION"}
	}

	return v, nil
}

func (s *httpServer) isAuthorizedAdminRequest(req *http.Request) bool {
	adminUsers := s.xmqadmin.getOpts().AdminUsers
	if len(adminUsers) == 0 {
		return true
	}
	aclHTTPHeader := s.xmqadmin.getOpts().ACLHTTPHeader
	user := req.Header.Get(aclHTTPHeader)
	for _, v := range adminUsers {
		if v == user {
			return true
		}
	}
	return false
}

func getOptByCfgName(opts interface{}, name string) (interface{}, bool) {
	val := reflect.ValueOf(opts).Elem()
	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		flagName := field.Tag.Get("flag")
		cfgName := field.Tag.Get("cfg")
		if flagName == "" {
			continue
		}
		if cfgName == "" {
			cfgName = strings.Replace(flagName, "-", "_", -1)
		}
		if name != cfgName {
			continue
		}
		return val.FieldByName(field.Name).Interface(), true
	}
	return nil, false
}
