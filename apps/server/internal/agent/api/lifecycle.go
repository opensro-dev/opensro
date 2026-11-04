/*
===========================================================================

lifecycle.go - the Agent control listener's life: start, shutdown

Start binds synchronously and retains the server for Shutdown, so a
successful return owns the listener. The address policy is checked
before any socket is bound (checkControlListenAddress).

===========================================================================
*/
package agentapi

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// httpLifecycle owns the listener reference used by the process shutdown path.
// Serve runs on a service goroutine while Shutdown runs from main.
type httpLifecycle struct {
	mu     sync.Mutex
	server *http.Server
	done   chan struct{}
}

const (
	agentReadHeaderTimeout = 5 * time.Second
	agentReadTimeout       = 15 * time.Second
	agentWriteTimeout      = 30 * time.Second
	agentIdleTimeout       = 60 * time.Second
	agentMaxHeaderBytes    = 16 << 10
)

/*
================
API.Start

Binds synchronously, retains the server for Shutdown, and only then
launches its serve goroutine. A successful return therefore closes the
startup/shutdown race: the listener is already owned by this API.
================
*/
func (api *API) Start(addr string) (<-chan error, error) {
	if addr == "" {
		addr = os.Getenv(EnvAddr)
	}
	if addr == "" {
		addr = DefaultAddr
	}
	if err := checkControlListenAddress(addr, api.privateNetwork); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("agentapi: listen %s: %w", addr, err)
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: agentReadHeaderTimeout,
		ReadTimeout:       agentReadTimeout,
		WriteTimeout:      agentWriteTimeout,
		IdleTimeout:       agentIdleTimeout,
		MaxHeaderBytes:    agentMaxHeaderBytes,
	}
	if !api.listener.retain(server) {
		_ = listener.Close()
		return nil, fmt.Errorf("agentapi: server already started")
	}

	log.Infof("agentapi: serving the GameWorld control surface on http://%s", listener.Addr())
	serveError := make(chan error, 1)
	go func() {
		defer api.listener.markDone()
		serveError <- server.Serve(listener)
	}()
	return serveError, nil
}

/*
================
checkControlListenAddress

The control listener's address policy, applied before any socket is
bound: loopback always serves. A non-loopback address serves only in
an explicit private-network deployment, and taking one is always
logged, because the control surface is plaintext.
================
*/
func checkControlListenAddress(addr string, privateNetwork bool) error {
	if isLoopbackAddress(addr) {
		return nil
	}
	if !privateNetwork {
		return fmt.Errorf("agentapi: non-loopback control listen requires an explicit private-network deployment")
	}
	log.Warnf(
		"agentapi: private-network control listener %s; never publish this plaintext port",
		addr,
	)
	return nil
}

/*
================
isLoopbackAddress
================
*/
func isLoopbackAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Shutdown stops the retained listener gracefully, bounded by ctx. A server
// that never started is a no-op because no HTTP writes can be in flight.
func (api *API) Shutdown(ctx context.Context) error {
	server, done := api.listener.current()
	if server == nil {
		return nil
	}
	shutdownError := server.Shutdown(ctx)
	if shutdownError != nil {
		_ = server.Close()
	}
	if done == nil {
		return shutdownError
	}
	select {
	case <-done:
		return shutdownError
	case <-ctx.Done():
		if shutdownError != nil {
			return shutdownError
		}
		return ctx.Err()
	}
}

func (lifecycle *httpLifecycle) retain(server *http.Server) bool {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	if lifecycle.server != nil {
		return false
	}
	lifecycle.server = server
	lifecycle.done = make(chan struct{})
	return true
}

func (lifecycle *httpLifecycle) current() (*http.Server, <-chan struct{}) {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	return lifecycle.server, lifecycle.done
}

func (lifecycle *httpLifecycle) markDone() {
	lifecycle.mu.Lock()
	done := lifecycle.done
	lifecycle.mu.Unlock()
	if done != nil {
		close(done)
	}
}
