/*
===========================================================================

publicstats.go - the community site's read-only public API (port-only)

Port-only, not native. GET-only JSON for opensro.online: the unique
tracker, kill feed, leaderboard and world-firsts, the player database, the
server status and event schedule (status.go) and the Original vs OpenSRO
rules (opensro-web docs/COMMUNITY.md, P1-P9).
It runs in the GameWorld, whose authority store holds the characters and
kills, on its own loopback-only listener (SRO_PUBLIC_API_ADDR), apart from
the control API.

The one write, the website's privacy setting, is in privacy.go.

Three rules hold for every endpoint:
  - Read-only and aggregate: no account ids, no positions, no evidence.
  - Privacy is applied here, at read time, against each character's current
    publicHidden: a hidden character is absent from search and profiles
    (404), left out of the leaderboard, and named "a hunter" in kills and
    firsts, including kills recorded before it chose to hide.
  - Never per request: each answer is built at most once per its cache
    period from injected sources that copy state off the tick, and the
    GameWorld tick never waits on a reader.

===========================================================================
*/
package publicstats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/social/chatfeed"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/platform/sealboard"
	"opensro.online/server/internal/transport"
)

// EnvAddr names the listener; DefaultAddr is its loopback default.
const (
	EnvAddr     = "SRO_PUBLIC_API_ADDR"
	DefaultAddr = "127.0.0.1:8790"
)

// HiddenName stands in for a character that chose to hide.
const HiddenName = "a hunter"

// Cache periods (Cache-Control max-age) per endpoint family.
const (
	uniquesMaxAge    = 10 * time.Second
	boardsMaxAge     = 60 * time.Second
	charactersMaxAge = 30 * time.Second
	rulesMaxAge      = 300 * time.Second
	chatMaxAge       = 5 * time.Second
)

// snapshotMaxAge is how long the shared character and kill copies serve
// every answer: the shortest answer period, so no answer is staler than it.
const snapshotMaxAge = uniquesMaxAge

// Request bounds.
const (
	defaultKillLimit   = 50
	maxKillLimit       = 100
	leaderboardSize    = 100
	defaultSearchLimit = 20
	maxSearchLimit     = 50
	maxNameLength      = 64
)

// weekMs is the leaderboard's "week" period: the last seven days.
const weekMs = 7 * 24 * 60 * 60 * 1000

/*
================
Sources

Everything the API reads, injected by the GameWorld wiring. Each function
returns a private copy; none may block on the simulation tick.
================
*/
type Sources struct {
	// Characters returns detached copies of every live (not deleted)
	// character of the shard's division.
	Characters func() []*domain.Character
	// Kills returns the division's recorded unique kills with a sequence
	// above afterSeq, oldest first; fewer than KillPage means caught up.
	Kills func(afterSeq int64) ([]domain.UniqueKill, error)
	// KillPage is the most kills one Kills call returns (store.UniqueKillPage).
	KillPage int
	// Uniques returns every unique's live state (MonsterState.UniqueStates).
	Uniques func() []simulation.UniqueState
	// UniqueName returns a unique's display name, or "" for its codename.
	UniqueName func(refObjID uint32) string
	// GuildOf returns the character's guild name and rank, or "" for none.
	GuildOf func(c *domain.Character) (name, rank string)
	// Guilds returns every guild of the shard's division (guilds.go); nil
	// serves an empty list.
	Guilds func() []GuildEntry
	// ItemGrade returns an equipment reference's degree and, when sealed,
	// its seal tier (enterworld.EquipmentDegree / EquipmentSeal: 0 Star,
	// 1 Moon, 2 Sun). Nil grades every piece degree 0, unsealed.
	ItemGrade func(refObjID uint32) (degree, tier int, sealed bool)
	// HasDurability reports whether an item reference carries durability
	// (itemdata Dur_U above 0); nil reports it for every item.
	HasDurability func(refObjID uint32) bool
	// ModelRef resolves the character's body reference id.
	ModelRef func(c *domain.Character) uint32
	// Vitals returns the character's maximum HP and MP.
	Vitals func(c *domain.Character) (hp, mp int64)
	// Online reports whether the character is in the world.
	Online func(characterID int64) bool
	// Rules is the P9 list, built once at boot from the live flags.
	Rules RulesResponse
	// Shard labels world-firsts ("beta" on the beta server).
	Shard string
	// Now is the clock; nil reads the wall clock.
	Now func() time.Time
	// SetHidden commits a character's privacy flag when it belongs to the
	// account, else returns ErrNotOwned. WriteToken guards it (privacy.go);
	// either one missing leaves the write unserved.
	SetHidden func(accountID, name string, hidden bool) error
	// SetGearHidden commits the "hide my gear" choice the same way.
	SetGearHidden func(accountID, name string, hidden bool) error
	// SetBreakNotice commits the item-break notice opt-in (#1738) the
	// same way.
	SetBreakNotice func(accountID, name string, on bool) error
	WriteToken     string
	// Sample reads the shard's online count and the hub's cumulative
	// counters for P7 (status.go); nil leaves the status figures zero.
	Sample func() (online int, metrics transport.Metrics)
	// StartedAt is when the GameWorld started (P7 uptime).
	StartedAt time.Time
	// MasteryTotalOverride is the beta total mastery allowance, 0 when
	// native (P7 status).
	MasteryTotalOverride int
	// ExpRequired is the level table's EXP to pass level (rankings).
	ExpRequired func(level int64) (int64, bool)
	// Schedule lists the events this server runs at now (P8).
	Schedule func(now time.Time) []ScheduledEvent
	// Chat returns the shard's public chat transcript, oldest first, with
	// muted authors already left out (chat.go); nil serves no lines.
	Chat func() []chatfeed.Line
	// Events returns the shard's automated gameplay announcements (seal
	// drops, enhancements, job kills, breaks), oldest first, kept apart
	// from Chat so they never push people's lines out; nil serves none.
	Events func() []chatfeed.Line
	// SealCounts and SealCountingSince read the weekly seal board's
	// counter (platform/sealboard, #1641): per-character tier counts and
	// when the week's counting began. Nil leaves the seals board
	// unserved (404): SRO_SEAL_BOARD off, the native default.
	SealCounts        func(weekStart time.Time) ([]sealboard.Count, error)
	SealCountingSince func(weekStart time.Time) (time.Time, error)
}

