package xmqd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
	"mlib.com/mlog"
	"mlib.com/mrun/cast"
	"mlib.com/mrun/diskqueue"
	"mlib.com/xmq/internal/quantile"
)

type kvFsm struct {
	db sync.Map
}

type setPayload struct {
	Method string
	Args   []string
	Value  string
}

func (n *XMQCluster) ClusterDeleteTopic(sp setPayload) error {
	if len(sp.Args) != 1 || sp.Args[0] == "" {
		mlog.Errorf("ClusterDeleteTopic failed: invalid arg %#v", sp)
		return errors.New("ClusterDeleteTopic Args is invalid")
	}
	val, ok := n.topics.Load(sp.Args[0])
	if !ok || val == nil {
		mlog.Errorf("ClusterDeleteTopic topic(%s) not exist", sp.Args[0])
		if ok && val == nil {
			n.topics.Delete(sp.Args[0])
		}
		return fmt.Errorf("ClusterDeleteTopic topic(%s) not exist", sp.Args[0])
	}
	topic := val.(*Topic)

	// delete empties all channels and the topic itself before closing
	// (so that we dont leave any messages around)
	//
	// we do this before removing the topic from map below (with no lock)
	// so that any incoming writes will error and not create a new topic
	// to enforce ordering
	topic.Delete()

	n.topics.Delete(sp.Args[0])

	mlog.Infof("ClusterDeleteTopic delete topic(%s)", sp.Args[0])
	return nil
}

func (n *XMQCluster) ClusterDeleteChannel(sp setPayload) error {
	if len(sp.Args) != 2 || sp.Args[0] == "" || sp.Args[1] == "" {
		mlog.Errorf("ClusterDeleteChannel failed: invalid arg %#v", sp)
		return errors.New("ClusterDeleteChannel Args is invalid")
	}
	val, ok := n.topics.Load(sp.Args[0])
	if !ok || val == nil {
		mlog.Errorf("ClusterDeleteChannel topic(%s) not exist", sp.Args[0])
		if ok && val == nil {
			n.topics.Delete(sp.Args[0])
		}
		return fmt.Errorf("ClusterDeleteChannel topic(%s) not exist", sp.Args[0])
	}
	topic := val.(*Topic)
	topic.Lock()
	if _, ok := topic.Channels[sp.Args[1]]; ok {
		channel := topic.Channels[sp.Args[1]]
		channel.Delete()
		delete(topic.Channels, sp.Args[1])
	}
	topic.Unlock()

	mlog.Infof("ClusterDeleteChannel delete channel(%s)", sp.Args[0]+":"+sp.Args[1])
	return nil
}

func (n *XMQCluster) ClusterTopicUpdate(sp setPayload) error {
	if len(sp.Args) != 2 || sp.Args[0] == "" || sp.Args[1] == "" {
		mlog.Errorf("ClusterTopicUpdate failed: invalid arg %#v", sp)
		return errors.New("ClusterTopicUpdate Args is invalid")
	}
	val, ok := n.topics.Load(sp.Args[0])
	if !ok || val == nil {
		mlog.Errorf("ClusterTopicUpdate topic(%s) not exist", sp.Args[0])
		if ok && val == nil {
			n.topics.Delete(sp.Args[0])
		}
		return fmt.Errorf("ClusterTopicUpdate topic(%s) not exist", sp.Args[0])
	}
	t := val.(*Topic)
	switch sp.Args[1] {
	case "message_count", "MessageCount":
		t.MessageCount = cast.ToUint64(sp.Value)
	case "MessageBytes", "message_bytes":
		t.MessageBytes = cast.ToUint64(sp.Value)
	case "Name", "name":
		t.Name = sp.Value
	case "Channels", "channels":
		if sp.Value == "" {
			t.Channels = make(map[string]*Channel)
		} else {
			err := json.Unmarshal([]byte(sp.Value), &t.Channels)
			if err != nil {
				mlog.Errorf("ClusterTopicUpdate json.Unmarshal(value=%s) to Topic.Channels fail, because of %v", sp.Value, err)
				return fmt.Errorf("ClusterTopicUpdate json.Unmarshal(value=%s) to Topic.Channels, because of %v", sp.Value, err)
			}
		}
	case "ExitFlag", "exit_flag":
		t.ExitFlag = cast.ToInt32(sp.Value)
	case "Ephemeral", "ephemeral":
		t.Ephemeral = cast.ToBool(sp.Value)
	case "Paused", "paused":
		t.Paused = cast.ToInt32(sp.Value)
	}

	mlog.Infof("ClusterTopicUpdate save channel(%s) to channels", sp.Args[0])
	n.topics.Store(sp.Args[0], t)
	return nil
}

