/*
===========================================================================

stream_ring.go - exactly-once ordered delivery across a resume (#1466)

Every frame the session writes on the reliable stream above the transport
range (opcode > maxReservedOpcode) advances the stream position; the client
counts the same frames as it drains them, so neither side needs a marker on
the wire. Ordered frames are kept in a bounded ring after their write until
the client acknowledges them (OpStreamAck) or resumes with the position it
last applied (Hello.LastApplied). On a resume the server re-sends exactly
the ring frames past that position, then the frames queued while detached.
Coalesced lossy frames advance the position but are never kept: a
superseded position is not re-sent.

A resume whose position the ring can no longer cover (an overflow, or a
client that presents no position) is refused, and the hub issues a fresh
session: a lost ordered frame must never pass silently.

The bound is enforced by pacing, not by dropping (#2146). While the
session is attached the writer waits for ack space when the ring is
full instead of numbering a frame whose arrival pushes the oldest
unacknowledged frame out: a drop moves ringFloor past the position the
client has applied, and from that moment every resume the session ever
makes is refused, although nothing is wrong with the link. Live, the
writer outran apply-then-ack during entry bursts and crowd churn until
91% of resumes presented positions the ring no longer covered. A client
that never acknowledges now degrades into the outbound queue's own
bound (the slow-consumer close) rather than a silently unresumable
session. One case sits outside the bound by design: a single frame
larger than the whole byte bound is held in the ring until the client
acknowledges it - dropping it would strand every client still
downloading it - and the writer waits behind it as behind any full
ring. The EnterWorld answer, the one frame that size live, is ringed
as a marker instead (below).

An unapplied EnterWorld answer is never re-sent. The browser asks for a
new entry on every resume, so a replayed answer was a second answer the
client refused, and the refusal itself kept it unacked for the next
resume (a reconnect loop). A resume drops it and everything behind it,
and the new entry's snapshot rebuilds that state (supersededEntryLocked).
Because it is never re-sent, the ring keeps the answer as a payload-free
marker at its position: the answer of a busy division is bigger than the
whole byte bound, and holding its bytes parked the writer, and the scene
behind it, until a slow client had downloaded all of it, past
RingParkBound into a detach and a fresh entry that parked again.

Positions are compared plainly, never wrap-aware (#1515 review): a session
would need four billion counted frames to reach the 2^32 wrap, so a
wrapped position can never meet itself. Revisit here only if per-session
frame volumes grow by orders of magnitude.

===========================================================================
*/
package transport

import (
	"errors"
)

// errResumeGap refuses a resume whose stream position the ring cannot serve.
var errResumeGap = errors.New("transport: resume position not covered")

const (
	// streamRingMaxFrames and streamRingMaxBytes bound the unacknowledged
	// window, and so the most a resume ever re-sends. The writer waits at
	// this bound (ringGateLocked); it is a flow-control threshold, not
	// the size of a window a live link may outgrow.
	streamRingMaxFrames = 8192
	streamRingMaxBytes  = 4 << 20
)

/*
================
ringFrame

An ordered frame already written, at its stream position.
================
*/
type ringFrame struct {
	pos uint32
	f   Frame
}

/*
================
CountsOnStream

Exported for the wire tests of other packages, whose fake clients present
the position they read on a resume. Whether a written frame advances the stream position. The client's network
owner consumes the transport range (WELCOME, PING, PONG, BYE) itself and
drains everything else, so both sides count exactly the frames above it.
================
*/
func CountsOnStream(opcode uint16) bool {
	return opcode > maxReservedOpcode
}

