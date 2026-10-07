/*
===========================================================================

agentapi.go - private character selection and control HTTP surface

===========================================================================
*/
// Package agentapi owns one GameWorld process's private character-select
// control surface. The global Agent authenticates accounts, owns the title
// server list, and proxies authenticated requests here. This package never
// opens a login path or chooses a shard.
//
// Response shapes are 1:1 with the client's recovered native folds
// (sub_72c0e0_CPSCharacterCreate_OnClickOk / sub_735c00 / authClient
// types) - the client is the surviving contract, and its error codes map
// through the native agent-error table (data_cc9e5c).
package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"opensro.online/server/internal/releaseprotocol"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/world/worldarea"
	"opensro.online/server/internal/platform/readiness"
	"opensro.online/server/internal/security/auth"
)

// Env configuration (documented in ops/docs/DEPLOYMENT.md).
const (
	// EnvAddr overrides the listen address. The default is the retired
	// Node launcher's address, so the client needs no configuration
	// change.
	EnvAddr     = "SRO_AGENT_API_ADDR"
	DefaultAddr = "127.0.0.1:8787"
	// DefaultAgentBaseURL is the dev heartbeat and Agent discovery origin
	// when SRO_AGENT_URL is unset on a GameWorld worker.
	DefaultAgentBaseURL = "http://127.0.0.1:8787"

	// EnvAllowedOrigins is a comma-separated allowlist of browser origins
	// permitted to call this loopback API. Wildcards and opaque "null"
	// origins are deliberately unsupported.
	EnvAllowedOrigins = "SRO_AGENT_ALLOWED_ORIGINS"

	// EnvMarksDir points at the guild-crest art directory GET /marks/
	// serves .crb blobs from (the native MarkFTP host role: the client
	// composes "http://" + MarkFTPAddr + MarkFTPPath + filename from its
	// option parser @0x725669 / data_cec1d4 and downloads crests at
	// runtime - NO crest art ships in the Media PK2). Unset (the default)
	// means the endpoint answers 404 for everything: the mechanism is
	// operable the moment an operator supplies art, and honestly empty
	// until then - a missing crest draws nothing on the client, which is
	// native-correct.
	EnvMarksDir = "SRO_AGENT_MARKS_DIR"

	// EnvBenchmarkFixtureControl enables the authenticated deterministic
	// character-spawn reset used by repository performance fixtures. It is
	// deliberately off unless the deployment explicitly enables it; the
	// Nomad development job does so only on its loopback network.
	EnvBenchmarkFixtureControl = "SRO_BENCHMARK_FIXTURE_CONTROL"
)

// Native agent error codes (the client's data_cc9e5c table; see
// sub_72c0e0's recovered mapping). Only the codes this surface emits.
const (
	errCodeServerConnect = 0x02 // generic "cannot reach" shape

	errCodeInvalidChargen  = 0x03 // UIO_SMERR_INVALID_CHARGEN_INFO
	errCodeInvalidName     = 0x0c // UIO_MSG_ERROR_CHARACTER_NAME_STRING
	errCodeUnknownID       = 0x10 // UIO_MSG_ERROR_ID
	errCodeNameOverlap     = 0x11 // UIO_MSG_ERROR_OVERLAP
	errCodeCreateFailed    = 0x06 // UIO_SMERR_FAILED_TO_CREATE_CHARACTER
	selectStartReqOpcode   = 0x7426
	selectStartRespOpcode  = 0xb426
	nextSceneMission       = "mission"
	createdAtTimeFormatISO = "2006-01-02T15:04:05.000Z"
)

// enterWorldTokenTTL bounds a minted EnterWorld bind token. The client
// fetches a FRESH division+character-bound token immediately before every
// 0x0006 frame (boot bind, lazy replay, reconnect re-entry), so the TTL only needs to cover the
// mint->bind latency plus clock skew — short on purpose, a leaked token
// is a bind ticket.
const enterWorldTokenTTL = time.Minute

// transportAdmissionTokenTTL is the pre-session HELLO admission-ticket lifetime.
// It is minted immediately before connection establishment and consumed once.
const transportAdmissionTokenTTL = time.Minute

