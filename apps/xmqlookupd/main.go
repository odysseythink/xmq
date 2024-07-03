package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"syscall"

	"github.com/BurntSushi/toml"
	"mlib.com/mlog"
	"mlib.com/mrun"
	"mlib.com/xmq/internal/version"
	"mlib.com/xmq/xmqlookupd"
)

func xmqlookupdFlagSet(opts *xmqlookupd.Options) *flag.FlagSet {
	flagSet := flag.NewFlagSet("xmqlookupd", flag.ExitOnError)

	flagSet.String("config", "", "path to config file")
	flagSet.Bool("version", false, "print version string")

	flagSet.String("log-level", opts.LogLevel, "set log verbosity: debug, info, warn, error, or fatal")
	flagSet.String("log-prefix", "[xmqlookupd] ", "log message prefix")
	flagSet.Bool("verbose", false, "[deprecated] has no effect, use --log-level")

	flagSet.String("grpc-address", opts.GrpcAddress, "<addr>:<port> to listen on for grpc clients")
	flagSet.String("http-address", opts.HTTPAddress, "<addr>:<port> to listen on for HTTP clients")
	flagSet.String("broadcast-address", opts.BroadcastAddress, "address of this lookupd node, (default to the OS hostname)")

	flagSet.Duration("inactive-producer-timeout", opts.InactiveProducerTimeout, "duration of time a producer will remain in the active list since its last ping")
	flagSet.Duration("tombstone-lifetime", opts.TombstoneLifetime, "duration of time a producer will remain tombstoned if registration remains")

	return flagSet
}

type program struct {
	once       sync.Once
	xmqlookupd *xmqlookupd.XMQLookupd
}

func main() {
	prg := &program{}
	if err := mrun.Run(prg, syscall.SIGINT, syscall.SIGTERM); err != nil {
		mlog.Errorf("%s", err)
	}
}

func (p *program) Init(args ...interface{}) error {
	opts := xmqlookupd.NewOptions()

	flagSet := xmqlookupdFlagSet(opts)
	flagSet.Parse(os.Args[1:])

	if flagSet.Lookup("version").Value.(flag.Getter).Get().(bool) {
		fmt.Println(version.String("xmqlookupd"))
		os.Exit(0)
	}

	var cfg config
	configFile := flagSet.Lookup("config").Value.String()
	if configFile != "" {
		_, err := toml.DecodeFile(configFile, &cfg)
		if err != nil {
			mlog.Errorf("failed to load config file %s - %s", configFile, err)
			return fmt.Errorf("failed to load config file %s - %s", configFile, err)
		}
	}
	err := cfg.Validate()
	if err != nil {
		return err
	}

	mrun.OptionsResolve(opts, flagSet, cfg)
	xmqlookupd, err := xmqlookupd.New(opts)
	if err != nil {
		mlog.Errorf("failed to instantiate xmqlookupd:%v", err)
		return fmt.Errorf("failed to instantiate xmqlookupd:%v", err)
	}
	p.xmqlookupd = xmqlookupd

	go func() {
		err := p.xmqlookupd.Main()
		if err != nil {
			p.Destroy()
			os.Exit(1)
		}
	}()

	return nil
}

func (p *program) RunOnce(ctx context.Context) error {
	return nil
}
func (p *program) UserData() interface{} {
	return nil
}

func (p *program) Destroy() {
	p.once.Do(func() {
		p.xmqlookupd.Exit()
	})
}
