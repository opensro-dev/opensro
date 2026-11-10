/*
===========================================================================

monsteridle_rest_door_test.go - the rest-facts door: equivalence with
the expanded read

#3027 stage 2 (design A): the settled verdict must be computable at
snapshot-build time from the stored rows, without the 2.7 KB fill.
These tests pin the door (MonsterState.restFactsAt) against the
expanded read (behaviorActorMover plus restGuardFailure over the
fill): the same named guard over seeded states, a named refusal for
every verdict input flipped in storage, and the settled outcome -
a candidate's step is a no-op on the rows, and each extra that
refuses settlement (a queued tactics event above all) is one
today's step observably performs.

===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/world/monster"
)

const restDoorDivision = "benchmark"
const restDoorNow = int64(10000)

/*
================
restDoorFixture

One actor at rest in a raw division state: instance row set, spawn
mover stepped through its idle entry, cadence stamped fresh - the
stage-1 resting monster, seeded at the storage the door reads.
================
*/
func restDoorFixture(t testing.TB) (*MonsterMoverOps, *MonsterState, *divisionMonsterState, monster.Instance) {
	t.Helper()
	s := NewMonsterState(monster.Template{})
	d := &divisionMonsterState{
		instances: newMonsterStorage(nil),
		movers:    newMoverStorage(nil),
		aiEvents:  map[uint32][]monsterAIEvent{},
	}
	s.divs[restDoorDivision] = d
	spawn := monster.SpawnPoint{RefObjID: 1933, RegionID: monsterTestRegion, X: 1000, Y: 20, Z: 1000}
	instance := monster.Instance{
		Gid:       1,
		CurrentHP: 100,
		Ref:       monster.MonsterRef{RefObjID: 1933, MaxHP: 100, WalkSpeed: 8, RunSpeed: 22},
		Nest:      monster.NestRow{SpawnPoint: spawn},
		Spawn:     spawn,
	}
	d.instances.set(instance.Gid, instance)
	d.movers.set(instance.Gid, monster.NewSpawnMover(instance, restDoorNow))

	ops := &MonsterMoverOps{Monsters: s, Rand: func() float64 { return 0.5 }}
	ops.activity = ops.captureActivity(nil, 0)
	mover, _ := s.Mover(restDoorDivision, instance.Gid)
	mustMoverTransition(&mover, monster.MoverEventSpawnHoldElapsed, 0)
	s.CommitMover(restDoorDivision, instance.Gid, mover)
	// One step completes the idle entry; the monster rests from here.
	ops.advanceInstance(restDoorDivision, instance, nil, restDoorNow)
	rest, _ := s.Mover(restDoorDivision, instance.Gid)
	rest.BehaviorDeadlineMs = restDoorNow + 60000
	rest.Activity = monster.ActivityCadence{Interval: 60000, LastCheck: uint32(restDoorNow)}
	s.CommitMover(restDoorDivision, instance.Gid, rest)
	return ops, s, d, instance
}

/*
================
restDoorVerdicts

Both verdicts over the fixture actor as storage now stands: the
door's guard and settled answers, and the expanded read's guard -
plus the row identity both reads stamp, which a later commit
guards on and so must agree too.
================
*/
func restDoorVerdicts(t testing.TB, ops *MonsterMoverOps, s *MonsterState, gid uint32, now int64) (guard, expanded restGuard, settled bool) {
	t.Helper()
	key := populationKey{division: restDoorDivision}
	ref := behaviorActorRef{gid: gid, slot: behaviorNoMoverSlot, monsterSlot: behaviorNoMonsterSlot}
	facts, ok := s.restFactsAt(key, ref, now)
	if !ok {
		t.Fatalf("gid %d: the door lost the actor", gid)
	}
	snapshot := s.behaviorActorMover(key, ref)
	if !snapshot.ok {
		t.Fatalf("gid %d: the expanded read lost the actor", gid)
	}
	if facts.slot != snapshot.slot || facts.monsterSlot != snapshot.monsterSlot || facts.moverGen != snapshot.moverGen {
		t.Fatalf("gid %d: identity (slot %d, monsterSlot %d, gen %d), expanded read stamps (%d, %d, %d)",
			gid, facts.slot, facts.monsterSlot, facts.moverGen, snapshot.slot, snapshot.monsterSlot, snapshot.moverGen)
	}
	expanded = restGuardFailure(ops, &snapshot.instance, snapshot.mover, monster.ResolveTactics(&snapshot.instance), now)
	return restGuardFailureFromFacts(facts, ops.activity != nil, now), expanded,
		restSettledFromFacts(facts, ops.activity != nil, now)
}

