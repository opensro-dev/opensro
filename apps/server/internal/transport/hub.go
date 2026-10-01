package transport

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

// HandlerFunc handles one inbound game frame. Handlers for the same session
// run serially on that session's read loop; handlers for different sessions
// run concurrently. A panic is recovered and logged, and the offending
// session is closed.
type HandlerFunc func(s *Session, opcode uint16, payload []byte)

// Hub owns every live session and the opcode dispatch table the game lanes
// register into. One Hub serves both the WebTransport and the
// WebSocket listeners.
type Hub struct {
	cfg Config

	mu       sync.RWMutex
	sessions map[uint64]*Session
	byToken  map[[ResumeTokenLen]byte]*Session
	closed   bool

	// Single-session bind: one live session per bind key
	// (division:character). bindingKeys is the reverse index for cleanup.
	bindings    map[string]*Session
	bindingKeys map[uint64]string
	controls    map[string]uint64

	// Account index: admission account -> its live sessions, and the
	// reverse for cleanup. Bounds what one account can hold
	// (Config.MaxSessionsPerAccount).
	accountSessions map[string]map[uint64]*Session
	sessionAccount  map[uint64]string

	// Division index: EFFECTIVE division -> live member sessions, so a
	// division push touches only its members instead of scanning every
	// session (O(sessions) per push was the scaling ceiling). sessionDiv
	// is the reverse index for cleanup. Maintained on every path that can
	// change membership: Session.Set of a division key (reindexDivision)
	// and final teardown (closeSession). Eviction deliberately does NOT
	// remove the lame duck here — delivery to it is already refused at
	// the Session level, matching the pre-index behaviour.
	divisions  map[string]map[uint64]*Session
	sessionDiv map[uint64]string

	nextID        atomic.Uint64
	nextControlID atomic.Uint64

	auth      enterWorldGate
	admission helloAdmissionGate
	handlers  handlerRegistry
	hooks     sessionHooks

	// handshakeSlots bounds connections waiting for their first HELLO.
	// Admission is non-blocking: a full server rejects new work instead of
	// accumulating goroutines until HelloTimeout.
	handshakeSlots chan struct{}

	// Transport observability, including simulation-tick duration, has its own
	// synchronization and lifecycle.
	metrics hubMetrics
}

// AdmissionIdentity is the authenticated account/shard identity carried by a
// HELLO admission ticket. It is intentionally smaller than gameplay identity: the
// character still requires a separate one-use EnterWorld authorization.
type AdmissionIdentity struct {
	AccountID string
	ShardID   string
}

// HelloAuthFunc verifies and consumes one HELLO admission ticket.
type HelloAuthFunc func(token []byte) (AdmissionIdentity, error)

// SetHelloAuth installs the required HELLO admission verifier.
func (h *Hub) SetHelloAuth(fn HelloAuthFunc) {
	h.admission.set(fn)
}

func (h *Hub) helloAuthFn() HelloAuthFunc {
	return h.admission.current()
}

func newHub(cfg Config) *Hub {
	return &Hub{
		cfg:             cfg,
		sessions:        make(map[uint64]*Session),
		byToken:         make(map[[ResumeTokenLen]byte]*Session),
		bindings:        make(map[string]*Session),
		bindingKeys:     make(map[uint64]string),
		controls:        make(map[string]uint64),
		accountSessions: make(map[string]map[uint64]*Session),
		sessionAccount:  make(map[uint64]string),
		divisions:       make(map[string]map[uint64]*Session),
		sessionDiv:      make(map[uint64]string),
		handlers:        newHandlerRegistry(),
		handshakeSlots:  make(chan struct{}, cfg.MaxPendingHandshakes),
	}
}

// EnterWorldAuthFunc vets an OpEnterWorld bind BEFORE the game handler
// runs. Return ok=false with a native error code to refuse: the transport
// answers OpEnterWorldResult{OK:false, NativeErrorCode:denyCode} itself and
// the handler never sees the frame. Installed via SetEnterWorldAuth;
// production wires launcher-token verification here.
type EnterWorldAuthFunc func(s *Session, ew EnterWorld) (ok bool, denyCode uint32)

