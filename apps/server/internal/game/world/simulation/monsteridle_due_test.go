/*
===========================================================================

monsteridle_due_test.go - an idle monster sleeps to its real due

The idle due no longer wakes on the activity cadence, which the gate
never reads for an idle monster (monsteractivity_schedule.go). The
proof is a pair of seeded worlds on one random stream: one scheduled
by the production rule, one by the conservative wake that stepped a
lapsed idle monster every tick. Stepping an actor more often must
change nothing a client or a later leg can see, so the two worlds
must publish the same frames and hold the same movers leg for leg -
and the production world must step far fewer actors to get there.

===========================================================================
*/

package simulation

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
idleDueWorld

The seeded leg world resolving tactics the way production does: off
each monster's own nest row, with no replacement resolver. Every
third nest is marked aggressive on evidence, so the acquisition
timer's due is exercised beside the passive idle deadline.
conservative installs monster.ResolveTactics as an explicit
replacement: the same answers, but the scheduler then keeps the old
cadence wake - the reference this file compares against.
================
*/
func idleDueWorld(t *testing.T, count int, conservative bool) (*MonsterMoverOps, []SessionSnapshot, *int64, []uint32) {
	t.Helper()
	ops, sessions, now := seededLegWorld(t, count)
	ops.TacticsFor = nil
	if conservative {
		ops.TacticsFor = monster.ResolveTactics
	}
	state := ops.Monsters.division(monsterTestDivision)
	instances := ops.Monsters.InstancesInRegions(monsterTestDivision, []uint16{monsterTestRegion})
	gids := make([]uint32, 0, len(instances))
	for _, instance := range instances {
		gids = append(gids, instance.Gid)
		if instance.Gid%3 == 0 {
			instance.Nest.PolicyPinned, instance.Nest.Aggressive = true, true
			instance.Nest.SightRange, instance.Nest.Radius = 115, 500
			state.instances.set(instance.Gid, instance)
		}
	}
	sort.Slice(gids, func(i, j int) bool { return gids[i] < gids[j] })
	return ops, sessions, now, gids
}

/*
================
idleDueLeg

One leg; what it pushed, in push order, and how many actors it
stepped.
================
*/
func idleDueLeg(ops *MonsterMoverOps, sessions []SessionSnapshot, now int64) (string, int) {
	push := &fakePusher{}
	ops.RunMonsterLeg(now, sessions, push)
	var out strings.Builder
	for _, pushed := range push.toSession {
		for _, frame := range pushed.frames {
			fmt.Fprintf(&out, "S %s %04x %s %v\n", pushed.sessionID, frame.Opcode, hex.EncodeToString(frame.Payload), frame.Scope)
		}
	}
	for _, pushed := range push.toDivision {
		for _, frame := range pushed.frames {
			fmt.Fprintf(&out, "D %s %04x %s %v\n", pushed.divisionID, frame.Opcode, hex.EncodeToString(frame.Payload), frame.Scope)
		}
	}
	return out.String(), ops.lastLeg.steps
}

/*
================
idleDueMovers

Every mover's behaviour-relevant value, in gid order: what the next
leg starts from.
================
*/
func idleDueMovers(t *testing.T, ops *MonsterMoverOps, gids []uint32) string {
	t.Helper()
	var out strings.Builder
	for _, gid := range gids {
		mover, ok := ops.Monsters.Mover(monsterTestDivision, gid)
		if !ok {
			t.Fatalf("mover %d missing", gid)
		}
		fmt.Fprintf(&out, "%d %d %+v %+v %+v %d %d %d %d %d %+v\n", gid, mover.Mode(), mover.Pose, mover.From, mover.To,
			mover.DepartMs, mover.ArriveMs, mover.BehaviorDeadlineMs, mover.NextAttackMs, mover.TargetGID(), mover.Activity)
	}
	return out.String()
}

/*
================
TestIdleDueMatchesTheEveryTickWake

Sixty seconds of the seeded world, the viewer walking away and back
so monsters idle, wander, chase, return and are suspended. The
production due and the conservative wake must tell the same story.
================
*/
func TestIdleDueMatchesTheEveryTickWake(t *testing.T) {
	const actors, legs = 120, 600
	production, sessions, now, gids := idleDueWorld(t, actors, false)
	reference, _, referenceNow, _ := idleDueWorld(t, actors, true)
	near := sessions[0]
	far := near
	far.World.Spawn.RegionID = monsterTestRegion + 0x0404
	productionSteps, referenceSteps := 0, 0
	for leg := 0; leg < legs; leg++ {
		viewer := near
		if leg >= 200 && leg < 400 {
			viewer = far
		}
		*now = seededLegBaseMs + int64(leg)*100
		*referenceNow = *now
		got, steps := idleDueLeg(production, []SessionSnapshot{viewer}, *now)
		want, stepsReference := idleDueLeg(reference, []SessionSnapshot{viewer}, *now)
		productionSteps += steps
		referenceSteps += stepsReference
		if got != want {
			t.Fatalf("leg %d: the production due published differently from the every-tick wake\nproduction:\n%s\nreference:\n%s", leg, got, want)
		}
		if got, want := idleDueMovers(t, production, gids), idleDueMovers(t, reference, gids); got != want {
			t.Fatalf("leg %d: movers differ from the every-tick wake\nproduction:\n%s\nreference:\n%s", leg, got, want)
		}
	}
	if productionSteps*10 > referenceSteps*9 {
		t.Fatalf("the production due stepped %d actors against the wake's %d: idle monsters are not sleeping", productionSteps, referenceSteps)
	}
	t.Logf("%d legs, %d actors: production due stepped %d, the every-tick wake %d", legs, actors, productionSteps, referenceSteps)
}

/*
================
TestIdleMonsterIsNotWokenByItsLapsedCadence

One passive idle monster whose cadence lapsed long ago and whose
idle deadline is a minute out: after the leg that arms it, no leg
steps it until the deadline, and the deadline leg does.
================
*/
func TestIdleMonsterIsNotWokenByItsLapsedCadence(t *testing.T) {
	ops, instance := monsterLegFixture(t, passiveTactics())
	ops.TacticsFor = nil
	const start = int64(100000)
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	mover.BehaviorDeadlineMs = start + 60000
	mover.Activity = monster.ActivityCadence{Interval: 1000, LastCheck: uint32(start) - 30000}
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	viewer := playerSessionAt(1, 1050, 1000)
	stepped := func(now int64) bool {
		ops.RunMonsterLeg(now, []SessionSnapshot{viewer}, &fakePusher{})
		after, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		return ops.lastLeg.restGuards[restGuardCadence]+ops.lastLeg.restGuards[restGuardDeadline] > 0 || after.Mode() != monster.MoverIdle
	}
	// The first legs complete the fixture's idle entry and arm the due.
	stepped(start)
	stepped(start + 100)
	for now := start + 200; now <= start+60000; now += 100 {
		if stepped(now) {
			t.Fatalf("idle monster stepped at +%d ms, %d ms before its deadline", now-start, start+60000-now)
		}
	}
	if !stepped(start + 60100) {
		t.Fatal("idle monster was not stepped on its deadline")
	}
}
