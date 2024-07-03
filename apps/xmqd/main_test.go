package main

import (
	"crypto/tls"
	"os"
	"testing"

	"github.com/BurntSushi/toml"
	"mlib.com/mrun"
	"mlib.com/xmq/xmqd"
)

func TestConfigFlagParsing(t *testing.T) {
	opts := xmqd.NewOptions()

	flagSet := xmqdFlagSet(opts)
	flagSet.Parse([]string{})

	var cfg config
	f, err := os.Open("../../contrib/xmqd.cfg.example")
	if err != nil {
		t.Fatalf("%s", err)
	}
	defer f.Close()
	toml.NewDecoder(f).Decode(&cfg)
	cfg["log_level"] = "debug"
	cfg.Validate()

	mrun.OptionsResolve(opts, flagSet, cfg)
	xmqd.New(opts)

	if opts.TLSMinVersion != tls.VersionTLS10 {
		t.Errorf("min %#v not expected %#v", opts.TLSMinVersion, tls.VersionTLS10)
	}

	t.Fatalf("log level: want debug, got %s", opts.LogLevel)
}
