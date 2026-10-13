/*
===========================================================================

pprof.go - the GameWorld's profiling listener, loopback only

Profiles answer "where did this tick's 120 ms go" on production, where no
debugger reaches. They ride their own listener, never the transport mux a
reverse proxy forwards to players: a proxy route widened later can then
never publish /debug/pprof/. The address must be a loopback IP, checked
before binding. Only the on-request handlers are served (CPU profile,
execution trace, heap, goroutine and the index); the mutex and block
profilers stay off, their sampling rates untouched.

===========================================================================
*/

package transport

import (
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"time"
)

// DefaultPprofAddr is the profiling listener when none is configured.
const DefaultPprofAddr = "127.0.0.1:8792"

// pprofReadHeaderTimeout bounds a stalled client; a 60 s CPU profile is the
// longest answer, so writes are not bounded.
const pprofReadHeaderTimeout = 5 * time.Second

/*
================
loopbackAddr

Whether addr is host:port with a loopback IP host. "localhost" is refused:
a resolver could map it elsewhere.
================
*/
func loopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

/*
================
newPprofServer

The profiling server for addr, or an error when addr is not loopback.
Handlers register on their own mux; http.DefaultServeMux, where the
net/http/pprof import also registers, is never served.
================
*/
func newPprofServer(addr string) (*http.Server, error) {
	if !loopbackAddr(addr) {
		return nil, fmt.Errorf("transport: pprof address %q is not a loopback host:port", addr)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: pprofReadHeaderTimeout}, nil
}
