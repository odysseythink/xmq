package main

import (
	"os"
	"regexp"
	"sync"
	"time"

	"mlib.com/go-xmq"
	"mlib.com/mlog"
	"mlib.com/xmq/internal/clusterinfo"
	"mlib.com/xmq/internal/http_api"
)

type TopicDiscoverer struct {
	opts     *Options
	ci       *clusterinfo.ClusterInfo
	topics   map[string]*FileLogger
	hupChan  chan os.Signal
	termChan chan os.Signal
	wg       sync.WaitGroup
	cfg      *xmq.Config
}

func newTopicDiscoverer(opts *Options, cfg *xmq.Config, hupChan chan os.Signal, termChan chan os.Signal) *TopicDiscoverer {
	client := http_api.NewClient(nil, opts.HTTPClientConnectTimeout, opts.HTTPClientRequestTimeout)
	return &TopicDiscoverer{
		opts:     opts,
		ci:       clusterinfo.New(client),
		topics:   make(map[string]*FileLogger),
		hupChan:  hupChan,
		termChan: termChan,
		cfg:      cfg,
	}
}

func (t *TopicDiscoverer) updateTopics(topics []string) {
	for _, topic := range topics {
		if _, ok := t.topics[topic]; ok {
			continue
		}

		if !t.isTopicAllowed(topic) {
			mlog.Warningf("skipping topic %s (doesn't match pattern %s)", topic, t.opts.TopicPattern)
			continue
		}

		fl, err := NewFileLogger(t.opts, topic, t.cfg)
		if err != nil {
			mlog.Errorf("couldn't create logger for new topic %s: %s", topic, err)
			continue
		}
		t.topics[topic] = fl

		t.wg.Add(1)
		go func(fl *FileLogger) {
			fl.router()
			t.wg.Done()
		}(fl)
	}
}

func (t *TopicDiscoverer) run() {
	var ticker <-chan time.Time
	if len(t.opts.Topics) == 0 {
		ticker = time.Tick(t.opts.TopicRefreshInterval)
	}
	t.updateTopics(t.opts.Topics)
forloop:
	for {
		select {
		case <-ticker:
			newTopics, err := t.ci.GetLookupdTopics(t.opts.XMQLookupdHTTPAddrs)
			if err != nil {
				mlog.Errorf("could not retrieve topic list: %s", err)
				continue
			}
			t.updateTopics(newTopics)
		case <-t.termChan:
			for _, fl := range t.topics {
				close(fl.termChan)
			}
			break forloop
		case <-t.hupChan:
			for _, fl := range t.topics {
				fl.hupChan <- true
			}
		}
	}
	t.wg.Wait()
}

func (t *TopicDiscoverer) isTopicAllowed(topic string) bool {
	if t.opts.TopicPattern == "" {
		return true
	}
	match, err := regexp.MatchString(t.opts.TopicPattern, topic)
	if err != nil {
		return false
	}
	return match
}
