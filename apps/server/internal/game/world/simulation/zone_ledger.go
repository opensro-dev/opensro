/*
===========================================================================

zone_ledger.go - exact, always-on totals for every tick zone (#2824)

The tick counters (tick_timing.go) keep phase histograms in coarse
buckets and name only what crossed 100 ms; the zone timeline
(ticktrace.go) keeps the shape of slow ticks only, and only while
SRO_TICK_TRACE arms it. Neither can answer "how many milliseconds of
an ordinary 56 ms tick does each zone take" - the read the 10 ms
budget (#2150) is planned and accepted against.

This ledger can. Every zone point the timeline already marks -
phases, divisions, a division's sub-steps, hooks - plus every action
step (StepTimer) adds its exact elapsed time and one count to a
cumulative total keyed by (kind, name). Two snapshots of
/transport/metrics tick_zones give the exact milliseconds per tick of
every zone over that window: no sampling, no bucket midpoints.

Cost: one time.Now pair and one uncontended mutex per zone (a few
hundred per tick, a few microseconds in all). The keys are a closed
set of code-named zones; a new key allocates once, then never again.

===========================================================================
*/

package simulation

import (
	"sort"
	"sync"
	"time"
)

// The zone kinds tick_zones keys carry ("<kind>:<name>").
const (
	ZoneKindPhase    = "phase"
	ZoneKindDivision = "division"
	ZoneKindSub      = "sub"
	ZoneKindHook     = "hook"
	ZoneKindStep     = "step"
)

/*
================
zoneKey
================
*/
type zoneKey struct {
	kind, name string
}

/*
================
zoneTotal
================
*/
type zoneTotal struct {
	count uint64
	total time.Duration
}

/*
================
ZoneTotal

One zone's cumulative count and time since the ledger was made.
================
*/
type ZoneTotal struct {
	Kind, Name string
	Count      uint64
	Total      time.Duration
}

/*
================
ZoneLedger

Shared by the tick goroutine and the shard goroutines that run
divisions, so every add takes the mutex; snapshots take it too.
================
*/
type ZoneLedger struct {
	mu    sync.Mutex
	zones map[zoneKey]*zoneTotal
}

/*
================
NewZoneLedger
================
*/
func NewZoneLedger() *ZoneLedger {
	return &ZoneLedger{zones: make(map[zoneKey]*zoneTotal)}
}

/*
================
ZoneLedger.add
================
*/
func (l *ZoneLedger) add(key zoneKey, elapsed time.Duration) {
	l.mu.Lock()
	total := l.zones[key]
	if total == nil {
		total = &zoneTotal{}
		l.zones[key] = total
	}
	total.count++
	total.total += elapsed
	l.mu.Unlock()
}

/*
================
ZoneLedger.AddStep

The StepTimer seam: one action step's elapsed time, from the lane
that owns the steps (wiring_runtime.go).
================
*/
func (l *ZoneLedger) AddStep(name string, elapsed time.Duration) {
	if l == nil {
		return
	}
	l.add(zoneKey{kind: ZoneKindStep, name: name}, elapsed)
}

/*
================
ZoneLedger.Snapshot

Every zone's totals, sorted by kind then name so two snapshots line
up row by row.
================
*/
func (l *ZoneLedger) Snapshot() []ZoneTotal {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	out := make([]ZoneTotal, 0, len(l.zones))
	for key, total := range l.zones {
		out = append(out, ZoneTotal{Kind: key.kind, Name: key.name, Count: total.count, Total: total.total})
	}
	l.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

/*
================
zoneSpan

One open zone: its ledger key and start, plus the armed timeline's
end when a TickTracer records it. A value, so opening and closing a
zone allocates nothing; the zero value (neither sink present) does
nothing at all.
================
*/
type zoneSpan struct {
	ledger *ZoneLedger
	key    zoneKey
	start  time.Time
	trace  func()
}

/*
================
zoneSpan.End
================
*/
func (s zoneSpan) End() {
	if s.trace != nil {
		s.trace()
	}
	if s.ledger != nil {
		s.ledger.add(s.key, time.Since(s.start))
	}
}
