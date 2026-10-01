/*
===========================================================================

server.go - the global Agent/Login control plane

Owns accounts and shard routing, but no character or world state.

===========================================================================
*/
// Package agentserver implements the global Agent/Login control plane.
//
// It owns accounts and shard routing, but no character or world state.
package agentserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"opensro.online/server/internal/releaseprotocol"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	agentapi "opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/agent/bugreport"
	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/platform/readiness"
	"opensro.online/server/internal/security/auth"
	"opensro.online/server/internal/security/workload"
)

const (
	maxRequestBytes  int64 = 64 << 10
	maxResponseBytes int64 = 2 << 20
	maxPasswordBytes       = 72
)

/*
================
AccountAuthority

What the Agent reads from the account authority: the live store
(auth.Accounts) in service, a fixed catalog in tests.
================
*/
type AccountAuthority interface {
	// Credential resolves the id typed at login (ASCII case ignored) to the
	// stored id and its hash; PasswordHash checks a stored id exactly.
	Credential(typedID string) (string, []byte, bool)
	PasswordHash(accountID string) ([]byte, bool)
	IDs() []string
	Len() int
}

/*
================
Config
================
*/
type Config struct {
	Accounts                AccountAuthority
	Catalog                 *shard.Catalog
	Directory               *shard.Directory
	SessionSigner           *auth.AgentSessionSigner
	ControlIdentityVerifier workload.IdentityVerifier
	ControlNamespace        string
	AllowedOrigins          []string
	HTTPClient              *http.Client
	Now                     func() time.Time
	Readiness               *readiness.Gate
	// BugReports is nil when the operator has not configured bug reports.
	BugReports *bugreport.Service
}

/*
================
Server
================
*/
type Server struct {
	accounts                AccountAuthority
	catalog                 *shard.Catalog
	directory               *shard.Directory
	sessionSigner           *auth.AgentSessionSigner
	controlIdentityVerifier workload.IdentityVerifier
	controlNamespace        string
	allowedOrigins          map[string]bool
	client                  *http.Client
	now                     func() time.Time
	dummyHash               []byte
	passwordSlots           chan struct{}
	loginAttempts           *loginLimiter
	passwordFailures        passwordFailures
	readiness               *readiness.Gate
	bugReports              *bugreport.Service
}

/*
================
New
================
*/
func New(config Config) (*Server, error) {
	if config.Accounts == nil {
		return nil, fmt.Errorf("agent: global account authority is required")
	}
	if config.Catalog == nil || config.Directory == nil {
		return nil, fmt.Errorf("agent: shard catalog and directory are required")
	}
	if config.Readiness == nil {
		return nil, fmt.Errorf("agent: readiness gate is required")
	}
	if config.SessionSigner == nil {
		return nil, fmt.Errorf("agent: session signer is required")
	}
	if config.ControlIdentityVerifier == nil {
		return nil, fmt.Errorf("agent: control identity verifier is required")
	}
	if strings.TrimSpace(config.ControlNamespace) == "" {
		return nil, fmt.Errorf("agent: control identity namespace is required")
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	dummyHash, err := bcrypt.GenerateFromPassword(
		[]byte("agent-invalid-account"),
		bcrypt.DefaultCost,
	)
	if err != nil {
		return nil, fmt.Errorf("agent: preparing login refusal: %w", err)
	}
	origins := make(map[string]bool, len(config.AllowedOrigins))
	for _, origin := range config.AllowedOrigins {
		origin = strings.TrimSuffix(strings.TrimSpace(origin), "/")
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
			return nil, fmt.Errorf("agent: invalid allowed origin %q", origin)
		}
		origins[origin] = true
	}
	return &Server{
		accounts:                config.Accounts,
		catalog:                 config.Catalog,
		directory:               config.Directory,
		sessionSigner:           config.SessionSigner,
		controlIdentityVerifier: config.ControlIdentityVerifier,
		controlNamespace:        config.ControlNamespace,
		allowedOrigins:          origins,
		client:                  client,
		now:                     now,
		dummyHash:               dummyHash,
		passwordSlots:           make(chan struct{}, 16),
		loginAttempts:           newLoginLimiter(now),
		readiness:               config.Readiness,
		bugReports:              config.BugReports,
	}, nil
}

