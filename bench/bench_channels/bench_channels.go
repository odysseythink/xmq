package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"sync"
	"time"

	"mlib.com/go-xmq"
)

var (
	num        = flag.Int("num", 10000, "num channels")
	tcpAddress = flag.String("xmqd-tcp-address", "127.0.0.1:4150", "<addr>:<port> to connect to xmqd")
)

func main() {
	flag.Parse()
	var wg sync.WaitGroup

	goChan := make(chan int)
	rdyChan := make(chan int)
	for j := 0; j < *num; j++ {
		wg.Add(1)
		go func(id int) {
			subWorker(*num, *tcpAddress, fmt.Sprintf("t%d", j), "ch", rdyChan, goChan, id)
			wg.Done()
		}(j)
		<-rdyChan
		time.Sleep(5 * time.Millisecond)
	}

	close(goChan)
	wg.Wait()
}

func subWorker(n int, tcpAddr string,
	topic string, channel string,
	rdyChan chan int, goChan chan int, id int) {
	conn, err := net.DialTimeout("tcp", tcpAddr, time.Second)
	if err != nil {
		panic(err.Error())
	}
	conn.Write(xmq.MagicV2)
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	ci := make(map[string]interface{})
	ci["client_id"] = "test"
	cmd, _ := xmq.Identify(ci)
	cmd.WriteTo(rw)
	xmq.Subscribe(topic, channel).WriteTo(rw)
	rdyCount := 1
	rdy := rdyCount
	rdyChan <- 1
	<-goChan
	xmq.Ready(rdyCount).WriteTo(rw)
	rw.Flush()
	xmq.ReadResponse(rw)
	xmq.ReadResponse(rw)
	for {
		resp, err := xmq.ReadResponse(rw)
		if err != nil {
			panic(err.Error())
		}
		frameType, data, err := xmq.UnpackResponse(resp)
		if err != nil {
			panic(err.Error())
		}
		if frameType == xmq.FrameTypeError {
			panic(string(data))
		} else if frameType == xmq.FrameTypeResponse {
			xmq.Nop().WriteTo(rw)
			rw.Flush()
			continue
		}
		msg, err := xmq.DecodeMessage(data)
		if err != nil {
			panic(err.Error())
		}
		xmq.Finish(msg.ID).WriteTo(rw)
		rdy--
		if rdy == 0 {
			xmq.Ready(rdyCount).WriteTo(rw)
			rdy = rdyCount
			rw.Flush()
		}
	}
}
