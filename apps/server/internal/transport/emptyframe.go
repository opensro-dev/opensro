/*
===========================================================================

emptyframe.go - the outbound queue refuses an opcode-0 frame

A zero Frame (opcode 0, no payload) is not a protocol message: no control
or native opcode is 0, and the browser fails such a packet and drops the
session ("Unsupported transport control 0"). It only ever reaches a session
when a producer returns an empty frame meaning "nothing to publish" and a
caller forgets to check (#618: a self heal at full HP). The queue refuses
it instead, so that bug costs a warning naming its producer, not a player.

===========================================================================
*/
package transport

import (
	"errors"
	"runtime"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// ErrEmptyFrame is Send's answer to an opcode-0 frame.
var ErrEmptyFrame = errors.New("transport: refused an empty (opcode 0) frame")

// emptyFrameLogInterval throttles the warning so a producer that emits an
// empty frame every tick cannot flood the log; the count still reaches the
// metrics and the next warning.
const emptyFrameLogInterval = 5 * time.Second

// emptyFrameStackDepth is how many callers the warning names: enough to get
// from the transport through the routing seam to the game producer.
const emptyFrameStackDepth = 12

/*
================
emptyFrameLog

The throttle for the refusal warning. One per hub; its mutex guards only
the throttle arithmetic.
================
*/
type emptyFrameLog struct {
	mu         sync.Mutex
	last       time.Time
	suppressed uint64
}

/*
================
refuseEmptyFrame

Counts one refused empty frame and, at most once per interval, logs the
session and the call stack that produced it. skip is how many frames of
the transport's own call path to leave out of the stack.
================
*/
func (h *Hub) refuseEmptyFrame(session uint64, skip int) {
	h.metrics.emptyFramesRefused.Add(1)

	h.emptyFrames.mu.Lock()
	now := time.Now()
	if !h.emptyFrames.last.IsZero() && now.Sub(h.emptyFrames.last) < emptyFrameLogInterval {
		h.emptyFrames.suppressed++
		h.emptyFrames.mu.Unlock()
		return
	}
	suppressed := h.emptyFrames.suppressed
	h.emptyFrames.last, h.emptyFrames.suppressed = now, 0
	h.emptyFrames.mu.Unlock()

	log.WithFields(log.Fields{
		"session":    session,
		"suppressed": suppressed,
		"producer":   callerChain(skip + 1),
	}).Warn("transport: refused an empty (opcode 0) frame; a producer published a zero frame")
}

/*
================
callerChain

The function names above the caller, innermost first, joined with " < ".
================
*/
func callerChain(skip int) string {
	pcs := make([]uintptr, emptyFrameStackDepth)
	n := runtime.Callers(skip+2, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	var names []string
	for {
		frame, more := frames.Next()
		names = append(names, frame.Function)
		if !more {
			break
		}
	}
	return strings.Join(names, " < ")
}

/*
================
withoutEmptyFrames

The batch minus its opcode-0 frames, refusing each one. The rest of the
batch is still a valid transaction: an empty frame carried nothing the
client could apply. Returns the input slice unchanged when nothing is empty.
================
*/
func (s *Session) withoutEmptyFrames(frames []Frame) []Frame {
	empty := 0
	for _, frame := range frames {
		if frame.Opcode == 0 {
			empty++
		}
	}
	if empty == 0 {
		return frames
	}
	kept := make([]Frame, 0, len(frames)-empty)
	for _, frame := range frames {
		if frame.Opcode == 0 {
			// Skip withoutEmptyFrames and sendSceneObjectBatch; the
			// public Send* wrapper above them is still named.
			s.hub.refuseEmptyFrame(s.ID, 2)
			continue
		}
		kept = append(kept, frame)
	}
	return kept
}
