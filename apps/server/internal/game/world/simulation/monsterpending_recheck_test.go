/*
===========================================================================

monsterpending_recheck_test.go - the pending re-check lane against the
full step

The lane (monsterpending_recheck.go) must leave the world exactly as
the step leaves it. Every test here runs the same world twice - once
with the lane, once with every due actor routed through the full
step - and compares what a player or the next leg could observe:
the stored mover and instance rows, the armed due, and the pushed
frames. The lane's own counters only prove it was the lane that ran.

===========================================================================
*/

package simulation

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/abnormal"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

const (
	pendingLaneDivision = "lane"
	pendingLaneGID      = uint32(1000)
	pendingLaneNowMs    = int64(200000)
	pendingLaneInterval = 1500
)

/*
================
pendingLaneFixture

One bare world holding suspended actors, with direct hands on its
rows: the test arranges an actor's facts, runs one leg and reads the
rows back.
================
*/
type pendingLaneFixture struct {
	ops      *MonsterMoverOps
	state    *divisionMonsterState
	push     *fakePusher
	sessions []SessionSnapshot
}

/*
================
newPendingLaneFixture

A world of count quiet suspended actors, each due for its re-check
at pendingLaneNowMs. fullStep builds the reference world: the same
rows, with the lane switched off.
================
*/
func newPendingLaneFixture(tb testing.TB, count int, fullStep bool) *pendingLaneFixture {
	tb.Helper()
	s := NewMonsterState(monster.Template{})
	s.StartDivision(pendingLaneDivision)
	state := s.divs[pendingLaneDivision]
	state.movers = newMoverStorage(nil)
	for i := 0; i < count; i++ {
		gid := pendingLaneGID + uint32(i)
		state.instances.set(gid, monster.Instance{Gid: gid, Ref: monster.MonsterRef{MaxHP: 100, WalkSpeed: 8, RunSpeed: 22},
			Nest: monster.NestRow{Respawn: true}, CurrentHP: 100})
		state.movers.set(gid, monster.PendingMover{
			Pose:     monster.Pose{RegionID: monsterTestRegion, X: 500 + float64(i%40), Y: 45, Z: 500 + float64(i/40)},
			Activity: monster.ActivityCadence{Interval: pendingLaneInterval, LastCheck: uint32(pendingLaneNowMs) - pendingLaneInterval - 1},
		}.Expand())
		state.behavior.set(gid, 0)
	}
	// One seed for both worlds: a lane that drew from the stream, or
	// stepped in another order, would pull them apart.
	rng := rand.New(rand.NewSource(3027))
	ops := &MonsterMoverOps{
		Monsters:            s,
		TacticsFor:          fixedTactics(passiveTactics()),
		Rand:                rng.Float64,
		stepPendingRechecks: fullStep,
	}
	ops.divisionID = pendingLaneDivision
	return &pendingLaneFixture{ops: ops, state: state, push: &fakePusher{}}
}

/*
================
pendingLaneFixture.viewer

A combat-eligible session of this world's own population standing at
the pose.
================
*/
func (fx *pendingLaneFixture) viewer(region uint16, x, z float64) SessionSnapshot {
	return SessionSnapshot{
		SessionID:      "viewer",
		DivisionID:     pendingLaneDivision,
		CharacterID:    1,
		WorldInstance:  uint32(fx.state.lease.ID),
		Population:     fx.state.lease,
		CombatEligible: true,
		World:          WorldState{Spawn: Spawn{RegionID: region, X: x, Y: 45, Z: z}, SpawnSet: true},
		BodyRadius:     BodyRadius(4),
	}
}

/*
================
pendingLaneFixture.editActor, editMover

Rewrite the first actor's instance or its compact mover through the
storage's own set, then make it due again.
================
*/
func (fx *pendingLaneFixture) editActor(edit func(*monster.Instance)) {
	actor, _ := fx.state.instances.lookup(pendingLaneGID)
	edit(&actor)
	fx.state.instances.set(pendingLaneGID, actor)
	fx.state.behavior.set(pendingLaneGID, 0)
}

func (fx *pendingLaneFixture) editMover(edit func(*monster.PendingMover)) {
	stored, _ := fx.state.movers.lookup(pendingLaneGID)
	pending, compact := stored.PendingSnapshot()
	if !compact {
		panic("fixture mover is not a compact pending row")
	}
	edit(&pending)
	fx.state.movers.set(pendingLaneGID, pending.Expand())
	fx.state.behavior.set(pendingLaneGID, 0)
}

