package xmqd

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"mlib.com/mlog"
	"mlib.com/xmq/internal/quantile"
	"mlib.com/xmq/internal/util"
)

type Topic struct {
	// 64bit atomic vars need to be first for proper alignment on 32bit platforms
	MessageCount uint64 `json:"message_count"`
	MessageBytes uint64 `json:"message_bytes"`

	sync.RWMutex

	Name              string                `json:"name"`
	Channels          map[string]*Channel   `json:"channels"`
	backend           BackendQueue          `json:"-"`
	memoryMsgChan     chan *Message         `json:"-"`
	startChan         chan int              `json:"-"`
	channelUpdateChan chan int              `json:"-"`
	waitGroup         util.WaitGroupWrapper `json:"-"`
	ExitFlag          int32                 `json:"exit_flag"`
	idFactory         *guidFactory          `json:"-"`

	Ephemeral      bool         `json:"ephemeral"`
	deleteCallback func(*Topic) `json:"-"`
	deleter        sync.Once    `json:"-"`

	Paused    int32    `json:"paused"`
	pauseChan chan int `json:"-"`

	xmqd      *XMQD              `json:"-"`
	ctx       context.Context    `json:"-"`
	ctxCancel context.CancelFunc `json:"-"`
}

func (t *Topic) Start() {
	mlog.Debugf("---topic(%#v) start", t)
	select {
	case t.startChan <- 1:
	default:
	}
}

// Exiting returns a boolean indicating if this topic is closed/exiting
func (t *Topic) Exiting() bool {
	return atomic.LoadInt32(&t.ExitFlag) == 1
}

// GetChannel performs a thread safe operation
// to return a pointer to a Channel object (potentially new)
// for the given Topic
func (t *Topic) GetChannel(channelName string) *Channel {
	channel, isNew := t.getOrCreateChannel(channelName)

	if isNew {
		// update messagePump state
		select {
		case t.channelUpdateChan <- 1:
		case <-t.ctx.Done():
		}
	}

	return channel
}

// this expects the caller to handle locking
func (t *Topic) getOrCreateChannel(channelName string) (*Channel, bool) {
	t.RLock()
	if _, ok := t.Channels[channelName]; !ok {
		t.RUnlock()
		err := t.xmqd.cluster.NewChannel(t.Name, channelName)
		if err != nil {
			mlog.Errorf("NewChannel failed:%v", err)
			return nil, true
		}

		t.RLock()

		if _, ok = t.Channels[channelName]; !ok {
			mlog.Errorf("NewChannel failed:not saved in channels")
			return nil, true
		}
		t.RUnlock()
		return t.Channels[channelName], true
	} else {
		t.RUnlock()
		return t.Channels[channelName], true
	}
}

func (t *Topic) GetExistingChannel(channelName string) (*Channel, error) {
	t.RLock()
	defer t.RUnlock()

	if _, ok := t.Channels[channelName]; !ok {
		return nil, errors.New("channel does not exist")
	}
	return t.Channels[channelName], nil
}

// DeleteExistingChannel removes a channel from the topic only if it exists
func (t *Topic) DeleteExistingChannel(channelName string) error {
	mlog.Infof("TOPIC(%s): deleting channel %s", t.Name, channelName)
	err := t.xmqd.cluster.DeleteChannel(t.Name, channelName)
	if err != nil {
		mlog.Errorf("DeleteChannel failed: %v", err)
		return errors.New("DeleteChannel failed")
	}

	numChannels := len(t.Channels)
	t.Unlock()

	// update messagePump state
	select {
	case t.channelUpdateChan <- 1:
	case <-t.ctx.Done():
	}

	if numChannels == 0 && t.Ephemeral {
		go t.deleter.Do(func() { t.deleteCallback(t) })
	}

	return nil
}

// PutMessage writes a Message to the queue
func (t *Topic) PutMessage(m *Message) error {
	t.RLock()
	defer t.RUnlock()
	if atomic.LoadInt32(&t.ExitFlag) == 1 {
		return errors.New("exiting")
	}
	err := t.put(m)
	if err != nil {
		return err
	}
	atomic.AddUint64(&t.MessageCount, 1)
	t.xmqd.cluster.TopicUpdate(t.Name, "MessageCount", t.MessageCount)

	atomic.AddUint64(&t.MessageBytes, uint64(len(m.Body)))
	t.xmqd.cluster.TopicUpdate(t.Name, "MessageBytes", t.MessageBytes)
	return nil
}

