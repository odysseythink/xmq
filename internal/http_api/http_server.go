package http_api

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	"mlib.com/mlog"
)

type logWriter struct {
}

func (l logWriter) Write(p []byte) (int, error) {
	mlog.Warningf("%s", string(p))
	return len(p), nil
}

func Serve(listener net.Listener, handler http.Handler, proto string) error {
	mlog.Infof("%s: listening on %s", proto, listener.Addr())

	server := &http.Server{
		Handler:  handler,
		ErrorLog: log.New(logWriter{}, "", 0),
	}
	err := server.Serve(listener)
	// theres no direct way to detect this error because it is not exposed
	if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
		return fmt.Errorf("http.Serve() error - %s", err)
	}

	mlog.Infof("%s: closing %s", proto, listener.Addr())

	return nil
}