func (n *XMQCluster) ClusterNewChannel(sp setPayload) error {
	if len(sp.Args) != 2 || sp.Args[1] == "" || sp.Args[0] == "" {
		mlog.Errorf("failed: invalid arg %#v", sp)
		return errors.New("invalid arg")
	}
	val, ok := n.topics.Load(sp.Args[0])
	if !ok || val == nil {
		mlog.Errorf("topic(%s) not exist", sp.Args[0])
		if ok && val == nil {
			n.topics.Delete(sp.Args[0])
		}
		return fmt.Errorf("topic(%s) not exist", sp.Args[0])
	}
	t := val.(*Topic)
	c := &Channel{}
	err := json.Unmarshal([]byte(sp.Value), c)
	if err != nil {
		mlog.Errorf("json.Unmarshal(value=%s) to Topic fail, because of %v", sp.Value, err)
		return fmt.Errorf("json.Unmarshal(value=%s) to Topic fail, because of %v", sp.Value, err)
	}
	if c.TopicName == "" || c.TopicName != sp.Args[0] {
		c.TopicName = sp.Args[0]
	}
	if c.Name == "" || c.Name != sp.Args[1] {
		c.Name = sp.Args[1]
	}

	c.clients = make(map[string]Consumer)

	deleteCallback := func(c *Channel) {
		t.DeleteExistingChannel(c.Name)
	}
	c.deleteCallback = deleteCallback
	c.xmqd = n.xmqd

	// create mem-queue only if size > 0 (do not use unbuffered chan)
	if n.xmqd.getOpts().MemQueueSize > 0 {
		c.memoryMsgChan = make(chan *Message, n.xmqd.getOpts().MemQueueSize)
	}
	if len(n.xmqd.getOpts().E2EProcessingLatencyPercentiles) > 0 {
		c.e2eProcessingLatencyStream = quantile.New(
			n.xmqd.getOpts().E2EProcessingLatencyWindowTime,
			n.xmqd.getOpts().E2EProcessingLatencyPercentiles,
		)
	}

	c.initPQ()

	if strings.HasSuffix(c.Name, "#ephemeral") {
		c.Ephemeral = true
		c.backend = newDummyBackendQueue()
	} else {
		// backend names, for uniqueness, automatically include the topic...
		backendName := getBackendName(c.TopicName, c.Name)
		c.backend = diskqueue.New(
			backendName,
			n.xmqd.getOpts().DataPath,
			n.xmqd.getOpts().MaxBytesPerFile,
			int32(minValidMsgLength),
			int32(n.xmqd.getOpts().MaxMsgSize)+minValidMsgLength,
			n.xmqd.getOpts().SyncEvery,
			n.xmqd.getOpts().SyncTimeout,
		)
	}

	c.xmqd.Notify(c, !c.Ephemeral)

	mlog.Infof("save channel(%s) to channels", sp.Args[0])
	t.Lock()
	t.Channels[c.Name] = c
	t.Unlock()
	return nil
}

