/*
===========================================================================

session.go - connection attachment, queues and transport IO

===========================================================================
*/
package transport

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

var (
	// ErrSessionClosed is returned by Send once the session is gone for good.
	ErrSessionClosed = errors.New("transport: session closed")
	// ErrSessionEvicted is returned by the Send family for game frames once
	// the session lost its bind key: a lame-duck drain flushes
	// only session-internal control (the BYE, PONGs) and then closes.
	ErrSessionEvicted = errors.New("transport: session evicted, game frames refused")
	// errGraceExpired closes a detached session whose client never came back.
	errGraceExpired = errors.New("transport: resume grace period expired")
	// errSlowConsumer closes a session whose outbound queue overflowed.
	errSlowConsumer = errors.New("transport: outbound queue overflow (slow consumer)")
	// errOutboundBurstTooLarge identifies one server-produced transaction
	// that cannot fit inside the configured per-session queue bounds.
	errOutboundBurstTooLarge = errors.New("transport: outbound burst exceeds bounded queue capacity")
	// errIdle detaches a connection that stopped answering keepalives.
	errIdle = errors.New("transport: idle timeout")
	// errHandlerPanic closes a session whose game handler panicked; a
	// sentinel so the close-reason metrics can classify it.
	errHandlerPanic = errors.New("transport: handler panic")
)

// Session is one logical client, surviving transport reconnects. Game
// handlers receive it in dispatch and use Send / SendUnreliable to push
// frames. Its player context is explicit: arbitrary game state cannot be
// attached to the transport lifecycle.
/*
================
Session
================
*/
type Session struct {
	ID uint64

	hub *Hub

	// account is the admitted account, stored once by admitAccountSession
	// before the session dispatches; nil for sessions attached without a
	// HELLO admission (in-process tests).
	account atomic.Pointer[string]

	// Dispatch spans the complete handler, including publication. Attach may
	// replace the reader while an old handler still owns a committed result.
	dispatchMu sync.Mutex

	mu              sync.Mutex
	cond            *sync.Cond
	conn            Conn // nil while detached
	gen             int  // bumped on every attach; loops check it before acting
	attached        bool
	closed          bool
	sceneRevision   uint64
	sceneLoading    bool
	observedObjects map[uint32]struct{}
	// publishedSorted is observedObjects in ascending order, rebuilt into a
	// fresh array on the first PublishedObjects read after any change marks
	// it stale. Readers share it, so it is never written once built.
	publishedSorted []uint32
	publishedStale  bool

	queue       []Frame
	queueBytes  int
	drainClose  bool         // close the session once the queue flushes
	closeReason *CloseReason // retained even when teardown is triggered by a drain timeout

	// The lossy lane: loss-tolerant frames coalesced per (opcode, key) so a
	// WebSocket client under backlog gets the LATEST position instead of a
	// pile of stale ones (and the queue cap never kills the session over
	// superseded data). Only SendUnreliableKeyed feeds it.
	lossy      map[lossySlot]Frame
	lossyKeys  []lossySlot // FIFO of pending slots, deduped
	lossyBytes int

	// Exactly-once ordered delivery across a resume (stream_ring.go):
	// streamPos counts the frames written on the reliable stream above the
	// transport range; ring keeps the ordered ones until acknowledged;
	// ringFloor is the newest position the ring has had to drop.
	streamPos uint32
	ring      []ringFrame
	ringBytes int
	ringFloor uint32

	resumeToken [ResumeTokenLen]byte
	// admissionCommit consumes the HELLO ticket this attachment was
	// admitted with. The write loop runs it after the WELCOME frame is
	// written and clears it under mu first, so it runs at most once
	// per arming; nil means the ticket was already consumed upstream
	// (the legacy HELLO-time verifier) or none is pending.
	admissionCommit func() error
	lastRecv        time.Time
	readCancel      context.CancelFunc

	// Telemetry of the current attachment (#1449): the transport kind of
	// the latest attach and the fallback reason the client reported for
	// it ("" until an OpTransportReport arrives). History reads both;
	// they describe how the session got here, never how it behaves.
	transport   string
	fallback    string
	expireTimer *time.Timer
	drainTimer  *time.Timer // CloseWhenDrained's cap; stopped once the session closes

	// evicted is the one-way lame-duck latch: set under h.mu
	// when BindExclusive replaces this session. From that instant the hub
	// drops its inbound game frames, the Send family refuses outbound game
	// frames, and the mission bridge skips it — the BYE drain is all that
	// remains. Atomic so no reader ever needs s.mu or h.mu.
	evicted atomic.Bool

	done chan struct{} // closed exactly once at final teardown

	// sceneReadyHooks run once, in order, when the current scene finishes
	// loading (FinishSceneReentry). Owners that hold requests during
	// loading register one to replay them into the ready scene.
	sceneReadyHooks []func()

	// limiter is the per-session inbound dispatch budget (ratelimit.go).
	// Owned by the session so its state dies with the session — churn can
	// never accumulate limiter state anywhere. nil = limiting disabled.
	limiter *frameLimiter

	player sessionPlayerContext
	// Native command-source restrictions belong to the logical connection, not
	// a character snapshot; reconnect and character binding retain them.
	restrictions sessionCommandRestrictions
}

