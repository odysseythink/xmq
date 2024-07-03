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
	"mlib.com/xmq/internal/app"
	"mlib.com/xmq/internal/version"
	"mlib.com/xmq/xmqadmin"
)

func xmqadminFlagSet(opts *xmqadmin.Options) *flag.FlagSet {
	flagSet := flag.NewFlagSet("xmqadmin", flag.ExitOnError)

	flagSet.String("config", "", "path to config file")
	flagSet.Bool("version", false, "print version string")

	flagSet.String("log-level", opts.LogLevel, "set log verbosity: debug, info, warn, error, or fatal")
	flagSet.String("log-prefix", "[xmqadmin] ", "log message prefix")
	flagSet.Bool("verbose", false, "[deprecated] has no effect, use --log-level")

	flagSet.String("http-address", opts.HTTPAddress, "<addr>:<port> to listen on for HTTP clients")
	flagSet.String("base-path", opts.BasePath, "URL base path")
	flagSet.String("dev-static-dir", opts.DevStaticDir, "(development use only)")

	flagSet.String("graphite-url", opts.GraphiteURL, "graphite HTTP address")
	flagSet.Bool("proxy-graphite", false, "proxy HTTP requests to graphite")

	flagSet.String("statsd-counter-format", opts.StatsdCounterFormat, "The counter stats key formatting applied by the implementation of statsd. If no formatting is desired, set this to an empty string.")
	flagSet.String("statsd-gauge-format", opts.StatsdGaugeFormat, "The gauge stats key formatting applied by the implementation of statsd. If no formatting is desired, set this to an empty string.")
	flagSet.String("statsd-prefix", opts.StatsdPrefix, "prefix used for keys sent to statsd (%s for host replacement, must match xmqd)")
	flagSet.Duration("statsd-interval", opts.StatsdInterval, "time interval xmqd is configured to push to statsd (must match xmqd)")

	flagSet.String("notification-http-endpoint", "", "HTTP endpoint (fully qualified) to which POST notifications of admin actions will be sent")

	flagSet.Duration("http-client-connect-timeout", opts.HTTPClientConnectTimeout, "timeout for HTTP connect")
	flagSet.Duration("http-client-request-timeout", opts.HTTPClientRequestTimeout, "timeout for HTTP request")

	flagSet.Bool("http-client-tls-insecure-skip-verify", false, "configure the HTTP client to skip verification of TLS certificates")
	flagSet.String("http-client-tls-root-ca-file", "", "path to CA file for the HTTP client")
	flagSet.String("http-client-tls-cert", "", "path to certificate file for the HTTP client")
	flagSet.String("http-client-tls-key", "", "path to key file for the HTTP client")

	flagSet.String("allow-config-from-cidr", opts.AllowConfigFromCIDR, "A CIDR from which to allow HTTP requests to the /config endpoint")
	flagSet.String("acl-http-header", opts.ACLHTTPHeader, "HTTP header to check for authenticated admin users")

	xmqlookupdHTTPAddresses := app.StringArray{}
	flagSet.Var(&xmqlookupdHTTPAddresses, "lookupd-http-address", "lookupd HTTP address (may be given multiple times)")
	xmqdHTTPAddresses := app.StringArray{}
	flagSet.Var(&xmqdHTTPAddresses, "xmqd-http-address", "xmqd HTTP address (may be given multiple times)")
	adminUsers := app.StringArray{}
	flagSet.Var(&adminUsers, "admin-user", "admin user (may be given multiple times; if specified, only these users will be able to perform privileged actions; acl-http-header is used to determine the authenticated user)")

	return flagSet
}

type program struct {
	once     sync.Once
	xmqadmin *xmqadmin.XMQAdmin
}

func main() {
	prg := &program{}
	if err := mrun.Run(prg, syscall.SIGINT, syscall.SIGTERM); err != nil {
		mlog.Errorf("%s", err)
	}
}

func (p *program) Init(args ...interface{}) error {
	opts := xmqadmin.NewOptions()

	flagSet := xmqadminFlagSet(opts)
	flagSet.Parse(os.Args[1:])

	if flagSet.Lookup("version").Value.(flag.Getter).Get().(bool) {
		fmt.Println(version.String("xmqadmin"))
		os.Exit(0)
	}

	var cfg config
	configFile := flagSet.Lookup("config").Value.String()
	if configFile != "" {
		_, err := toml.DecodeFile(configFile, &cfg)
		if err != nil {
			mlog.Errorf("[xmqadmin] failed to load config file %s - %s", configFile, err)
			return fmt.Errorf("[xmqadmin] failed to load config file %s - %s", configFile, err)
		}
	}
	err := cfg.Validate()
	if err != nil {
		return err
	}

	mrun.OptionsResolve(opts, flagSet, cfg)
	xmqadmin, err := xmqadmin.New(opts)
	if err != nil {
		mlog.Errorf("[xmqadmin] failed to instantiate xmqadmin - %s", err)
		return fmt.Errorf("[xmqadmin] failed to instantiate xmqadmin - %s", err)
	}
	p.xmqadmin = xmqadmin

	go func() {
		err := p.xmqadmin.Main()
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
		p.xmqadmin.Exit()
	})
	return
}
