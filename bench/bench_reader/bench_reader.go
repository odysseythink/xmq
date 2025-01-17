package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"mlib.com/go-xmq"
)

var (
	runfor     = flag.Duration("runfor", 10*time.Second, "duration of time to run")
	tcpAddress = flag.String("xmqd-tcp-address", "127.0.0.1:4150", "<addr>:<port> to connect to xmqd")
	size       = flag.Int("size", 200, "size of messages")
	topic      = flag.String("topic", "sub_bench", "topic to receive messages on")
	channel    = flag.String("channel", "ch", "channel to receive messages on")
	deadline   = flag.String("deadline", "", "deadline to start the benchmark run")
	rdy        = flag.Int("rdy", 2500, "RDY count to use")
)

var totalMsgCount int64

func main() {
	flag.Parse()
	var wg sync.WaitGroup

	log.SetPrefix("[bench_reader] ")

	goChan := make(chan int)
	rdyChan := make(chan int)
	workers := runtime.GOMAXPROCS(0)
	for j := 0; j < workers; j++ {
		wg.Add(1)
		go func(id int) {
			subWorker(*runfor, workers, *tcpAddress, *topic, *channel, rdyChan, goChan, id)
			wg.Done()
		}(j)
		<-rdyChan
	}

	if *deadline != "" {
		t, err := time.Parse("2006-01-02 15:04:05", *deadline)
		if err != nil {
			log.Fatal(err)
		}
		d := time.Until(t)
		log.Printf("sleeping until %s (%s)", t, d)
		time.Sleep(d)
	}

	start := time.Now()
	close(goChan)
	wg.Wait()
	end := time.Now()
	duration := end.Sub(start)
	tmc := atomic.LoadInt64(&totalMsgCount)
	log.Printf("duration: %s - %.03fmb/s - %.03fops/s - %.03fus/op",
		duration,
		float64(tmc*int64(*size))/duration.Seconds()/1024/1024,
		float64(tmc)/duration.Seconds(),
		float64(duration/time.Microsecond)/float64(tmc))
}

func subWorker(td time.Duration, workers int, tcpAddr string, topic string, channel string, rdyChan chan int, goChan chan int, id int) {
	conn, err := net.DialTimeout("tcp", tcpAddr, time.Second)
	if err != nil {
		panic(err.Error())
	}
	conn.Write(xmq.MagicV2)
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	ci := make(map[string]interface{})
	ci["client_id"] = "reader"
	ci["hostname"] = "reader"
	ci["user_agent"] = fmt.Sprintf("bench_reader/%s", xmq.VERSION)
	cmd, _ := xmq.Identify(ci)
	cmd.WriteTo(rw)
	xmq.Subscribe(topic, channel).WriteTo(rw)
	rdyChan <- 1
	<-goChan
	xmq.Ready(*rdy).WriteTo(rw)
	rw.Flush()
	xmq.ReadResponse(rw)
	xmq.ReadResponse(rw)
	var msgCount int64
	go func() {
		time.Sleep(td)
		conn.Close()
	}()
	for {
		resp, err := xmq.ReadResponse(rw)
		if err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				break
			}
			panic(err.Error())
		}
		frameType, data, err := xmq.UnpackResponse(resp)
		if err != nil {
			panic(err.Error())
		}
		if frameType == xmq.FrameTypeError {
			panic(string(data))
		} else if frameType == xmq.FrameTypeResponse {
			continue
		}
		msg, err := xmq.DecodeMessage(data)
		if err != nil {
			panic(err.Error())
		}
		xmq.Finish(msg.ID).WriteTo(rw)
		msgCount++
		if float64(msgCount%int64(*rdy)) > float64(*rdy)*0.75 {
			rw.Flush()
		}
	}
	atomic.AddInt64(&totalMsgCount, msgCount)
}