// lossySlot identifies one coalesce slot on the lossy lane.
/*
================
lossySlot
================
*/
type lossySlot struct {
	Opcode uint16
	Key    uint64
}

/*
================
newSession
================
*/
func newSession(hub *Hub, id uint64, token [ResumeTokenLen]byte) *Session {
	s := &Session{
		ID:          id,
		hub:         hub,
		resumeToken: token,
		done:        make(chan struct{}),
		lossy:       make(map[lossySlot]Frame),
	}
	if hub.cfg.RateLimitPerSec > 0 {
		s.limiter = newFrameLimiter(hub.cfg.RateLimitPerSec, hub.cfg.RateLimitBurst)
	}
	s.cond = sync.NewCond(&s.mu)
	go s.writeLoop()
	go s.keepaliveLoop()
	return s
}

// Kind reports the current transport ("webtransport", "websocket") or
// "detached".
/*
================
Kind
================
*/
func (s *Session) Kind() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return "detached"
	}
	return s.conn.Kind()
}

/*
================
noteTransportReport

Records the fallback reason the client reported for this attachment
(OpTransportReport, #1449) and journals it as its own lifecycle row, so
the session history shows how the session got here. A report about a
stale attachment never lands: the read loop only runs for the current
generation, and a closed session keeps its final record.
================
*/
func (s *Session) noteTransportReport(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.fallback = name
	s.recordHistoryLocked("transport_reported", nil)
}

// RemoteAddr is the peer of the current connection, or "" while detached.
/*
================
RemoteAddr
================
*/
func (s *Session) RemoteAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return ""
	}
	return s.conn.RemoteAddr()
}

// metricsSnapshot returns the two session-local gauges used by Hub.Metrics.
// It deliberately exposes no mutable queue storage.
/*
================
metricsSnapshot
================
*/
func (s *Session) metricsSnapshot() (attached bool, queueDepth, queueBytes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attached, len(s.queue) + len(s.lossyKeys), s.queueBytes + s.lossyBytes
}

// Done is closed when the session is torn down for good.
/*
================
Done
================
*/
func (s *Session) Done() <-chan struct{} { return s.done }

// markEvicted flips the session into lame-duck mode. One-way; called by
// Hub.BindExclusive when another session takes this session's bind key.
/*
================
markEvicted
================
*/
func (s *Session) markEvicted() { s.evicted.Store(true) }

// Evicted reports whether the session lost its bind key to a replacement
// (single-session bind, BYE Replaced) and is now a lame duck: inbound game
// frames are dropped, outbound game frames are refused, and the mission
// tick no longer sees it. Control frames still flow so the BYE drain works.
/*
================
Evicted
================
*/
func (s *Session) Evicted() bool { return s.evicted.Load() }