// SetEnterWorldAuth installs the required post-WELCOME identity gate.
func (h *Hub) SetEnterWorldAuth(fn EnterWorldAuthFunc) {
	h.auth.set(fn)
}

func (h *Hub) enterWorldAuthFn() EnterWorldAuthFunc {
	return h.auth.current()
}

// BindExclusive claims a bind key (division:character) for a session. If a
// DIFFERENT live session already holds the key it is evicted: it is marked
// lame-duck IMMEDIATELY (inbound game frames dropped, outbound game frames
// refused, invisible to the simulation tick), gets a reliable
// BYE(Replaced), and is torn down, which also kills its resume token — the
// replaced client cannot silently reattach. Rebinding the same session to
// the same key is a no-op; binding it to a new key releases the old one.
// Returns the evicted session when a replacement happened.
//
// A session that has itself been evicted can never bind again: an in-flight
// EnterWorld handler of the loser must not steal the key back from the
// winner. Callers detect that refusal via s.Evicted() (the latch is one-way
// and set before the winning BindExclusive returns, so the check is exact).
func (h *Hub) BindExclusive(key string, s *Session) (*Session, bool) {
	var evict *Session
	h.mu.Lock()
	if s.evicted.Load() {
		h.mu.Unlock()
		log.WithFields(log.Fields{"key": key, "session": s.ID}).
			Warn("transport: bind refused for evicted session")
		return nil, false
	}
	if _, controlled := h.controls[key]; controlled {
		s.markEvicted()
		h.mu.Unlock()
		log.WithFields(log.Fields{"key": key, "session": s.ID}).
			Warn("transport: bind refused during character control transaction")
		s.CloseWhenDrained(ByeReasonReplaced)
		return nil, false
	}
	cur := h.bindings[key]
	if cur == s {
		h.mu.Unlock()
		return nil, false
	}
	if prevKey, ok := h.bindingKeys[s.ID]; ok && prevKey != key && h.bindings[prevKey] == s {
		delete(h.bindings, prevKey)
	}
	h.bindings[key] = s
	h.bindingKeys[s.ID] = key
	evict = cur
	if evict != nil {
		// Latch the loser while the registry swap is still atomic: any
		// tick snapshot or dispatch racing this bind either sees the old
		// binding with a live victim or the new binding with a lame duck,
		// never a bound-out victim that can still drive the character.
		// Atomic store only — h.mu never nests session.mu here.
		evict.markEvicted()
	}
	h.mu.Unlock()

	if evict == nil {
		return nil, false
	}
	log.WithFields(log.Fields{"key": key, "old": evict.ID, "new": s.ID}).
		Info("transport: single-bind eviction, replacing session")
	evict.CloseWhenDrained(ByeReasonReplaced)
	return evict, true
}

// BindingControlLease serializes one host-side character mutation against
// gameplay binding. Its generation prevents a stale Release from deleting a
// newer lease for the same key.
type BindingControlLease struct {
	hub        *Hub
	key        string
	generation uint64
	once       sync.Once
}

// AcquireBindingControl obtains exclusive control of a character binding. A
// currently bound session is evicted and reported busy; the caller retries
// only after final teardown removes that exact session. While a lease is held,
// BindExclusive refuses new world admission for the key.
func (h *Hub) AcquireBindingControl(key string) (*BindingControlLease, bool) {
	h.mu.Lock()
	if _, controlled := h.controls[key]; controlled {
		h.mu.Unlock()
		return nil, false
	}
	if bound := h.bindings[key]; bound != nil {
		bound.markEvicted()
		h.mu.Unlock()
		bound.CloseWhenDrained(ByeReasonReplaced)
		return nil, false
	}
	generation := h.nextControlID.Add(1)
	h.controls[key] = generation
	h.mu.Unlock()
	return &BindingControlLease{
		hub:        h,
		key:        key,
		generation: generation,
	}, true
}

// Release relinquishes only this lease generation and is idempotent.
func (lease *BindingControlLease) Release() {
	if lease == nil || lease.hub == nil {
		return
	}
	lease.once.Do(func() {
		lease.hub.mu.Lock()
		if lease.hub.controls[lease.key] == lease.generation {
			delete(lease.hub.controls, lease.key)
		}
		lease.hub.mu.Unlock()
	})
}

