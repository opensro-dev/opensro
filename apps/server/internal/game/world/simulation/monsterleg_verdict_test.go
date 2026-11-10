/*
===========================================================================

monsterleg_verdict_test.go - the settled lane's stage-1 census:
verdict counters over seeded legs

#3027 stage 1: before any settled-lane code exists, the leg record
counts every rest verdict by its failing guard and every settled
candidate - a cadence-due rest step that emitted and committed
nothing. These tests pin the counters on one fixture step, drained
events included, and measure the settled fraction on the two
seeded worlds the stage gate prices: the busy golden world, and a
resting world whose actors sit idle past their cadence.
===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
legVerdictTotals

One measurement window's verdict census, folded leg by leg from
the retained leg record (ops.lastLeg).
================
*/
type legVerdictTotals struct {
	steps       int
	restGuards  [restGuardCount]int
	settled     int
	drained     int
	cadenceArms int
	suspended   int
	actors      int
}

/*
================
legVerdictTotals.add

Fold one finished leg's record into the window.
================
*/
func (totals *legVerdictTotals) add(stats *legStats) {
	totals.steps += stats.steps
	for guard := range stats.restGuards {
		totals.restGuards[guard] += stats.restGuards[guard]
	}
	totals.settled += stats.restSettled
	totals.drained += stats.restSettledDrained
	totals.cadenceArms += stats.cadenceArms
	totals.suspended += stats.gateSuspended
	totals.actors += stats.actors
}

/*
================
legVerdictTotals.verdicts

The window's rest verdicts in all: one per resting step that
reached the verdict, pass included.
================
*/
func (totals *legVerdictTotals) verdicts() int {
	verdicts := 0
	for _, count := range totals.restGuards {
		verdicts += count
	}
	return verdicts
}

/*
================
runVerdictLegs

Run legs at the seeded 100 ms cadence and fold each leg's verdict
census into one window.
================
*/
func runVerdictLegs(ops *MonsterMoverOps, sessions []SessionSnapshot, now *int64, legs int) legVerdictTotals {
	var totals legVerdictTotals
	for leg := 0; leg < legs; leg++ {
		*now += 100
		ops.RunMonsterLeg(*now, sessions, discardPusher{})
		totals.add(ops.lastLeg)
	}
	return totals
}

/*
================
TestRestVerdictCountersCountSettledSteps

One resting fixture actor stepped at an overdue cadence: the step
counts once, under the cadence guard, as settled. With a tactics
event queued the step still settles - the drain is the step's
whole side effect - and counts as the drained share the lane's
drain-and-carry ruling must reproduce.
================
*/
func TestRestVerdictCountersCountSettledSteps(t *testing.T) {
	ops, instance := monsterLegFixture(t, passiveTactics())
	ops.activity = ops.captureActivity(nil, 0)
	const now = int64(10000)
	seeded, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	seeded.BehaviorDeadlineMs = now + 60000
	seeded.Activity = monster.ActivityCadence{Interval: 60000, LastCheck: uint32(now)}
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, seeded)
	// The fixture mover still owes its idle entry; one step completes
	// it and the monster is at rest from there.
	ops.advanceInstance(monsterTestDivision, instance, nil, now)
	rest, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	rest.BehaviorDeadlineMs = now + 60000
	rest.Activity = monster.ActivityCadence{Interval: 1000, LastCheck: uint32(now) - 5000}
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, rest)

	leg := &legStats{}
	ops.leg = leg
	if frames, _ := ops.advanceInstance(monsterTestDivision, instance, nil, now+100); len(frames) != 0 {
		t.Fatalf("settled step published %d frames", len(frames))
	}
	if leg.steps != 1 || leg.restGuards[restGuardCadence] != 1 || leg.restSettled != 1 || leg.restSettledDrained != 0 {
		t.Fatalf("counters after one settled step: steps=%d cadence=%d settled=%d drained=%d",
			leg.steps, leg.restGuards[restGuardCadence], leg.restSettled, leg.restSettledDrained)
	}

	ops.Monsters.mu.Lock()
	state := ops.Monsters.populationForObject(monsterTestDivision, instance.Gid)
	state.queueAIEvents(instance.Gid, []monsterAIEvent{{Event: aiEventStart, Kind: 9, Source: 7}})
	ops.Monsters.mu.Unlock()
	if frames, _ := ops.advanceInstance(monsterTestDivision, instance, nil, now+200); len(frames) != 0 {
		t.Fatalf("drained settled step published %d frames", len(frames))
	}
	if leg.steps != 2 || leg.restGuards[restGuardCadence] != 2 || leg.restSettled != 2 || leg.restSettledDrained != 1 {
		t.Fatalf("counters after the drained step: steps=%d cadence=%d settled=%d drained=%d",
			leg.steps, leg.restGuards[restGuardCadence], leg.restSettled, leg.restSettledDrained)
	}
	if queued := ops.Monsters.peekAIEvents(monsterTestDivision, instance.Gid); len(queued) != 0 {
		t.Fatalf("settled step left %d tactics events queued", len(queued))
	}
}