var defaultAllowedOrigins = []string{
	"http://127.0.0.1:5180",
	"http://localhost:5180",
	"http://127.0.0.1:4180",
	"http://localhost:4180",
	"http://127.0.0.1:5174",
	"http://localhost:5174",
	"http://127.0.0.1:4173",
	"http://localhost:4173",
	"http://127.0.0.1:8787",
	"http://localhost:8787",
}

/*
================
DefaultAllowedOrigins

Loopback browser origins permitted when SRO_AGENT_ALLOWED_ORIGINS is unset.
Shared by agentapi and cmd/services/sro-agent so CORS defaults cannot drift.
================
*/
func DefaultAllowedOrigins() []string {
	return append([]string(nil), defaultAllowedOrigins...)
}

// Config wires the API to the authority store.
/*
================
Config
================
*/
type Config struct {
	Store *store.Store
	// CharacterPresentation projects persisted creation provenance into the
	// one canonical identity/loadout contract returned by character-list and
	// create/delete responses.
	CharacterPresentation CharacterPresentationProjector
	// CharacterCreationValid is the gameplay-owned validator for authored
	// model, weapon and protector combinations. The API only decodes requests.
	CharacterCreationValid CharacterCreationValidator
	// AuthoredAreas is the immutable game-area identity/access catalogue.
	// Browsers may request a server-owned slug but never coordinates or an
	// access grade.
	AuthoredAreas *worldarea.Catalog
	// AllowedOrigins is the exact browser-origin allowlist. nil selects
	// defaultAllowedOrigins; an explicit list replaces the defaults.
	AllowedOrigins []string
	// MarksDir is the guild-crest art directory /marks/ serves from;
	// empty = no art hosted, every crest answers 404 (see EnvMarksDir).
	MarksDir string
	// BenchmarkFixtureControl registers the development-only deterministic
	// fixture reset authority. Production deployments must leave it false.
	BenchmarkFixtureControl bool
	// SkillGroup resolves a skill id to its group for the fixture loadout;
	// nil refuses any loadout that names skills.
	SkillGroup SkillGroupResolver
	// LevelCap is the game's level cap (progression.LevelCap), the bound of a
	// fixture loadout's level; 0 refuses every loadout.
	LevelCap int64
	// CharacterInPlay reports whether the character is bound to a live
	// game session (the Hub's exclusive-bind view). nil = no live-session
	// view (tests). A retail client cannot compose a delete for a
	// character it is playing, so the guard refuses instead of kicking.
	CharacterInPlay func(divisionID, characterName string) bool
	// AcquireCharacterControl serializes an authenticated host-side mutation
	// against gameplay binding. Production wires a generation-checked Hub
	// lease; nil falls back to CharacterInPlay for isolated API tests.
	AcquireCharacterControl func(divisionID, characterName string) (release func(), acquired bool)
	// EnterWorldAuthSecret is generated in memory by this GameWorld and
	// shared only with its transport verifier.
	EnterWorldAuthSecret []byte
	// ShardID is the sole shard owned by this GameWorld.
	ShardID string
	// AgentSessionVerifier holds Agent's public signing keys only.
	AgentSessionVerifier *auth.AgentSessionVerifier
	// PrivateNetwork permits the control listener to bind outside loopback.
	// The caller must place it on a private network; bearer sessions and
	// control credentials are plaintext inside that boundary.
	PrivateNetwork bool
	Readiness      *readiness.Gate
	Now            func() time.Time
}

// API serves the agent HTTP surface.
/*
================
API
================
*/
type API struct {
	history                 http.Handler
	operator                http.Handler
	notices                 *noticePublisher
	observatory             *observatoryReader
	monsterQuery            MonsterPositionQuery
	followFixture           FollowFixtureControl
	passiveCritical         PassiveCriticalReader
	store                   *store.Store
	characterPresentation   CharacterPresentationProjector
	characterCreationValid  CharacterCreationValidator
	authoredAreas           *worldarea.Catalog
	marksDir                string // "" = no crest art hosted
	benchmarkFixtureControl bool
	skillGroup              SkillGroupResolver
	levelCap                int64
	characterInPlay         func(divisionID, characterName string) bool
	acquireCharacterControl func(divisionID, characterName string) (release func(), acquired bool)
	enterWorldAuthSecret    []byte
	workerShardID           string
	agentSessionVerifier    *auth.AgentSessionVerifier
	readiness               *readiness.Gate
	privateNetwork          bool
	now                     func() time.Time

	allowedOrigins map[string]struct{}
	listener       httpLifecycle
}