// BoundSession returns the live session holding a bind key.
func (h *Hub) BoundSession(key string) (*Session, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.bindings[key]
	return s, ok
}

// HandleErr registers the handler for one native opcode or
// control-extension opcode (OpEnterWorld and up) and reports a refused
// registration as an error. The session-internal opcodes (HELLO..BYE,
// 0x0001-0x0005) cannot be registered. Registering during live traffic is
// safe; last write wins. Callers MUST NOT drop the error: an unregistered
// opcode is a silent gameplay outage, so refuse to boot (or at minimum
// log at Error) when it is non-nil.
func (h *Hub) HandleErr(opcode uint16, fn HandlerFunc) error {
	if opcode <= maxReservedOpcode {
		return fmt.Errorf("transport: opcode 0x%04X is session-internal, not registrable", opcode)
	}
	h.handlers.register(opcode, fn)
	return nil
}

// Handle is HandleErr for BOOT-TIME registration only: it panics on a
// refused registration. That is deliberate fail-fast at process start —
// a mis-registered lane must not come up half-wired — but it is the wrong
// tool anywhere a panic would take live players down; register from
// runtime code through HandleErr instead.
func (h *Hub) Handle(opcode uint16, fn HandlerFunc) {
	if err := h.HandleErr(opcode, fn); err != nil {
		panic(err.Error())
	}
}

// HandleDefault registers the catch-all for opcodes with no specific
// handler; without it unhandled frames are logged and dropped.
func (h *Hub) HandleDefault(fn HandlerFunc) {
	h.handlers.setDefault(fn)
}

// OnSessionOpen runs after a brand-new session completes its handshake.
func (h *Hub) OnSessionOpen(fn func(*Session)) {
	h.hooks.addOpen(fn)
}

// OnSessionResumed runs after a client reattaches to a detached session.
// Game lanes re-push authoritative state here.
func (h *Hub) OnSessionResumed(fn func(*Session)) {
	h.hooks.addResumed(fn)
}

// OnSessionClose runs exactly once per session at final teardown. err is
// nil on a clean close (client BYE, drained shutdown).
func (h *Hub) OnSessionClose(fn func(*Session, error)) {
	h.hooks.addClose(fn)
}

// Session returns a live session by ID.
func (h *Hub) Session(id uint64) (*Session, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[id]
	return s, ok
}

// Sessions snapshots the live sessions.
func (h *Hub) Sessions() []*Session {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		out = append(out, s)
	}
	return out
}

// Broadcast queues a frame to every live session.
func (h *Hub) Broadcast(opcode uint16, payload []byte) {
	for _, s := range h.Sessions() {
		_ = s.Send(opcode, payload)
	}
}

// BroadcastFunc queues a frame to every live session the predicate accepts.
// The game lanes can filter by the explicit player identity:
//
//	hub.BroadcastFunc(func(s *transport.Session) bool {
//	    div, _ := s.DivisionID()
//	    return div == wantedDiv && s.ID != exceptID
//	}, opcode, payload)
func (h *Hub) BroadcastFunc(accept func(*Session) bool, opcode uint16, payload []byte) {
	for _, s := range h.Sessions() {
		if accept(s) {
			_ = s.Send(opcode, payload)
		}
	}
}

