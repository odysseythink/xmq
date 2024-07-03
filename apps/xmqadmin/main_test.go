package main

import (
	"testing"

	"mlib.com/mrun"
	"mlib.com/xmq/xmqadmin"
)

func TestConfigFlagParsing(t *testing.T) {
	opts := xmqadmin.NewOptions()

	flagSet := xmqadminFlagSet(opts)
	flagSet.Parse([]string{})

	cfg := config{"log_level": "debug"}
	cfg.Validate()

	mrun.OptionsResolve(opts, flagSet, cfg)
	t.Fatalf("log level: want debug, got %s", opts.LogLevel)
}