/*
================
pendingLaneFixture.leg

One leg at now; the leg's attribution record comes back.
================
*/
func (fx *pendingLaneFixture) leg(now int64) *legStats {
	fx.ops.RunMonsterLeg(now, fx.sessions, fx.push)
	return fx.ops.lastLeg
}

/*
================
pendingLaneFixture.observed

Everything about one actor a later leg or a client could tell apart:
whether it still exists, its rows, whether it sleeps, and its armed
due.
================
*/
func (fx *pendingLaneFixture) observed(gid uint32) string {
	mover, moverOK := fx.state.movers.lookup(gid)
	actor, actorOK := fx.state.instances.lookup(gid)
	_, dormant := fx.state.dormant[gid]
	due, armed := int64(0), false
	if slot, ok := fx.state.behavior.byGID[gid]; ok {
		due, armed = fx.state.behavior.slots[slot].at, fx.state.behavior.slots[slot].live
	}
	// The two pointer fields are compared by what they point at: each
	// world owns its own copies.
	abnormalBlock, linked := "none", "none"
	if actor.Abnormal != nil {
		abnormalBlock = fmt.Sprintf("%+v", *actor.Abnormal)
	}
	if actor.LinkedEffects != nil {
		linked = fmt.Sprintf("%+v", *actor.LinkedEffects)
	}
	actor.Abnormal, actor.LinkedEffects = nil, nil
	return fmt.Sprintf("actor=%v %+v abnormal=%s linked=%s\nmover=%v gen=%d %+v\ndormant=%v due=%d armed=%v",
		actorOK, actor, abnormalBlock, linked, moverOK, fx.state.movers.generation(gid), mover, dormant, due, armed)
}

/*
================
requireSameWorld

The lane world and the reference world agree on every actor and on
every frame pushed since the last comparison.
================
*/
func requireSameWorld(t *testing.T, stage string, lane, reference *pendingLaneFixture, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		gid := pendingLaneGID + uint32(i)
		if got, want := lane.observed(gid), reference.observed(gid); got != want {
			t.Fatalf("%s: actor %d differs from the full step\nlane:\n%s\nfull step:\n%s", stage, gid, got, want)
		}
	}
	if !reflect.DeepEqual(lane.push.toSession, reference.push.toSession) || !reflect.DeepEqual(lane.push.toDivision, reference.push.toDivision) {
		t.Fatalf("%s: pushed frames differ from the full step\nlane: %+v %+v\nfull step: %+v %+v", stage,
			lane.push.toSession, lane.push.toDivision, reference.push.toSession, reference.push.toDivision)
	}
	lane.push.toSession, lane.push.toDivision = nil, nil
	reference.push.toSession, reference.push.toDivision = nil, nil
}

/*
================
safeZoneRegion

Any region the combat table denies: where the step's exit defeats a
monster.
================
*/
func safeZoneRegion(t *testing.T) uint16 {
	t.Helper()
	for region := 1; region < 0x8000; region++ {
		if SafeZoneRegion(uint16(region)) {
			return uint16(region)
		}
	}
	t.Fatal("the combat table denies no outdoor region")
	return 0
}