// AcceptConn performs the HELLO/WELCOME handshake on a fresh transport
// connection, then hands the connection to a new or resumed session. Run it
// on its own goroutine per connection.
func (h *Hub) AcceptConn(conn Conn) {
	startedAt := time.Now()
	select {
	case h.handshakeSlots <- struct{}{}:
		defer func() { <-h.handshakeSlots }()
	default:
		h.metrics.handshakeBusy.Add(1)
		_ = conn.WriteFrame(Frame{Opcode: OpBye, Payload: []byte{ByeReasonServerBusy}})
		_ = conn.Close("server handshake capacity reached")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), h.cfg.HelloTimeout)
	f, err := conn.ReadFrame(ctx)
	cancel()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			h.metrics.handshakeHelloTimeout.Add(1)
		} else {
			h.metrics.handshakeReadFailed.Add(1)
		}
		_ = conn.WriteFrame(Frame{Opcode: OpBye, Payload: []byte{ByeReasonHelloTimeout}})
		_ = conn.Close("no HELLO before deadline")
		return
	}
	if f.EncodedLen() > MaxInboundFrameBytes {
		h.metrics.handshakeProtocolRejected.Add(1)
		_ = conn.WriteFrame(Frame{Opcode: OpBye, Payload: []byte{ByeReasonProtocolErr}})
		_ = conn.Close(ErrInboundFrameTooLarge.Error())
		return
	}
	if f.Opcode != OpHello {
		h.metrics.handshakeProtocolRejected.Add(1)
		_ = conn.WriteFrame(Frame{Opcode: OpBye, Payload: []byte{ByeReasonProtocolErr}})
		_ = conn.Close("first frame was not HELLO")
		return
	}
	hello, err := DecodeHello(f.Payload)
	if err != nil {
		h.metrics.handshakeProtocolRejected.Add(1)
		_ = conn.WriteFrame(Frame{Opcode: OpBye, Payload: []byte{ByeReasonProtocolErr}})
		_ = conn.Close(err.Error())
		return
	}
	verify := h.helloAuthFn()
	if verify == nil {
		h.metrics.helloAdmissionRefused.Add(1)
		_ = conn.WriteFrame(Frame{Opcode: OpBye, Payload: []byte{ByeReasonUnauthorized}})
		_ = conn.Close("HELLO admission verifier unavailable")
		return
	}
	admissionIdentity, verifyErr := verify(hello.AdmissionToken)
	if verifyErr != nil || admissionIdentity.AccountID == "" || admissionIdentity.ShardID == "" {
		h.metrics.helloAdmissionRefused.Add(1)
		log.WithFields(log.Fields{"kind": conn.Kind(), "remote": conn.RemoteAddr(), "error": verifyErr}).
			Warn("transport: HELLO admission refused")
		_ = conn.WriteFrame(Frame{Opcode: OpBye, Payload: []byte{ByeReasonUnauthorized}})
		_ = conn.Close("HELLO admission refused")
		return
	}

	resumePresented := len(hello.ResumeToken) == ResumeTokenLen
	if resumePresented {
		h.metrics.resumeAttempted.Add(1)
		var token [ResumeTokenLen]byte
		copy(token[:], hello.ResumeToken)
		if sess := h.lookupByToken(token); sess != nil {
			if !sess.BindAdmissionIdentity(
				admissionIdentity.AccountID,
				admissionIdentity.ShardID,
			) {
				h.metrics.helloAdmissionRefused.Add(1)
				_ = conn.WriteFrame(Frame{Opcode: OpBye, Payload: []byte{ByeReasonUnauthorized}})
				_ = conn.Close("resume admission identity mismatch")
				return
			}
			if err := sess.attach(conn, true); err == nil {
				h.metrics.helloAdmissionAccepted.Add(1)
				h.metrics.resumeAccepted.Add(1)
				h.metrics.handshakes.record(time.Since(startedAt))
				h.countAttach(conn.Kind())
				h.fireResumed(sess)
				return
			}
			// The session closed between lookup and attach; fall through to a
			// fresh session so the client still gets in.
		} else {
			// Detection only: without this line a failed resume and a fresh
			// token-less connect are indistinguishable in the log. The token
			// was already deleted before this point, so the cause is not
			// knowable here; known causes include grace expiry, single-bind
			// eviction and process restart (an OPEN set, not a taxonomy) and
			// are attributed by joining on the surrounding detach / eviction
			// / boot lines. The message deliberately names no cause.
			log.WithFields(log.Fields{"kind": conn.Kind(), "remote": conn.RemoteAddr()}).
				Info("transport: resume token presented but unknown; issuing fresh session (see surrounding detach/eviction/boot lines for cause)")
		}
	}
	if resumePresented {
		h.metrics.resumeFresh.Add(1)
	}

	sess, err := h.createSession()
	if err != nil {
		reason := ByeReasonShutdown
		if errors.Is(err, ErrSessionCapacity) {
			reason = ByeReasonServerBusy
		}
		_ = conn.WriteFrame(Frame{Opcode: OpBye, Payload: []byte{reason}})
		_ = conn.Close(err.Error())
		return
	}
	if !sess.BindAdmissionIdentity(
		admissionIdentity.AccountID,
		admissionIdentity.ShardID,
	) {
		h.closeSession(sess, errors.New("transport: admission identity install failed"))
		_ = conn.Close("admission identity install failed")
		return
	}
	h.admitAccountSession(admissionIdentity.AccountID, sess)
	if err := sess.attach(conn, false); err != nil {
		h.closeSession(sess, err)
		_ = conn.Close(err.Error())
		return
	}
	h.metrics.helloAdmissionAccepted.Add(1)
	h.metrics.handshakes.record(time.Since(startedAt))
	h.countAttach(conn.Kind())
	h.fireOpen(sess)
}