// Send queues a frame on the reliable, ordered channel. It never blocks on
// the network; a queue overflow closes the session (slow consumer) and
// subsequent Sends fail with ErrSessionClosed. The payload is copied, so
// callers may reuse their buffer immediately.
/*
================
Send
================
*/
func (s *Session) Send(opcode uint16, payload []byte) error {
	if opcode == 0 {
		s.hub.refuseEmptyFrame(s.ID, 1)
		return ErrEmptyFrame
	}
	if opcode > maxReservedOpcode && s.evicted.Load() {
		// Lame duck: the drain may flush only what the
		// session machinery itself queues (BYE, PONG); no new game or
		// control-extension frames reach a replaced client.
		return ErrSessionEvicted
	}
	owned := append([]byte(nil), payload...)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrSessionClosed
	}
	frameBytes := 2 + len(owned)
	if len(s.queue) >= s.hub.cfg.OutboundQueue ||
		s.queueBytes+s.lossyBytes > s.hub.cfg.OutboundQueueBytes-frameBytes {
		s.mu.Unlock()
		s.hub.closeSessionFromSend(s, errSlowConsumer)
		return errSlowConsumer
	}
	s.queue = append(s.queue, Frame{Opcode: opcode, Payload: owned})
	s.queueBytes += frameBytes
	s.hub.observeQueueDepth(len(s.queue)+len(s.lossyKeys), s.queueBytes+s.lossyBytes)
	s.cond.Broadcast()
	s.mu.Unlock()
	return nil
}

// SendUnreliable pushes a frame on the unreliable channel when the client
// is on WebTransport (QUIC datagram, safe from any goroutine) and falls
// back to the reliable queue on WebSocket. Use for per-tick superseded
// state only; when the frames supersede each other per entity, prefer
// SendUnreliableKeyed so the WebSocket path coalesces.
//
// Only IsLossTolerantOpcode frames may travel unreliably (freeze rule);
// any other opcode is transparently routed onto the reliable lane.
/*
================
SendUnreliable
================
*/
func (s *Session) SendUnreliable(opcode uint16, payload []byte) error {
	if opcode > maxReservedOpcode && s.evicted.Load() {
		return ErrSessionEvicted // lame duck: no ticks on any lane
	}
	if !IsLossTolerantOpcode(opcode) {
		return s.Send(opcode, payload)
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrSessionClosed
	}
	conn := s.conn
	s.mu.Unlock()
	if conn != nil && conn.SupportsUnreliable() {
		err := conn.WriteUnreliable(Frame{Opcode: opcode, Payload: payload})
		if err == nil {
			s.hub.metrics.datagramsOut.Add(1)
			s.hub.metrics.datagramBytesOut.Add(uint64(2 + len(payload)))
			return nil
		}
		// Datagram refused (too big for the path MTU, or transient): the
		// frame still matters, deliver it reliably.
	}
	return s.Send(opcode, payload)
}

// SendUnreliableKeyed is SendUnreliable for per-entity superseded state
// (movement ticks: key = entity gid). On WebTransport it goes out as a
// datagram now; on WebSocket (or while detached) it lands in a coalesce
// slot per (opcode, key), so only the LATEST frame per entity is delivered
// once the connection drains, and backlog can never close the session over
// stale positions. The payload is copied.
//
// Only IsLossTolerantOpcode frames may travel unreliably or coalesce
// (coalescing DROPS superseded frames, which must never happen to
// must-deliver traffic); anything else routes onto the reliable lane.
/*
================
SendUnreliableKeyed
================
*/
func (s *Session) SendUnreliableKeyed(opcode uint16, key uint64, payload []byte) error {
	return s.sendSceneUnreliableKeyed(opcode, key, payload, nil)
}

// pushFront queues a frame ahead of everything pending; only the WELCOME
// uses it so a resuming client always hears WELCOME first.
/*
================
pushFront
================
*/
func (s *Session) pushFront(f Frame) {
	s.queue = append([]Frame{f}, s.queue...)
	s.queueBytes += f.EncodedLen()
	s.hub.observeQueueDepth(len(s.queue)+len(s.lossyKeys), s.queueBytes+s.lossyBytes)
	s.cond.Broadcast()
}

/*
================
armAdmissionCommit

Arms the one-use HELLO ticket consumption for the WELCOME the coming
attach writes (nil disarms). The hub calls it before attach, so the
commit can never be missed by a fast writer.
================
*/
func (s *Session) armAdmissionCommit(commit func() error) {
	s.mu.Lock()
	s.admissionCommit = commit
	s.mu.Unlock()
}