func (n *XMQCluster) ClusterNewTopic(sp setPayload) error {
	if len(sp.Args) != 1 || sp.Args[0] == "" {
		mlog.Errorf("failed: invalid arg %#v", sp)
		return errors.New("invalid arg")
	}
	deleteCallback := func(t *Topic) {
		n.xmqd.DeleteExistingTopic(t.Name)
	}
	topic := &Topic{}
	err := json.Unmarshal([]byte(sp.Value), topic)
	if err != nil || topic == nil {
		mlog.Errorf("json.Unmarshal(value=%s) to Topic fail, because of %v", sp.Value, err)
		return fmt.Errorf("json.Unmarshal(value=%s) to Topic fail, because of %v", sp.Value, err)
	}
	if topic.Name == "" || topic.Name != sp.Args[0] {
		topic.Name = sp.Args[0]
	}

	if topic.Channels == nil {
		topic.Channels = make(map[string]*Channel)
	}

	if topic.startChan == nil {
		topic.startChan = make(chan int, 1)
	}

	if topic.channelUpdateChan == nil {
		topic.channelUpdateChan = make(chan int)
	}
	if topic.xmqd == nil {
		topic.xmqd = n.xmqd
	}
	if topic.pauseChan == nil {
		topic.pauseChan = make(chan int)
	}
	if topic.deleteCallback == nil {
		topic.deleteCallback = deleteCallback
	}
	if topic.idFactory == nil {
		topic.idFactory = NewGUIDFactory(n.xmqd.getOpts().ID)
	}
	topic.ctx, topic.ctxCancel = context.WithCancel(n.xmqd.ctx)

	// create mem-queue only if size > 0 (do not use unbuffered chan)
	if topic.memoryMsgChan == nil && n.xmqd.getOpts().MemQueueSize > 0 {
		topic.memoryMsgChan = make(chan *Message, n.xmqd.getOpts().MemQueueSize)
	}
	if strings.HasSuffix(topic.Name, "#ephemeral") {
		topic.Ephemeral = true
		topic.backend = newDummyBackendQueue()
	} else {
		topic.backend = diskqueue.New(
			topic.Name,
			n.xmqd.getOpts().DataPath,
			n.xmqd.getOpts().MaxBytesPerFile,
			int32(minValidMsgLength),
			int32(n.xmqd.getOpts().MaxMsgSize)+minValidMsgLength,
			n.xmqd.getOpts().SyncEvery,
			n.xmqd.getOpts().SyncTimeout,
		)
	}

	topic.waitGroup.Wrap(topic.messagePump)

	topic.xmqd.Notify(topic, !topic.Ephemeral)

	mlog.Infof("save topic(%s) to topics", sp.Args[0])
	n.topics.Store(sp.Args[0], topic)
	topic.Start()
	return nil
}

type snapshotNoop struct{}

func (sn snapshotNoop) Persist(_ raft.SnapshotSink) error { return nil }
func (sn snapshotNoop) Release()                          {}

type XMQCluster struct {
	r        *raft.Raft
	clusters map[string]string

	topics sync.Map
	xmqd   *XMQD
}