// New builds the API. A configured accounts file that fails to load is a
// boot refusal (a silent fall-back to open login would be a security
// downgrade wearing a helpful face).
/*
================
New
================
*/
func New(config Config) (*API, error) {
	if config.Store == nil {
		return nil, fmt.Errorf("agentapi: authority store is required")
	}
	if config.CharacterPresentation == nil {
		return nil, fmt.Errorf("agentapi: character presentation projector is required")
	}
	if config.CharacterCreationValid == nil {
		return nil, fmt.Errorf("agentapi: character creation validator is required")
	}
	if config.ShardID == "" {
		return nil, fmt.Errorf("agentapi: GameWorld shard id is required")
	}
	if config.Readiness == nil {
		return nil, fmt.Errorf("agentapi: readiness gate is required")
	}
	if config.AgentSessionVerifier == nil {
		return nil, fmt.Errorf("agentapi: Agent session verifier is required")
	}
	if err := auth.ValidateSecret(config.EnterWorldAuthSecret); err != nil {
		return nil, fmt.Errorf("agentapi: EnterWorld process key: %w", err)
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	allowedOrigins, err := buildAllowedOriginSet(config.AllowedOrigins)
	if err != nil {
		return nil, fmt.Errorf("agentapi: allowed origins: %w", err)
	}
	api := &API{
		store:                   config.Store,
		characterPresentation:   config.CharacterPresentation,
		characterCreationValid:  config.CharacterCreationValid,
		authoredAreas:           config.AuthoredAreas,
		marksDir:                config.MarksDir,
		benchmarkFixtureControl: config.BenchmarkFixtureControl,
		skillGroup:              config.SkillGroup,
		levelCap:                config.LevelCap,
		characterInPlay:         config.CharacterInPlay,
		acquireCharacterControl: config.AcquireCharacterControl,
		enterWorldAuthSecret:    append([]byte(nil), config.EnterWorldAuthSecret...),
		workerShardID:           config.ShardID,
		agentSessionVerifier:    config.AgentSessionVerifier,
		readiness:               config.Readiness,
		privateNetwork:          config.PrivateNetwork,
		now:                     now,
		allowedOrigins:          allowedOrigins,
	}
	log.Infof(
		"agentapi: GameWorld control surface for shard %q; title login is owned by Agent",
		config.ShardID,
	)
	log.Infof("agentapi: browser origins allowed: %s (override with %s)", strings.Join(sortedOriginNames(allowedOrigins), ", "), EnvAllowedOrigins)
	if api.marksDir != "" {
		log.Infof("agentapi: serving guild-crest art (/marks/) from %s", api.marksDir)
	} else {
		log.Infof("agentapi: no guild-crest art directory (set %s to host .crb marks); /marks/ answers 404", EnvMarksDir)
	}
	log.Infof("agentapi: EnterWorld token mint enabled (POST /auth/enterworld-token, ttl %s)", enterWorldTokenTTL)
	log.Infof("agentapi: transport admission mint enabled (POST /auth/transport-token, ttl %s)", transportAdmissionTokenTTL)
	if api.benchmarkFixtureControl {
		log.Infof("agentapi: deterministic benchmark fixture control enabled at %s", BenchmarkFixtureResetPath)
	}
	return api, nil
}

/*
================
buildAllowedOriginSet
================
*/
func buildAllowedOriginSet(configured []string) (map[string]struct{}, error) {
	origins := configured
	if origins == nil {
		origins = defaultAllowedOrigins
	}
	out := make(map[string]struct{}, len(origins))
	for _, raw := range origins {
		origin := strings.TrimSuffix(strings.TrimSpace(raw), "/")
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
			parsed.Host == "" || parsed.User != nil || parsed.Path != "" ||
			parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("invalid origin %q (want scheme://host[:port])", raw)
		}
		if origin == "null" || origin == "*" {
			return nil, fmt.Errorf("unsafe origin %q is not supported", raw)
		}
		out[origin] = struct{}{}
	}
	return out, nil
}

/*
================
sortedOriginNames
================
*/
func sortedOriginNames(origins map[string]struct{}) []string {
	names := make([]string, 0, len(origins))
	for origin := range origins {
		names = append(names, origin)
	}
	sort.Strings(names)
	return names
}