/*
================
Handler
================
*/
func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(readiness.PathHealth, readiness.HealthHandler)
	mux.HandleFunc(readiness.PathReady, server.handleReady)
	// Browser routes answer only the release protocol this build speaks
	// (releaseprotocol.Require). Health, readiness, cluster-internal routes
	// and guild-mark images (fetched by <img>, which cannot declare) do not.
	browser := releaseprotocol.Require
	mux.Handle(
		"/title/servers",
		browser(server.requireRunning(http.HandlerFunc(server.handleServers))),
	)
	mux.Handle(
		"/title/login",
		browser(server.requireRunning(http.HandlerFunc(server.handleLogin))),
	)
	mux.HandleFunc("/marks/", server.handleMark)
	mux.Handle("/title/session", browser(server.requireRunning(http.HandlerFunc(server.handleBrowserSession))))
	mux.Handle("/title/logout", browser(http.HandlerFunc(server.handleBrowserLogout)))
	mux.Handle("/title/character-select", browser(http.HandlerFunc(server.handleBrowserCharacterSelect)))
	mux.Handle(bugReportPath, browser(server.requireRunning(http.HandlerFunc(server.handleBugReport))))
	mux.HandleFunc("/internal/cluster/shards/heartbeat", server.handleHeartbeat)
	mux.HandleFunc("/internal/cluster/shards/release", server.handleLeaseRelease)
	mux.HandleFunc("/internal/accounts", server.handleAccountDirectory)
	for _, path := range []string{
		"/character/list",
		"/character/name-overlap",
		"/character/create",
		"/character/delete-action",
		"/character/enter-area",
		"/character/leave-area",
		agentapi.BenchmarkFixtureResetPath,
		agentapi.FollowFixturePath,
		agentapi.PassiveCriticalFixturePath,
		"/agent/packet",
		"/auth/enterworld-token",
		"/auth/transport-token",
	} {
		mux.Handle(
			path,
			browser(server.requireRunning(
				http.HandlerFunc(server.handleShardRequest),
			)),
		)
	}
	return server.cors(mux)
}

/*
================
handleReady
================
*/
func (server *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	digest, digestErr := server.sessionSigner.Digest()
	activeKeyID, activeErr := server.sessionSigner.ActiveKeyID()
	if digestErr != nil || activeErr != nil {
		http.Error(
			w,
			"session signing keys unavailable",
			http.StatusServiceUnavailable,
		)
		return
	}
	w.Header().Set("X-SRO-Session-Keyring", digest)
	w.Header().Set("X-SRO-Session-Active-Key", activeKeyID)
	server.readiness.ReadyHandler(w, r)
}

/*
================
requireRunning
================
*/
func (server *Server) requireRunning(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !server.readiness.Ready() {
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"ok":      false,
				"code":    "PROCESS_DRAINING",
				"message": "The Agent is draining.",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

/*
================
cors
================
*/
func (server *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		origin := strings.TrimSuffix(strings.TrimSpace(r.Header.Get("Origin")), "/")
		if origin != "" {
			if !server.allowedOrigins[origin] {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, "+releaseprotocol.Header)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

/*
================
clientIP
================
*/
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil || !peer.IsLoopback() {
		return host
	}
	// Agent only accepts a loopback listener. Therefore forwarding headers
	// are trusted only from that local TLS edge, never from a network peer
	// that could forge its own rate-limit identity.
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		if ip := net.ParseIP(strings.TrimSpace(first)); ip != nil {
			return ip.String()
		}
	}
	if realIP := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); realIP != nil {
		return realIP.String()
	}
	return host
}

/*
================
writeJSON
================
*/
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

/*
================
decodeJSON
================
*/
func decodeJSON(r *http.Request, destination any) error {
	payload, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes+1))
	if err != nil {
		return err
	}
	if int64(len(payload)) > maxRequestBytes {
		return fmt.Errorf("request exceeds %d bytes", maxRequestBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

/*
================
bearerClaims
================
*/
func (server *Server) bearerClaims(r *http.Request) (auth.AgentSessionClaims, bool) {
	scheme, token, found := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return auth.AgentSessionClaims{}, false
	}
	claims, err := server.sessionSigner.Verify(token, server.now())
	return claims, err == nil
}

/*
================
authorizedControlShard
================
*/
func (server *Server) authorizedControlShard(
	r *http.Request,
) (string, bool) {
	scheme, token, found := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	shardID := strings.TrimSpace(r.Header.Get(shard.ControlShardHeader))
	if shardID == "" {
		return "", false
	}
	definition, found := server.catalog.Resolve(shardID)
	if !found || !definition.Enabled {
		return "", false
	}
	claims, err := server.controlIdentityVerifier.Verify(r.Context(), token)
	if err != nil {
		return "", false
	}
	if claims.Namespace != server.controlNamespace ||
		claims.JobID != "sro-gameworld-"+definition.ID ||
		claims.Task != "gameworld" {
		return "", false
	}
	return definition.ID, true
}