func NewXMQCluster(xmqd *XMQD) (*XMQCluster, error) {
	if xmqd.getOpts().RaftAddress == "" {
		return nil, errors.New("addr must be provided")
	}
	clu := &XMQCluster{
		clusters: make(map[string]string),
		xmqd:     xmqd,
	}
	fmt.Println("----------------XMQCluster=", clu)
	if len(xmqd.getOpts().ClusterRaftAddresses) > 0 {
		for _, v := range xmqd.getOpts().ClusterRaftAddresses {
			tmplist := strings.Split(v, ",")
			if len(tmplist) != 2 || tmplist[0] == "" || tmplist[1] == "" {
				return nil, errors.New("each servers must have the format nodename,nodeaddr")
			}
			if _, ok := clu.clusters[tmplist[0]]; ok {
				return nil, fmt.Errorf("servers eliment(%s) repeated", v)
			}
			clu.clusters[tmplist[0]] = tmplist[1]
		}
	}
	fmt.Println("----------------", clu.clusters)
	err := os.MkdirAll(xmqd.getOpts().DataPath, os.ModePerm)
	if err != nil {
		return nil, fmt.Errorf("could not create data directory: %s", err)
	}

	store, err := raftboltdb.NewBoltStore(path.Join(xmqd.getOpts().DataPath, "bolt"))
	if err != nil {
		return nil, fmt.Errorf("could not create bolt store: %s", err)
	}

	snapshots, err := raft.NewFileSnapshotStore(path.Join(xmqd.getOpts().DataPath, "snapshot"), 2, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("could not create snapshot store: %s", err)
	}

	tcpAddr, err := net.ResolveTCPAddr("tcp", xmqd.getOpts().RaftAddress)
	if err != nil {
		return nil, fmt.Errorf("could not resolve address: %s", err)
	}

	transport, err := raft.NewTCPTransport(xmqd.getOpts().RaftAddress, tcpAddr, 10, time.Second*10, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("could not create tcp transport: %s", err)
	}

	raftCfg := raft.DefaultConfig()
	raftCfg.LocalID = raft.ServerID(xmqd.getOpts().ID)

	r, err := raft.NewRaft(raftCfg, clu, store, store, snapshots, transport)
	if err != nil {
		return nil, fmt.Errorf("could not create raft instance: %s", err)
	}
	fmt.Println("--------localAddr=", transport.LocalAddr())
	if len(clu.clusters) > 0 {
		tmp := make([]raft.Server, 0)
		for k, v := range clu.clusters {
			tmp = append(tmp, raft.Server{
				ID:      raft.ServerID(k),
				Address: raft.ServerAddress(v),
			})
		}
		r.BootstrapCluster(raft.Configuration{
			Servers: tmp})
	} else {
		// Cluster consists of unjoined leaders. Picking a leader and
		// creating a real cluster is done manually after startup.
		r.BootstrapCluster(raft.Configuration{
			Servers: []raft.Server{
				{
					ID:      raft.ServerID(xmqd.getOpts().ID),
					Address: transport.LocalAddr(),
				},
			},
		})
	}

	clu.r = r
	return clu, nil
}

func (c *XMQCluster) Close() {
	if c.r != nil {
		c.r.Shutdown()
	}
}

// 对follower节点来说，leader会通知它来commit log entry，被commit的log entry需要调用应用层提供的Apply方法来执行日志，这里就是从logEntry拿到具体的数据，然后写入缓存里面即可。
func (n *XMQCluster) Apply(log *raft.Log) interface{} {
	fmt.Println("****************-----", log.Type)
	switch log.Type {
	case raft.LogCommand:
		var sp setPayload
		err := json.Unmarshal(log.Data, &sp)
		if err != nil {
			return fmt.Errorf("could not parse payload: %s", err)
		}
		if sp.Method == "NewTopic" {
			fmt.Println("****************-----", sp)
			return n.ClusterNewTopic(sp)
		} else if sp.Method == "NewChannel" {
			fmt.Println("****************-----", sp)
			return n.ClusterNewChannel(sp)
		} else if sp.Method == "TopicUpdate" {
			fmt.Println("****************-----", sp)
			return n.ClusterTopicUpdate(sp)
		} else if sp.Method == "DeleteChannel" {
			fmt.Println("****************-----", sp)
			return n.ClusterDeleteChannel(sp)
		} else if sp.Method == "DeleteTopic" {
			fmt.Println("****************-----", sp)
			return n.ClusterDeleteTopic(sp)
		}
		// kf.db.Store(sp.Key, sp.Value)
	case raft.LogAddPeerDeprecated:
		fmt.Println("***********LogAddPeerDeprecated")
	case raft.LogRemovePeerDeprecated:
		fmt.Println("***********LogRemovePeerDeprecated")
	case raft.LogConfiguration:
		fmt.Println("***********LogConfiguration")
	default:
		return fmt.Errorf("unknown raft log type: %#v", log.Type)
	}

	return nil
}

func (n *XMQCluster) Snapshot() (raft.FSMSnapshot, error) {
	return snapshotNoop{}, nil
}

