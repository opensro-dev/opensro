package transport

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// URL paths served by both listeners.
const (
	PathWT       = "/transport/wt"
	PathWS       = "/transport/ws"
	PathCertHash = "/transport/cert-hash"
	PathHealth   = "/transport/healthz"
	PathReady    = "/transport/readyz"
	PathMetrics  = "/transport/metrics"

	httpReadHeaderTimeout = 5 * time.Second
	httpReadTimeout       = 15 * time.Second
	httpWriteTimeout      = 30 * time.Second
	httpIdleTimeout       = 60 * time.Second
	httpMaxHeaderBytes    = 16 << 10

	quicHandshakeIdleTimeout           = 5 * time.Second
	quicInitialStreamReceiveWindow     = 64 << 10
	quicMaxStreamReceiveWindow         = 256 << 10
	quicInitialConnectionReceiveWindow = 128 << 10
	quicMaxConnectionReceiveWindow     = 1 << 20
	quicMaxIncomingStreams             = 8
	quicMaxIncomingUniStreams          = 16
)

// Server runs the game transport: WebTransport over UDP as the primary
// channel and the WebSocket alternate plus bootstrap endpoints over TCP, all
// in this one process. Both feed the same Hub.
type Server struct {
	Hub *Hub
	// Cert is the served TLS pair; nil when public WebTransport is disabled
	// for this run because its pair failed at boot (wtDisabled says why).
	Cert *Certificate

	cfg        Config
	wtDisabled string
	wtServer   *webtransport.Server
	httpServer *http.Server
	// pprofServer is the loopback profiling listener (pprof.go), nil when
	// profiling is off.
	pprofServer *http.Server
	pprofAddr   string

	mu      sync.Mutex
	udpConn *net.UDPConn
	tcpLn   net.Listener
	started bool
	closed  bool

	// wtServeDone closes when the WebTransport Serve goroutine has fully
	// returned. Shutdown MUST wait on it before calling wtServer.Close():
	// webtransport-go v0.11.1 Close() does refCount.Wait() while Serve()
	// does refCount.Add(1) on entry, and a Close overlapping the first
	// Serve is a WaitGroup add-from-zero-vs-wait race (C37).
	wtServeDone chan struct{}
	// httpServeDone joins the TCP serve goroutine during shutdown.
	httpServeDone chan struct{}

	// readyCheck is the injectable half of /transport/readyz: the
	// authority store lives in a package the transport does not import,
	// so main injects its health there via SetReadyCheck. nil (unwired)
	// means readiness is just "listeners up".
	readyMu          sync.RWMutex
	readyCheck       func() error
	publicReferences http.Handler

	errCh chan error
}

// SetReadyCheck injects the external readiness source consulted by
// GET /transport/readyz on top of the listeners-up check. Returning a
// non-nil error reports not-ready with that error's text; a nil function
// (the default) skips the extra check. Safe to call at any time.
func (s *Server) SetReadyCheck(fn func() error) {
	s.readyMu.Lock()
	s.readyCheck = fn
	s.readyMu.Unlock()
}

func (s *Server) readyCheckFn() func() error {
	s.readyMu.RLock()
	defer s.readyMu.RUnlock()
	return s.readyCheck
}

// handleReadyz answers readiness, which is deliberately DISTINCT from
// PathHealth's liveness: healthz says "the process runs" (restart me if
// not), readyz says "I can serve players" (route traffic elsewhere if
// not) — listeners bound and, when a check is injected, the authority
// store not permanently degraded.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if !allowReadMethod(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := s.readinessError(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not ready: " + err.Error()))
		return
	}
	_, _ = w.Write([]byte("ready"))
}

