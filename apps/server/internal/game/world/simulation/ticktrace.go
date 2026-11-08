/*
===========================================================================

ticktrace.go - a slow tick as a nested zone timeline (#2301)

The tick counters (tick_timing.go) keep phase totals and the names
of whatever crossed 100 ms. That answers "was it slow", never "what
ran": no start offsets, no children, no goroutine. This file keeps
the shape. While a TickTracer is armed on the Ticker (production
wiring arms it from SRO_TICK_TRACE, transport's trace writer; unset
means no tracer and this file costs one nil check per zone point),
every phase, division and its sub-steps, hook, action step and the
monster leg records a zone: a begin/end pair stamped with the
recording goroutine's id and its offset from the tick start. At
tick end a slow tick's zones are rendered as Perfetto (chrome
trace) JSON - the file opens in an existing viewer, no viewer is
built. The rolling recorder that keeps ticks in memory and dumps
on an over-budget tick is #2302; it builds on this substrate.

Zones never nest across goroutines here - divisions are zoned on
the goroutines that run them, below the tick goroutine's phase
zone, and Perfetto places them by timestamp on their own tracks.
The monster leg's pooled workers record no zones; the leg is one
zone on the division goroutine, as in the serial leg.

===========================================================================
*/

package simulation

import (
	"encoding/json"
	"runtime"
	"strconv"
	"sync"
	"time"
)

// tickTraceSlowThreshold is the tick total that keeps a trace: the
// snap vocabulary of tick_timing.go (SlowHookThreshold), so the
// ticks a player can feel are the ticks with timelines.
const tickTraceSlowThreshold = SlowHookThreshold

// tickTraceEventPrealloc is one tick's expected zone count: phases,
// divisions and their sub-steps, hooks and action steps land in the
// low hundreds even on a crowded tick.
const tickTraceEventPrealloc = 1024

// tickTracePerfettoPID is the single process id every rendered
// event carries; the goroutine id is the track (tid).
const tickTracePerfettoPID = 1

/*
================
tickTraceEvent

One zone edge: a begin or an end, its offset from the tick start,
and the goroutine that recorded it (the Perfetto track).
================
*/
type tickTraceEvent struct {
	name string
	end  bool
	at   time.Duration
	gid  uint64
}

/*
================
TickTracer

The per-tick zone recorder. Begin/end calls come from every
goroutine the tick spans, so the event list is mutex-guarded; the
guard is taken only while armed, and the disarmed path never
reaches it (Ticker.zone answers a shared no-op first). Owned by
the Ticker: beginTick and finish run on the tick goroutine, and
the tick joins every worker before finish, so no zone can close
into the next tick's record.
================
*/
type TickTracer struct {
	mu     sync.Mutex
	start  time.Time
	events []tickTraceEvent
}

/*
================
NewTickTracer
================
*/
func NewTickTracer() *TickTracer {
	return &TickTracer{events: make([]tickTraceEvent, 0, tickTraceEventPrealloc)}
}

/*
================
TickTracer.beginTick
================
*/
func (tr *TickTracer) beginTick(now time.Time) {
	tr.mu.Lock()
	tr.start = now
	tr.events = tr.events[:0]
	tr.mu.Unlock()
}

/*
================
TickTracer.zone

Records a zone begin and returns its end. A nil tracer answers
the shared no-op, so call sites need no branch of their own.
================
*/
func (tr *TickTracer) zone(name string) func() {
	if tr == nil {
		return tickTraceZoneNoop
	}
	gid := currentGoroutineID()
	tr.mu.Lock()
	tr.events = append(tr.events, tickTraceEvent{name: name, at: time.Since(tr.start), gid: gid})
	tr.mu.Unlock()
	return func() {
		tr.mu.Lock()
		tr.events = append(tr.events, tickTraceEvent{name: name, end: true, at: time.Since(tr.start), gid: gid})
		tr.mu.Unlock()
	}
}

/*
================
tickTraceZoneNoop

The disarmed zone end: one shared function, allocates nothing,
does nothing.
================
*/
func tickTraceZoneNoop() {}

/*
================
TickTracer.finish

Closes one tick's record: the tick's start and its events. A fast
tick's events are dropped with the record; a slow tick's are
handed over (the tracer keeps no reference) for the caller to
render and export.
================
*/
func (tr *TickTracer) finish(total time.Duration) (time.Time, []tickTraceEvent) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if total < tickTraceSlowThreshold || len(tr.events) == 0 {
		tr.events = tr.events[:0]
		return time.Time{}, nil
	}
	events := tr.events
	tr.events = make([]tickTraceEvent, 0, tickTraceEventPrealloc)
	return tr.start, events
}

/*
================
currentGoroutineID

The recording goroutine's id, parsed from its stack header
("goroutine <id> [state]:"). Only zones pay for it (armed ticks
only); the counters never do.
================
*/
func currentGoroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	start := 0
	for start < n && buf[start] != ' ' {
		start++
	}
	start++
	var id uint64
	for start < n && buf[start] >= '0' && buf[start] <= '9' {
		id = id*10 + uint64(buf[start]-'0')
		start++
	}
	return id
}

