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
	"mlib.com/xmq/xmqd"
)

type program struct {
	once sync.Once
	xmqd *xmqd.XMQD
}

func main() {
	prg := &program{}
	opts := xmqd.NewOptions()

	flagSet := xmqdFlagSet(opts)
	flagSet.Parse(os.Args[1:])

	if flagSet.Lookup("version").Value.(flag.Getter).Get().(bool) {
		mlog.Debugf(version.String("xmqd"))
		os.Exit(0)
	}

	var cfg config
	configFile := flagSet.Lookup("config").Value.String()
	if configFile != "" {
		_, err := toml.DecodeFile(configFile, &cfg)
		if err != nil {
			mlog.Errorf("failed to load config file %s - %v", configFile, err)
			os.Exit(-1)
			// return fmt.Errorf("failed to load config file %s - %s", configFile, err)
		}
	}
	err := cfg.Validate()
	if err != nil {
		mlog.Errorf("%v", err)
		os.Exit(-1)
		// return err
	}

	mrun.OptionsResolve(opts, flagSet, cfg)

	xmqd, err := xmqd.New(opts)
	if err != nil {
		mlog.Errorf("failed to instantiate xmqd - %v", err)
		os.Exit(-2)
		// return fmt.Errorf("failed to instantiate xmqd - %s", err)
	}
	prg.xmqd = xmqd

	if err := mrun.Run(prg, syscall.SIGINT, syscall.SIGTERM); err != nil {
		mlog.Errorf("%s", err)
	}
}

func (p *program) Init(args ...interface{}) error {
	err := p.xmqd.LoadMetadata()
	if err != nil {
		mlog.Errorf("failed to load metadata - %s", err)
		return fmt.Errorf("failed to load metadata - %s", err)
	}
	err = p.xmqd.PersistMetadata()
	if err != nil {
		mlog.Errorf("failed to persist metadata - %s", err)
		return fmt.Errorf("failed to persist metadata - %s", err)
	}

	go func() {
		err := p.xmqd.Main()
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
		p.xmqd.Exit()
	})
}

// Context returns a context that will be canceled when xmqd initiates the shutdown
func (p *program) Context() context.Context {
	return p.xmqd.Context()
}