/*
================
snapshot

The source copies every answer shares (views.go loadRoster, allKills). Per
answer caching alone keys on the request, so each new name or search would
copy the whole shard under the store's read lock; these copies are taken at
most once per snapshotMaxAge however many distinct requests arrive. Guarded
by Service.mu, which every build holds.
================
*/
type snapshot struct {
	roster      roster
	rosterUntil time.Time
	kills       []domain.UniqueKill
	killsUntil  time.Time
	lastSeq     int64
	// rankings are the orderings built from this roster and kill list.
	rankings map[rankingKey]rankingSet
	// guilds is the ranked guild list (guilds.go loadGuilds); it names
	// masters, so it goes with the roster when a flag changes.
	guilds      []GuildRow
	guildsUntil time.Time
}

/*
================
cacheEntry
================
*/
type cacheEntry struct {
	body   []byte
	status int
	until  time.Time
}

/*
================
Service

The handler set and its answer cache.
================
*/
type Service struct {
	src Sources

	mu    sync.Mutex
	cache map[string]cacheEntry
	snap  snapshot

	status statusState

	server   *http.Server
	listener net.Listener
}

/*
================
New
================
*/
func New(src Sources) *Service {
	return &Service{src: src, cache: map[string]cacheEntry{}}
}

/*
================
now
================
*/
func (s *Service) now() time.Time {
	if s.src.Now != nil {
		return s.src.Now()
	}
	return time.Now()
}

/*
================
Handler

The GET-only route table.
================
*/
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /public/v1/uniques", s.cached(uniquesMaxAge, s.uniques))
	mux.HandleFunc("GET /public/v1/uniques/kills", s.cached(uniquesMaxAge, s.kills))
	mux.HandleFunc("GET /public/v1/leaderboards/uniques", s.cached(boardsMaxAge, s.leaderboard))
	// The seal board's route exists only while its counter is wired
	// (SRO_SEAL_BOARD): off must answer 404 like any unknown route, so
	// the flag's state leaves no fingerprint (the census' own rule).
	if s.src.SealCounts != nil {
		mux.HandleFunc("GET /public/v1/leaderboards/seals", s.cached(boardsMaxAge, s.sealBoard))
	}
	mux.HandleFunc("GET /public/v1/firsts", s.cached(boardsMaxAge, s.firsts))
	mux.HandleFunc("GET /public/v1/characters", s.cached(charactersMaxAge, s.search))
	mux.HandleFunc("GET /public/v1/characters/{name}", s.cached(charactersMaxAge, s.profile))
	mux.HandleFunc("GET /public/v1/rules", s.cached(rulesMaxAge, s.rules))
	mux.HandleFunc("GET /public/v1/rankings", s.cached(charactersMaxAge, s.rankings))
	mux.HandleFunc("GET /public/v1/guilds", s.cached(charactersMaxAge, s.guilds))
	mux.HandleFunc("GET /public/v1/schema", s.cached(rulesMaxAge, s.schema))
	mux.HandleFunc("GET /public/v1/status", s.cached(uniquesMaxAge, s.serverStatus))
	mux.HandleFunc("GET /public/v1/schedule", s.cached(boardsMaxAge, s.schedule))
	mux.HandleFunc("GET /public/v1/chat", s.cached(chatMaxAge, s.chat))
	mux.HandleFunc("GET /public/v1/events", s.cached(chatMaxAge, s.events))
	s.installPrivacy(mux)
	return mux
}

