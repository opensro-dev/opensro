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
	"sort"
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

/*
================
TestPrepareTickOrderMatchesFreshSortAcrossMembershipChanges

The comparator-equal golden (#2140): across attach, detach, a shrink,
a same-count replacement, a resume under a new scene revision and a
division move, the grouped order prepareTick serves equals a fresh
sort of the same membership by (division, session, character) on
every tick. Steps 5-6 are the shrink-then-replace sequence: the kept
order shrinks to two, then a same-count replacement fails placement
on its first session.
================
*/
func TestPrepareTickOrderMatchesFreshSortAcrossMembershipChanges(t *testing.T) {
	membership := []SessionSnapshot{}
	source := snapshotFunc(func() []SessionSnapshot {
		return append([]SessionSnapshot(nil), membership...)
	})
	ticker := &Ticker{Source: source, Push: discardPusher{}}

	freshOrder := func() []string {
		sorted := append([]SessionSnapshot(nil), membership...)
		sort.SliceStable(sorted, func(i, j int) bool {
			if sorted[i].DivisionID != sorted[j].DivisionID {
				return sorted[i].DivisionID < sorted[j].DivisionID
			}
			if sorted[i].SessionID != sorted[j].SessionID {
				return sorted[i].SessionID < sorted[j].SessionID
			}
			return sorted[i].CharacterID < sorted[j].CharacterID
		})
		out := make([]string, 0, len(sorted))
		for _, s := range sorted {
			out = append(out, s.DivisionID+"/"+s.SessionID)
		}
		return out
	}
	check := func(step string) {
		_, work := ticker.prepareTick()
		var got []string
		for _, w := range work {
			if len(w.sessions) == 0 {
				continue
			}
			for _, s := range w.sessions {
				got = append(got, s.DivisionID+"/"+s.SessionID)
			}
		}
		want := freshOrder()
		if len(got) != len(want) {
			t.Fatalf("%s: served %d sessions %v, want %d %v", step, len(got), got, len(want), want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: order[%d] = %s, want %s (served %v, fresh %v)", step, i, got[i], want[i], got, want)
			}
		}
	}
	set := func(next ...SessionSnapshot) {
		membership = next
	}

	// 1: first sight, unsorted walk (also the placement whose scratch
	// the shrink below leaves stale: its first slot holds position 3).
	set(
		SessionSnapshot{SessionID: "s40", DivisionID: "DIV_B", CharacterID: 40},
		SessionSnapshot{SessionID: "s10", DivisionID: "DIV_A", CharacterID: 10},
		SessionSnapshot{SessionID: "s30", DivisionID: "DIV_B", CharacterID: 30},
		SessionSnapshot{SessionID: "s20", DivisionID: "DIV_A", CharacterID: 20},
	)
	check("first sight")
	// 2: the same membership in another walk order (kept order applies).
	set(
		SessionSnapshot{SessionID: "s40", DivisionID: "DIV_B", CharacterID: 40},
		SessionSnapshot{SessionID: "s30", DivisionID: "DIV_B", CharacterID: 30},
		SessionSnapshot{SessionID: "s20", DivisionID: "DIV_A", CharacterID: 20},
		SessionSnapshot{SessionID: "s10", DivisionID: "DIV_A", CharacterID: 10},
	)
	check("steady shuffle")
	// 3: attach grows the membership.
	set(
		SessionSnapshot{SessionID: "s50", DivisionID: "DIV_B", CharacterID: 50},
		SessionSnapshot{SessionID: "s40", DivisionID: "DIV_B", CharacterID: 40},
		SessionSnapshot{SessionID: "s30", DivisionID: "DIV_B", CharacterID: 30},
		SessionSnapshot{SessionID: "s20", DivisionID: "DIV_A", CharacterID: 20},
		SessionSnapshot{SessionID: "s10", DivisionID: "DIV_A", CharacterID: 10},
	)
	check("attach")
	// 4: the shrink - three detach at once.
	set(
		SessionSnapshot{SessionID: "s20", DivisionID: "DIV_A", CharacterID: 20},
		SessionSnapshot{SessionID: "s10", DivisionID: "DIV_A", CharacterID: 10},
	)
	check("shrink")
	// 5: a same-count replacement, the new session first in the walk:
	// placement fails on its first session, past the shrunken scratch.
	set(
		SessionSnapshot{SessionID: "s15", DivisionID: "DIV_A", CharacterID: 15},
		SessionSnapshot{SessionID: "s10", DivisionID: "DIV_A", CharacterID: 10},
	)
	check("replace after shrink")
	// 6: steady under the new membership (placement must still apply:
	// the failed attempt above left no used marks behind).
	set(
		SessionSnapshot{SessionID: "s10", DivisionID: "DIV_A", CharacterID: 10},
		SessionSnapshot{SessionID: "s15", DivisionID: "DIV_A", CharacterID: 15},
	)
	check("steady after replace")
	// 7: a resume re-enters under a new scene revision, so its tick
	// session id changes: a membership change, not a kept session.
	set(
		SessionSnapshot{SessionID: "s10:2", DivisionID: "DIV_A", CharacterID: 10},
		SessionSnapshot{SessionID: "s15", DivisionID: "DIV_A", CharacterID: 15},
	)
	check("resume revision")
	// 8: a session changes division: the key carries the division, so
	// this re-sorts too.
	set(
		SessionSnapshot{SessionID: "s15", DivisionID: "DIV_B", CharacterID: 15},
		SessionSnapshot{SessionID: "s10:2", DivisionID: "DIV_A", CharacterID: 10},
	)
	check("division move")
}