/*
================
TestSeededRestWorldSettledFraction

The stage gate's resting world: the seeded grid with passive
tactics and idle deadlines minutes out, so every actor sits at
rest past its activity cadence. In steady state nearly every due
step is a settled candidate, the gate writes nothing for any of
them (an idle re-check stamps no row; the remainder arm re-arms
the untouched row at leg end), and no step drains an event.
================
*/
func TestSeededRestWorldSettledFraction(t *testing.T) {
	ops, sessions, now := seededLegWorld(t, 240)
	ops.TacticsFor = fixedTactics(passiveTactics())
	instances := ops.Monsters.InstancesInRegions(monsterTestDivision, []uint16{monsterTestRegion})
	if len(instances) != 240 {
		t.Fatalf("rest world seeded %d monsters, want 240", len(instances))
	}
	for _, instance := range instances {
		mover, ok := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if !ok {
			t.Fatalf("rest world monster %d has no mover", instance.Gid)
		}
		mover.BehaviorDeadlineMs = *now + 600000
		ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	}
	// Warmup: idle entries complete and every cadence falls due.
	runVerdictLegs(ops, sessions, now, 40)
	totals := runVerdictLegs(ops, sessions, now, 30)

	candidates := totals.restGuards[restGuardCadence]
	if totals.steps == 0 {
		t.Fatal("rest world stepped nobody")
	}
	if totals.verdicts() > totals.steps {
		t.Fatalf("rest verdicts %d exceed steps %d", totals.verdicts(), totals.steps)
	}
	if candidates == 0 {
		t.Fatal("rest world produced no cadence candidates")
	}
	if totals.settled > candidates {
		t.Fatalf("settled %d exceeds candidates %d", totals.settled, candidates)
	}
	if totals.settled*10 < candidates*9 {
		t.Fatalf("settled %d of %d candidates: the resting step does more than re-check its cadence",
			totals.settled, candidates)
	}
	if totals.drained != 0 {
		t.Fatalf("rest world settled steps drained %d events, want none queued", totals.drained)
	}
	if totals.cadenceArms != 0 || totals.suspended != 0 {
		t.Fatalf("gate wrote for idle resters: cadence arms %d, suspensions %d", totals.cadenceArms, totals.suspended)
	}
	t.Logf("rest world: steps/leg=%d settled/leg=%d settled/steps=%.3f settled/candidates=%.3f verdicts=%v",
		totals.steps/30, totals.settled/30, float64(totals.settled)/float64(totals.steps),
		float64(totals.settled)/float64(candidates), totals.restGuards)
}

/*
================
TestSeededBusyWorldSettledFraction

The stage gate's busy bound: the golden world as seeded -
aggressive actors on a live viewer, deadlines at zero - where
resting steps are the minority. Consistency only; the fraction
is the measurement the stage-gate record quotes.
================
*/
func TestSeededBusyWorldSettledFraction(t *testing.T) {
	ops, sessions, now := seededLegWorld(t, 240)
	// Warmup: spawn holds finish and the population reaches its
	// working mix of wandering, chasing and resting actors.
	runVerdictLegs(ops, sessions, now, 60)
	totals := runVerdictLegs(ops, sessions, now, 40)

	candidates := totals.restGuards[restGuardCadence]
	if totals.steps == 0 {
		t.Fatal("busy world stepped nobody")
	}
	if totals.verdicts() > totals.steps {
		t.Fatalf("rest verdicts %d exceed steps %d", totals.verdicts(), totals.steps)
	}
	if totals.settled > candidates {
		t.Fatalf("settled %d exceeds candidates %d", totals.settled, candidates)
	}
	if totals.drained > totals.settled {
		t.Fatalf("drained %d exceeds settled %d", totals.drained, totals.settled)
	}
	t.Logf("busy world: steps/leg=%d settled/leg=%d settled/steps=%.3f candidates/steps=%.3f verdicts=%v cadarms=%d suspended=%d",
		totals.steps/40, totals.settled/40, float64(totals.settled)/float64(totals.steps),
		float64(candidates)/float64(totals.steps), totals.restGuards, totals.cadenceArms, totals.suspended)
}