/*
================
noteWrittenLocked

Gives a frame the writer has just dequeued its stream position (and an
ordered one its ring slot) before it is written, under the lock that
dequeued it: a resume landing while the write is in flight then re-sends
it from the ring unless the client's position shows it arrived, and the
old write can never number it on the new stream. Returns the position, 0
when the frame does not count. Caller holds s.mu.
================
*/
func (s *Session) noteWrittenLocked(f Frame, ordered bool) uint32 {
	if !CountsOnStream(f.Opcode) {
		return 0
	}
	s.streamPos++
	pos := s.streamPos
	if !ordered {
		return pos
	}
	kept := f
	if f.Opcode == OpEnterWorldResult {
		// An EnterWorld answer is never re-sent (supersededEntryLocked),
		// so its slot keeps only the opcode that marks it: the
		// multi-megabyte payload held here parked the writer and every
		// frame behind it until the client had downloaded it all, and a
		// download slower than RingParkBound was detached as wedged.
		kept = Frame{Opcode: f.Opcode}
	}
	s.ring = append(s.ring, ringFrame{pos: s.streamPos, f: kept})
	s.ringBytes += kept.EncodedLen()
	for len(s.ring) > 1 && (len(s.ring) > streamRingMaxFrames || s.ringBytes > streamRingMaxBytes) {
		// The oldest unacknowledged frame falls out: a resume that still
		// needs it is refused instead of silently skipping it. The
		// writer's gate (ringGateLocked) keeps a live session from ever
		// reaching this; what reaches it is a numbering caller that
		// bypassed the writer, and the refusal stays honest there. The
		// newest frame never falls out here, however far past the bound
		// it sits: it leaves only when the client acknowledges it, so a
		// client still downloading it can always resume from the
		// position before it (#2146).
		s.ringFloor = s.ring[0].pos
		s.ringBytes -= s.ring[0].f.EncodedLen()
		s.ring[0] = ringFrame{}
		s.ring = s.ring[1:]
	}
	return pos
}

/*
================
unnoteLocked

Takes back the position a dequeued frame was given when it turns out not
to be written (its Current check says it went stale). Only the newest
position can be taken back; after a resume the frame is back in the queue
from the ring and is judged again when it is dequeued. Caller holds s.mu.
================
*/
func (s *Session) unnoteLocked(pos uint32, ordered bool) {
	if pos == 0 || pos != s.streamPos {
		return
	}
	s.streamPos--
	if ordered && len(s.ring) > 0 && s.ring[len(s.ring)-1].pos == pos {
		last := len(s.ring) - 1
		s.ringBytes -= s.ring[last].f.EncodedLen()
		s.ring[last] = ringFrame{}
		s.ring = s.ring[:last]
	}
}

/*
================
ackLocked

Drops the ring frames the client has applied. Freed room wakes the
writer when the ring gate held it (ringGateLocked, #2146), and the
advance restamps lastStreamAck: ack progress is what keeps a parked
writer inside its bound. A duplicate ack (n == 0) is not progress
and restamps nothing. Caller holds s.mu.
================
*/
func (s *Session) ackLocked(pos uint32) {
	if pos > s.streamPos {
		pos = s.streamPos
	}
	n := 0
	for n < len(s.ring) && s.ring[n].pos <= pos {
		s.ringBytes -= s.ring[n].f.EncodedLen()
		s.ring[n] = ringFrame{}
		n++
	}
	s.ring = s.ring[n:]
	if n > 0 {
		s.lastStreamAck = s.now()
		s.cond.Broadcast()
	}
}

/*
================
ringGateLocked

Whether the writer must wait for ack space before numbering the
ordered frame at the head of the queue: it advances the stream, and
the ring cannot take it without passing its frame or byte bound. A
waited frame stays queued; a dropped one (the pre-#2146 behaviour)
would advance ringFloor past the client's applied position and make
the session unresumable for the rest of its life. Frames in the
transport range are never gated - they take no position and no ring
slot. Caller holds s.mu.
================
*/
func (s *Session) ringGateLocked() bool {
	if len(s.queue) == 0 || len(s.ring) == 0 {
		// An empty ring always takes the frame: a single frame larger
		// than the whole byte bound could otherwise wait forever for
		// room that can never exist. It is written and held until the
		// client acknowledges it (noteWrittenLocked never drops the
		// newest frame); the writer waits behind it, and a resume from
		// before it re-sends it instead of refusing.
		return false
	}
	head := s.queue[0]
	if !CountsOnStream(head.Opcode) {
		return false
	}
	return len(s.ring) >= streamRingMaxFrames ||
		s.ringBytes+head.EncodedLen() > streamRingMaxBytes
}