/*
================
TestPendingRecheckSettlesWhatTheStepWrites

The plain case, read field by field: a quiet suspended actor with no
player in its block is settled without a step - the cadence stamped
at the leg's clock, the generation left alone, the due armed one
interval on - and the full step leaves the very same rows.
================
*/
func TestPendingRecheckSettlesWhatTheStepWrites(t *testing.T) {
	lane, reference := newPendingLaneFixture(t, 1, false), newPendingLaneFixture(t, 1, true)
	genBefore := lane.state.movers.generation(pendingLaneGID)
	stats := lane.leg(pendingLaneNowMs)
	referenceStats := reference.leg(pendingLaneNowMs)
	if stats.rechecked != 1 || stats.steps != 0 || stats.actors != 0 {
		t.Fatalf("lane: rechecked=%d steps=%d actors=%d, want the actor settled with no step", stats.rechecked, stats.steps, stats.actors)
	}
	if referenceStats.rechecked != 0 || referenceStats.steps != 1 {
		t.Fatalf("reference: rechecked=%d steps=%d, want the full step", referenceStats.rechecked, referenceStats.steps)
	}
	mover, _ := lane.state.movers.lookup(pendingLaneGID)
	if mover.Mode() != monster.MoverPending || mover.Activity.LastCheck != uint32(pendingLaneNowMs) || mover.Activity.Interval != pendingLaneInterval {
		t.Fatalf("settled row: mode %v cadence %+v, want PENDING stamped at %d", mover.Mode(), mover.Activity, pendingLaneNowMs)
	}
	if gen := lane.state.movers.generation(pendingLaneGID); gen != genBefore {
		t.Fatalf("a cadence stamp advanced the commit generation: %d -> %d", genBefore, gen)
	}
	slot := lane.state.behavior.byGID[pendingLaneGID]
	if due := lane.state.behavior.slots[slot].at; due != pendingLaneNowMs+pendingLaneInterval+1 {
		t.Fatalf("settled actor armed at %d, want one interval on (%d)", due, pendingLaneNowMs+pendingLaneInterval+1)
	}
	requireSameWorld(t, "plain re-check", lane, reference, 1)

	// The next leg finds it not due; a full interval on it settles again.
	if stats := lane.leg(pendingLaneNowMs + 100); stats.rechecked != 0 || stats.steps != 0 {
		t.Fatalf("a settled actor was popped before its due: rechecked=%d steps=%d", stats.rechecked, stats.steps)
	}
	reference.leg(pendingLaneNowMs + 100)
	again := pendingLaneNowMs + pendingLaneInterval + 1
	if stats := lane.leg(again); stats.rechecked != 1 || stats.steps != 0 {
		t.Fatalf("second re-check: rechecked=%d steps=%d, want one more settle", stats.rechecked, stats.steps)
	}
	reference.leg(again)
	requireSameWorld(t, "second re-check", lane, reference, 1)
}

/*
================
TestPendingRecheckLeavesTheStepItsCases

One fact flipped at a time. For each, the lane must take the actor
or leave it as named - a guard that stops reading its fact fails its
own row - and either way both worlds must end identical: the lane
may only ever be faster, never different.
================
*/
func TestPendingRecheckLeavesTheStepItsCases(t *testing.T) {
	safe := safeZoneRegion(t)
	cases := []struct {
		name    string
		settles bool
		arrange func(fx *pendingLaneFixture)
	}{
		{"quiet", true, func(fx *pendingLaneFixture) {}},
		{"a viewer in a far region", true, func(fx *pendingLaneFixture) {
			fx.sessions = []SessionSnapshot{fx.viewer(monsterTestRegion+0x0404, 500, 500)}
		}},
		{"a viewer in the actor's block", false, func(fx *pendingLaneFixture) {
			fx.sessions = []SessionSnapshot{fx.viewer(monsterTestRegion, 500, 500)}
		}},
		{"the actor's block cannot be resolved", true, func(fx *pendingLaneFixture) {
			fx.ops.MessageBlockAt = func(Spawn) (worldgeom.MessageBlock, bool) { return worldgeom.MessageBlock{}, false }
		}},
		{"the world's census is unresolved", true, func(fx *pendingLaneFixture) {
			fx.sessions = []SessionSnapshot{fx.viewer(monsterTestRegion, 500, 500)}
			fx.ops.MessageBlockAt = func(p Spawn) (worldgeom.MessageBlock, bool) {
				// The viewer's pose alone fails to resolve.
				if p.Angle == 0 && p.X == 500 && p.Z == 500 && p.RegionID == monsterTestRegion {
					return worldgeom.MessageBlock{}, false
				}
				return worldgeom.OutdoorMessageBlock(worldgeom.RegionXZ{RegionID: p.RegionID, X: p.X, Z: p.Z})
			}
			fx.editMover(func(m *monster.PendingMover) { m.Pose.Heading = 1 })
		}},
		{"cadence not yet due", false, func(fx *pendingLaneFixture) {
			fx.editMover(func(m *monster.PendingMover) { m.Activity.LastCheck = uint32(pendingLaneNowMs) })
		}},
		{"still spawning", false, func(fx *pendingLaneFixture) {
			fx.editMover(func(m *monster.PendingMover) {
				m.Spawning = true
				m.BehaviorDeadlineMs = pendingLaneNowMs + 5000
			})
		}},
		{"stuck latch set", false, func(fx *pendingLaneFixture) {
			fx.editMover(func(m *monster.PendingMover) { m.Stuck = true })
		}},
		{"help pending", false, func(fx *pendingLaneFixture) {
			fx.editActor(func(a *monster.Instance) { a.Help = a.Help.Replace(monster.HelpEvent{}) })
		}},
		{"motion hold", false, func(fx *pendingLaneFixture) {
			fx.editActor(func(a *monster.Instance) {
				a.Motion = monster.MotionHold{State: 1, UntilMs: pendingLaneNowMs + 1000}
			})
		}},
		{"abnormal block attached", false, func(fx *pendingLaneFixture) {
			fx.editActor(func(a *monster.Instance) {
				a.Abnormal = &abnormal.Block{}
				a.Abnormal.Slots[abnormal.Confusion].Active = true
			})
		}},
		{"tactics event queued", false, func(fx *pendingLaneFixture) {
			fx.state.queueAIEvents(pendingLaneGID, []monsterAIEvent{{Event: aiEventStart, Kind: 9, Source: 7}})
		}},
		{"training dummy", false, func(fx *pendingLaneFixture) {
			fx.editActor(func(a *monster.Instance) { a.Ref.TrainingDummy = true })
		}},
		{"nest on the other plane", false, func(fx *pendingLaneFixture) {
			fx.editActor(func(a *monster.Instance) { a.Nest.RegionID = monsterTestRegion | 0x8000 })
		}},
		{"detached from a nest on the other plane", true, func(fx *pendingLaneFixture) {
			fx.editActor(func(a *monster.Instance) {
				a.Nest.RegionID = monsterTestRegion | 0x8000
				a.NestDetached = true
			})
		}},
		{"standing in a safe zone", false, func(fx *pendingLaneFixture) {
			fx.editMover(func(m *monster.PendingMover) { m.Pose.RegionID = safe })
		}},
		{"dead", false, func(fx *pendingLaneFixture) {
			fx.editActor(func(a *monster.Instance) { a.CurrentHP = 0 })
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lane, reference := newPendingLaneFixture(t, 1, false), newPendingLaneFixture(t, 1, true)
			c.arrange(lane)
			c.arrange(reference)
			stats := lane.leg(pendingLaneNowMs)
			reference.leg(pendingLaneNowMs)
			if settled := stats.rechecked == 1; settled != c.settles {
				t.Fatalf("lane settled=%v (rechecked %d, steps %d), want settled=%v", settled, stats.rechecked, stats.steps, c.settles)
			}
			if c.settles && stats.steps != 0 {
				t.Fatalf("a settled actor was also stepped: steps=%d", stats.steps)
			}
			requireSameWorld(t, "first leg", lane, reference, 1)
			// The leg after: whatever the first leg armed fires the same.
			lane.leg(pendingLaneNowMs + pendingLaneInterval + 1)
			reference.leg(pendingLaneNowMs + pendingLaneInterval + 1)
			requireSameWorld(t, "one interval on", lane, reference, 1)
		})
	}
}

