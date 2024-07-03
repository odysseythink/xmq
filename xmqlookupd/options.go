package xmqlookupd

import (
	"log"
	"os"
	"time"
)

type Options struct {
	LogLevel  string `flag:"log-level"`
	LogPrefix string `flag:"log-prefix"`
	// Logger    Logger

	GrpcAddress      string `flag:"grpc-address"`
	HTTPAddress      string `flag:"http-address"`
	BroadcastAddress string `flag:"broadcast-address"`

	InactiveProducerTimeout time.Duration `flag:"inactive-producer-timeout"`
	TombstoneLifetime       time.Duration `flag:"tombstone-lifetime"`
}

func NewOptions() *Options {
	hostname, err := os.Hostname()
	if err != nil {
		log.Fatal(err)
	}

	return &Options{
		LogPrefix:        "[xmqlookupd] ",
		LogLevel:         "info",
		GrpcAddress:      "0.0.0.0:4160",
		HTTPAddress:      "0.0.0.0:4161",
		BroadcastAddress: hostname,

		InactiveProducerTimeout: 300 * time.Second,
		TombstoneLifetime:       45 * time.Second,
	}
}