// Handler builds the HTTP mux with CORS (the browser client is a
// different origin in every deployment shape).
/*
================
Handler
================
*/
func (api *API) Handler() http.Handler {
	mux := http.NewServeMux()
	if api.history != nil {
		mux.Handle("/internal/operations/history", api.history)
	}
	if api.operator != nil {
		mux.Handle("/internal/operations/player", api.requireRunning(api.operator))
	}
	if api.notices != nil {
		mux.Handle(NoticePath, api.requireRunning(http.HandlerFunc(api.handleNotice)))
	}
	if api.observatory != nil {
		mux.Handle(ObservatoryPath, api.requireRunning(http.HandlerFunc(api.handleObservatory)))
	}
	if api.monsterQuery != nil {
		mux.Handle(MonsterQueryPath, api.requireRunning(http.HandlerFunc(api.handleMonsterQuery)))
	}
	mux.HandleFunc(readiness.PathHealth, readiness.HealthHandler)
	mux.HandleFunc(readiness.PathReady, api.handleReady)
	mux.Handle(
		"/character/list",
		api.requireRunning(
			api.browserSession(http.HandlerFunc(api.handleCharacterList)),
		),
	)
	mux.Handle(
		"/character/name-overlap",
		api.requireRunning(
			api.browserSession(http.HandlerFunc(api.handleNameOverlap)),
		),
	)
	mux.Handle(
		"/character/create",
		api.requireRunning(
			api.browserSession(http.HandlerFunc(api.handleCharacterCreate)),
		),
	)
	mux.Handle(
		"/character/delete-action",
		api.requireRunning(
			api.browserSession(http.HandlerFunc(api.handleDeleteAction)),
		),
	)
	mux.Handle(
		"/character/enter-area",
		api.requireRunning(
			api.browserSession(http.HandlerFunc(api.handleCharacterEnterArea)),
		),
	)
	mux.Handle(
		"/character/leave-area",
		api.requireRunning(
			api.browserSession(http.HandlerFunc(api.handleCharacterLeaveArea)),
		),
	)
	if api.benchmarkFixtureControl {
		if api.passiveCritical != nil {
			mux.Handle(PassiveCriticalFixturePath, api.requireRunning(api.browserSession(http.HandlerFunc(api.handlePassiveCriticalFixture))))
		}
		if api.followFixture != nil {
			mux.Handle(FollowFixturePath, api.requireRunning(api.browserSession(http.HandlerFunc(api.handleFollowFixture))))
		}
		mux.Handle(
			BenchmarkFixtureResetPath,
			api.requireRunning(
				api.browserSession(http.HandlerFunc(api.handleBenchmarkFixtureReset)),
			),
		)
	}
	mux.Handle(
		"/agent/packet",
		api.requireRunning(
			api.browserSession(http.HandlerFunc(api.handleAgentPacket)),
		),
	)
	mux.Handle(
		"/auth/enterworld-token",
		api.requireRunning(
			api.browserSession(http.HandlerFunc(api.handleEnterWorldToken)),
		),
	)
	mux.Handle(
		"/auth/transport-token",
		api.requireRunning(
			api.browserSession(http.HandlerFunc(api.handleTransportAdmissionToken)),
		),
	)
	mux.HandleFunc("/marks/", api.handleGuildMark)
	return api.corsMiddleware(mux)
}

/*
================
handleReady
================
*/
func (api *API) handleReady(w http.ResponseWriter, r *http.Request) {
	digest, err := api.agentSessionVerifier.Digest()
	if err != nil {
		http.Error(
			w,
			"session verification keys unavailable",
			http.StatusServiceUnavailable,
		)
		return
	}
	w.Header().Set("X-SRO-Session-Keyring", digest)
	api.readiness.ReadyHandler(w, r)
}

// guildMarkNamePattern pins the ONLY filename shapes the client ever
// composes for crest art (sub_833d40: "G%u_%u_%u.crb" @0x833e42 for the
// guild crest, "A%u_%u_%u.crb" @0x833e75 for the alliance crest; %u caps
// at 10 decimal digits for a u32). Anything else is not a crest request
// and answers 404 - which also makes path traversal unexpressable.
var guildMarkNamePattern = regexp.MustCompile(`^[GA][0-9]{1,10}_[0-9]{1,10}_[0-9]{1,10}\.crb$`)

