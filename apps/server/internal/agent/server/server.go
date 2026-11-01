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
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"opensro.online/server/internal/releaseprotocol"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
	agentapi "opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/agent/bugreport"
	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/platform/history"
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
sessionEpochAuthority

Optional capability implemented by auth.Accounts for persistent session
epoch tracking and revocation notifications.
================
*/
type sessionEpochAuthority interface {
	SessionEpoch(accountID string) (uint64, bool)
	RevokeSessions(ctx context.Context, accountID string) (uint64, error)
	RevokedEpochs() map[string]uint64
	SetRevokeObserver(fn func(accountID string, epoch uint64))
}

/*
================
Config
================
*/
type Config struct {
	History                 *history.Journal
	HistoryHandler          http.Handler
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
	// MaintenanceMessage names this Agent's not-operating shard refusals
	// MAINTENANCE instead of SHARD_OFFLINE (SRO_MAINTENANCE_MESSAGE).
	// Port-only, not native: see agentapi.EnvMaintenanceMessage.
	MaintenanceMessage bool
	// BugReports is nil when the operator has not configured bug reports.
	BugReports *bugreport.Service
	// BcryptCost is the cost of the unknown-account refusal and of the hashes
	// a login raises to; zero is auth.AccountBcryptCost. A store that reports
	// its own cost must agree. Tests pass bcrypt.MinCost.
	BcryptCost int
}

/*
================
Server
================
*/
type Server struct {
	history                 *history.Journal
	historyHandler          http.Handler
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
	bcryptCost              int
	passwordSlots           chan struct{}
	loginAttempts           *loginLimiter
	// incidentReports paces /client/incident per account; degradedReports
	// paces its non-fatal "degraded" reports apart, so they never use up the
	// allowance a session-ending failure needs.
	incidentReports *loginLimiter
	degradedReports *loginLimiter
	// anonymousIncidents paces reports sent before login, per client
	// address, apart from every account's allowance. refusalLines paces the
	// refusal log lines one client can cause (refusal_log.go).
	anonymousIncidents *loginLimiter
	refusalLines       *loginLimiter
	refusalCeiling     refusalCeiling
	anonymousCeiling   refusalCeiling
	// clientKey keys the client tags in refusal lines (clientTag). It is
	// drawn at start and never stored, so a tag cannot be reversed from the
	// log by hashing every address.
	clientKey        []byte
	passwordFailures passwordFailures
	readiness        *readiness.Gate
	// maintenanceMessage mirrors Config.MaintenanceMessage.
	maintenanceMessage bool
	bugReports         *bugreport.Service
	// startedAt is when this Agent was built, for /title/build's uptime.
	startedAt        time.Time
	revokedMu        sync.Mutex
	revokedNonces    map[string]time.Time
	accountMinEpoch  map[string]uint64
	shardInstance    map[string]string
	shardSyncedEpoch map[string]map[string]uint64
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
	// The refusal for an unknown account costs what a real check does, so
	// the answer's timing does not tell the two apart.
	bcryptCost := config.BcryptCost
	if bcryptCost == 0 {
		bcryptCost = auth.AccountBcryptCost
	}
	if store, ok := config.Accounts.(interface{ BcryptCost() int }); ok && store.BcryptCost() != bcryptCost {
		return nil, fmt.Errorf("agent: account store bcrypt cost %d, configured %d", store.BcryptCost(), bcryptCost)
	}
	dummyHash, err := bcrypt.GenerateFromPassword(
		[]byte("agent-invalid-account"),
		bcryptCost,
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
	clientKey := make([]byte, clientKeyBytes)
	if _, err := rand.Read(clientKey); err != nil {
		return nil, fmt.Errorf("agent server: client tag key: %w", err)
	}
	srv := &Server{
		history:                 config.History,
		historyHandler:          config.HistoryHandler,
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
		bcryptCost:              bcryptCost,
		passwordSlots:           make(chan struct{}, 16),
		loginAttempts:           newLoginLimiter(now),
		incidentReports:         newLoginLimiter(now),
		degradedReports:         newLoginLimiter(now),
		anonymousIncidents:      newLoginLimiter(now),
		refusalLines:            newLoginLimiter(now),
		clientKey:               clientKey,
		readiness:               config.Readiness,
		maintenanceMessage:      config.MaintenanceMessage,
		bugReports:              config.BugReports,
		startedAt:               now(),
		revokedNonces:           make(map[string]time.Time),
		accountMinEpoch:         make(map[string]uint64),
		shardInstance:           make(map[string]string),
		shardSyncedEpoch:        make(map[string]map[string]uint64),
	}
	if epochAuth, ok := config.Accounts.(sessionEpochAuthority); ok {
		for accountID, epoch := range epochAuth.RevokedEpochs() {
			if epoch >= 2 {
				srv.accountMinEpoch[accountID] = epoch
			}
		}
		epochAuth.SetRevokeObserver(srv.RevokeAccount)
	}
	return srv, nil
}

/*
================
Handler
================
*/
func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	if server.historyHandler != nil {
		mux.Handle(history.Path, server.historyHandler)
	}
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
		loginPath,
		browser(server.requireRunning(http.HandlerFunc(server.handleLogin))),
	)
	mux.HandleFunc("/marks/", server.handleMark)
	mux.Handle("/title/session", browser(server.requireRunning(http.HandlerFunc(server.handleBrowserSession))))
	mux.Handle("/title/logout", browser(http.HandlerFunc(server.handleBrowserLogout)))
	mux.Handle("/title/character-select", browser(http.HandlerFunc(server.handleBrowserCharacterSelect)))
	mux.Handle(bugReportPath, browser(server.requireRunning(http.HandlerFunc(server.handleBugReport))))
	mux.Handle(buildPath, browser(http.HandlerFunc(server.handleBuild)))
	mux.Handle(clientIncidentPath, browser(http.HandlerFunc(server.handleClientIncident)))
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
			code, message := server.readiness.Refusal("Agent")
			w.Header().Set("Retry-After", strconv.Itoa(readiness.RetryAfterSeconds))
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"ok":         false,
				"code":       code,
				"message":    message,
				"retryAfter": readiness.RetryAfterSeconds,
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
	if err != nil {
		return auth.AgentSessionClaims{}, false
	}
	// A signed session stays valid until it expires; the account behind it
	// and its session epoch are re-read on every use, so a logged-out,
	// password-rotated, disabled, or removed account is refused at once.
	if !server.sessionActive(claims) {
		return auth.AgentSessionClaims{}, false
	}
	return claims, true
}

