// This is a client that writes out to a file, and optionally rolls the file

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mlib.com/go-xmq"
	"mlib.com/mrun"
	"mlib.com/xmq/internal/app"
	"mlib.com/xmq/internal/version"
)

func flagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("xmqd", flag.ExitOnError)

	fs.Bool("version", false, "print version string")
	fs.String("log-level", "info", "set log verbosity: debug, info, warn, error, or fatal")
	fs.String("log-prefix", "[xmq_to_file] ", "log message prefix")

	fs.String("channel", "xmq_to_file", "xmq channel")
	fs.Int("max-in-flight", 200, "max number of messages to allow in flight")

	fs.String("output-dir", "/tmp", "directory to write output files to")
	fs.String("work-dir", "", "directory for in-progress files before moving to output-dir")
	fs.String("datetime-format", "%Y-%m-%d_%H", "strftime compatible format for <DATETIME> in filename format")
	fs.String("filename-format", "<TOPIC>.<HOST><REV>.<DATETIME>.log", "output filename format (<TOPIC>, <HOST>, <PID>, <DATETIME>, <REV> are replaced. <REV> is increased when file already exists)")
	fs.String("host-identifier", "", "value to output in log filename in place of hostname. <SHORT_HOST> and <HOSTNAME> are valid replacement tokens")
	fs.Int("gzip-level", 6, "gzip compression level (1-9, 1=BestSpeed, 9=BestCompression)")
	fs.Bool("gzip", false, "gzip output files.")
	fs.Bool("skip-empty-files", false, "skip writing empty files")
	fs.Duration("topic-refresh", time.Minute, "how frequently the topic list should be refreshed")
	fs.String("topic-pattern", "", "only log topics matching the following pattern")

	fs.Int64("rotate-size", 0, "rotate the file when it grows bigger than `rotate-size` bytes")
	fs.Duration("rotate-interval", 0, "rotate the file every duration")
	fs.Duration("sync-interval", 30*time.Second, "sync file to disk every duration")

	fs.Duration("http-client-connect-timeout", 2*time.Second, "timeout for HTTP connect")
	fs.Duration("http-client-request-timeout", 5*time.Second, "timeout for HTTP request")

	xmqdTCPAddrs := app.StringArray{}
	lookupdHTTPAddrs := app.StringArray{}
	topics := app.StringArray{}
	consumerOpts := app.StringArray{}
	fs.Var(&xmqdTCPAddrs, "xmqd-tcp-address", "xmqd TCP address (may be given multiple times)")
	fs.Var(&lookupdHTTPAddrs, "lookupd-http-address", "lookupd HTTP address (may be given multiple times)")
	fs.Var(&topics, "topic", "xmq topic (may be given multiple times)")
	fs.Var(&consumerOpts, "consumer-opt", "option to passthrough to xmq.Consumer (may be given multiple times")

	return fs
}

func main() {
	fs := flagSet()
	fs.Parse(os.Args[1:])

	if args := fs.Args(); len(args) > 0 {
		log.Fatalf("unknown arguments: %s", args)
	}

	opts := NewOptions()
	mrun.OptionsResolve(opts, fs, nil)

	if fs.Lookup("version").Value.(flag.Getter).Get().(bool) {
		fmt.Printf("xmq_to_file v%s\n", version.Binary)
		return
	}

	if opts.Channel == "" {
		log.Fatal("--channel is required")
	}

	if opts.HTTPClientConnectTimeout <= 0 {
		log.Fatal("--http-client-connect-timeout should be positive")
	}

	if opts.HTTPClientRequestTimeout <= 0 {
		log.Fatal("--http-client-request-timeout should be positive")
	}

	if len(opts.XMQDTCPAddrs) == 0 && len(opts.XMQLookupdHTTPAddrs) == 0 {
		log.Fatal("--xmqd-tcp-address or --lookupd-http-address required.")
	}
	if len(opts.XMQDTCPAddrs) != 0 && len(opts.XMQLookupdHTTPAddrs) != 0 {
		log.Fatal("use --xmqd-tcp-address or --lookupd-http-address not both")
	}

	if opts.GZIPLevel < 1 || opts.GZIPLevel > 9 {
		log.Fatalf("invalid --gzip-level value (%d), should be 1-9", opts.GZIPLevel)
	}

	if len(opts.Topics) == 0 && len(opts.TopicPattern) == 0 {
		log.Fatal("--topic or --topic-pattern required")
	}

	if len(opts.Topics) == 0 && len(opts.XMQLookupdHTTPAddrs) == 0 {
		log.Fatal("--lookupd-http-address must be specified when no --topic specified")
	}

	if opts.WorkDir == "" {
		opts.WorkDir = opts.OutputDir
	}

	cfg := xmq.NewConfig()
	cfgFlag := xmq.ConfigFlag{cfg}
	for _, opt := range opts.ConsumerOpts {
		cfgFlag.Set(opt)
	}
	cfg.UserAgent = fmt.Sprintf("xmq_to_file/%s go-xmq/%s", version.Binary, xmq.VERSION)
	cfg.MaxInFlight = opts.MaxInFlight

	hupChan := make(chan os.Signal, 1)
	termChan := make(chan os.Signal, 1)
	signal.Notify(hupChan, syscall.SIGHUP)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	discoverer := newTopicDiscoverer(opts, cfg, hupChan, termChan)
	discoverer.run()
}