// attach binds a new connection to the session and starts its read loop.
// Caller must NOT hold s.mu.
/*
================
attach
================
*/
func (s *Session) attach(conn Conn, resumed bool, lastApplied uint32) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrSessionClosed
	}
	// A resume serves exactly the ordered frames past the client's position
	// (stream_ring.go); one the ring cannot cover is refused, and the hub
	// issues a fresh session instead of letting a lost frame pass silently.
	if resumed && !s.resumeFromLocked(lastApplied) {
		s.mu.Unlock()
		return errResumeGap
	}
	if s.attached {
		// Zombie replacement: the old transport thinks it is alive (half-open
		// TCP, stale WT session). The new connection wins.
		old := s.conn
		s.attached = false
		s.conn = nil
		if s.readCancel != nil {
			s.readCancel()
		}
		s.mu.Unlock()
		if old != nil {
			old.Close("replaced by new connection")
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return ErrSessionClosed
		}
	}
	if s.expireTimer != nil {
		s.expireTimer.Stop()
		s.expireTimer = nil
	}
	s.gen++
	gen := s.gen
	s.conn = conn
	s.attached = true
	// A new attachment replaces the old one's telemetry: the fallback a
	// previous connection reported says nothing about this one (#1449).
	s.transport = conn.Kind()
	s.fallback = ""
	s.lastRecv = time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	s.readCancel = cancel
	s.pushFront(Frame{Opcode: OpWelcome, Payload: EncodeWelcome(Welcome{
		Version:     ProtocolVersion,
		Resumed:     resumed,
		SessionID:   s.ID,
		ResumeToken: s.resumeToken[:],
	})})
	kind := "connected"
	if resumed {
		kind = "resumed"
	}
	s.recordHistoryLocked(kind, nil)
	s.mu.Unlock()

	go s.readLoop(conn, gen, ctx)
	log.WithFields(log.Fields{
		"session": s.ID, "kind": conn.Kind(), "remote": conn.RemoteAddr(), "resumed": resumed,
	}).Info("transport: session attached")
	return nil
}

// detachGen moves the session into the detached (resumable) state if gen is
// still the current attachment. Stale loops calling in after a resume are
// no-ops. Caller must NOT hold s.mu.
/*
================
detachGen
================
*/
func (s *Session) detachGen(gen int, cause error) {
	s.mu.Lock()
	if s.closed || gen != s.gen || !s.attached {
		s.mu.Unlock()
		return
	}
	conn := s.conn
	s.attached = false
	s.conn = nil
	if s.readCancel != nil {
		s.readCancel()
		s.readCancel = nil
	}
	// A condemned session (CloseWhenDrained) is never resumable, and with no
	// connection its BYE can never leave: close it now, as CloseWhenDrained
	// does for a session that is already detached, instead of sitting out
	// the drain cap. The cap still bounds a live client that reads slowly.
	condemned := s.drainClose
	grace := s.hub.cfg.GracePeriod
	if !condemned {
		s.expireTimer = time.AfterFunc(grace, func() { s.expireIfDetached() })
	}
	s.recordHistoryLocked("detached", cause)
	s.cond.Broadcast()
	s.mu.Unlock()
	s.hub.metrics.detaches.Add(1)
	if errors.Is(cause, errIdle) {
		s.hub.metrics.detachesIdle.Add(1)
	} else {
		s.hub.metrics.detachesOther.Add(1)
	}

	if conn != nil {
		conn.Close("detached")
	}
	if condemned {
		// closeSession takes s.mu via closeNow; never call it locked.
		s.hub.closeSession(s, nil)
		return
	}
	log.WithFields(log.Fields{"session": s.ID, "cause": fmt.Sprint(cause), "grace": grace}).
		Info("transport: session detached, awaiting resume")
}