// lookupByToken resolves a presented resume token by EXACT map lookup —
// membership is O(1), not a scan of every live token (the old
// ConstantTimeCompare-over-all-entries loop cost O(sessions) per HELLO).
// The constant-time comparison is kept where it guards the secret: the
// presented token against the matched entry's stored token. What the map
// lookup does NOT hide is existence timing (hit vs miss), but a resume
// token is a bearer credential — 16 bytes from crypto/rand, presented
// whole, never compared byte-by-byte against an attacker-controlled
// prefix oracle — so learning "some token exists" faster than "none does"
// yields nothing guessable; this is the standard shape for token tables.
func (h *Hub) lookupByToken(token [ResumeTokenLen]byte) *Session {
	h.mu.RLock()
	defer h.mu.RUnlock()
	sess, ok := h.byToken[token]
	if !ok {
		return nil
	}
	if subtle.ConstantTimeCompare(sess.resumeToken[:], token[:]) != 1 {
		return nil
	}
	return sess
}

// ErrSessionCapacity reports that the bounded live-session registry is full.
// Callers may retry after another session reaches final teardown.
var ErrSessionCapacity = errors.New("transport: live session capacity reached")

func (h *Hub) createSession() (*Session, error) {
	var token [ResumeTokenLen]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, fmt.Errorf("transport: generating resume token: %w", err)
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, errors.New("transport: hub shutting down")
	}
	if len(h.sessions) >= h.cfg.MaxSessions {
		h.mu.Unlock()
		return nil, ErrSessionCapacity
	}
	id := h.nextID.Add(1)
	sess := newSession(h, id, token)
	h.sessions[id] = sess
	h.byToken[token] = sess
	h.mu.Unlock()
	h.metrics.sessOpened.Add(1)
	return sess, nil
}

// SessionsInDivision snapshots the live sessions whose effective division
// (Session.Set under "mission.divisionId", falling back to "divisionId")
// equals division. Division pushes iterate this instead of every session.
func (h *Hub) SessionsInDivision(division string) []*Session {
	h.mu.RLock()
	defer h.mu.RUnlock()
	set := h.divisions[division]
	out := make([]*Session, 0, len(set))
	for _, s := range set {
		out = append(out, s)
	}
	return out
}

// CharacterSessions returns the sessions in division bound to characterName
// (ASCII case-insensitive, as character names compare). It reads only the hub
// index and each session's binding, never game state, so a producer may call
// it while holding any game lock.
func (h *Hub) CharacterSessions(division, characterName string) []*Session {
	var out []*Session
	for _, s := range h.SessionsInDivision(division) {
		if _, bound, ok := s.CharacterBinding(); ok && strings.EqualFold(bound, characterName) {
			out = append(out, s)
		}
	}
	return out
}

// Population returns the number of live sessions indexed into one shard.
// It does not allocate a session snapshot, so heartbeat reporting stays
// constant-work apart from the map lookup.
func (h *Hub) Population(division string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.divisions[division])
}

// reindexDivision recomputes one session's effective division and moves it
// between division sets. Called by Session.Set whenever a division key
// changes; a session no longer in the registry (racing its own close) is
// left out so the index can never resurrect a closed session.
func (h *Hub) reindexDivision(s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, live := h.sessions[s.ID]; !live {
		return
	}
	div := s.effectiveDivision()
	old, had := h.sessionDiv[s.ID]
	if had && old == div {
		return
	}
	if had {
		h.dropFromDivisionLocked(old, s.ID)
	}
	if div == "" {
		delete(h.sessionDiv, s.ID)
		return
	}
	h.sessionDiv[s.ID] = div
	set := h.divisions[div]
	if set == nil {
		set = make(map[uint64]*Session)
		h.divisions[div] = set
	}
	set[s.ID] = s
}