/*
================
TestRestFactsVerdictMatchesExpandedRead

At rest the door and the expanded read agree (pass, not settled);
making the cadence due settles both. Every single verdict input
flipped in storage then refuses at the same named guard on both
reads - a door projection that stops reading its fact fails here
under that guard's name. The target guard's mover side is the one
flip not seeded: a held target under idle mode is not a state the
transitions produce, and for live rows the door reads the same
TargetGID accessor the expanded read does, in place.
================
*/
func TestRestFactsVerdictMatchesExpandedRead(t *testing.T) {
	ops, s, d, instance := restDoorFixture(t)

	guard, expanded, settled := restDoorVerdicts(t, ops, s, instance.Gid, restDoorNow)
	if guard != restGuardPass || expanded != restGuardPass || settled {
		t.Fatalf("at rest: door %s, expanded %s, settled %v; want pass, pass, false",
			restGuardNames[guard], restGuardNames[expanded], settled)
	}

	// The cadence falls due: both reads name the cadence guard and
	// the actor settles.
	mover, _ := s.Mover(restDoorDivision, instance.Gid)
	mover.Activity.LastCheck = wrappedLastCheck(restDoorNow)
	s.CommitMover(restDoorDivision, instance.Gid, mover)
	guard, expanded, settled = restDoorVerdicts(t, ops, s, instance.Gid, restDoorNow)
	if guard != restGuardCadence || expanded != restGuardCadence || !settled {
		t.Fatalf("cadence due: door %s, expanded %s, settled %v; want cadence, cadence, true",
			restGuardNames[guard], restGuardNames[expanded], settled)
	}
	mover.Activity.LastCheck = uint32(restDoorNow)
	s.CommitMover(restDoorDivision, instance.Gid, mover)

	flips := []struct {
		name   string
		guard  restGuard
		mutate func()
	}{
		{"wandering mode", restGuardMode, func() {
			m, _ := s.Mover(restDoorDivision, instance.Gid)
			mustMoverTransition(&m, monster.MoverEventStartWander, 0)
			s.CommitMover(restDoorDivision, instance.Gid, m)
		}},
		{"help pending", restGuardHelp, func() {
			row := d.instances.get(instance.Gid)
			row.Help = row.Help.Replace(monster.HelpEvent{})
			d.instances.set(instance.Gid, row)
		}},
		{"tempted", restGuardTarget, func() {
			row := d.instances.get(instance.Gid)
			row.Abnormal = &abnormal.Block{}
			row.Abnormal.Slots[abnormal.Confusion].Active = true
			d.instances.set(instance.Gid, row)
		}},
		{"aggressive nest", restGuardAggressive, func() {
			row := d.instances.get(instance.Gid)
			row.Nest.Aggressive = true
			row.Nest.HasControls = true
			d.instances.set(instance.Gid, row)
		}},
		{"idle deadline passed", restGuardDeadline, func() {
			m, _ := s.Mover(restDoorDivision, instance.Gid)
			m.BehaviorDeadlineMs = restDoorNow - 1
			s.CommitMover(restDoorDivision, instance.Gid, m)
		}},
		{"navigation goal held", restGuardGoal, func() {
			m, _ := s.Mover(restDoorDivision, instance.Gid)
			m.WaitNavigation(m.Pose, restDoorNow+1000)
			s.CommitMover(restDoorDivision, instance.Gid, m)
		}},
		{"segment endpoints held", restGuardSegment, func() {
			m, _ := s.Mover(restDoorDivision, instance.Gid)
			m.From = m.Pose
			s.CommitMover(restDoorDivision, instance.Gid, m)
		}},
		{"segment matured", restGuardSegment, func() {
			m, _ := s.Mover(restDoorDivision, instance.Gid)
			m.DepartMs, m.ArriveMs = restDoorNow-2000, restDoorNow-1000
			s.CommitMover(restDoorDivision, instance.Gid, m)
		}},
		{"segment in flight", restGuardSegment, func() {
			m, _ := s.Mover(restDoorDivision, instance.Gid)
			m.From, m.To = m.Pose, m.Pose
			m.DepartMs, m.ArriveMs = restDoorNow-1000, restDoorNow+1000
			s.CommitMover(restDoorDivision, instance.Gid, m)
		}},
	}
	for _, flip := range flips {
		// Every flip starts from a fresh resting state: rebuild the
		// fixture so no flip can lean on the last one's rows.
		ops, s, d, instance = restDoorFixture(t)
		flip.mutate()
		guard, expanded, settled = restDoorVerdicts(t, ops, s, instance.Gid, restDoorNow)
		if guard != flip.guard || expanded != flip.guard {
			t.Fatalf("%s: door %s, expanded %s; want %s from both",
				flip.name, restGuardNames[guard], restGuardNames[expanded], restGuardNames[flip.guard])
		}
		if settled {
			t.Fatalf("%s: the door settled an actor the %s guard refuses", flip.name, restGuardNames[flip.guard])
		}
	}
}