// PutMessages writes multiple Messages to the queue
func (t *Topic) PutMessages(msgs []*Message) error {
	t.RLock()
	defer t.RUnlock()
	if atomic.LoadInt32(&t.ExitFlag) == 1 {
		return errors.New("exiting")
	}

	messageTotalBytes := 0

	for i, m := range msgs {
		err := t.put(m)
		if err != nil {
			atomic.AddUint64(&t.MessageCount, uint64(i))
			t.xmqd.cluster.TopicUpdate(t.Name, "MessageCount", t.MessageCount)
			atomic.AddUint64(&t.MessageBytes, uint64(messageTotalBytes))
			t.xmqd.cluster.TopicUpdate(t.Name, "MessageBytes", t.MessageBytes)
			return err
		}
		messageTotalBytes += len(m.Body)
	}

	atomic.AddUint64(&t.MessageBytes, uint64(messageTotalBytes))
	t.xmqd.cluster.TopicUpdate(t.Name, "MessageBytes", t.MessageBytes)
	atomic.AddUint64(&t.MessageCount, uint64(len(msgs)))
	t.xmqd.cluster.TopicUpdate(t.Name, "MessageCount", t.MessageCount)
	return nil
}

func (t *Topic) put(m *Message) error {
	// If mem-queue-size == 0, avoid memory chan, for more consistent ordering,
	// but try to use memory chan for deferred messages (they lose deferred timer
	// in backend queue) or if topic is ephemeral (there is no backend queue).
	mlog.Debugf("----cap(t.memoryMsgChan)=%d", cap(t.memoryMsgChan))
	if cap(t.memoryMsgChan) > 0 || t.Ephemeral || m.deferred != 0 {
		select {
		case t.memoryMsgChan <- m:
			mlog.Debugf("----topic(%#v) <-- msg(%#v)", t, m)
			return nil
		default:
			break // write to backend
		}
	}
	err := writeMessageToBackend(m, t.backend)
	t.xmqd.SetHealth(err)
	if err != nil {
		mlog.Errorf(
			"TOPIC(%s) ERROR: failed to write message to backend - %s",
			t.Name, err)
		return err
	}
	return nil
}

func (t *Topic) Depth() int64 {
	return int64(len(t.memoryMsgChan)) + t.backend.Depth()
}

// messagePump selects over the in-memory and backend queue and
// writes messages to every channel for this topic
func (t *Topic) messagePump() {
	var msg *Message
	var buf []byte
	var err error
	var chans []*Channel
	var memoryMsgChan chan *Message
	var backendChan <-chan []byte

	// do not pass messages before Start(), but avoid blocking Pause() or GetChannel()
	for {
		select {
		case <-t.channelUpdateChan:
			continue
		case <-t.pauseChan:
			continue
		case <-t.ctx.Done():
			goto exit
		case <-t.startChan:
		}
		break
	}
	t.RLock()
	for _, cn := range t.Channels {
		chans = append(chans, cn)
	}
	t.RUnlock()
	if len(chans) > 0 && !t.IsPaused() {
		memoryMsgChan = t.memoryMsgChan
		backendChan = t.backend.ReadChan()
	}

	// main message loop
	for {
		select {
		case msg = <-memoryMsgChan:
		case buf = <-backendChan:
			msg, err = decodeMessage(buf)
			if err != nil {
				mlog.Errorf("failed to decode message - %s", err)
				continue
			}
		case <-t.channelUpdateChan:
			chans = chans[:0]
			t.RLock()
			for _, cn := range t.Channels {
				chans = append(chans, cn)
			}
			t.RUnlock()
			if len(chans) == 0 || t.IsPaused() {
				memoryMsgChan = nil
				backendChan = nil
			} else {
				memoryMsgChan = t.memoryMsgChan
				backendChan = t.backend.ReadChan()
			}
			continue
		case <-t.pauseChan:
			if len(chans) == 0 || t.IsPaused() {
				memoryMsgChan = nil
				backendChan = nil
			} else {
				memoryMsgChan = t.memoryMsgChan
				backendChan = t.backend.ReadChan()
			}
			continue
		case <-t.ctx.Done():
			goto exit
		}
		mlog.Debugf("------ <--- msg=%#v", msg)
		for i, channel := range chans {
			chanMsg := msg
			mlog.Debugf("-channel(%#v) <--- msg=%#v", channel, msg)
			// copy the message because each channel
			// needs a unique instance but...
			// fastpath to avoid copy if its the first channel
			// (the topic already created the first copy)
			if i > 0 {
				chanMsg = NewMessage(msg.ID, msg.Body)
				chanMsg.Timestamp = msg.Timestamp
				chanMsg.deferred = msg.deferred
			}
			if chanMsg.deferred != 0 {
				channel.PutMessageDeferred(chanMsg, chanMsg.deferred)
				continue
			}
			err := channel.PutMessage(chanMsg)
			if err != nil {
				mlog.Errorf(
					"TOPIC(%s) ERROR: failed to put msg(%s) to channel(%s) - %s",
					t.Name, msg.ID, channel.Name, err)
			}
		}
	}

exit:
	mlog.Infof("TOPIC(%s): closing ... messagePump", t.Name)
}

