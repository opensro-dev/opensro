/*
===========================================================================

session_write.go - the session's writer: one transport write per batch

The writer drains the session's queues onto its connection. It dequeues,
numbers and judges (Current) one frame at a time exactly as it always
did, but it collects every frame it may write now into one batch and
hands the batch to the connection in one write: a WebSocket writes the
same binary messages back to back in one buffer, WebTransport the same
length-prefixed records in one stream write. The client reads the same
messages in the same order; only the number of system calls, and so of
TCP segments, changes.

One TCP segment per 22-byte frame was what closed healthy sessions in a
crowd: at a few hundred thousand segments a second the kernel's loopback
TCP stalled single connections past the 10 s write deadline, the
detached session's queue then filled with the crowd's movement and it
closed as a slow consumer. The original coalesces too: its socket
stream queues messages and sends them with one WSASend per flush.

The tick's write window makes a tick's output one batch. The tick's
pusher holds a session's writer when it first pushes to it in a tick
(HoldTickWrites) and releases it after the tick's last push
(ReleaseTickWrites). Everything still enqueues at once, in the order it
was produced and under the same bounds, so a frame another producer
sends during the tick keeps its place among the tick's frames; the hold
only keeps the writer from starting before the tick has finished
pushing. Frames queued before the hold began, and every frame of a
session the tick has not pushed to, are written at once as before.

===========================================================================
*/
package transport

import (
	log "github.com/sirupsen/logrus"
)

const (
	// writeBatchMaxFrames and writeBatchMaxBytes cap one transport
	// write. A tick's output for one session is 40-60 small frames at
	// 600 players (the crowd bench), far under both; the caps bound the
	// buffer a backlog after a stall would otherwise build in one go.
	writeBatchMaxFrames = 1024
	writeBatchMaxBytes  = 64 << 10
)

/*
================
writeBatch

The frames the writer has dequeued and numbered but not yet written,
all for one attachment (conn, gen). welcome and drainClose mark the two
frames whose follow-up runs after the write that carries them: the
HELLO ticket's consumption and the drained session's close.
================
*/
type writeBatch struct {
	frames     []Frame
	bytes      int
	conn       Conn
	gen        int
	welcome    bool
	drainClose bool
}

/*
================
writeBatch.add
================
*/
func (b *writeBatch) add(f Frame, conn Conn, gen int) {
	if len(b.frames) == 0 {
		b.conn, b.gen = conn, gen
	}
	b.frames = append(b.frames, f)
	b.bytes += f.EncodedLen()
	b.welcome = b.welcome || f.Opcode == OpWelcome
}

/*
================
writeBatch.full

Whether f would carry the batch past one of its caps.
================
*/
func (b *writeBatch) full(f Frame) bool {
	return len(b.frames)+1 > writeBatchMaxFrames || b.bytes+f.EncodedLen() > writeBatchMaxBytes
}

/*
================
writeBatch.reset

Empties the batch for reuse, releasing its payload references.
================
*/
func (b *writeBatch) reset() {
	clear(b.frames)
	b.frames = b.frames[:0]
	b.bytes = 0
	b.conn = nil
	b.welcome, b.drainClose = false, false
}

//============================================================================
// The tick's write window
//============================================================================