/*
================
TestRestFactsAggressiveGateAndUnwiredCadence

The two verdict inputs that are derivations, not stored facts:
ResolveTactics calls a nest aggressive only behind its enabling
flags, so a nest carrying the aggressive bit alone must pass the
aggressive guard on both reads; and the cadence guard exists only
while an activity snapshot is wired (the expanded read's own
condition), so a due cadence with none wired names no guard and
settles nothing.
================
*/
func TestRestFactsAggressiveGateAndUnwiredCadence(t *testing.T) {
	ops, s, d, instance := restDoorFixture(t)
	row := d.instances.get(instance.Gid)
	row.Nest.Aggressive = true
	d.instances.set(instance.Gid, row)
	guard, expanded, settled := restDoorVerdicts(t, ops, s, instance.Gid, restDoorNow)
	if guard != restGuardPass || expanded != restGuardPass || settled {
		t.Fatalf("ungated aggressive nest: door %s, expanded %s, settled %v; want pass, pass, false",
			restGuardNames[guard], restGuardNames[expanded], settled)
	}

	mover, _ := s.Mover(restDoorDivision, instance.Gid)
	mover.Activity.LastCheck = wrappedLastCheck(restDoorNow)
	s.CommitMover(restDoorDivision, instance.Gid, mover)
	key := populationKey{division: restDoorDivision}
	ref := behaviorActorRef{gid: instance.Gid, slot: behaviorNoMoverSlot, monsterSlot: behaviorNoMonsterSlot}
	facts, ok := s.restFactsAt(key, ref, restDoorNow)
	if !ok {
		t.Fatal("the door lost the actor")
	}
	if got := restGuardFailureFromFacts(facts, false, restDoorNow); got != restGuardPass {
		t.Fatalf("unwired cadence: door named %s, want pass", restGuardNames[got])
	}
	if restSettledFromFacts(facts, false, restDoorNow) {
		t.Fatal("unwired cadence: the door settled a due actor with no activity snapshot")
	}
}

/*
================
TestRestFactsSummonedRefusesAtItsGuard

A summoned binding pends the idle entry it arrives with (the
stage-1 recipe): complete the entry through the step's own door,
and both reads then refuse at the summoned guard alone.
================
*/
func TestRestFactsSummonedRefusesAtItsGuard(t *testing.T) {
	ops, s, _, instance := restDoorFixture(t)
	mover, _ := s.Mover(restDoorDivision, instance.Gid)
	if err := mover.BindController(monster.ControlSummoned, 777); err != nil {
		t.Fatalf("bind summoned controller: %v", err)
	}
	ops.commitIdleEntry(restDoorDivision, &instance, mover, nil, restDoorNow)
	guard, expanded, settled := restDoorVerdicts(t, ops, s, instance.Gid, restDoorNow)
	if guard != restGuardSummoned || expanded != restGuardSummoned || settled {
		t.Fatalf("summoned: door %s, expanded %s, settled %v; want summoned, summoned, false",
			restGuardNames[guard], restGuardNames[expanded], settled)
	}
}