/*
================
sessionActive

Checks that the account is enabled, the token's nonce has not been revoked,
and the token's session epoch matches the account's current epoch.
================
*/
func (server *Server) sessionActive(claims auth.AgentSessionClaims) bool {
	now := server.now()
	server.revokedMu.Lock()
	if exp, revoked := server.revokedNonces[claims.Nonce]; revoked {
		if now.Before(exp) {
			server.revokedMu.Unlock()
			return false
		}
		delete(server.revokedNonces, claims.Nonce)
	}
	minEpoch := server.accountMinEpoch[claims.AccountID]
	server.revokedMu.Unlock()

	if minEpoch > 0 && claims.Epoch < minEpoch {
		return false
	}
	if _, active := server.accounts.PasswordHash(claims.AccountID); !active {
		return false
	}
	if epochAuth, ok := server.accounts.(sessionEpochAuthority); ok {
		currentEpoch, active := epochAuth.SessionEpoch(claims.AccountID)
		if !active {
			return false
		}
		if minEpoch > currentEpoch {
			currentEpoch = minEpoch
		}
		if claims.Epoch == 0 {
			return currentEpoch == 1
		}
		return claims.Epoch == currentEpoch
	}
	return true
}

/*
================
revokeTokenNonce
================
*/
func (server *Server) revokeTokenNonce(nonce string, expiresAt time.Time) {
	if nonce == "" {
		return
	}
	now := server.now()
	server.revokedMu.Lock()
	for priorNonce, exp := range server.revokedNonces {
		if !now.Before(exp) {
			delete(server.revokedNonces, priorNonce)
		}
	}
	server.revokedNonces[nonce] = expiresAt
	server.revokedMu.Unlock()
}

/*
================
RevokeAccount

Advances the minimum accepted session epoch for accountID on this Agent and
pushes a signed revocation notice to each enabled GameWorld shard.
================
*/
func (server *Server) RevokeAccount(accountID string, epoch uint64) {
	if epoch < 2 {
		epoch = 2
	}
	server.revokedMu.Lock()
	if epoch > server.accountMinEpoch[accountID] {
		server.accountMinEpoch[accountID] = epoch
	} else {
		epoch = server.accountMinEpoch[accountID]
	}
	server.revokedMu.Unlock()

	now := server.now()
	for _, definition := range server.catalog.Definitions() {
		if !definition.Enabled || strings.TrimSpace(definition.ControlURL) == "" {
			continue
		}
		if server.pushShardRevocation(definition, accountID, epoch, now) {
			server.markShardEpochSynced(definition.ID, "", accountID, epoch)
		}
	}
}