/*
================
expireIfDetached
================
*/
func (s *Session) expireIfDetached() {
	s.mu.Lock()
	if s.closed || s.attached {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.hub.closeSession(s, errGraceExpired)
}

// closeNow is the final teardown. Returns false if already closed. Caller
// must NOT hold s.mu. Hooks and map removal are the hub's job.
/*
================
closeNow
================
*/
func (s *Session) closeNow(cause error) (bool, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false, cause
	}
	s.closed = true
	if cause == nil && s.closeReason != nil {
		cause = *s.closeReason
	}
	s.recordHistoryLocked("ended", cause)
	conn := s.conn
	s.conn = nil
	s.attached = false
	if s.readCancel != nil {
		s.readCancel()
		s.readCancel = nil
	}
	if s.expireTimer != nil {
		s.expireTimer.Stop()
		s.expireTimer = nil
	}
	if s.drainTimer != nil {
		// The drain finished (or the session died some other way) before
		// the cap: stop the timer instead of letting it fire into the
		// (idempotent, but pointless) closeSession two seconds from now.
		s.drainTimer.Stop()
		s.drainTimer = nil
	}
	s.queue = nil
	s.queueBytes = 0
	s.lossy = nil
	s.lossyKeys = nil
	s.lossyBytes = 0
	s.cond.Broadcast()
	s.mu.Unlock()

	if conn != nil {
		conn.Close("session closed")
	}
	close(s.done)
	return true, cause
}

// CloseWhenDrained queues a BYE and tears the session down once the queue
// has flushed (or after a short cap, if the client stopped reading). A
// DETACHED session has no connection to drain to — the writeLoop only
// flushes while attached, so the BYE could never leave and teardown would
// just sit out the cap — so it is closed immediately instead. That also
// kills its resume token on the spot: a condemned session must not be
// resumable, so a client racing a resume against this close falls back to
// a fresh session (AcceptConn already mints one for unknown tokens). If an
// attach does win the race between the unlock below and closeSession, both
// interleavings stay safe: attach refuses a closed session, and
// closeSession is idempotent (closeNow returns false once closed).
/*
================
CloseWhenDrained
================
*/
func (s *Session) CloseWhenDrained(reason uint8) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if s.closeReason == nil {
		s.closeReason = &CloseReason{Reason: reason}
	}
	if !s.attached {
		// closeSession takes s.mu via closeNow; never call it locked.
		s.mu.Unlock()
		s.hub.closeSession(s, nil)
		return
	}
	s.queue = append(s.queue, Frame{Opcode: OpBye, Payload: []byte{reason}})
	s.queueBytes += 3
	s.hub.observeQueueDepth(len(s.queue)+len(s.lossyKeys), s.queueBytes+s.lossyBytes)
	s.drainClose = true
	if s.drainTimer != nil {
		// A second CloseWhenDrained (eviction then shutdown, say) re-arms
		// the cap; the superseded timer is stopped rather than left to
		// fire a redundant (idempotent) close.
		s.drainTimer.Stop()
	}
	s.drainTimer = time.AfterFunc(2*time.Second, func() {
		s.hub.closeSession(s, nil)
	})
	s.cond.Broadcast()
	s.mu.Unlock()
}

// writeLoop is the session's single writer for its whole life. It drains
// the ordered queue first, then the lossy coalesce slots, whenever a
// connection is attached; re-queues an ordered frame at the front when a
// write fails against the same attachment (lossy frames are simply lost);
// and exits at final close.
/*
================
writeLoop
================
*/
func (s *Session) writeLoop() {
	for {
		s.mu.Lock()
		for !s.closed && (!s.attached || (len(s.queue) == 0 && len(s.lossyKeys) == 0)) {
			s.cond.Wait()
		}
		if s.closed {
			s.mu.Unlock()
			return
		}
		var f Frame
		ordered := len(s.queue) > 0
		if ordered {
			f = s.queue[0]
			s.queue[0] = Frame{} // release payload references in the consumed backing array
			s.queue = s.queue[1:]
			s.queueBytes -= f.EncodedLen()
		} else {
			slot := s.lossyKeys[0]
			s.lossyKeys = s.lossyKeys[1:]
			f = s.lossy[slot]
			delete(s.lossy, slot)
			s.lossyBytes -= f.EncodedLen()
		}
		drainAndEmpty := s.drainClose && len(s.queue) == 0
		conn := s.conn
		gen := s.gen
		pos := s.noteWrittenLocked(f, ordered)
		s.mu.Unlock()

		if f.Current != nil && !f.Current() {
			s.mu.Lock()
			s.unnoteLocked(pos, ordered)
			s.mu.Unlock()
			if ordered && drainAndEmpty {
				s.hub.closeSession(s, nil)
				return
			}
			continue
		}
		if err := conn.WriteFrame(f); err != nil {
			s.hub.metrics.writeErrors.Add(1)
			// No re-queue: a write can fail after the bytes reached the
			// client (a deadline on a stalled link), and re-sending it then
			// applied it twice (#1466). It already holds its position: the
			// client's resume position says whether it arrived, and the ring
			// re-sends it only if it did not.
			s.detachGen(gen, err)
			continue
		}
		s.hub.recordFrameOut(f)
		if f.Opcode == OpWelcome {
			s.mu.Lock()
			commit := s.admissionCommit
			s.admissionCommit = nil
			s.mu.Unlock()
			if commit != nil {
				if err := commit(); err != nil {
					// The ticket this session was admitted with was
					// consumed by a concurrent admission: this session
					// must not outlive its WELCOME.
					log.WithFields(log.Fields{
						"session": s.ID, "error": err,
					}).Warn("transport: admission consumption failed after WELCOME; closing session")
					s.hub.closeSession(s, err)
					return
				}
			}
		}
		if ordered && drainAndEmpty {
			s.hub.closeSession(s, nil)
			return
		}
	}
}