/*
================
TestRestFactsSettledExtrasRefuse

Each settled extra refuses on its own while the guard verdict
still names only the cadence: a held motion and a dead actor (the
step's head guard), a cross-plane nest the detach would fire on,
and a queued tactics event the step must drain. The verdict is
conservative here by construction - the lane settles only the
population whose every stage is proven nil.
================
*/
func TestRestFactsSettledExtrasRefuse(t *testing.T) {
	extras := []struct {
		name   string
		mutate func(ops *MonsterMoverOps, s *MonsterState, d *divisionMonsterState, instance monster.Instance)
	}{
		{"motion held", func(_ *MonsterMoverOps, _ *MonsterState, d *divisionMonsterState, instance monster.Instance) {
			row := d.instances.get(instance.Gid)
			row.Motion = monster.MotionHold{State: 1, UntilMs: restDoorNow + 60000}
			d.instances.set(instance.Gid, row)
		}},
		{"dead", func(_ *MonsterMoverOps, _ *MonsterState, d *divisionMonsterState, instance monster.Instance) {
			row := d.instances.get(instance.Gid)
			row.CurrentHP = 0
			d.instances.set(instance.Gid, row)
		}},
		{"cross-plane nest", func(_ *MonsterMoverOps, _ *MonsterState, d *divisionMonsterState, instance monster.Instance) {
			row := d.instances.get(instance.Gid)
			row.Nest.RegionID |= 0x8000
			d.instances.set(instance.Gid, row)
		}},
		{"tactics event queued", func(_ *MonsterMoverOps, _ *MonsterState, d *divisionMonsterState, instance monster.Instance) {
			d.aiEvents[instance.Gid] = []monsterAIEvent{{Event: aiEventStart, Kind: 9, Source: 42}}
		}},
	}
	for _, extra := range extras {
		ops, s, d, instance := restDoorFixture(t)
		mover, _ := s.Mover(restDoorDivision, instance.Gid)
		mover.Activity.LastCheck = wrappedLastCheck(restDoorNow)
		s.CommitMover(restDoorDivision, instance.Gid, mover)
		extra.mutate(ops, s, d, instance)
		guard, expanded, settled := restDoorVerdicts(t, ops, s, instance.Gid, restDoorNow)
		if guard != restGuardCadence || expanded != restGuardCadence {
			t.Fatalf("%s: door %s, expanded %s; want the cadence guard from both",
				extra.name, restGuardNames[guard], restGuardNames[expanded])
		}
		if settled {
			t.Fatalf("%s: the door settled an actor the settled extras refuse", extra.name)
		}
	}

	// Already detached, the same cross-plane nest can never detach
	// again, so the face is a no-op and the actor settles.
	ops, s, d, instance := restDoorFixture(t)
	mover, _ := s.Mover(restDoorDivision, instance.Gid)
	mover.Activity.LastCheck = wrappedLastCheck(restDoorNow)
	s.CommitMover(restDoorDivision, instance.Gid, mover)
	row := d.instances.get(instance.Gid)
	row.Nest.RegionID |= 0x8000
	row.NestDetached = true
	d.instances.set(instance.Gid, row)
	if _, _, settled := restDoorVerdicts(t, ops, s, instance.Gid, restDoorNow); !settled {
		t.Fatal("a detached cross-plane actor did not settle")
	}
}