// Delete empties the topic and all its channels and closes
func (t *Topic) Delete() error {
	return t.exit(true)
}

// Close persists all outstanding topic data and closes all its channels
func (t *Topic) Close() error {
	return t.exit(false)
}

func (t *Topic) exit(deleted bool) error {
	if !atomic.CompareAndSwapInt32(&t.ExitFlag, 0, 1) {
		return errors.New("exiting")
	}
	t.xmqd.cluster.TopicUpdate(t.Name, "ExitFlag", t.ExitFlag)

	if deleted {
		mlog.Infof("TOPIC(%s): deleting", t.Name)

		// since we are explicitly deleting a topic (not just at system exit time)
		// de-register this from the lookupd
		t.xmqd.Notify(t, !t.Ephemeral)
	} else {
		mlog.Infof("TOPIC(%s): closing", t.Name)
	}

	t.ctxCancel()

	// synchronize the close of messagePump()
	t.waitGroup.Wait()

	if deleted {
		t.Lock()
		for cn := range t.Channels {
			t.xmqd.cluster.DeleteChannel(t.Name, cn)
		}
		t.Unlock()

		// empty the queue (deletes the backend files, too)
		t.Empty()
		return t.backend.Delete()
	}

	// close all the channels
	t.RLock()
	for _, cn := range t.Channels {
		err := cn.Close()
		if err != nil {
			// we need to continue regardless of error to close all the channels
			mlog.Errorf("channel(%s) close - %s", cn.Name, err)
		}
	}
	t.RUnlock()

	// write anything leftover to disk
	t.flush()
	return t.backend.Close()
}

func (t *Topic) Empty() error {
	for {
		select {
		case <-t.memoryMsgChan:
		default:
			goto finish
		}
	}

finish:
	return t.backend.Empty()
}

func (t *Topic) flush() error {
	if len(t.memoryMsgChan) > 0 {
		mlog.Infof(
			"TOPIC(%s): flushing %d memory messages to backend",
			t.Name, len(t.memoryMsgChan))
	}

	for {
		select {
		case msg := <-t.memoryMsgChan:
			err := writeMessageToBackend(msg, t.backend)
			if err != nil {
				mlog.Errorf(
					"ERROR: failed to write message to backend - %s", err)
			}
		default:
			goto finish
		}
	}

finish:
	return nil
}

func (t *Topic) AggregateChannelE2eProcessingLatency() *quantile.Quantile {
	var latencyStream *quantile.Quantile
	t.RLock()
	realChannels := make([]*Channel, 0, len(t.Channels))
	for _, cn := range t.Channels {
		realChannels = append(realChannels, cn)
	}
	t.RUnlock()
	for _, c := range realChannels {
		if c.e2eProcessingLatencyStream == nil {
			continue
		}
		if latencyStream == nil {
			latencyStream = quantile.New(
				t.xmqd.getOpts().E2EProcessingLatencyWindowTime,
				t.xmqd.getOpts().E2EProcessingLatencyPercentiles)
		}
		latencyStream.Merge(c.e2eProcessingLatencyStream)
	}
	return latencyStream
}

func (t *Topic) Pause() error {
	return t.doPause(true)
}

func (t *Topic) UnPause() error {
	return t.doPause(false)
}

func (t *Topic) doPause(pause bool) error {
	if pause {
		atomic.StoreInt32(&t.Paused, 1)
	} else {
		atomic.StoreInt32(&t.Paused, 0)
	}
	t.xmqd.cluster.TopicUpdate(t.Name, "Paused", t.Paused)

	select {
	case t.pauseChan <- 1:
	case <-t.ctx.Done():
	}

	return nil
}

func (t *Topic) IsPaused() bool {
	return atomic.LoadInt32(&t.Paused) == 1
}

func (t *Topic) GenerateID() MessageID {
	var i int64 = 0
	for {
		id, err := t.idFactory.NewGUID()
		if err == nil {
			return id.Hex()
		}
		if i%10000 == 0 {
			mlog.Errorf("TOPIC(%s): failed to create guid - %s", t.Name, err)
		}
		time.Sleep(time.Millisecond)
		i++
	}
}