// Guild crests are tiny native image blobs. A generous hard ceiling prevents
// a host-supplied file from turning one public GET into an unbounded
// allocation or transfer.
const maxGuildMarkBytes int64 = 1 << 20

// handleGuildMark serves one guild-crest .crb blob by its native filename
// from the configured marks directory - a raw passthrough, no
// transformation (the client's sub_4ba950 loader consumes the bytes and
// caches them locally as .rd under its 0xcbc340 cache root). The server
// stores only crest REVISION params (the v1.188 _Guild.CurCrestRev
// lineage), never images, so art is host-supplied: no directory or no
// file is an honest 404 and the client draws nothing for that crest.
/*
================
handleGuildMark
================
*/
func (api *API) handleGuildMark(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/marks/")
	if api.marksDir == "" || !guildMarkNamePattern.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(api.marksDir, name)
	pathInfo, err := os.Lstat(path)
	if err != nil || !pathInfo.Mode().IsRegular() || pathInfo.Size() > maxGuildMarkBytes {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	openInfo, err := file.Stat()
	if err != nil || !openInfo.Mode().IsRegular() ||
		openInfo.Size() > maxGuildMarkBytes || !os.SameFile(pathInfo, openInfo) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Native filename includes the revision. Replacing art requires a new
	// revision, so warm browser sessions can reuse it without another transfer.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, name, openInfo.ModTime(), file)
}

// maxBodyBytes caps every POST body. The largest legitimate request is a
// character create (a few hundred bytes); 64 KiB is generous headroom and
// still makes memory exhaustion through this surface impossible.
const maxBodyBytes = 64 << 10

// corsMiddleware rejects browser origins outside the exact configured
// allowlist before they can reach a handler. Originless callers (native
// tools, curl, same-machine probes) remain usable; bearer authentication
// independently guards every private route.
/*
================
corsMiddleware
================
*/
func (api *API) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		origin := strings.TrimSuffix(strings.TrimSpace(r.Header.Get("Origin")), "/")
		if origin != "" {
			if _, allowed := api.allowedOrigins[origin]; !allowed {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, "+releaseprotocol.Header)
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

/*
================
browserSession

A browser route: it answers only the release protocol this build speaks
(releaseprotocol.Require), then requires an authenticated session.
================
*/
func (api *API) browserSession(next http.Handler) http.Handler {
	return releaseprotocol.Require(api.requireSession(next))
}

/*
================
requireSession
================
*/
func (api *API) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		authorization := r.Header.Get("Authorization")
		token := strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
		claims, err := api.agentSessionVerifier.Verify(token, api.now())
		if !strings.HasPrefix(authorization, prefix) ||
			err != nil ||
			claims.ShardID != api.workerShardID {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"ok": false, "code": "UNAUTHORIZED",
			})
			return
		}
		identity := sessionIdentity{
			accountID: claims.AccountID,
			shardID:   api.workerShardID,
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(
			r.Context(),
			sessionIdentityContextKey{},
			identity,
		)))
	})
}

/*
================
requireRunning
================
*/
func (api *API) requireRunning(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !api.readiness.Ready() {
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"ok":      false,
				"code":    "PROCESS_DRAINING",
				"message": "The GameWorld is draining.",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

/*
================
sessionIdentityContextKey
================
*/
type sessionIdentityContextKey struct{}

/*
================
sessionIdentity
================
*/
type sessionIdentity struct {
	accountID string
	shardID   string
}

/*
================
requestIdentity
================
*/
func requestIdentity(r *http.Request) sessionIdentity {
	identity, _ := r.Context().Value(sessionIdentityContextKey{}).(sessionIdentity)
	return identity
}

/*
================
requestAccountID
================
*/
func requestAccountID(r *http.Request) string {
	return requestIdentity(r).accountID
}

/*
================
requestShardID
================
*/
func requestShardID(r *http.Request) string {
	return requestIdentity(r).shardID
}

/*
================
writeJSON
================
*/
func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Warnf("agentapi: encoding response: %v", err)
	}
}

/*
================
decodeJSONRequest
================
*/
func decodeJSONRequest(body io.Reader, destination interface{}) error {
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return fmt.Errorf("trailing JSON: %w", err)
	}
	return nil
}
