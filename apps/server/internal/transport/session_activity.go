/*
===========================================================================

session_activity.go - what a census span did on the wire (#3307)

The connection census keeps one row per entered span (connjournal). The
11-12 live investigation could not tell a client that entered and then
sat from one that played: the row held times, not traffic. This counts,
per span, the frames that arrived and - for each opcode a handler
answered - how many arrived and when the first did, as an offset from
the span's start.

A span here is the census's: it starts at a world entry and ends at the
next entry on the same session or at the session's end. The counts are
taken (and reset) at exactly those two events, so a row's summary
describes that row. The 0x3012 that enters the world is noted before its
handler runs, and the take happens inside that handler, so the entry
frame belongs to the span it closes - the new span starts clean.

Raw facts only. "Time to first action" is a reading of the opcode list
at read time: the client sends frames of its own after an entry
(continuations, pickups on every step), and a server list mirroring
which of those count as "action" would drift from the client.

Counted only while the census runs (Hub.Census installed); off, the
cost is one nil check per frame. Port-only, not native.

===========================================================================
*/
package transport

import (
	"sort"
	"sync"
	"sync/atomic"
)

/*
================
OpcodeActivity

One handled opcode in a span: how many frames of it arrived, and the
first one's offset from the span's start in milliseconds.
================
*/
type OpcodeActivity struct {
	Opcode  uint16
	Count   uint32
	FirstMs int64
}

/*
================
ActivitySummary

A span's traffic as taken at its end. FirstHandledMs is the earliest
handled frame's offset, -1 when no frame was handled. Opcodes are
ordered by count, most first, then by opcode.
================
*/
type ActivitySummary struct {
	FramesIn       uint32
	FirstHandledMs int64
	Opcodes        []OpcodeActivity
}

// opcodeFact is one opcode's running count inside a span.
type opcodeFact struct {
	count   uint32
	firstMs int64
}

/*
================
spanActivity

The session's running counts. framesIn is bumped by the read loop;
the opcode facts by dispatch, under mu. The handled opcodes are bounded
by the registered handlers: an unhandled opcode never enters the map.
================
*/
type spanActivity struct {
	framesIn atomic.Uint32

	mu        sync.Mutex
	started   bool
	startedMs int64
	opcodes   map[uint16]opcodeFact
}

/*
================
noteFrame

One inbound frame of any kind, control frames included (the same
frames the hub's framesIn metric counts).
================
*/
func (a *spanActivity) noteFrame() {
	a.framesIn.Add(1)
}

/*
================
noteHandled

One frame a handler is about to answer. Before the first take the span
has no start yet; the first handled frame starts it, which only affects
the pre-entry span the census never stores.
================
*/
func (a *spanActivity) noteHandled(opcode uint16, nowMs int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.started {
		a.started, a.startedMs = true, nowMs
	}
	if a.opcodes == nil {
		a.opcodes = make(map[uint16]opcodeFact)
	}
	fact, seen := a.opcodes[opcode]
	if !seen {
		fact.firstMs = nowMs - a.startedMs
	}
	fact.count++
	a.opcodes[opcode] = fact
}

/*
================
take

The span's summary, and the start of the next span at nowMs.
================
*/
func (a *spanActivity) take(nowMs int64) *ActivitySummary {
	a.mu.Lock()
	defer a.mu.Unlock()
	summary := &ActivitySummary{FramesIn: a.framesIn.Swap(0), FirstHandledMs: -1,
		Opcodes: make([]OpcodeActivity, 0, len(a.opcodes))}
	for opcode, fact := range a.opcodes {
		summary.Opcodes = append(summary.Opcodes, OpcodeActivity{Opcode: opcode, Count: fact.count, FirstMs: fact.firstMs})
		if summary.FirstHandledMs < 0 || fact.firstMs < summary.FirstHandledMs {
			summary.FirstHandledMs = fact.firstMs
		}
	}
	sort.Slice(summary.Opcodes, func(i, j int) bool {
		left, right := summary.Opcodes[i], summary.Opcodes[j]
		if left.Count != right.Count {
			return left.Count > right.Count
		}
		return left.Opcode < right.Opcode
	})
	clear(a.opcodes)
	a.started, a.startedMs = true, nowMs
	return summary
}