func (s *Server) readinessError() error {
	s.mu.Lock()
	up := s.started && !s.closed
	s.mu.Unlock()
	if !up {
		return fmt.Errorf("listeners not up")
	}
	if fn := s.readyCheckFn(); fn != nil {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) requireReadyConnection(
	w http.ResponseWriter,
) bool {
	if err := s.readinessError(); err != nil {
		http.Error(
			w,
			"transport not ready: "+err.Error(),
			http.StatusServiceUnavailable,
		)
		return false
	}
	return true
}

// NewServerFromViper builds the transport server from viper config, or
// returns (nil, nil) when transport.enabled is false.
func NewServerFromViper() (*Server, error) {
	RegisterViperDefaults()
	if !viper.GetBool(KeyEnabled) {
		log.Info("transport: disabled by config")
		return nil, nil
	}
	return NewServer(ConfigFromViper())
}

// NewServer builds the transport server: it loads the explicit TLS pair or
// manages a development certificate, then prepares both listeners' handlers.
// Nothing binds until Start, so game lanes register on Hub in between.
func NewServer(cfg Config) (*Server, error) {
	cfg.applyDefaults()
	if cfg.HelloTimeout <= 0 || cfg.GracePeriod <= 0 ||
		cfg.KeepaliveInterval <= 0 || cfg.IdleTimeout <= 0 || cfg.EntryBootstrapBound <= 0 ||
		cfg.RingParkBound <= 0 {
		return nil, fmt.Errorf("transport: handshake, grace, keepalive, idle, entry bootstrap and ring park durations must be positive")
	}
	if cfg.OutboundQueue <= 0 || cfg.OutboundQueueBytes <= 0 {
		return nil, fmt.Errorf("transport: outbound queue frame and byte limits must be positive")
	}
	if cfg.RateLimitPerSec <= 0 || cfg.RateLimitBurst <= 0 {
		return nil, fmt.Errorf("transport: rate limits must be positive")
	}
	if cfg.MaxSessions <= 0 {
		return nil, fmt.Errorf("transport: max_sessions must be positive")
	}
	if cfg.MaxSessionsPerAccount <= 0 {
		return nil, fmt.Errorf("transport: max_sessions_per_account must be positive")
	}
	if cfg.MaxPendingHandshakes <= 0 {
		return nil, fmt.Errorf("transport: max_pending_handshakes must be positive")
	}
	wsLoopback := isLoopbackHostPort(cfg.WSAddr)
	if cfg.EnablePprof && !wsLoopback {
		return nil, fmt.Errorf("transport: pprof requires a loopback ws_addr")
	}
	if !wsLoopback && !cfg.PrivateNetwork {
		return nil, fmt.Errorf("transport: non-loopback ws_addr requires an explicit private-network deployment behind a TLS edge")
	}
	if !wsLoopback {
		log.Warnf(
			"transport: private-network plaintext WS/HTTP listener %s; never publish this port directly",
			cfg.WSAddr,
		)
	}
	wtLoopback := isLoopbackHostPort(cfg.WTAddr)
	if !wtLoopback && cfg.CertFile == "" {
		return nil, fmt.Errorf("transport: a non-loopback wt_addr requires an explicit production certificate and key")
	}
	if !wtLoopback && !cfg.WebTransportPublic {
		return nil, fmt.Errorf("transport: a non-loopback wt_addr requires the public WebTransport switch (%s), so a bind change alone cannot expose the UDP listener", EnvWebTransportPublic)
	}
	if cfg.WebTransportPublic {
		if cfg.CertFile == "" || cfg.KeyFile == "" {
			return nil, fmt.Errorf("transport: public WebTransport requires an explicit certificate and key file; the managed development certificate is never served publicly")
		}
		if len(cfg.AllowedOrigins) == 0 {
			return nil, fmt.Errorf("transport: public WebTransport requires allowed origins; the empty-list loopback-only posture is a development default")
		}
	}

	// A public pair that fails at boot (missing, unreadable by this user, or
	// outside its validity) disables only the WebTransport listener:
	// WebSocket keeps serving and clients fall back (owner 2026-10-27,
	// #1447). Deploy-time checks stay fail-closed; this is the boot a crash
	// restart or a renewal that never synced reaches, which must not take
	// WebSocket down with it.
	cert, err := LoadCertificate(cfg.CertFile, cfg.KeyFile, cfg.CertDir)
	wtDisabled := ""
	if err != nil {
		if !cfg.WebTransportPublic {
			return nil, err
		}
		wtDisabled = err.Error()
		log.WithError(err).Error("transport: public WebTransport disabled for this run; WebSocket keeps serving")
		cert = nil
	}
	if cert == nil {
		// Disabled above: nothing to report.
	} else if cert.Regenerated {
		log.WithFields(log.Fields{"notAfter": cert.Leaf.NotAfter, "sha256": cert.SHA256Hex()}).
			Info("transport: generated new dev certificate")
	} else if cfg.CertFile != "" {
		log.WithFields(log.Fields{
			"certFile": cfg.CertFile,
			"notAfter": cert.Leaf.NotAfter,
		}).Info("transport: loaded explicit TLS certificate")
	}

	s := &Server{
		Hub:        newHub(cfg),
		Cert:       cert,
		cfg:        cfg,
		errCh:      make(chan error, 2),
		wtDisabled: wtDisabled,
	}

	checkOrigin := s.originChecker()

	// The TCP side: WebSocket alternate plus the cert-hash and health
	// endpoints. Plain HTTP in dev; production puts real TLS in front.
	// EnableCompression only negotiates permessage-deflate when the
	// peer offers it; which messages actually deflate is decided per
	// message in wsConn.WriteFrame (wsCompressMinBytes, #2311). A
	// peer that never offers it is served exactly as before.
	upgrader := websocket.Upgrader{
		ReadBufferSize:    4096,
		WriteBufferSize:   4096,
		CheckOrigin:       checkOrigin,
		EnableCompression: true,
	}
	tcpMux := http.NewServeMux()
	tcpMux.HandleFunc("/transport/references/", func(w http.ResponseWriter, r *http.Request) {
		s.readyMu.RLock()
		handler := s.publicReferences
		s.readyMu.RUnlock()
		if handler == nil {
			http.NotFound(w, r)
			return
		}
		handler.ServeHTTP(w, r)
	})
	tcpMux.HandleFunc(PathWS, func(w http.ResponseWriter, r *http.Request) {
		if !s.requireReadyConnection(w) {
			return
		}
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return // Upgrade already replied with an error status
		}
		go s.Hub.AcceptConn(newWSConn(c))
	})
	tcpMux.HandleFunc(PathCertHash, s.handleCertHash)
	tcpMux.HandleFunc(PathHealth, func(w http.ResponseWriter, r *http.Request) {
		if !allowReadMethod(w, r) {
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	tcpMux.HandleFunc(PathReady, s.handleReadyz)
	tcpMux.HandleFunc(PathMetrics, func(w http.ResponseWriter, r *http.Request) {
		if !allowReadMethod(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		document := struct {
			Metrics
			CertificateNotAfter         string `json:"certificate_not_after"`
			CertificateSecondsRemaining int64  `json:"certificate_seconds_remaining"`
			// WebTransportDisabled is why this run serves no WebTransport
			// (the pair failed at boot); empty while it serves.
			WebTransportDisabled string `json:"webtransport_disabled,omitempty"`
		}{
			Metrics:              s.Hub.Metrics(),
			WebTransportDisabled: s.wtDisabled,
		}
		if s.Cert != nil {
			remaining := max(time.Until(s.Cert.Leaf.NotAfter), 0)
			document.CertificateNotAfter = s.Cert.Leaf.NotAfter.UTC().Format(time.RFC3339)
			document.CertificateSecondsRemaining = int64(remaining / time.Second)
		}
		_ = json.NewEncoder(w).Encode(document)
	})
	s.httpServer = &http.Server{
		Handler:           tcpMux,
		ReadHeaderTimeout: httpReadHeaderTimeout,
		ReadTimeout:       httpReadTimeout,
		WriteTimeout:      httpWriteTimeout,
		IdleTimeout:       httpIdleTimeout,
		MaxHeaderBytes:    httpMaxHeaderBytes,
	}

	if cert == nil {
		return s, nil // WebTransport disabled: no UDP side (see the load above)
	}

	// The UDP side: HTTP/3 + WebTransport. The h3 ALPN must be set here
	// ourselves: webtransport-go hands this tls.Config straight to
	// quic.ListenEarly without adding it.
	h3 := &http3.Server{
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert.TLS},
			MinVersion:   tls.VersionTLS13,
			NextProtos:   []string{http3.NextProtoH3},
		},
		QUICConfig: &quic.Config{
			HandshakeIdleTimeout:           quicHandshakeIdleTimeout,
			MaxIdleTimeout:                 httpIdleTimeout,
			InitialStreamReceiveWindow:     quicInitialStreamReceiveWindow,
			MaxStreamReceiveWindow:         quicMaxStreamReceiveWindow,
			InitialConnectionReceiveWindow: quicInitialConnectionReceiveWindow,
			MaxConnectionReceiveWindow:     quicMaxConnectionReceiveWindow,
			MaxIncomingStreams:             quicMaxIncomingStreams,
			MaxIncomingUniStreams:          quicMaxIncomingUniStreams,
		},
		MaxHeaderBytes: httpMaxHeaderBytes,
		IdleTimeout:    httpIdleTimeout,
	}
	wtMux := http.NewServeMux()
	h3.Handler = wtMux
	s.wtServer = &webtransport.Server{H3: h3, CheckOrigin: checkOrigin}
	// Installs the ConnContext hook Upgrade needs, datagram support, and the
	// SETTINGS Safari requires.
	webtransport.ConfigureHTTP3Server(h3)
	wtMux.HandleFunc(PathWT, func(w http.ResponseWriter, r *http.Request) {
		if !s.requireReadyConnection(w) {
			return
		}
		sess, err := s.wtServer.Upgrade(w, r)
		if err != nil {
			log.WithField("error", err).Warn("transport: webtransport upgrade rejected")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		go s.Hub.AcceptConn(newWTConn(sess))
	})
	wtMux.HandleFunc(PathCertHash, s.handleCertHash)

	return s, nil
}

func (s *Server) originChecker() func(*http.Request) bool {
	if len(s.cfg.AllowedOrigins) == 0 {
		log.Warn("transport: allowed_origins empty — only LOOPBACK Origins accepted (dev posture); set transport.allowed_origins / TRANSPORT_ALLOWED_ORIGINS for any real deployment")
	}
	return newOriginChecker(s.cfg.AllowedOrigins)
}

// newOriginChecker builds the Origin gate shared by the WS upgrade and the
// WT CONNECT. The posture is fail-closed:
//
//   - Configured allowlist: the list is exhaustive — exact match (scheme,
//     host, port; case-insensitive, trailing slash ignored) or rejected.
//     Loopback is NOT implicitly allowed once a list exists.
//   - Empty allowlist: only loopback Origins (localhost, 127.0.0.1, ::1 —
//     any scheme/port) are accepted, which keeps dev zero-config WITHOUT
//     ever accepting arbitrary sites. A non-loopback deployment with an
//     empty list rejects every browser: misconfiguration fails closed.
//   - Requests with no Origin header pass either way: non-browser clients
//     (tests, tools, server peers) send none, and a browser CSRF cannot
//     strip its own Origin.
func newOriginChecker(allowedOrigins []string) func(*http.Request) bool {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[strings.ToLower(strings.TrimRight(o, "/"))] = struct{}{}
	}
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		if len(allowed) > 0 {
			_, ok := allowed[strings.ToLower(strings.TrimRight(origin, "/"))]
			return ok
		}
		return isLoopbackOrigin(origin)
	}
}

// isLoopbackOrigin reports whether the Origin's host is a loopback address.
func isLoopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// isLoopbackHostPort reports whether a listen address ("host:port") binds
// loopback only. Used solely to pick the pprof exposure log's severity.
func isLoopbackHostPort(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.ToLower(host) == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// handleCertHash serves the serverCertificateHashes bootstrap document. It
// is world-readable on purpose: a cert fingerprint is public information
// and the client fetches it before every connect.
func (s *Server) handleCertHash(w http.ResponseWriter, r *http.Request) {
	if !allowReadMethod(w, r) {
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-store")
	if s.Cert == nil {
		// WebTransport is disabled this run: no hash to pin. The client
		// reads a non-OK answer as "no pins" and still dials; with no UDP
		// listener that dial fails (or hits its 3 s connect deadline) and
		// its one WebSocket fallback runs.
		http.Error(w, "webtransport unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	doc := struct {
		certHashDoc
		WTPath string `json:"wtPath"`
		WSPath string `json:"wsPath"`
		WTAddr string `json:"wtAddr,omitempty"`
		WSAddr string `json:"wsAddr,omitempty"`
	}{
		certHashDoc: certHashDoc{
			Algorithm:    "sha-256",
			SHA256Hex:    s.Cert.SHA256Hex(),
			SHA256Base64: s.Cert.SHA256Base64(),
			NotBefore:    s.Cert.Leaf.NotBefore.UTC().Format("2006-01-02T15:04:05Z07:00"),
			NotAfter:     s.Cert.Leaf.NotAfter.UTC().Format("2006-01-02T15:04:05Z07:00"),
		},
		WTPath: PathWT,
		WSPath: PathWS,
		WTAddr: s.WTAddr(),
		WSAddr: s.WSAddr(),
	}
	_ = json.NewEncoder(w).Encode(doc)
}

// Start binds the UDP and TCP listeners and begins serving. It returns
// immediately; fatal listener errors surface on Err.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return fmt.Errorf("transport: server already started")
	}
	if s.Hub.helloAuthFn() == nil && s.Hub.admission.commitCurrent() == nil {
		return fmt.Errorf("transport: HELLO admission verifier is not installed")
	}
	if s.Hub.enterWorldAuthFn() == nil {
		return fmt.Errorf("transport: EnterWorld authentication verifier is not installed")
	}

	// Each listener bound so far closes when a later bind fails, so a failed
	// Start leaves no port held.
	var bound []io.Closer
	fail := func(err error) error {
		for _, closer := range bound {
			closer.Close()
		}
		return err
	}
	var pprofLn net.Listener
	if s.cfg.EnablePprof {
		addr := s.cfg.PprofAddr
		if addr == "" {
			addr = DefaultPprofAddr
		}
		server, err := newPprofServer(addr)
		if err != nil {
			return err
		}
		if pprofLn, err = net.Listen("tcp", addr); err != nil {
			return fmt.Errorf("transport: binding pprof %s: %w", addr, err)
		}
		bound = append(bound, pprofLn)
		s.pprofServer = server
	}
	tcpLn, err := net.Listen("tcp", s.cfg.WSAddr)
	if err != nil {
		return fail(fmt.Errorf("transport: binding TCP %s: %w", s.cfg.WSAddr, err))
	}
	bound = append(bound, tcpLn)
	// With WebTransport disabled at boot (NewServer), no UDP socket is bound:
	// udpConn stays nil, which Shutdown and WTAddr already read as "no WT".
	var udpConn *net.UDPConn
	if s.Cert != nil {
		udpAddr, err := net.ResolveUDPAddr("udp", s.cfg.WTAddr)
		if err != nil {
			return fail(fmt.Errorf("transport: resolving UDP %s: %w", s.cfg.WTAddr, err))
		}
		if udpConn, err = net.ListenUDP("udp", udpAddr); err != nil {
			return fail(fmt.Errorf("transport: binding UDP %s: %w", s.cfg.WTAddr, err))
		}
	}
	s.tcpLn = tcpLn
	s.udpConn = udpConn
	s.started = true
	s.httpServeDone = make(chan struct{})
	if udpConn != nil {
		s.wtServeDone = make(chan struct{})
	}

	if pprofLn != nil {
		go func() {
			if err := s.pprofServer.Serve(pprofLn); err != nil && err != http.ErrServerClosed {
				log.WithError(err).Warn("transport: pprof listener stopped")
			}
		}()
		s.pprofAddr = pprofLn.Addr().String()
		log.WithField("addr", s.pprofAddr).Info("transport: pprof enabled on its loopback listener (/debug/pprof/)")
	}
	go func() {
		defer close(s.httpServeDone)
		if err := s.httpServer.Serve(tcpLn); err != nil && err != http.ErrServerClosed {
			s.errCh <- fmt.Errorf("transport: websocket listener: %w", err)
		}
	}()
	webtransportField := "disabled: " + s.wtDisabled
	if udpConn != nil {
		go func() {
			defer close(s.wtServeDone)
			if err := s.wtServer.Serve(udpConn); err != nil && !s.isClosed() {
				s.errCh <- fmt.Errorf("transport: webtransport listener: %w", err)
			}
		}()
		webtransportField = "https://" + udpConn.LocalAddr().String() + PathWT + " (UDP)"
	}

	wsAddr := tcpLn.Addr().String()
	log.WithFields(log.Fields{
		"webtransport": webtransportField,
		"websocket":    "ws://" + wsAddr + PathWS + " (TCP)",
		"certHash":     "http://" + wsAddr + PathCertHash,
	}).Info("transport: listening")
	return nil
}

func allowReadMethod(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
}

func (s *Server) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

/*
================
WebTransportDisabled

Why this run serves no WebTransport: the public pair's load error at boot.
Empty while WebTransport serves (or was never public).
================
*/
func (s *Server) WebTransportDisabled() string { return s.wtDisabled }

// WTAddr is the bound UDP address (host:port) once started.
func (s *Server) WTAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.udpConn == nil {
		return ""
	}
	return s.udpConn.LocalAddr().String()
}

// WSAddr is the bound TCP address (host:port) once started.
func (s *Server) WSAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tcpLn == nil {
		return ""
	}
	return s.tcpLn.Addr().String()
}