/*
================
pushShardRevocation
================
*/
func (server *Server) pushShardRevocation(
	definition shard.Definition,
	accountID string,
	epoch uint64,
	now time.Time,
) bool {
	token, err := server.sessionSigner.MintAccountRevocation(
		definition.ID,
		accountID,
		epoch,
		now,
	)
	if err != nil {
		return false
	}
	target := strings.TrimSuffix(definition.ControlURL, "/") + agentapi.AccountRevokePath
	req, err := http.NewRequest(http.MethodPost, target, strings.NewReader(token))
	if err != nil {
		return false
	}
	resp, err := server.client.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

/*
================
markShardEpochSynced
================
*/
func (server *Server) markShardEpochSynced(shardID, instanceID, accountID string, epoch uint64) {
	server.revokedMu.Lock()
	defer server.revokedMu.Unlock()
	if instanceID != "" && server.shardInstance[shardID] != instanceID {
		return
	}
	synced := server.shardSyncedEpoch[shardID]
	if synced == nil {
		synced = make(map[string]uint64)
		server.shardSyncedEpoch[shardID] = synced
	}
	if epoch > synced[accountID] {
		synced[accountID] = epoch
	}
}

/*
================
syncShardRevocations

Tracks the active GameWorld instance for heartbeat.ShardID and pushes any
account revocations not yet acknowledged by that instance once the shard is
past its startup phase.
================
*/
func (server *Server) syncShardRevocations(heartbeat shard.Heartbeat) {
	definition, found := server.catalog.Resolve(heartbeat.ShardID)
	if !found || !definition.Enabled || strings.TrimSpace(definition.ControlURL) == "" {
		return
	}
	type pendingRevocation struct {
		accountID string
		epoch     uint64
	}
	var pending []pendingRevocation
	server.revokedMu.Lock()
	if server.shardInstance[heartbeat.ShardID] != heartbeat.InstanceID {
		server.shardInstance[heartbeat.ShardID] = heartbeat.InstanceID
		delete(server.shardSyncedEpoch, heartbeat.ShardID)
	}
	if heartbeat.Phase != shard.PhaseStarting {
		synced := server.shardSyncedEpoch[heartbeat.ShardID]
		for accountID, epoch := range server.accountMinEpoch {
			if epoch >= 2 && synced[accountID] < epoch {
				pending = append(pending, pendingRevocation{
					accountID: accountID,
					epoch:     epoch,
				})
			}
		}
	}
	server.revokedMu.Unlock()

	if len(pending) == 0 {
		return
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].accountID < pending[j].accountID
	})
	now := server.now()
	for _, item := range pending {
		if server.pushShardRevocation(definition, item.accountID, item.epoch, now) {
			server.markShardEpochSynced(heartbeat.ShardID, heartbeat.InstanceID, item.accountID, item.epoch)
		}
	}
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

// passwordRehasher is the account store's compare-and-swap hash upgrade
// (auth.Accounts); a fixed catalog has none.
type passwordRehasher interface {
	UpgradePasswordHash(ctx context.Context, accountID string, verified []byte, password string) (bool, error)
}

/*
================
upgradePasswordCost

After a successful login, rewrites a hash made at a lower bcrypt cost at
the configured cost, so every stored hash, the seeded and provisioned
ones included, costs the same as the unknown-account refusal. The swap is
conditional on the hash this login verified, so a password change or staff
reset that lands meanwhile is never overwritten by the old password. A
failure only leaves the old hash; the login has already succeeded.
================
*/
func (server *Server) upgradePasswordCost(ctx context.Context, accountID string, hash []byte, password string) {
	cost, err := bcrypt.Cost(hash)
	if err != nil || cost >= server.bcryptCost {
		return
	}
	rehasher, ok := server.accounts.(passwordRehasher)
	if !ok {
		return
	}
	// A seeded password outside today's length bounds cannot be rehashed; it
	// keeps its old hash rather than warning on every login.
	if _, err := rehasher.UpgradePasswordHash(ctx, accountID, hash, password); err != nil && !errors.Is(err, auth.ErrPasswordInvalid) {
		log.Warnf("agent: upgrading the password hash cost failed: %v", err)
	}
}
