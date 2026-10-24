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

===========================================================================
*/
package transport

import "errors"

// errResumeGap refuses a resume whose stream position the ring cannot serve.
var errResumeGap = errors.New("transport: resume position not covered")

const (
	// streamRingMaxFrames and streamRingMaxBytes bound the unacknowledged
	// window. The client acknowledges about once a second, so a live link
	// never comes close; a detached session keeps at most this much.
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

Records a frame the stream accepted. Caller holds s.mu.
================
*/
func (s *Session) noteWrittenLocked(f Frame, ordered bool) {
	if !CountsOnStream(f.Opcode) {
		return
	}
	s.streamPos++
	if !ordered {
		return
	}
	s.ring = append(s.ring, ringFrame{pos: s.streamPos, f: f})
	s.ringBytes += f.EncodedLen()
	for len(s.ring) > streamRingMaxFrames || s.ringBytes > streamRingMaxBytes {
		// The oldest unacknowledged frame falls out: a resume that still
		// needs it is refused instead of silently skipping it.
		s.ringFloor = s.ring[0].pos
		s.ringBytes -= s.ring[0].f.EncodedLen()
		s.ring[0] = ringFrame{}
		s.ring = s.ring[1:]
	}
}

/*
================
ackLocked

Drops the ring frames the client has applied. Caller holds s.mu.
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
}

/*
================
resumeFromLocked

Prepares a resume from the client's last applied position: the ring frames
past it are re-queued ahead of the frames queued while detached, and the
position restarts there (they are counted again as they are rewritten).
False when the position cannot be served - beyond what was written, or
older than a frame the ring has already dropped. Caller holds s.mu.
================
*/
func (s *Session) resumeFromLocked(lastApplied uint32) bool {
	if lastApplied > s.streamPos || lastApplied < s.ringFloor {
		return false
	}
	s.ackLocked(lastApplied)
	resend := make([]Frame, 0, len(s.ring)+len(s.queue))
	for _, r := range s.ring {
		resend = append(resend, r.f)
		s.queueBytes += r.f.EncodedLen()
	}
	s.queue = append(resend, s.queue...)
	s.ring = nil
	s.ringBytes = 0
	s.streamPos = lastApplied
	return true
}