// dropFromDivisionLocked removes one member from a division set. Caller
// holds h.mu.
func (h *Hub) dropFromDivisionLocked(division string, id uint64) {
	set := h.divisions[division]
	if set == nil {
		return
	}
	delete(set, id)
	if len(set) == 0 {
		delete(h.divisions, division)
	}
}

// closeSession finishes a session exactly once: final teardown, registry
// removal, close hooks.
func (h *Hub) closeSession(s *Session, cause error) {
	if !s.closeNow() {
		return
	}
	h.mu.Lock()
	delete(h.sessions, s.ID)
	delete(h.byToken, s.resumeToken)
	if key, ok := h.bindingKeys[s.ID]; ok {
		delete(h.bindingKeys, s.ID)
		if h.bindings[key] == s {
			delete(h.bindings, key)
		}
	}
	if div, ok := h.sessionDiv[s.ID]; ok {
		delete(h.sessionDiv, s.ID)
		h.dropFromDivisionLocked(div, s.ID)
	}
	if account, ok := h.sessionAccount[s.ID]; ok {
		delete(h.sessionAccount, s.ID)
		delete(h.accountSessions[account], s.ID)
		if len(h.accountSessions[account]) == 0 {
			delete(h.accountSessions, account)
		}
	}
	h.mu.Unlock()

	h.metrics.sessClosed.Add(1)
	switch {
	case cause == nil:
		h.metrics.closedClean.Add(1)
	case errors.Is(cause, errGraceExpired):
		h.metrics.closedGrace.Add(1)
	case errors.Is(cause, errSlowConsumer):
		h.metrics.closedSlow.Add(1)
	case errors.Is(cause, errHandlerPanic):
		h.metrics.closedPanic.Add(1)
	default:
		h.metrics.closedOther.Add(1)
	}

	log.WithFields(log.Fields{"session": s.ID, "cause": fmt.Sprint(cause)}).
		Info("transport: session closed")
	for _, fn := range h.hooks.closeSnapshot() {
		func() {
			defer recoverHookPanic(s, "OnSessionClose")
			fn(s, cause)
		}()
	}
}

/*
================
admitAccountSession

Records a fresh session under its admission account and, when the account
now holds more than MaxSessionsPerAccount, evicts its oldest sessions the
way single-bind replacement does: lame duck first, then BYE Replaced and
teardown once drained. Sessions are ordered by ID, which only increases.
================
*/
func (h *Hub) admitAccountSession(account string, s *Session) {
	var evict []*Session
	h.mu.Lock()
	owned := h.accountSessions[account]
	if owned == nil {
		owned = make(map[uint64]*Session)
		h.accountSessions[account] = owned
	}
	owned[s.ID] = s
	h.sessionAccount[s.ID] = account
	// Sessions already evicted stay indexed until their drain finishes; only
	// the ones still live count against the limit.
	live := 0
	for _, candidate := range owned {
		if !candidate.evicted.Load() {
			live++
		}
	}
	for ; live > h.cfg.MaxSessionsPerAccount; live-- {
		var oldest *Session
		for id, candidate := range owned {
			if id == s.ID || candidate.evicted.Load() {
				continue
			}
			if oldest == nil || id < oldest.ID {
				oldest = candidate
			}
		}
		if oldest == nil {
			break
		}
		oldest.markEvicted()
		evict = append(evict, oldest)
	}
	h.mu.Unlock()
	for _, victim := range evict {
		h.metrics.accountSessionEvictions.Add(1)
		log.WithFields(log.Fields{"account": account, "old": victim.ID, "new": s.ID, "limit": h.cfg.MaxSessionsPerAccount}).
			Warn("transport: account session limit reached, evicting its oldest session")
		victim.CloseWhenDrained(ByeReasonReplaced)
	}
}

