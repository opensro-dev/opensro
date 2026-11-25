/*
===========================================================================

hold_breakdown.go - where a slow publication-gate hold spent its time

publication_holds names the holder of a long gate hold ("returnArrival",
"requestOp"), not the part of its work that kept the gate. A holder that
runs several steps under the gate marks each boundary on a HoldBreakdown,
and when the whole hold reaches SlowHookThreshold it reports
every part as "<label>.<part>" beside its own row, the shape
requestOp.prepare already has (register.go). A calm hold reports nothing
and the breakdown allocates nothing: the parts sit in a fixed array.

It lives here, beside SlowHookThreshold, so every holder shares one:
the return-scroll arrival (action) and the re-entry rebuild it waits on
(enterworld) both mark their parts on it.

Safe for any goroutine that owns its breakdown value; the sink it reports
to (action.Runtime.PublicationHold, transport.Hub.RecordPublicationHold)
takes its own lock.

===========================================================================
*/

package simulation

import "time"

// maxHoldParts bounds the parts one breakdown keeps; a mark past it is
// folded into the last part, so the total still adds up.
const maxHoldParts = 8

// minHoldPart is the shortest part a slow hold reports: below it a part
// is noise beside the 100 ms threshold the hold itself crossed.
const minHoldPart = time.Millisecond

/*
================
holdPart
================
*/
type holdPart struct {
	name    string
	elapsed time.Duration
}

/*
================
HoldBreakdown

One gate hold's parts, timed from the moment the holder took the gate.
The zero value records nothing (a nil-sink runtime, or a caller that
passes no breakdown), so callers need no branch.
================
*/
type HoldBreakdown struct {
	label   string
	now     func() time.Time
	started time.Time
	last    time.Time
	parts   [maxHoldParts]holdPart
	n       int
}

/*
================
NewHoldBreakdown

A breakdown for a gate taken just now under label, on clock now: the
holder's own clock (Runtime.Now), so a test drives it without sleeping.
================
*/
func NewHoldBreakdown(label string, now func() time.Time) HoldBreakdown {
	at := now()
	return HoldBreakdown{label: label, now: now, started: at, last: at}
}

/*
================
HoldBreakdown.Mark

Closes the part that ran since the previous mark under name. Nil-safe
and a no-op on the zero value, so a completion shared with callers that
time nothing (tests, other lanes) takes a nil breakdown.
================
*/
func (b *HoldBreakdown) Mark(name string) {
	if b == nil || b.started.IsZero() {
		return
	}
	at := b.now()
	elapsed := at.Sub(b.last)
	b.last = at
	if b.n == maxHoldParts {
		b.parts[b.n-1].elapsed += elapsed
		return
	}
	b.parts[b.n] = holdPart{name: name, elapsed: elapsed}
	b.n++
}

/*
================
HoldBreakdown.Report

When the hold as a whole reached the slow threshold, sends each part of
at least minHoldPart to sink as "<label>.<part>". The label strings are
built only on this slow path.
================
*/
func (b *HoldBreakdown) Report(sink func(label string, elapsed time.Duration)) {
	if b == nil || sink == nil || b.started.IsZero() {
		return
	}
	if b.now().Sub(b.started) < SlowHookThreshold {
		return
	}
	for _, part := range b.parts[:b.n] {
		if part.elapsed >= minHoldPart {
			sink(b.label+"."+part.name, part.elapsed)
		}
	}
}