// errNotFound answers 404; errBadRequest answers 400.
var (
	errNotFound   = errors.New("not found")
	errBadRequest = errors.New("bad request")
)

/*
================
cached

Serves an answer from the cache while it is fresh, otherwise builds it once
(concurrent readers of the same key wait for that build rather than
repeating it) and stores it for maxAge. Errors are cached too, so a burst of
404s costs one build.
================
*/
func (s *Service) cached(maxAge time.Duration, build func(r *http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entry := s.cachedEntry(r, maxAge, build)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "max-age="+strconv.Itoa(int(maxAge/time.Second)))
		w.WriteHeader(entry.status)
		_, _ = w.Write(entry.body)
	}
}

/*
================
cachedEntry

The fresh entry for the request, building it under s.mu. The lock is
released by defer: a builder that panics must never leave every later
request waiting on it.
================
*/
func (s *Service) cachedEntry(r *http.Request, maxAge time.Duration, build func(r *http.Request) (any, error)) cacheEntry {
	key := r.URL.Path + "?" + r.URL.RawQuery
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, fresh := s.cache[key]
	if !fresh || !now.Before(entry.until) {
		entry = s.build(build, r, now.Add(maxAge))
		if len(s.cache) > 4096 {
			s.cache = map[string]cacheEntry{}
		}
		s.cache[key] = entry
	}
	return entry
}

/*
================
build
================
*/
func (s *Service) build(build func(r *http.Request) (any, error), r *http.Request, until time.Time) cacheEntry {
	value, err := build(r)
	status := http.StatusOK
	switch {
	case errors.Is(err, errNotFound):
		status, value = http.StatusNotFound, map[string]string{"error": "not found"}
	case errors.Is(err, errBadRequest):
		status, value = http.StatusBadRequest, map[string]string{"error": err.Error()}
	case err != nil:
		status, value = http.StatusServiceUnavailable, map[string]string{"error": "unavailable"}
	}
	body, encodeErr := json.Marshal(value)
	if encodeErr != nil {
		status, body = http.StatusInternalServerError, []byte(`{"error":"encode"}`)
	}
	return cacheEntry{body: body, status: status, until: until}
}

/*
================
Start

Listens on a loopback address only and serves until Close.
================
*/
func (s *Service) Start(addr string) error {
	if addr == "" {
		addr = DefaultAddr
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("public api: %w", err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("public api: %s is not a loopback address", addr)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("public api: %w", err)
	}
	s.listener = listener
	s.server = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = s.server.Serve(listener) }()
	s.startStatusSampler()
	return nil
}

/*
================
Addr
================
*/
func (s *Service) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

/*
================
Close
================
*/
func (s *Service) Close() error {
	s.stopStatusSampler()
	if s.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.server.Shutdown(ctx)
}

/*
================
rfc3339
================
*/
func rfc3339(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

/*
================
optionalTime
================
*/
func optionalTime(ms int64) *string {
	if ms <= 0 {
		return nil
	}
	at := rfc3339(ms)
	return &at
}

/*
================
raceGender

The race and gender a model codename names (CHAR_CH_MAN_*, CHAR_EU_WOMAN_*).
================
*/
func raceGender(codename string) (race, gender string) {
	upper := strings.ToUpper(codename)
	race, gender = "unknown", "unknown"
	switch {
	case strings.HasPrefix(upper, "CHAR_CH_"):
		race = "chinese"
	case strings.HasPrefix(upper, "CHAR_EU_"):
		race = "european"
	}
	switch {
	case strings.Contains(upper, "_WOMAN_"):
		gender = "female"
	case strings.Contains(upper, "_MAN_"):
		gender = "male"
	}
	return race, gender
}

/*
================
jobName
================
*/
func jobName(job uint8) string {
	switch job {
	case 1:
		return "trader"
	case 2:
		return "thief"
	case 3:
		return "hunter"
	}
	return "none"
}

/*
================
level
================
*/
func level(c *domain.Character) int64 {
	if c.Level == nil {
		return 1
	}
	return *c.Level
}