// readLoop pulls frames off one attachment. Transport-control frames are
// handled here; application dispatch is serialized across all attachments.
/*
================
readLoop
================
*/
func (s *Session) readLoop(conn Conn, gen int, ctx context.Context) {
	for {
		f, err := conn.ReadFrame(ctx)
		if err != nil {
			s.detachGen(gen, err)
			return
		}
		if f.EncodedLen() > MaxInboundFrameBytes {
			s.hub.closeSession(s, ErrInboundFrameTooLarge)
			return
		}
		s.hub.metrics.framesIn.Add(1)
		s.hub.metrics.bytesIn.Add(uint64(f.EncodedLen()))
		s.mu.Lock()
		current := !s.closed && gen == s.gen
		if current {
			s.lastRecv = time.Now()
		}
		s.mu.Unlock()
		if !current {
			return
		}
		if f.IsControl() {
			switch f.Opcode {
			case OpPing:
				if len(f.Payload) > MaxPingPayloadBytes {
					s.hub.closeSession(s, errors.New("transport: PING payload exceeds limit"))
					return
				}
				if !s.admitFrame(f.Opcode, f.EncodedLen()) {
					continue
				}
				_ = s.Send(OpPong, f.Payload)
			case OpPong:
				if len(f.Payload) > MaxPingPayloadBytes {
					s.hub.closeSession(s, errors.New("transport: PONG payload exceeds limit"))
					return
				}
				if !s.admitFrame(f.Opcode, f.EncodedLen()) {
					continue
				}
				// lastRecv already refreshed; nothing else to do.
			case OpStreamAck:
				pos, ok := DecodeStreamAck(f.Payload)
				if !ok {
					s.hub.closeSession(s, errors.New("transport: malformed stream ACK"))
					return
				}
				if !s.admitFrame(f.Opcode, f.EncodedLen()) {
					continue
				}
				s.mu.Lock()
				s.ackLocked(pos)
				s.mu.Unlock()
			case OpTransportReport:
				if !s.admitFrame(f.Opcode, f.EncodedLen()) {
					continue
				}
				// Telemetry only (#1449): a report this build cannot
				// decode is dropped, never a protocol fault.
				if name, ok := DecodeTransportReport(f.Payload); ok {
					s.noteTransportReport(name)
				}
			case OpBye:
				s.hub.closeSession(s, CloseReason{Reason: ByeReasonNormal})
				return
			case OpHello:
				// A second HELLO mid-session is a protocol violation.
				s.hub.closeSession(s, errors.New("transport: unexpected HELLO after handshake"))
				return
			default:
				// Control-extension opcodes (EnterWorld, sidecars, future)
				// go through normal dispatch; unknown ones fall out as
				// unhandled there, which keeps forward compatibility.
				s.dispatchGeneration(gen, f)
			}
			continue
		}
		s.dispatchGeneration(gen, f)
	}
}

/*
================
dispatchGeneration

An admitted handler finishes publishing before a resumed EnterWorld can
replace its snapshot. Readers superseded while waiting must not dispatch.
Neither socket reads nor writes run under this lock; Send only queues.
================
*/
func (s *Session) dispatchGeneration(gen int, f Frame) {
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	s.mu.Lock()
	current := !s.closed && gen == s.gen
	s.mu.Unlock()
	if current {
		s.hub.dispatch(s, f)
	}
}