/*
================
HoldTickWrites

Holds the writer at the frames queued so far: they stay writable, and
everything queued from now on waits for ReleaseTickWrites. True when
this call began the hold, so the caller owes the release; false when
the session is already held, closed or draining (a draining session's
BYE never waits on a tick).
================
*/
func (s *Session) HoldTickWrites() bool {
	if s.writeHeld.Load() {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.drainClose || s.writeHold {
		return false
	}
	s.writeHold = true
	s.writeHoldAhead = len(s.queue)
	s.writeHeld.Store(true)
	return true
}

/*
================
TickWritesHeld

Whether the writer is held, read without the session lock: the pusher's
check before it asks for the hold.
================
*/
func (s *Session) TickWritesHeld() bool { return s.writeHeld.Load() }

/*
================
ReleaseTickWrites

Ends the hold and wakes the writer, which then writes everything it may
in one batch. A session that is not held is left as it is.
================
*/
func (s *Session) ReleaseTickWrites() {
	s.mu.Lock()
	s.releaseWriteHoldLocked()
	s.mu.Unlock()
}

/*
================
releaseWriteHoldLocked

Caller holds s.mu.
================
*/
func (s *Session) releaseWriteHoldLocked() {
	if !s.writeHold {
		return
	}
	s.writeHold = false
	s.writeHoldAhead = 0
	s.writeHeld.Store(false)
	s.cond.Broadcast()
}

/*
================
writableLocked

Whether the writer has a frame it may dequeue now. The ordered head
waits while the ring gate holds (ringGateLocked, #2146); a pending
lossy frame is never ringed and always writable, so movement keeps
flowing past a gated ordered head. While the tick's window holds the
writer, only the ordered frames queued before the hold may go, and the
lossy lane waits with the rest of the tick's output. writeHoldAhead can
count frames a resume's entry drop has since removed; an over-count
only lets a held frame leave early, it never keeps one back. Caller
holds s.mu.
================
*/
func (s *Session) writableLocked() bool {
	if s.writeHold {
		return min(s.writeHoldAhead, len(s.queue)) > 0 && !s.ringGateLocked()
	}
	if len(s.lossyKeys) > 0 {
		return true
	}
	if len(s.queue) == 0 {
		return false
	}
	return !s.ringGateLocked()
}

//============================================================================
// The writer
//============================================================================

/*
================
writeLoop

The session's single writer for its whole life. It drains the ordered
queue first, then the lossy coalesce slots, whenever a connection is
attached, and exits at final close. Each frame is dequeued, numbered
(noteWrittenLocked) and judged by its Current check before the next is
dequeued, so a stale frame is always the newest numbered one and its
position can be taken back (unnoteLocked). The frames that pass collect
in one batch, written when nothing more is writable now, at a cap, when
the attachment changes, and right after a WELCOME or the drained
session's last frame, whose follow-ups run after the write that carried
them.
================
*/
func (s *Session) writeLoop() {
	var batch writeBatch
	for {
		s.mu.Lock()
		if len(batch.frames) > 0 {
			if s.closed || !s.attached || s.gen != batch.gen {
				// The attachment the batch was numbered for is gone and
				// its connection closed with it: the batch is lost as a
				// write into that dead socket is, and a resume that
				// needs its ordered frames gets them from the ring.
				batch.reset()
			} else if !s.writableLocked() {
				s.mu.Unlock()
				if s.flushWriteBatch(&batch) {
					return
				}
				continue
			}
		}
		for !s.closed && (!s.attached || !s.writableLocked()) {
			s.cond.Wait()
		}
		if s.closed {
			s.mu.Unlock()
			return
		}
		var f Frame
		ordered := len(s.queue) > 0 && (s.writeHold || len(s.lossyKeys) == 0 || !s.ringGateLocked())
		if ordered {
			f = s.queue[0]
			s.queue[0] = Frame{} // release payload references in the consumed backing array
			s.queue = s.queue[1:]
			s.queueBytes -= f.EncodedLen()
			if s.writeHold && s.writeHoldAhead > 0 {
				s.writeHoldAhead--
			}
			if s.resendFrames > 0 && CountsOnStream(f.Opcode) {
				// The re-sent block leaves first and only it is
				// counted: transport frames ahead of it (WELCOME)
				// and the lossy lane never held a ring slot.
				s.resendFrames--
				s.resendBytes -= f.EncodedLen()
			}
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
				// The frames ahead of the stale one still leave first.
				if len(batch.frames) > 0 && s.flushWriteBatch(&batch) {
					return
				}
				s.hub.closeSession(s, nil)
				return
			}
			continue
		}
		if len(batch.frames) > 0 && batch.full(f) {
			if s.flushWriteBatch(&batch) {
				return
			}
		}
		batch.add(f, conn, gen)
		batch.drainClose = ordered && drainAndEmpty
		if batch.welcome || batch.drainClose ||
			len(batch.frames) >= writeBatchMaxFrames || batch.bytes >= writeBatchMaxBytes {
			if s.flushWriteBatch(&batch) {
				return
			}
		}
	}
}

/*
================
flushWriteBatch

Writes the batch to its attachment in one transport write and runs what
follows a successful write. True when the session ended and the writer
must exit.

A failed write detaches the attachment and re-queues nothing: a write
can fail after the bytes reached the client (a deadline on a stalled
link), and re-sending them then applied them twice (#1466). Every frame
of the batch already holds its stream position, so the client's resume
position says what arrived and the ring re-sends only what did not;
loss-tolerant frames of the batch are lost, as a failed write of one
always lost it.
================
*/
func (s *Session) flushWriteBatch(b *writeBatch) bool {
	frames, conn, gen := b.frames, b.conn, b.gen
	welcome, drainClose := b.welcome, b.drainClose
	writes, err := writeFrameBatch(conn, frames)
	s.hub.metrics.writesOut.Add(uint64(writes))
	if err != nil {
		b.reset()
		s.hub.metrics.writeErrors.Add(1)
		s.detachGen(gen, err)
		return false
	}
	s.mu.Lock()
	s.lastSent = s.now()
	s.mu.Unlock()
	for _, f := range frames {
		s.hub.recordFrameOut(f)
	}
	b.reset()
	if welcome {
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
				return true
			}
		}
	}
	if drainClose {
		s.hub.closeSession(s, nil)
		return true
	}
	return false
}

/*
================
writeFrameBatch

Hands frames to conn: one frame through WriteFrame, several through
WriteFrames when the connection has it (frameBatchWriter), else one
WriteFrame each, stopping at the first error. Returns how many
transport writes succeeded, for writes_out.
================
*/
func writeFrameBatch(conn Conn, frames []Frame) (int, error) {
	if len(frames) == 1 {
		if err := conn.WriteFrame(frames[0]); err != nil {
			return 0, err
		}
		return 1, nil
	}
	if batched, ok := conn.(frameBatchWriter); ok {
		if err := batched.WriteFrames(frames); err != nil {
			return 0, err
		}
		return 1, nil
	}
	for i, f := range frames {
		if err := conn.WriteFrame(f); err != nil {
			return i, err
		}
	}
	return len(frames), nil
}
