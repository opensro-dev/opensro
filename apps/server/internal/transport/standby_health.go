/*
===========================================================================

standby_health.go - the standby-health listener and its standby answer

The canary handover's second listener (opensro-ops#281): healthz and
standbyz, and nothing a player could use, on the address the deployment
assigns, bound before any public port exists and closed by Shutdown with
the rest. server.go owns the public listeners and the boot phase this
file's answers name.

===========================================================================
*/
package transport

import (
	"fmt"
	"net"
	"net/http"

	log "github.com/sirupsen/logrus"
)

/*
================
Server.handleStandbyz

The canary handover's deployment gate (opensro-ops#281): 200 once the
boot's save-free load is done - the standby phase and every phase after
it, so the answer never flaps while the process takes over or serves -
and 503 while that load runs (or before any phase was named). Nomad
promotes the canary on this answer, so it must be up in standby and in
service alike; the standby-health listener serves it before any public
port exists.
================
*/
func (s *Server) handleStandbyz(w http.ResponseWriter, r *http.Request) {
	if !allowReadMethod(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	phase := s.BootPhase()
	if phase == "" || phase == BootPhaseLoadingData {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not standby: " + phase))
		return
	}
	_, _ = w.Write([]byte("ok " + phase))
}

/*
================
StartStandbyHealth

Bind the standby-health listener (opensro-ops#281): healthz and
standbyz, and nothing a player could use, at the address the caller
names - in deployment the Nomad-assigned health port, so a standby and
the live allocation never collide while the public transport port
stays unbound until the guard holder's Start. The listener serves for
the process's whole life, because the deployment's standby check must
keep passing while the allocation serves, and Shutdown closes it with
the rest.
================
*/
func (s *Server) StartStandbyHealth(addr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("transport: server is shut down")
	}
	if s.standbyLn != nil {
		return fmt.Errorf("transport: standby health listener already started")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("transport: binding standby health %s: %w", addr, err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc(PathHealth, s.handleHealthz)
	mux.HandleFunc(PathStandby, s.handleStandbyz)
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: httpReadHeaderTimeout,
		ReadTimeout:       httpReadTimeout,
		WriteTimeout:      httpWriteTimeout,
		IdleTimeout:       httpIdleTimeout,
		MaxHeaderBytes:    httpMaxHeaderBytes,
	}
	s.standbyLn = ln
	s.standbyServer = server
	s.standbyDone = make(chan struct{})
	go func() {
		defer close(s.standbyDone)
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.errCh <- fmt.Errorf("transport: standby health listener: %w", err)
		}
	}()
	log.Infof("transport: standby health listener up at http://%s%s (boot phase %q)", ln.Addr(), PathHealth, s.BootPhase())
	return nil
}

/*
================
StandbyHealthAddr

The standby-health listener's bound address, or "" before it starts.
================
*/
func (s *Server) StandbyHealthAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.standbyLn == nil {
		return ""
	}
	return s.standbyLn.Addr().String()
}