func (h *Hub) fireOpen(s *Session) {
	for _, fn := range h.hooks.openSnapshot() {
		func() {
			defer recoverHookPanic(s, "OnSessionOpen")
			fn(s)
		}()
	}
}

func (h *Hub) fireResumed(s *Session) {
	for _, fn := range h.hooks.resumedSnapshot() {
		func() {
			defer recoverHookPanic(s, "OnSessionResumed")
			fn(s)
		}()
	}
}

func recoverHookPanic(s *Session, hook string) {
	if r := recover(); r != nil {
		log.WithFields(log.Fields{"session": s.ID, "hook": hook, "panic": r}).
			Error("transport: hook panicked")
	}
}

// dispatch routes one game frame. A handler panic closes the session rather
// than the server.
func (h *Hub) dispatch(s *Session, f Frame) {
	// Per-session inbound budget (ratelimit.go): every dispatched frame
	// costs one token, so a flooding client is shed HERE — before the
	// evicted check's per-frame log line and before any handler burns CPU.
	// The frame is dropped, never the session.
	if !s.admitFrame(f.Opcode, f.EncodedLen()) {
		return
	}

	if s.evicted.Load() {
		// Lame duck: a replaced session must not drive the
		// character it lost while its BYE drains. Everything dispatchable
		// (game opcodes AND control extensions like a fresh EnterWorld) is
		// dropped; PING/PONG/BYE are handled in readLoop and still flow.
		log.WithFields(log.Fields{"session": s.ID, "opcode": fmt.Sprintf("0x%04X", f.Opcode)}).
			Info("transport: dropping frame from evicted session")
		return
	}

	// Identity gate: every OpEnterWorld must decode and pass the installed
	// verifier before game code sees it. Server.Start prevents a live server
	// from reaching this branch without a verifier; the explicit nil refusal
	// keeps directly attached test/in-process sessions fail-closed too.
	if f.Opcode == OpEnterWorld {
		auth := h.enterWorldAuthFn()
		ew, err := DecodeEnterWorld(f.Payload)
		if auth == nil || err != nil {
			h.refuseEnterWorldAuth(s, enterWorldUnauthorizedCode, err)
			return
		}
		if ok, denyCode := auth(s, ew); !ok {
			h.refuseEnterWorldAuth(s, denyCode, nil)
			return
		}
	}

	fn := h.handlers.lookup(f.Opcode)
	if fn == nil {
		h.metrics.unhandledFrames.Add(1)
		log.WithFields(log.Fields{"session": s.ID, "opcode": fmt.Sprintf("0x%04X", f.Opcode)}).
			Debug("transport: unhandled opcode")
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.WithFields(log.Fields{"session": s.ID, "opcode": fmt.Sprintf("0x%04X", f.Opcode), "panic": r}).
				Error("transport: handler panicked, closing session")
			h.closeSession(s, fmt.Errorf("%w on 0x%04X: %v", errHandlerPanic, f.Opcode, r))
		}
	}()
	fn(s, f.Opcode, f.Payload)
}

const enterWorldUnauthorizedCode uint32 = 0x00A1

func (h *Hub) refuseEnterWorldAuth(s *Session, denyCode uint32, decodeErr error) {
	h.metrics.enterWorldAuthRefused.Add(1)
	fields := log.Fields{"session": s.ID, "denyCode": denyCode}
	if decodeErr != nil {
		fields["error"] = decodeErr
	}
	log.WithFields(fields).Warn("transport: EnterWorld refused by auth gate")
	_ = s.Send(OpEnterWorldResult, EncodeEnterWorldResult(EnterWorldResult{
		NativeErrorCode: denyCode,
	}))
}

// shutdown says BYE to every session and waits for them to drain, capped by
// ctx.
func (h *Hub) shutdown(ctx context.Context) {
	h.mu.Lock()
	h.closed = true
	sessions := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		sessions = append(sessions, s)
	}
	h.mu.Unlock()

	for _, s := range sessions {
		s.CloseWhenDrained(ByeReasonShutdown)
	}
	for _, s := range sessions {
		select {
		case <-s.Done():
		case <-ctx.Done():
			h.closeSession(s, errors.New("transport: shutdown deadline"))
		}
	}
}