/*
================
resumeFromLocked

Prepares a resume from the client's last applied position: the ring frames
past it are re-queued ahead of the frames queued while detached, and the
position restarts there (they are counted again as they are rewritten).
False when the position cannot be served - beyond what was written, or
older than a frame the ring has already dropped, or when the re-send
with everything already queued would pass the outbound byte bound (the
one capacity that still refuses: memory). The frame bound does not
refuse here: the re-sent block is retained ring state, not producer
output, and Session.Send's bound subtracts it while it drains
(resendFrames) - refusing it turned every mid-burst detach into a
fresh session even though the ring covered the position (#2146).
Caller holds s.mu.
================
*/
func (s *Session) resumeFromLocked(lastApplied uint32) bool {
	if lastApplied > s.streamPos || lastApplied < s.ringFloor {
		return false
	}
	s.ackLocked(lastApplied)
	cut := supersededEntryLocked(s.ring)
	if cut < len(s.ring) {
		s.dropSupersededEntryLocked(cut)
	} else if queued := queuedEntryLocked(s.queue); queued < len(s.queue) {
		s.dropQueuedEntryLocked(queued)
	}
	resendBytes := 0
	for _, r := range s.ring {
		resendBytes += r.f.EncodedLen()
	}
	if resendBytes+s.queueBytes+s.lossyBytes > s.hub.cfg.OutboundQueueBytes {
		return false
	}
	resendCount := len(s.ring)
	resend := make([]Frame, 0, resendCount+len(s.queue))
	for _, r := range s.ring {
		resend = append(resend, r.f)
		s.queueBytes += r.f.EncodedLen()
	}
	s.queue = append(resend, s.queue...)
	s.ring = nil
	s.ringBytes = 0
	s.streamPos = lastApplied
	s.resendFrames += resendCount
	s.resendBytes += resendBytes
	return true
}

/*
================
supersededEntryLocked

The index of the first EnterWorld answer the client has not applied, or
len(ring) when the ring holds none. A resumed browser asks for a new
entry on every resumed WELCOME (world.ts), so an unapplied answer is
already superseded: re-sending it hands the client two answers, and it
refuses the second with "Unexpected EnterWorld result" before acking it,
so every later resume replays it again (the post-#2338 reconnect loop).
The ring holds the answer as a payload-free marker (noteWrittenLocked);
this is the only reader that needs it, and it reads only the opcode.
Caller holds s.mu.
================
*/
func supersededEntryLocked(ring []ringFrame) int {
	for i, r := range ring {
		if r.f.Opcode == OpEnterWorldResult {
			return i
		}
	}
	return len(ring)
}

/*
================
dropSupersededEntryLocked

Drops the unapplied EnterWorld answer at ring[cut], every ring frame
after it, and every counted frame still queued or coalesced behind it:
all of it is that answer's scene or later state, and the new entry's
snapshot rebuilds it whole. Sending it without its answer would reach a
client that has no world yet as world packets before EnterWorld, which
it refuses. Positions stay consistent: the re-sent frames are numbered
again as they are rewritten (resumeFromLocked), and nothing dropped is
ever numbered on the new stream. Transport-control frames in the queue
stay. Caller holds s.mu.
================
*/
func (s *Session) dropSupersededEntryLocked(cut int) {
	for i := cut; i < len(s.ring); i++ {
		s.ringBytes -= s.ring[i].f.EncodedLen()
		s.ring[i] = ringFrame{}
	}
	s.ring = s.ring[:cut]
	kept := s.queue[:0]
	for _, f := range s.queue {
		if CountsOnStream(f.Opcode) {
			s.queueBytes -= f.EncodedLen()
			continue
		}
		kept = append(kept, f)
	}
	for i := len(kept); i < len(s.queue); i++ {
		s.queue[i] = Frame{}
	}
	s.queue = kept
	for _, slot := range s.lossyKeys {
		delete(s.lossy, slot)
	}
	s.lossyKeys = nil
	s.lossyBytes = 0
	// Every counted queued frame is gone, an earlier resume's
	// still-unwritten re-send block included.
	s.resendFrames, s.resendBytes = 0, 0
}