/*
================
TestRestFactsPendingAndSpawnRows

The door's two mover projections beside the live row: a pending
(spawning) row and an actor with no mover row at all - the fill
synthesizes its spawn mover - both refuse at the mode guard on
both reads, and a gone actor answers not-ok on both.
================
*/
func TestRestFactsPendingAndSpawnRows(t *testing.T) {
	ops, s, d, instance := restDoorFixture(t)

	pending := monster.Instance{
		Gid:       2,
		CurrentHP: 100,
		Ref:       monster.MonsterRef{RefObjID: 1933, MaxHP: 100, WalkSpeed: 8},
		Nest:      instance.Nest,
		Spawn:     instance.Spawn,
	}
	d.instances.set(pending.Gid, pending)
	d.movers.set(pending.Gid, monster.NewSpawnMover(pending, restDoorNow))
	guard, expanded, settled := restDoorVerdicts(t, ops, s, pending.Gid, restDoorNow)
	if guard != restGuardMode || expanded != restGuardMode || settled {
		t.Fatalf("spawning: door %s, expanded %s, settled %v; want mode, mode, false",
			restGuardNames[guard], restGuardNames[expanded], settled)
	}

	spawnless := pending
	spawnless.Gid = 3
	d.instances.set(spawnless.Gid, spawnless)
	guard, expanded, settled = restDoorVerdicts(t, ops, s, spawnless.Gid, restDoorNow)
	if guard != restGuardMode || expanded != restGuardMode || settled {
		t.Fatalf("no mover row: door %s, expanded %s, settled %v; want mode, mode, false",
			restGuardNames[guard], restGuardNames[expanded], settled)
	}

	key := populationKey{division: restDoorDivision}
	ref := behaviorActorRef{gid: 99, slot: behaviorNoMoverSlot, monsterSlot: behaviorNoMonsterSlot}
	if _, ok := s.restFactsAt(key, ref, restDoorNow); ok {
		t.Fatal("the door answered for a gone actor")
	}
	if snapshot := s.behaviorActorMover(key, ref); snapshot.ok {
		t.Fatal("the expanded read answered for a gone actor")
	}
}

/*
================
TestRestSettledStepOutcomeIsNil

The settled population's outcome, proven against the step itself:
a candidate's step publishes nothing and leaves the mover row
exactly as it stood, while an actor with a queued tactics event -
which the door refuses to settle - has its queue drained by the
same step. The drain is the observable difference the events
extra exists for.
================
*/
func TestRestSettledStepOutcomeIsNil(t *testing.T) {
	ops, s, d, instance := restDoorFixture(t)
	mover, _ := s.Mover(restDoorDivision, instance.Gid)
	mover.Activity.LastCheck = wrappedLastCheck(restDoorNow)
	s.CommitMover(restDoorDivision, instance.Gid, mover)
	if _, _, settled := restDoorVerdicts(t, ops, s, instance.Gid, restDoorNow); !settled {
		t.Fatal("the candidate did not settle")
	}
	before, _ := s.Mover(restDoorDivision, instance.Gid)
	if frames, _ := ops.advanceInstance(restDoorDivision, instance, nil, restDoorNow); len(frames) != 0 {
		t.Fatalf("a settled candidate's step published %d frames", len(frames))
	}
	after, _ := s.Mover(restDoorDivision, instance.Gid)
	if after.Mode() != before.Mode() || after.BehaviorDeadlineMs != before.BehaviorDeadlineMs ||
		after.Activity != before.Activity {
		t.Fatalf("a settled candidate's step moved the row: %+v -> %+v", before, after)
	}

	// Queue an event and the same step drains it: not a settled
	// outcome, and the door says so.
	ops, s, d, instance = restDoorFixture(t)
	mover, _ = s.Mover(restDoorDivision, instance.Gid)
	mover.Activity.LastCheck = wrappedLastCheck(restDoorNow)
	s.CommitMover(restDoorDivision, instance.Gid, mover)
	d.aiEvents[instance.Gid] = []monsterAIEvent{{Event: aiEventStart, Kind: 9, Source: 42}}
	if _, _, settled := restDoorVerdicts(t, ops, s, instance.Gid, restDoorNow); settled {
		t.Fatal("an actor with a queued event settled")
	}
	ops.advanceInstance(restDoorDivision, instance, nil, restDoorNow)
	if len(d.aiEvents[instance.Gid]) != 0 {
		t.Fatal("the step left a queued tactics event undrained")
	}
}