// Err surfaces fatal listener errors after Start.
func (s *Server) Err() <-chan error { return s.errCh }

// Shutdown says BYE to every session, drains them, and closes both
// listeners. Context bounds network draining; session hooks must finish
// before callers release the gameplay resources those hooks use.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	udpConn := s.udpConn
	wtServeDone := s.wtServeDone
	httpServeDone := s.httpServeDone
	s.mu.Unlock()

	s.Hub.shutdown(ctx)

	// Stop the WT accept loop by closing OUR socket and wait for Serve to
	// return before wtServer.Close(): calling Close concurrently with
	// Serve's entry is a WaitGroup race inside webtransport-go (C37), so
	// the ordering here is load-bearing, not cosmetic.
	var err error
	if udpConn != nil {
		udpConn.Close()
		select {
		case <-wtServeDone:
			err = s.wtServer.Close()
		case <-ctx.Done():
			// Escape hatch: Serve never exited inside the deadline. Skip
			// wtServer.Close rather than race it; process teardown reclaims
			// the rest.
			log.Warn("transport: shutdown deadline hit before WT serve loop exited; skipping wtServer.Close")
			err = ctx.Err()
		}
	}
	if s.pprofServer != nil {
		_ = s.pprofServer.Close()
	}
	if httpError := s.httpServer.Shutdown(ctx); httpError != nil {
		_ = s.httpServer.Close()
		if err == nil {
			err = httpError
		}
	}
	if httpServeDone != nil {
		select {
		case <-httpServeDone:
		case <-ctx.Done():
			if err == nil {
				err = ctx.Err()
			}
		}
	}
	return err
}

/*
================
Server.PprofAddr

The bound profiling listener, or "" when profiling is off.
================
*/
func (s *Server) PprofAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pprofAddr
}