/*
================
TestPendingRecheckCommitRefusesARowThatMoved

The candidate list is read in one locked pass and committed in a
later one. Anything that touches the actor between the two must
stop the commit: the stamp is not written, the actor keeps its place
in the batch, and the write that moved it survives.
================
*/
func TestPendingRecheckCommitRefusesARowThatMoved(t *testing.T) {
	moves := []struct {
		name string
		move func(fx *pendingLaneFixture)
	}{
		{"a whole-value mover write", func(fx *pendingLaneFixture) {
			stored, _ := fx.state.movers.lookup(pendingLaneGID)
			stored.LastBattleActivityMs = legStepSlowerLaneMark
			fx.state.movers.set(pendingLaneGID, stored)
		}},
		{"a cadence retime", func(fx *pendingLaneFixture) {
			fx.state.movers.setActivity(pendingLaneGID, monster.ActivityCadence{Interval: pendingLaneInterval, LastCheck: uint32(pendingLaneNowMs) - 5000})
		}},
		{"a help command arriving", func(fx *pendingLaneFixture) {
			actor, _ := fx.state.instances.lookup(pendingLaneGID)
			actor.Help = actor.Help.Replace(monster.HelpEvent{})
			fx.state.instances.set(pendingLaneGID, actor)
		}},
		{"a tactics event queued", func(fx *pendingLaneFixture) {
			fx.state.queueAIEvents(pendingLaneGID, []monsterAIEvent{{Event: aiEventStart, Kind: 9, Source: 7}})
		}},
		{"a killing hit", func(fx *pendingLaneFixture) {
			actor, _ := fx.state.instances.lookup(pendingLaneGID)
			actor.CurrentHP = 0
			fx.state.instances.set(pendingLaneGID, actor)
		}},
	}
	for _, m := range moves {
		t.Run(m.name, func(t *testing.T) {
			fx := newPendingLaneFixture(t, 1, false)
			key := populationKey{division: pendingLaneDivision, lease: fx.state.lease}
			refs := []behaviorActorRef{{gid: pendingLaneGID, queueSlot: behaviorNoQueueSlot, slot: behaviorNoMoverSlot, monsterSlot: behaviorNoMonsterSlot}}
			candidates := fx.ops.Monsters.pendingRecheckCandidates(key, refs, pendingLaneNowMs, nil)
			if len(candidates) != 1 {
				t.Fatalf("fixture produced %d candidates, want the one quiet actor", len(candidates))
			}
			candidates[0].settle = true
			m.move(fx)
			before, _ := fx.state.movers.lookup(pendingLaneGID)
			if settled := fx.ops.Monsters.settlePendingRechecks(key, candidates, pendingLaneNowMs); settled != 0 || candidates[0].settle {
				t.Fatalf("commit landed on a moved row: settled=%d settle=%v", settled, candidates[0].settle)
			}
			if after, _ := fx.state.movers.lookup(pendingLaneGID); after != before {
				t.Fatalf("a refused commit wrote the mover: %+v -> %+v", before, after)
			}
		})
	}
}