//============================================================================

/*
================
TickTrace

One finished slow tick: when it started, how long it ran, and its
zones rendered for a viewer. JSON is the Perfetto (chrome trace)
event list; it opens in Perfetto or chrome://tracing unchanged.
================
*/
type TickTrace struct {
	At     time.Time
	Total  time.Duration
	Events int
	JSON   []byte
}

/*
================
perfettoEvent

One rendered zone edge (or the track's name record). Begin and
end carry the same name and track; Perfetto nests them per track.
================
*/
type perfettoEvent struct {
	Name string        `json:"name"`
	Ph   string        `json:"ph"`
	Ts   float64       `json:"ts"`
	Pid  int           `json:"pid"`
	Tid  uint64        `json:"tid"`
	Cat  string        `json:"cat,omitempty"`
	Args *perfettoArgs `json:"args,omitempty"`
}

/*
================
perfettoArgs

The metadata record's payload (a track's display name).
================
*/
type perfettoArgs struct {
	Name string `json:"name"`
}

/*
================
renderTickTrace

Renders one slow tick's zones. Timestamps are microseconds from
the tick start, the chrome trace unit. Rendering happens only
for ticks that kept a trace, on the tick goroutine, before the
sink takes the result - the export never blocks on this either.
================
*/
func renderTickTrace(start time.Time, events []tickTraceEvent, total time.Duration) TickTrace {
	rendered := make([]perfettoEvent, 0, len(events)+8)
	tracks := make(map[uint64]bool)
	for _, event := range events {
		if !tracks[event.gid] {
			tracks[event.gid] = true
			rendered = append(rendered, perfettoEvent{
				Name: "thread_name", Ph: "M", Pid: tickTracePerfettoPID, Tid: event.gid,
				Args: &perfettoArgs{Name: "goroutine " + strconv.FormatUint(event.gid, 10)},
			})
		}
		phase := "B"
		if event.end {
			phase = "E"
		}
		rendered = append(rendered, perfettoEvent{
			Name: event.name, Ph: phase, Pid: tickTracePerfettoPID, Tid: event.gid, Cat: "tick",
			Ts: float64(event.at) / float64(time.Microsecond),
		})
	}
	body, err := json.Marshal(struct {
		TraceEvents []perfettoEvent `json:"traceEvents"`
	}{TraceEvents: rendered})
	if err != nil {
		// The events are plain data; a marshal failure is a bug in
		// this file, and the trace degrades to an empty export
		// rather than failing the tick.
		body = []byte(`{"traceEvents":[]}`)
	}
	return TickTrace{At: start, Total: total, Events: len(events), JSON: body}
}

//============================================================================

/*
================
Ticker.traceBegin
================
*/
func (t *Ticker) traceBegin() {
	if t.Trace != nil {
		t.Trace.beginTick(time.Now())
	}
}

/*
================
Ticker.zone

One zone on the running tick: timed into the always-on ledger
(zone_ledger.go, #2824) and, while a tracer is armed, recorded on the
slow-tick timeline. The armed timeline keeps the names it always had
- phases as "phase:<name>", a division's sub-steps bare, divisions as
"division:<id>", hooks as "hook:<name>" - and builds them only when
armed, so the disarmed path stays allocation-free.
================
*/
func (t *Ticker) zone(kind, name string) zoneSpan {
	span := zoneSpan{ledger: t.Zones, key: zoneKey{kind: kind, name: name}}
	if t.Trace != nil {
		span.trace = t.Trace.zone(tickTraceZoneName(kind, name))
	}
	if span.ledger != nil {
		span.start = time.Now()
	}
	return span
}

/*
================
tickTraceZoneName

A zone's name on the Perfetto timeline (#2301), unchanged by the
ledger: sub-steps were always bare.
================
*/
func tickTraceZoneName(kind, name string) string {
	if kind == ZoneKindSub {
		return name
	}
	return kind + ":" + name
}

/*
================
Ticker.TraceZone

The timeline seam other lanes time against the running tick - the
action runtime's StepTimer steps (wiring_runtime.go). Timeline only:
the steps reach the ledger through StepTimer.Total, which already
holds their elapsed time.
================
*/
func (t *Ticker) TraceZone(name string) func() {
	if t.Trace == nil {
		return tickTraceZoneNoop
	}
	return t.Trace.zone(name)
}

/*
================
Ticker.traceFinish

Keeps a slow tick's timeline: renders it and hands it to the
export sink. Runs inside observePhases, after every zone of the
tick has closed.
================
*/
func (t *Ticker) traceFinish(timing TickTiming) {
	if t.Trace == nil {
		return
	}
	start, events := t.Trace.finish(timing.Total)
	if events == nil || t.TraceSink == nil {
		return
	}
	t.TraceSink(renderTickTrace(start, events, timing.Total))
}