/*
================
queuedEntryLocked

The index of the first EnterWorld answer still queued (never written),
or len(queue). An answer the writer had not reached when the link
dropped is as superseded as an unapplied one in the ring: the resumed
browser asks for a new entry, and re-sent, the stale answer is applied
in its place while the new one lands on a client already in the world
and is refused ("Unexpected EnterWorld result", #2443: a teleport's
re-entry queued behind a writer parked on a slow link). Caller holds
s.mu.
================
*/
func queuedEntryLocked(queue []Frame) int {
	for i, f := range queue {
		if f.Opcode == OpEnterWorldResult {
			return i
		}
	}
	return len(queue)
}

/*
================
dropQueuedEntryLocked

Drops the queued EnterWorld answer at queue[cut] and every counted
frame behind it, with the coalesced lane: all of it is that answer's
scene or later state, which the new entry's snapshot rebuilds whole.
Counted frames ahead of it (a re-entry's reset) and transport-control
frames stay, in order. Caller holds s.mu.
================
*/
func (s *Session) dropQueuedEntryLocked(cut int) {
	kept := s.queue[:cut]
	for _, f := range s.queue[cut:] {
		if CountsOnStream(f.Opcode) {
			s.queueBytes -= f.EncodedLen()
			continue
		}
		kept = append(kept, f)
	}
	for i := len(kept); i < len(s.queue); i++ {
		s.queue[i] = Frame{}
	}
	s.queue = kept
	for _, slot := range s.lossyKeys {
		delete(s.lossy, slot)
	}
	s.lossyKeys = nil
	s.lossyBytes = 0
	// An earlier resume's unwritten re-send block leads the queue; the
	// cut may fall inside it, so recount what is left of it.
	counted, bytes := 0, 0
	for _, f := range kept {
		if counted == s.resendFrames {
			break
		}
		if CountsOnStream(f.Opcode) {
			counted++
			bytes += f.EncodedLen()
		}
	}
	s.resendFrames, s.resendBytes = counted, bytes
}

/*
================
resumeRefusal

Why a resume from a reported position cannot be served, with the ring's
own numbers beside it (#2146): the reported position against the oldest
position the ring still holds and the newest it has written. Logged on
every refusal, so a live refusal wave names its own cause - a client
ahead of the stream, a client behind frames the ring already dropped,
or a re-send that would overflow the outbound byte bound.
================
*/
type resumeRefusal struct {
	Reason       string
	Reported     uint32
	Floor        uint32
	Oldest       uint32
	Head         uint32
	RingFrames   int
	QueuedFrames int
	QueuedBytes  int
}

/*
================
ResumeRefusal

The refusal snapshot for a position resumeFromLocked just turned away.
Caller must NOT hold s.mu.
================
*/
func (s *Session) ResumeRefusal(lastApplied uint32) resumeRefusal {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := resumeRefusal{
		Reported:     lastApplied,
		Floor:        s.ringFloor,
		Head:         s.streamPos,
		RingFrames:   len(s.ring),
		QueuedFrames: len(s.queue),
		QueuedBytes:  s.queueBytes + s.lossyBytes,
		Reason:       "resend-overflow",
	}
	if len(s.ring) > 0 {
		d.Oldest = s.ring[0].pos
	}
	switch {
	case lastApplied > s.streamPos:
		d.Reason = "beyond-head"
	case lastApplied < s.ringFloor:
		d.Reason = "behind-floor"
	}
	return d
}
