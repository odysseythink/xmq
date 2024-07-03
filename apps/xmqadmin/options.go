package main

import (
	"fmt"

	"mlib.com/mlog"
)

type config map[string]interface{}

// Validate settings in the config file, and fatal on errors
func (cfg config) Validate() error {
	if v, exists := cfg["log_level"]; exists {
		if _, ok := v.(string); !ok {
			mlog.Errorf("invalid log_level type(%#v)", v)
			return fmt.Errorf("invalid log_level type(%#v)", v)
		}
		switch v.(string) {
		case "debug":
			mlog.SetLogLevel(0)
		case "info":
			mlog.SetLogLevel(1)
		case "warning":
			mlog.SetLogLevel(2)
		case "error":
			mlog.SetLogLevel(3)
		case "fatal":
			mlog.SetLogLevel(4)
		default:
			mlog.Errorf("unsurport log_level(%s)", v.(string))
			return fmt.Errorf("unsurport log_level(%s)", v.(string))
		}
	}
	return nil
}