/*
================
TestPendingRecheckYieldsToASetLandedAtItsLockEntry

The RunAction double stands in for the division's operation lock
and lands a slower lane's whole-value set as the lane enters it:
after the candidates were read, before they commit. The lane must
drop that actor back to the step, and the set must survive both.
================
*/
func TestPendingRecheckYieldsToASetLandedAtItsLockEntry(t *testing.T) {
	fx := newPendingLaneFixture(t, 1, false)
	entries := 0
	fx.ops.RunAction = func(_ string, run func(MonsterAttackOperation)) {
		if entries++; entries == 1 {
			stored, _ := fx.ops.Monsters.Mover(pendingLaneDivision, pendingLaneGID)
			stored.LastBattleActivityMs = legStepSlowerLaneMark
			if !fx.ops.Monsters.CommitMover(pendingLaneDivision, pendingLaneGID, stored) {
				t.Fatal("slower lane's set was rejected")
			}
		}
		run(nil)
	}
	stats := fx.leg(pendingLaneNowMs)
	if entries < 2 || stats.rechecked != 0 || stats.steps != 1 {
		t.Fatalf("entries=%d rechecked=%d steps=%d, want the lane's entry, no settle, then the actor's own step", entries, stats.rechecked, stats.steps)
	}
	stored, _ := fx.state.movers.lookup(pendingLaneGID)
	if stored.LastBattleActivityMs != legStepSlowerLaneMark {
		t.Fatalf("the slower lane's set was overwritten: LastBattleActivityMs %d, want %d", stored.LastBattleActivityMs, legStepSlowerLaneMark)
	}
}

/*
================
TestPendingRecheckEntersTheLockOncePerBatch

Forty due actors, one entry: the lane's whole point is that the
suspended actors stop paying the operation lock one by one. A batch
with no candidate must not enter it at all.
================
*/
func TestPendingRecheckEntersTheLockOncePerBatch(t *testing.T) {
	const actors = 40
	fx := newPendingLaneFixture(t, actors, false)
	entries := 0
	fx.ops.RunAction = func(_ string, run func(MonsterAttackOperation)) {
		entries++
		run(nil)
	}
	if stats := fx.leg(pendingLaneNowMs); stats.rechecked != actors || entries != 1 {
		t.Fatalf("rechecked=%d entries=%d, want %d settled inside one entry", stats.rechecked, entries, actors)
	}
	// Nobody is due on the next leg: no candidate, no entry.
	entries = 0
	if stats := fx.leg(pendingLaneNowMs + 100); stats.rechecked != 0 || entries != 0 {
		t.Fatalf("an idle leg entered the lock %d times (rechecked %d)", entries, stats.rechecked)
	}
}

