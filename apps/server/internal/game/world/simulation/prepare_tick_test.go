/*
===========================================================================

prepare_tick_test.go - prepareTick's steady-state cost and session order

One tick's preparation takes the session snapshot, orders it by
(division, session, character) and groups it into division work. The
population changes only on attach and detach, so the ordered list is
kept between ticks and rebuilt only when membership changes; these
tests pin the order contract and measure the steady state.

===========================================================================
*/
package simulation

import (
	"fmt"
	"testing"
)

/*
================
prepareTickBenchSource

A snapshotFunc-shaped source of benchPrepareSessions sessions over two
divisions whose walk order rotates every call, the way a hub map walk
does. The snapshots are fresh values per call, like the bridge's.
================
*/
const benchPrepareSessions = 52

func prepareTickBenchSource() (SessionSource, *int) {
	calls := new(int)
	source := snapshotFunc(func() []SessionSnapshot {
		*calls++
		out := make([]SessionSnapshot, 0, benchPrepareSessions)
		for i := 0; i < benchPrepareSessions; i++ {
			n := (i + *calls) % benchPrepareSessions
			division := "DIV_A"
			if n%2 == 1 {
				division = "DIV_B"
			}
			out = append(out, SessionSnapshot{
				SessionID:   fmt.Sprintf("s%03d", n),
				DivisionID:  division,
				CharacterID: int64(n + 1),
			})
		}
		return out
	})
	return source, calls
}

/*
================
BenchmarkPrepareTickSteady

prepareTick over an unchanged population whose snapshot walk arrives
in a different order every tick.
================
*/
func BenchmarkPrepareTickSteady(b *testing.B) {
	source, _ := prepareTickBenchSource()
	ticker := &Ticker{Source: source, Push: discardPusher{}}
	ticker.prepareTick()
	b.ReportAllocs()
	for b.Loop() {
		ticker.prepareTick()
	}
}

/*
================
TestPrepareTickKeepsSortedOrderAcrossShuffles

The division work order is the (division, session, character) order on
every tick, however the snapshot walk arrives; a membership change
re-sorts.
================
*/
func TestPrepareTickKeepsSortedOrderAcrossShuffles(t *testing.T) {
	source, _ := prepareTickBenchSource()
	ticker := &Ticker{Source: source, Push: discardPusher{}}
	var first []string
	for tick := 0; tick < 3; tick++ {
		_, work := ticker.prepareTick()
		var order []string
		for _, w := range work {
			for _, s := range w.sessions {
				order = append(order, s.DivisionID+"/"+s.SessionID)
			}
		}
		if tick == 0 {
			first = order
			continue
		}
		if len(order) != len(first) {
			t.Fatalf("tick %d order has %d sessions, want %d", tick, len(order), len(first))
		}
		for i := range order {
			if order[i] != first[i] {
				t.Fatalf("tick %d order[%d] = %s, want %s (full order %v)", tick, i, order[i], first[i], order)
			}
		}
	}
	for i := 1; i < len(first); i++ {
		if first[i-1] > first[i] {
			t.Fatalf("order not sorted at %d: %v", i, first)
		}
	}
}

/*
================
TestPrepareTickResortsOnMembershipChange

A session swap changes membership: the kept order no longer applies
and the tick re-sorts, so the new session lands in sorted position
and the departed one is gone.
================
*/
func TestPrepareTickResortsOnMembershipChange(t *testing.T) {
	calls := 0
	source := snapshotFunc(func() []SessionSnapshot {
		calls++
		out := []SessionSnapshot{
			{SessionID: "s003", DivisionID: "DIV_A", CharacterID: 3},
			{SessionID: "s001", DivisionID: "DIV_A", CharacterID: 1},
			{SessionID: "s002", DivisionID: "DIV_A", CharacterID: 2},
		}
		if calls > 1 {
			out[2].SessionID = "s000"
			out[2].CharacterID = 0
		}
		return out
	})
	ticker := &Ticker{Source: source, Push: discardPusher{}}
	ticker.prepareTick()
	_, work := ticker.prepareTick()
	if len(work) != 1 || len(work[0].sessions) != 3 {
		t.Fatalf("work = %v, want one division of three", work)
	}
	want := []string{"s000", "s001", "s003"}
	for i, s := range work[0].sessions {
		if s.SessionID != want[i] {
			t.Fatalf("sessions[%d] = %s, want %s (kept order leaked across a membership change)", i, s.SessionID, want[i])
		}
	}
}