// 服务重启的时候，会先读取本地的快照来恢复数据，在FSM里面定义的Restore函数会被调用，这里我们就简单的对数据解析json反序列化然后写入内存即可。至此，我们已经能够正常的保存快照，也能在重启的时候从文件恢复快照数据。
func (n *XMQCluster) Restore(rc io.ReadCloser) error {
	// deleting first isn't really necessary since there's no exposed DELETE operation anyway.
	// so any changes over time will just get naturally overwritten

	decoder := json.NewDecoder(rc)

	for decoder.More() {
		var sp setPayload
		err := decoder.Decode(&sp)
		if err != nil {
			return fmt.Errorf("could not decode payload: %s", err)
		}

		if sp.Method == "NewTopic" {
			fmt.Println("------------------", sp)
			return n.ClusterNewTopic(sp)
		}
	}

	return rc.Close()
}

func (c *XMQCluster) NewChannel(topicname, channelname string) error {
	if topicname == "" {
		log.Printf("invalid arg")
		return fmt.Errorf("invalid arg")
	}
	if c.r.State() != raft.Leader {
		log.Printf("current note not leader")
		return fmt.Errorf("current note not leader")
	}
	payload := &setPayload{
		Method: "NewChannel",
		Args:   []string{topicname, channelname},
	}
	channel := &Channel{
		TopicName: topicname,
		Name:      channelname,
	}
	bindata, _ := json.Marshal(channel)
	payload.Value = string(bindata)
	binpayload, _ := json.Marshal(payload)
	future := c.r.Apply(binpayload, 500*time.Millisecond)
	if err := future.Error(); err != nil {
		log.Printf("Could not write key-value: %s\n", err)
		return fmt.Errorf("could not write key-value: %s", err)
	}

	e := future.Response()
	if e != nil {
		log.Printf("Could not write key-value, application: %s\n", e)
		return fmt.Errorf("could not write key-value, application: %s", e)
	}

	return nil
}

func (c *XMQCluster) NewTopic(topicname string) error {
	if topicname == "" {
		log.Printf("invalid arg")
		return fmt.Errorf("invalid arg")
	}
	if c.r.State() != raft.Leader {
		log.Printf("current note not leader")
		return fmt.Errorf("current note not leader")
	}
	payload := &setPayload{
		Method: "NewTopic",
		Args:   []string{topicname},
	}
	topic := &Topic{
		Name: topicname,
	}
	bindata, _ := json.Marshal(topic)
	payload.Value = string(bindata)
	binpayload, _ := json.Marshal(payload)
	future := c.r.Apply(binpayload, 500*time.Millisecond)
	if err := future.Error(); err != nil {
		log.Printf("Could not write key-value: %s\n", err)
		return fmt.Errorf("could not write key-value: %s", err)
	}

	e := future.Response()
	if e != nil {
		log.Printf("Could not write key-value, application: %s\n", e)
		return fmt.Errorf("could not write key-value, application: %s", e)
	}

	return nil
}

func (c *XMQCluster) TopicUpdate(topicname, field string, val interface{}) error {
	if topicname == "" || field == "" {
		log.Printf("invalid arg")
		return fmt.Errorf("invalid arg")
	}
	if c.r.State() != raft.Leader {
		log.Printf("current note not leader")
		return fmt.Errorf("current note not leader")
	}
	payload := &setPayload{
		Method: "TopicUpdate",
		Args:   []string{topicname, field},
	}
	switch val.(type) {
	case map[string]bool:
		if val != nil {
			bindata, _ := json.Marshal(val)
			payload.Value = string(bindata)
		}
	default:
		payload.Value = cast.ToString(val)
	}

	binpayload, _ := json.Marshal(payload)
	future := c.r.Apply(binpayload, 500*time.Millisecond)
	if err := future.Error(); err != nil {
		log.Printf("Could not write key-value: %s\n", err)
		return fmt.Errorf("could not write key-value: %s", err)
	}

	e := future.Response()
	if e != nil {
		log.Printf("Could not write key-value, application: %s\n", e)
		return fmt.Errorf("could not write key-value, application: %s", e)
	}

	return nil
}