/*
================
TestPendingRecheckWorldMatchesFullStepOverManyLegs

Sixty seconds of a world with a viewer who walks in and out of the
actors' blocks, so they are settled, resumed, stepped and suspended
again: after every leg the lane world and the full-step world must
be the same world.
================
*/
func TestPendingRecheckWorldMatchesFullStepOverManyLegs(t *testing.T) {
	const actors = 120
	lane, reference := newPendingLaneFixture(t, actors, false), newPendingLaneFixture(t, actors, true)
	for _, fx := range []*pendingLaneFixture{lane, reference} {
		// A spread of phases, so every leg has some actors due.
		for i := 0; i < actors; i++ {
			gid := pendingLaneGID + uint32(i)
			fx.state.movers.setActivity(gid, monster.ActivityCadence{
				Interval:  uint16(1000 + (i*37)%1000),
				LastCheck: uint32(pendingLaneNowMs) - uint32((i*211)%2000),
			})
			fx.ops.Monsters.scheduleBehavior(fx.state, gid, pendingLaneNowMs-1)
		}
	}
	rechecked, recheckedAfterResume, laneSteps, referenceSteps := 0, 0, 0, 0
	for leg := 0; leg < 600; leg++ {
		now := pendingLaneNowMs + int64(leg)*100
		var sessions []SessionSnapshot
		switch {
		case leg >= 150 && leg < 300:
			// Among the actors: their blocks count a player.
			sessions = []SessionSnapshot{lane.viewer(monsterTestRegion, 520, 501)}
		case leg >= 300 && leg < 420:
			// A far region: the census has a player, their blocks none.
			sessions = []SessionSnapshot{lane.viewer(monsterTestRegion+0x0404, 500, 500)}
		}
		lane.sessions, reference.sessions = sessions, sessions
		laneStats := lane.leg(now)
		referenceStats := reference.leg(now)
		rechecked += laneStats.rechecked
		if leg >= 300 {
			recheckedAfterResume += laneStats.rechecked
		}
		laneSteps += laneStats.steps
		referenceSteps += referenceStats.steps
		if referenceStats.rechecked != 0 {
			t.Fatalf("leg %d: the reference world ran the lane", leg)
		}
		requireSameWorld(t, fmt.Sprintf("leg %d", leg), lane, reference, actors)
	}
	if rechecked == 0 || laneSteps >= referenceSteps {
		t.Fatalf("the lane never engaged: rechecked=%d, lane steps %d against %d full steps", rechecked, laneSteps, referenceSteps)
	}
	if recheckedAfterResume == 0 {
		t.Fatal("no actor was suspended again after the viewer left: the world never came back to the lane")
	}
	t.Logf("600 legs, %d actors: lane settled %d re-checks (%d after the viewer left), stepped %d; the full-step world stepped %d",
		actors, rechecked, recheckedAfterResume, laneSteps, referenceSteps)
}

/*
================
TestLegCensusReachesTheMetricsBlock

What a leg did with its actors is readable from the live metrics,
not only from a slow-leg log line: forty suspended actors count as
forty settled in the lane world, and as forty dispatched, stepped
and refused at the mode guard in the full-step world. The census is
the monster_leg document (LegSnapshot, #3119), not navigation's.
================
*/
func TestLegCensusReachesTheMetricsBlock(t *testing.T) {
	const actors = 40
	for _, fullStep := range []bool{false, true} {
		fx := newPendingLaneFixture(t, actors, fullStep)
		metrics := &NavigationMetrics{}
		fx.ops.Navigation = metrics
		fx.leg(pendingLaneNowMs)
		got := metrics.LegSnapshot()
		want := map[string]uint64{"count": 1, "rechecked": actors}
		if fullStep {
			want = map[string]uint64{"count": 1, "actors": actors, "steps": actors,
				"rest.mode": actors, "cadence_arms": actors, "mode.pending": actors}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("full step %v: census %v, want %v", fullStep, got, want)
		}
	}
}

/*
================
BenchmarkPendingRecheckLeg

One leg in which every one of 2,000 suspended actors is due for its
re-check, with the lane and through the full step, dispatch wired
as production wires it. The live leg steps about 2,700 actors a
tick (2026-11-10), most of them these.
================
*/
func BenchmarkPendingRecheckLeg(b *testing.B) {
	for _, mode := range []struct {
		name     string
		fullStep bool
	}{{"lane", false}, {"full-step", true}} {
		b.Run(mode.name, func(b *testing.B) {
			fx := newPendingLaneFixture(b, 2000, mode.fullStep)
			fx.ops.RunAction = func(_ string, run func(MonsterAttackOperation)) { run(nil) }
			push := discardPusher{}
			now := pendingLaneNowMs
			fx.ops.RunMonsterLeg(now, nil, push)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Past every actor's interval: all are due again.
				now += pendingLaneInterval + 1
				fx.ops.RunMonsterLeg(now, nil, push)
			}
		})
	}
}