func (c *XMQCluster) DeleteChannel(topicname, channelname string) error {
	if topicname == "" || channelname == "" {
		log.Printf("invalid arg")
		return fmt.Errorf("invalid arg")
	}
	if c.r.State() != raft.Leader {
		log.Printf("current note not leader")
		return fmt.Errorf("current note not leader")
	}
	payload := &setPayload{
		Method: "DeleteChannel",
		Args:   []string{topicname, channelname},
	}

	binpayload, _ := json.Marshal(payload)
	future := c.r.Apply(binpayload, 500*time.Millisecond)
	if err := future.Error(); err != nil {
		log.Printf("Could not write key-value: %s\n", err)
		return fmt.Errorf("could not write key-value: %s", err)
	}

	e := future.Response()
	if e != nil {
		log.Printf("Could not write key-value, application: %s\n", e)
		return fmt.Errorf("could not write key-value, application: %s", e)
	}

	return nil
}

func (c *XMQCluster) DeleteTopic(topicname string) error {
	if topicname == "" {
		log.Printf("invalid arg")
		return fmt.Errorf("invalid arg")
	}
	if c.r.State() != raft.Leader {
		log.Printf("current note not leader")
		return fmt.Errorf("current note not leader")
	}
	payload := &setPayload{
		Method: "DeleteTopic",
		Args:   []string{topicname},
	}

	binpayload, _ := json.Marshal(payload)
	future := c.r.Apply(binpayload, 500*time.Millisecond)
	if err := future.Error(); err != nil {
		log.Printf("Could not write key-value: %s\n", err)
		return fmt.Errorf("could not write key-value: %s", err)
	}

	e := future.Response()
	if e != nil {
		log.Printf("Could not write key-value, application: %s\n", e)
		return fmt.Errorf("could not write key-value, application: %s", e)
	}

	return nil
}

func (c *XMQCluster) Join(nodeid, addr string) error {
	if nodeid == "" || addr == "" {
		log.Printf("[E]Join(%s, %s) failed:invalid arg\n", nodeid, addr)
		return errors.New("invalid arg")
	}
	if nodeid == c.xmqd.getOpts().ID {
		log.Printf("[W]Join(%s, %s) failed:id is current node id\n", nodeid, addr)
		return nil
	}

	if addr == c.xmqd.getOpts().RaftAddress {
		log.Printf("[W]Join(%s, %s) failed:addr is current node addr\n", nodeid, addr)
		return nil
	}
	_, leaderID := c.r.LeaderWithID()

	// log.Printf("[D]-----------raft state=%s\n", c.r.State().String())
	if c.r.State() != raft.Leader {
		if leaderID != "" {
			return nil
		}
		log.Printf("[E]Join(%s, %s) failed:Not the leader\n", nodeid, addr)
		return errors.New("not the leader")
	}
	stats := c.r.Stats()
	if _, ok := stats["latest_configuration"]; !ok || stats["latest_configuration"] == "" {
		log.Printf("[E]Join(%s, %s) failed:raft Stats function return invalid format\n", nodeid, addr)
		return errors.New("raft Stats function return invalid format")
	}
	if !strings.HasPrefix(stats["latest_configuration"], "[") || strings.HasPrefix(stats["latest_configuration"], "]") {
		log.Printf("[E]Join(%s, %s) failed:raft Stats function return latest_configuration(%s) field is invalid\n", nodeid, addr, stats["latest_configuration"])
		return errors.New("raft Stats function return latest_configuration field is invalid")
	}

	idAddressReg := regexp.MustCompile(`ID:[\S]+ Address:[\S]+\}`)
	if idAddressReg == nil {
		log.Printf("[E]Join(%s, %s) failed:regexp.MustCompile(`ID:[\\S]+ Address:[\\S]+\\}`) failed\n", nodeid, addr)
		return errors.New("regexp.MustCompile(`ID:[\\S]+ Address:[\\S]+\\}`) failed")
	}
	idAddresses := idAddressReg.FindAllString(stats["latest_configuration"], -1)
	if len(idAddresses) == 0 {
		log.Printf("[E]Join(%s, %s) failed:idAddressReg.FindAllString=%vis not invalid\n", nodeid, addr, idAddresses)
		return fmt.Errorf("idAddressReg.FindAllString=%v is not invalid", idAddresses)
	}

	for _, v := range idAddresses {
		tmplist := strings.Split(v, " ")
		tmpid := strings.TrimPrefix(tmplist[0], "ID:")
		tmpaddr := strings.TrimPrefix(tmplist[1], "Address:")
		if tmpid == "" || tmpaddr == "" {
			log.Printf("[E]Join(%s, %s) failed:raft Stats function return latest_configuration(%s) field is invalid, not id or address\n", nodeid, addr, stats["latest_configuration"])
			return errors.New("raft Stats function return latest_configuration field is invalid, not id or address")
		}
		if tmpid != nodeid {
			err := c.r.AddVoter(raft.ServerID(nodeid), raft.ServerAddress(addr), 0, 0).Error()
			if err != nil {
				log.Printf("[E]Join(%s, %s) failed:Failed to add follower because of %s\n", nodeid, addr, err)
				return fmt.Errorf("failed to add follower because of %s", err)
			}
		}
	}

	return nil
}

func (n *XMQCluster) ioLoop() {
	var joinServerMap sync.Map
	// for announcements, lookupd determines the host automatically
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			joinServerMap.Range(func(k, v interface{}) bool {
				if n.Join(k.(string), v.(string)) == nil {
					joinServerMap.Delete(k)
				}
				return true
			})

		case val := <-n.xmqd.joinNotifyChan:
			joinServerMap.Store(val.NodeID, val.Addr)
		case <-n.xmqd.ctx.Done():
			goto exit
		}
	}

exit:
	mlog.Infof("LOOKUP: closing")
}

func (n *XMQD) GetTopic(topicName string) *Topic {
	return n.cluster.GetTopic(topicName)
}

// GetTopic performs a thread safe operation
// to return a pointer to a Topic object (potentially new)
func (n *XMQCluster) GetTopic(topicName string) *Topic {
	mlog.Debugf("---topicName=%s", topicName)
	t, ok := n.topics.Load(topicName)
	if ok {
		return t.(*Topic)
	}
	if n.r.State() != raft.Leader {
		mlog.Errorf("current xmqd is not leader, can't create new topic")
		return nil
	}
	err := n.NewTopic(topicName)
	if err != nil {
		return nil
	}

	t, ok = n.topics.Load(topicName)
	if !ok {
		mlog.Errorf("creat new topic(%s) failed", topicName)
		return nil
	}

	if atomic.LoadInt32(&n.xmqd.isLoading) == 1 {
		return t.(*Topic)
	}

	// if using lookupd, make a blocking call to get channels and immediately create them
	// to ensure that all channels receive published messages
	lookupdHTTPAddrs := n.xmqd.lookupdHTTPAddrs()
	if len(lookupdHTTPAddrs) > 0 {
		channelNames, err := n.xmqd.ci.GetLookupdTopicChannels(topicName, lookupdHTTPAddrs)
		if err != nil {
			mlog.Warningf("failed to query xmqlookupd for channels to pre-create for topic %s - %s", topicName, err)
		}
		for _, channelName := range channelNames {
			if strings.HasSuffix(channelName, "#ephemeral") {
				continue // do not create ephemeral channel with no consumer client
			}
			t.(*Topic).GetChannel(channelName)
		}
	} else if len(n.xmqd.getOpts().XMQLookupdGrpcAddresses) > 0 {
		mlog.Errorf("no available xmqlookupd to query for channels to pre-create for topic %s", topicName)
	}

	// now that all channels are added, start topic messagePump
	t.(*Topic).Start()
	return t.(*Topic)
}

// GetExistingTopic gets a topic only if it exists
func (n *XMQCluster) GetExistingTopic(topicName string) (*Topic, error) {
	t, ok := n.topics.Load(topicName)
	// topic, ok := n.topicMap[topicName]
	if !ok {
		return nil, errors.New("topic does not exist")
	}
	return t.(*Topic), nil
}
