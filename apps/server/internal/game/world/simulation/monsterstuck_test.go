/*
===========================================================================

monsterstuck_test.go - verification of native monster stuck counting and
event-0x28 dispatch

===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

const testStuckFailureLimit = 10

func testShortNavPath(from, goal monster.Pose) *monster.NavigationPath {
	return monster.NewNavigationPath(from, goal, goal, 0, func(f float64, _ monster.Pose) (float64, bool) {
		return from.Y + (goal.Y-from.Y)*f, true
	})
}

/*
================
TestMonsterStuckDetachAndIdleOnBlockedHoming

Verifies that a non-COS monster in HOMING (MoverReturning) whose return legs
repeatedly stop within 20 units of its sequence origin accumulates 10 stuck
counts, detaches its nest command while preserving ControllerGID (+0x14C),
enters MoverIdle, and stays idle at its detached pose rather than
re-entering homing.
================
*/
func TestMonsterStuckDetachAndIdleOnBlockedHoming(t *testing.T) {
	ops, instance, mover, _ := pursuitControlsFixture(t)
	// Place the monster far outside its nest's InitRadius so needsHoming is true
	// while the nest remains attached.
	away := monster.Pose{
		RegionID: instance.Nest.RegionID,
		X:        instance.Nest.X + 250,
		Y:        instance.Nest.Y,
		Z:        instance.Nest.Z,
	}
	// Each return step clips to 1 unit (< 20 from sequence origin) so each
	// admitted leg matures within 20 units and increments the stuck counter.
	ops.PlanPath = func(from, goal monster.Pose) *monster.NavigationPath {
		if from == anchorPose(instance) {
			return testShortNavPath(from, goal)
		}
		short := from
		short.X += 1
		return testShortNavPath(from, short)
	}

	mustMoverTransition(&mover, monster.MoverEventTargetLost, 0)
	if err := mover.BindController(monster.ControlSummoned, 9001); err != nil {
		t.Fatalf("bind controller: %v", err)
	}
	mover.Pose = away
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	nowMs := int64(10_000)
	ops.commitIdleEntry(monsterTestDivision, instance, mover, nil, nowMs)

	for step := 1; step <= testStuckFailureLimit; step++ {
		m, ok := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if !ok {
			t.Fatalf("step %d: mover missing", step)
		}
		if m.ArriveMs <= nowMs {
			t.Fatalf("step %d: expected active return segment, got mode=%v arrive=%d now=%d",
				step, m.Mode(), m.ArriveMs, nowMs)
		}
		nowMs = m.ArriveMs
		current, ok := ops.Monsters.Get(monsterTestDivision, instance.Gid)
		if !ok {
			t.Fatalf("step %d: instance missing", step)
		}
		ops.advanceInstance(monsterTestDivision, current, nil, nowMs)
	}

	detachedInst, ok := ops.Monsters.Get(monsterTestDivision, instance.Gid)
	if !ok {
		t.Fatal("instance missing after 10 short homing stops")
	}
	if !detachedInst.NestDetached {
		t.Fatal("expected NestDetached = true after 10 short homing stops")
	}
	settled, ok := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if !ok {
		t.Fatal("mover missing after stuck detach")
	}
	if settled.Mode() != monster.MoverIdle {
		t.Fatalf("mover mode = %v, want MoverIdle", settled.Mode())
	}
	if settled.ControllerGID() != 9001 {
		t.Fatalf("controller GID = %d, want 9001 (the release-command path preserves +0x14C)", settled.ControllerGID())
	}
	if settled.Progress().Failures() != 0 || settled.Stuck() {
		t.Fatalf("stuck progress not reset on idle transition: failures=%d stuck=%v", settled.Progress().Failures(), settled.Stuck())
	}

	// Advancing again after the idle entry deadline must NOT re-enter homing
	// because the nest command was released (the slot query returns 1 when +0x20 == 0).
	ops.Rand = func() float64 { return 0.10 } // stay idle on wander roll
	ops.advanceInstance(monsterTestDivision, detachedInst, nil, settled.BehaviorDeadlineMs+100)
	afterIdle, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if afterIdle.Mode() != monster.MoverIdle {
		t.Fatalf("detached monster re-entered %v from idle, want MoverIdle", afterIdle.Mode())
	}
}

/*
================
TestMonsterStuckWarpToBattleTargetAndDungeonGuard

Verifies the battle stuck relocation:
  - When TraceBoundary == 2, 10 short pursuit stops relocate the monster onto
    its live combat target via 0xB2F5 (OpObjectSourceCorrection) while keeping
    MoverChasing and the combat target.
  - When the monster is outdoors and the combat target is in a dungeon region,
    the server's position-relocation request refuses the relocation.

================
*/
func TestMonsterStuckWarpToBattleTargetAndDungeonGuard(t *testing.T) {
	ops, instance, mover, target := pursuitControlsFixture(t)
	instance.Nest.Controls.TraceBoundary = 2
	ops.Monsters.mu.Lock()
	ops.Monsters.division(monsterTestDivision).instances.set(instance.Gid, instance)
	ops.Monsters.mu.Unlock()

	target.Pose.X = mover.Pose.X + 80
	target.Pose.Z = mover.Pose.Z + 40
	target.Pose.Y = 33
	ops.GroundHeight = func(monster.Pose) (float64, bool) {
		return 37.5, true
	}
	// Every pursuit leg clips to 1 unit (< 20), so arrival after each short
	// leg increments the stuck counter.
	ops.PlanPath = func(from, goal monster.Pose) *monster.NavigationPath {
		short := from
		short.X += 1
		return testShortNavPath(from, short)
	}

	nowMs := int64(20_000)
	current, _ := ops.Monsters.Get(monsterTestDivision, instance.Gid)
	ops.advanceInstance(monsterTestDivision, current, []playerPose{target}, nowMs)
	var lastFrames []Frame
	for step := 1; step <= testStuckFailureLimit; step++ {
		m, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if m.ArriveMs <= nowMs {
			t.Fatalf("step %d: expected active chase segment, got mode=%v arrive=%d now=%d",
				step, m.Mode(), m.ArriveMs, nowMs)
		}
		nowMs = m.ArriveMs
		current, _ = ops.Monsters.Get(monsterTestDivision, instance.Gid)
		lastFrames, _ = ops.advanceInstance(monsterTestDivision, current, []playerPose{target}, nowMs)
	}

	warped, ok := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if !ok {
		t.Fatal("mover missing after battle stuck relocation")
	}
	if warped.Mode() != monster.MoverChasing || warped.TargetGID() != target.Gid {
		t.Fatalf("expected MoverChasing on target %d after battle warp, got mode=%v target=%d",
			target.Gid, warped.Mode(), warped.TargetGID())
	}
	if warped.Pose.X != target.Pose.X || warped.Pose.Z != target.Pose.Z || warped.Pose.Y != 37.5 {
		t.Fatalf("warped pose = %+v, want target XZ (%v,%v) and terrain Y 37.5",
			warped.Pose, target.Pose.X, target.Pose.Z)
	}
	foundCorrection := false
	for _, f := range lastFrames {
		if f.Opcode == wire.OpObjectSourceCorrection {
			foundCorrection = true
		}
	}
	if !foundCorrection {
		t.Fatal("expected OpObjectSourceCorrection frame on battle stuck relocation")
	}

	// Outdoor -> dungeon relocation guard:
	outdoorPose := monster.Pose{RegionID: 25000, X: 100, Y: 10, Z: 100}
	warped.Pose = outdoorPose
	warped.From, warped.To = monster.Pose{}, monster.Pose{}
	warped.DepartMs, warped.ArriveMs = 0, 0
	warped.ResetStuck()
	for i := 0; i < testStuckFailureLimit; i++ {
		warped.NoteMoveIssued(outdoorPose, false)
		warped.NoteMoveStopped(outdoorPose)
	}
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, warped)
	dungeonTarget := target
	dungeonTarget.Pose.RegionID = 0x8001
	dungeonTarget.Pose.X = 500
	dungeonTarget.Pose.Z = 500
	frames := ops.resolveStuck(monsterTestDivision, instance.Gid, []playerPose{dungeonTarget}, nowMs, nil)
	if len(frames) != 0 {
		t.Fatalf("expected no relocation frame for outdoor -> dungeon target, got %d", len(frames))
	}
	afterGuard, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if afterGuard.Pose.RegionID != outdoorPose.RegionID || afterGuard.Pose.X != outdoorPose.X {
		t.Fatalf("outdoor monster warped into dungeon: pose=%+v", afterGuard.Pose)
	}
}

/*
================
TestMonsterStuckRetainBattleWhenTraceBoundaryNotTwo

Verifies that the battle stuck relocation is a no-op
when TraceBoundary != 2: the monster neither relocates nor detaches its nest.
================
*/
func TestMonsterStuckRetainBattleWhenTraceBoundaryNotTwo(t *testing.T) {
	ops, instance, mover, target := pursuitControlsFixture(t)
	instance.Nest.Controls.TraceBoundary = 1
	ops.Monsters.mu.Lock()
	ops.Monsters.division(monsterTestDivision).instances.set(instance.Gid, instance)
	ops.Monsters.mu.Unlock()

	origin := mover.Pose
	for i := 0; i < testStuckFailureLimit; i++ {
		mover.NoteMoveIssued(origin, false)
		mover.NoteMoveStopped(origin)
	}
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)

	frames := ops.resolveStuck(monsterTestDivision, instance.Gid, []playerPose{target}, 30_000, nil)
	if len(frames) != 0 {
		t.Fatalf("expected no frames when TraceBoundary != 2, got %d", len(frames))
	}
	after, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if after.Mode() != monster.MoverChasing || after.Pose != origin {
		t.Fatalf("expected mover to remain in MoverChasing at origin, got mode=%v pose=%+v", after.Mode(), after.Pose)
	}
	instAfter, _ := ops.Monsters.Get(monsterTestDivision, instance.Gid)
	if instAfter.NestDetached {
		t.Fatal("battle stuck with TraceBoundary != 2 must not detach nest")
	}
}

/*
================
TestMonsterStuckWarpToControllerAndIdleForCOS

Verifies the owner stuck relocation: a COS actor (TID matching 0x1C6 with
kind 3..5) outside BATTLE relocates onto its live controller and transitions
to MoverIdle.
================
*/
func TestMonsterStuckWarpToControllerAndIdleForCOS(t *testing.T) {
	ops, instance, mover, controllerPlayer := pursuitControlsFixture(t)
	// COS TID word: tid&0x7fe == 0x1c6 and kind = 3 (tid >> 11 == 3).
	cosTidWord := uint16(0x1c6) | (3 << 11)
	if monster.NativeStuckResponse(false, 0, cosTidWord) != monster.StuckWarpToControllerAndIdle {
		t.Fatalf("expected tidWord %#x to map to StuckWarpToControllerAndIdle", cosTidWord)
	}

	instance.Ref.TidWord = cosTidWord
	ops.Monsters.mu.Lock()
	ops.Monsters.division(monsterTestDivision).instances.set(instance.Gid, instance)
	ops.Monsters.mu.Unlock()

	controllerPlayer.Pose.X = 420
	controllerPlayer.Pose.Y = 18
	controllerPlayer.Pose.Z = 310

	cosOrigin := monster.Pose{RegionID: instance.Spawn.RegionID, X: 100, Y: 10, Z: 100}
	mustMoverTransition(&mover, monster.MoverEventTargetLost, 0)
	if err := mover.BindController(monster.ControlSummoned, controllerPlayer.Gid); err != nil {
		t.Fatalf("bind controller: %v", err)
	}
	mover.Pose = cosOrigin
	mustMoverTransition(&mover, monster.MoverEventFollowStarted, controllerPlayer.Gid)
	for i := 0; i < testStuckFailureLimit; i++ {
		mover.NoteMoveIssued(cosOrigin, false)
		mover.NoteMoveStopped(cosOrigin)
	}
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)

	frames := ops.resolveStuck(monsterTestDivision, instance.Gid, []playerPose{controllerPlayer}, 40_000, nil)
	if len(frames) != 1 || frames[0].Opcode != wire.OpObjectSourceCorrection {
		t.Fatalf("expected 1 OpObjectSourceCorrection frame for COS controller warp, got %d", len(frames))
	}
	after, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if after.Mode() != monster.MoverIdle {
		t.Fatalf("COS mode after stuck = %v, want MoverIdle", after.Mode())
	}
	if after.Pose.X != controllerPlayer.Pose.X || after.Pose.Z != controllerPlayer.Pose.Z {
		t.Fatalf("COS pose after stuck = %+v, want controller XZ (%v,%v)", after.Pose, controllerPlayer.Pose.X, controllerPlayer.Pose.Z)
	}
	if after.ControllerGID() != controllerPlayer.Gid {
		t.Fatalf("COS controllerGID = %d, want %d", after.ControllerGID(), controllerPlayer.Gid)
	}
	if after.Progress().Failures() != 0 || after.Stuck() {
		t.Fatalf("COS stuck progress not reset: failures=%d stuck=%v", after.Progress().Failures(), after.Stuck())
	}
}

/*
================
TestMonsterStuckProductionPlanRouteAndZeroDistanceClip

Verifies [P0]:
  - When production ops.PlanRoute is wired and returns a blocked route (or
    when PlanRoute yields a corner that ops.PlanPath clips short), each
    waitForNavigation refusal counts NoteMoveIssued + NoteMoveStopped and
    fires resolveStuck after 10 failures across retry intervals.
  - When ops.PlanPath clips a non-in-flight direct segment to distance < 0.01
    at the departure point, NoteMoveIssued (pre-clip) + NoteMoveStopped still
    increment Failures() and fire resolveStuck on the 10th failure.

================
*/
func TestMonsterStuckProductionPlanRouteAndZeroDistanceClip(t *testing.T) {
	t.Run("routed_blocked", func(t *testing.T) {
		ops, instance, mover, target := pursuitControlsFixture(t)
		instance.Nest.Controls.TraceBoundary = 2
		ops.Monsters.mu.Lock()
		ops.Monsters.division(monsterTestDivision).instances.set(instance.Gid, instance)
		ops.Monsters.mu.Unlock()

		target.Pose.X = mover.Pose.X + 80
		target.Pose.Z = mover.Pose.Z + 40
		ops.PlanPath = testShortNavPath
		ops.PlanRoute = func(_ monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
			return monster.UnresolvedNavigationRoute(to, monster.NavigationRouteBlocked)
		}

		nowMs := int64(10_000)
		var lastFrames []Frame
		for step := 1; step <= testStuckFailureLimit; step++ {
			current, _ := ops.Monsters.Get(monsterTestDivision, instance.Gid)
			lastFrames, _ = ops.advanceInstance(monsterTestDivision, current, []playerPose{target}, nowMs)
			m, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
			if step < testStuckFailureLimit && m.Progress().Failures() != uint8(step) {
				t.Fatalf("step %d: failures = %d, want %d", step, m.Progress().Failures(), step)
			}
			nowMs += monsterNavigationRetryMs
		}
		warped, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if warped.Pose.X != target.Pose.X || warped.Pose.Z != target.Pose.Z {
			t.Fatalf("routed_blocked did not relocate onto target: pose=%+v want (%v,%v)",
				warped.Pose, target.Pose.X, target.Pose.Z)
		}
		if len(lastFrames) == 0 || lastFrames[len(lastFrames)-1].Opcode != wire.OpObjectSourceCorrection {
			t.Fatalf("expected relocation OpObjectSourceCorrection frame, got %+v", lastFrames)
		}
	})

	t.Run("routed_clipped_corner", func(t *testing.T) {
		ops, instance, mover, target := pursuitControlsFixture(t)
		instance.Nest.Controls.TraceBoundary = 2
		ops.Monsters.mu.Lock()
		ops.Monsters.division(monsterTestDivision).instances.set(instance.Gid, instance)
		ops.Monsters.mu.Unlock()

		target.Pose.X = mover.Pose.X + 80
		target.Pose.Z = mover.Pose.Z + 40
		ops.PlanRoute = func(from monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
			corner := from
			corner.X += 30
			return monster.NewNavigationRoute(to, []monster.Pose{corner, to})
		}
		// Clip the corner short so planSegment falls back to waitForNavigation.
		ops.PlanPath = func(from, _ monster.Pose) *monster.NavigationPath {
			clipped := from
			clipped.X += 1
			return testShortNavPath(from, clipped)
		}

		nowMs := int64(10_000)
		for step := 1; step <= testStuckFailureLimit; step++ {
			current, _ := ops.Monsters.Get(monsterTestDivision, instance.Gid)
			ops.advanceInstance(monsterTestDivision, current, []playerPose{target}, nowMs)
			m, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
			if step < testStuckFailureLimit && m.Progress().Failures() != uint8(step) {
				t.Fatalf("step %d: failures = %d, want %d", step, m.Progress().Failures(), step)
			}
			nowMs += monsterNavigationRetryMs
		}
		warped, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if warped.Pose.X != target.Pose.X || warped.Pose.Z != target.Pose.Z {
			t.Fatalf("routed_clipped_corner did not relocate onto target: pose=%+v want (%v,%v)",
				warped.Pose, target.Pose.X, target.Pose.Z)
		}
	})

	t.Run("direct_zero_clip_not_in_flight", func(t *testing.T) {
		ops, instance, mover, _ := pursuitControlsFixture(t)
		ops.PlanPath = func(from, _ monster.Pose) *monster.NavigationPath {
			return testShortNavPath(from, from)
		}
		goal := mover.Pose
		goal.X += 40
		nowMs := int64(10_000)
		for step := 1; step <= testStuckFailureLimit; step++ {
			mover, _ = ops.planDirectSegment(instance, mover, goal, 20, wire.MoveStateRun, nowMs)
			if step < testStuckFailureLimit && mover.Progress().Failures() != uint8(step) {
				t.Fatalf("step %d: failures = %d, want %d", step, mover.Progress().Failures(), step)
			}
			nowMs += 100
		}
		if !mover.Stuck() {
			t.Fatalf("expected Stuck() = true after %d zero-clip direct attempts, failures=%d",
				testStuckFailureLimit, mover.Progress().Failures())
		}
	})
}

/*
================
TestMonsterStuckPreservesMaturedSettleFrameWhenNotYetStuck

Verifies [P1]: when a previous segment matures at nowMs with stuck == false
and the successor planSegment / planDirectSegment waits on navigation or clips
to distance < 0.01, the 0xB2F5 settle frame for the completed leg is still
emitted.
================
*/
func TestMonsterStuckPreservesMaturedSettleFrameWhenNotYetStuck(t *testing.T) {
	ops, instance, mover := routedFixture(t, monster.MoverChasing)
	from := mover.Pose
	to := from
	to.X += 5
	mover.NoteMoveIssued(from, false)
	mover.From = from
	mover.To = to
	mover.DepartMs = 10_000
	mover.ArriveMs = 10_500

	ops.PlanRoute = func(_ monster.Pose, _ NavOwner, goal monster.Pose) *monster.NavigationRoute {
		return monster.UnresolvedNavigationRoute(goal, monster.NavigationRouteBlocked)
	}
	goal := to
	goal.X += 50
	next, frames := ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, 10_500)
	if len(frames) != 1 || frames[0].Opcode != wire.OpObjectSourceCorrection {
		t.Fatalf("expected 1 OpObjectSourceCorrection settle frame on blocked successor after matured leg, got %+v", frames)
	}
	if next.Pose != to {
		t.Fatalf("settled pose = %+v, want %+v", next.Pose, to)
	}
}

/*
================
TestMonsterStuckCounterClearedOnBattleEntry

Issue #1704: the native stuck counter (tactics record +0x148) is zeroed on every
state entry, so failures collected while wandering never carry into a
fight. Nine short stops in MoverWandering, then battle entry - by sight
aggro and by retaliation - must read zero, and one short chase step after
entry counts one, nowhere near the ten-failure latch.
================
*/
func TestMonsterStuckCounterClearedOnBattleEntry(t *testing.T) {
	wanderWithNineFailures := func(t *testing.T, ops *MonsterMoverOps, instance monster.Instance) monster.MoverState {
		t.Helper()
		mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if err := mover.Transition(monster.MoverEventStartWander, 0); err != nil {
			t.Fatalf("start wander: %v", err)
		}
		origin := mover.Pose
		for i := 0; i < 9; i++ {
			mover.NoteMoveIssued(origin, false)
			stop := origin
			stop.X += 5
			if mover.NoteMoveStopped(stop) {
				t.Fatalf("wander stop %d latched stuck before the limit", i+1)
			}
		}
		if got := mover.Progress().Failures(); got != 9 {
			t.Fatalf("failures before battle entry = %d, want 9", got)
		}
		ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
		return mover
	}
	clipChaseLegs := func(ops *MonsterMoverOps) {
		ops.PlanPath = func(from, goal monster.Pose) *monster.NavigationPath {
			short := from
			short.X += 1
			return testShortNavPath(from, short)
		}
	}

	t.Run("sight aggro", func(t *testing.T) {
		const t0 = int64(1_784_000_000_000)
		ops, instance := monsterLegFixture(t, aggressiveTactics())
		push := &fakePusher{}
		wanderWithNineFailures(t, ops, instance)
		clipChaseLegs(ops)

		player := playerSessionAt(1, 1050, 1000)
		ops.RunMonsterLeg(t0, []SessionSnapshot{player}, push)
		after, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if after.Mode() != monster.MoverChasing {
			t.Fatalf("mode after sight tick = %v, want chasing", after.Mode())
		}
		if got := after.Progress().Failures(); got != 0 {
			t.Fatalf("failures after aggro = %d, want 0 (battle entry resets)", got)
		}

		ops.RunMonsterLeg(t0+1000, []SessionSnapshot{player}, push)
		settled, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if settled.Stuck() {
			t.Fatal("one short chase step after aggro latched the stuck event")
		}
		if settled.Mode() != monster.MoverChasing {
			t.Fatalf("mode after one chase stop = %v, want chasing (no mid-fight stuck response)", settled.Mode())
		}
		if got := settled.Progress().Failures(); got != 1 {
			t.Fatalf("failures after one chase stop = %d, want 1", got)
		}
	})

	t.Run("retaliation", func(t *testing.T) {
		ops, instance := monsterLegFixture(t, aggressiveTactics())
		wanderWithNineFailures(t, ops, instance)
		clipChaseLegs(ops)
		target := playerPose{Gid: PlayerObjectID(1), Pose: Spawn{RegionID: monsterTestRegion, X: 1050, Y: 20, Z: 1000}, BodyRadius: 4}

		if !ops.Monsters.ArmRetaliation(monsterTestDivision, instance.Gid, target.Gid) {
			t.Fatal("retaliation was not armed")
		}
		after, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if after.Mode() != monster.MoverChasing {
			t.Fatalf("mode after retaliation = %v, want chasing", after.Mode())
		}
		if got := after.Progress().Failures(); got != 0 {
			t.Fatalf("failures after retaliation = %d, want 0 (battle entry resets)", got)
		}

		nowMs := int64(10_000)
		current, _ := ops.Monsters.Get(monsterTestDivision, instance.Gid)
		ops.advanceInstance(monsterTestDivision, current, []playerPose{target}, nowMs)
		m, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if m.ArriveMs <= nowMs {
			t.Fatalf("expected an in-flight chase leg, got mode=%v arrive=%d now=%d", m.Mode(), m.ArriveMs, nowMs)
		}
		current, _ = ops.Monsters.Get(monsterTestDivision, instance.Gid)
		ops.advanceInstance(monsterTestDivision, current, []playerPose{target}, m.ArriveMs)
		settled, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if settled.Stuck() {
			t.Fatal("one short chase step after retaliation latched the stuck event")
		}
		if settled.Mode() != monster.MoverChasing {
			t.Fatalf("mode after one chase stop = %v, want chasing (no mid-fight stuck response)", settled.Mode())
		}
		if got := settled.Progress().Failures(); got != 1 {
			t.Fatalf("failures after one chase stop = %d, want 1", got)
		}
	})
}

/*
================
TestMonsterStuckWarpToBattleTargetCompanion

Issue #1702: the battle warp finds its combat target through the global
object lookup, so a companion the monster is fighting is a legal warp
destination. The companion entry below carries a body status the tactics
admission refuses (status 3): the warp must not re-run that admission on
an already-owned target.
================
*/
func TestMonsterStuckWarpToBattleTargetCompanion(t *testing.T) {
	ops, instance, mover, owner := pursuitControlsFixture(t)
	instance.Nest.Controls.TraceBoundary = 2
	ops.Monsters.mu.Lock()
	ops.Monsters.division(monsterTestDivision).instances.set(instance.Gid, instance)
	ops.Monsters.mu.Unlock()

	const petGID = uint32(777001)
	if err := mover.Transition(monster.MoverEventRetaliationArmed, petGID); err != nil {
		t.Fatalf("retarget onto companion: %v", err)
	}
	origin := mover.Pose
	for i := 0; i < testStuckFailureLimit; i++ {
		mover.NoteMoveIssued(origin, false)
		mover.NoteMoveStopped(origin)
	}
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)

	companion := playerPose{
		Gid: petGID, OwnerGid: owner.Gid,
		Pose:             Spawn{RegionID: monsterTestRegion, X: 420, Y: 18, Z: 310},
		BodyRadius:       5,
		NativeBodyStatus: 3,
	}
	frames := ops.resolveStuck(monsterTestDivision, instance.Gid, []playerPose{owner, companion}, 40_000, nil)
	if len(frames) != 1 || frames[0].Opcode != wire.OpObjectSourceCorrection {
		t.Fatalf("expected 1 OpObjectSourceCorrection frame for companion battle warp, got %d", len(frames))
	}
	after, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if after.Mode() != monster.MoverChasing || after.TargetGID() != petGID {
		t.Fatalf("expected MoverChasing on companion %d after battle warp, got mode=%v target=%d",
			petGID, after.Mode(), after.TargetGID())
	}
	if after.Pose.X != companion.Pose.X || after.Pose.Z != companion.Pose.Z {
		t.Fatalf("warped pose = %+v, want companion XZ (%v,%v)", after.Pose, companion.Pose.X, companion.Pose.Z)
	}
}

/*
================
TestMonsterStuckWarpToBattleTargetMonster

Issue #1702: the combat target may also be another monster in the
population (a summon or a monster foe), which never appears in the tick's
player list. The warp must resolve it through the population instead.
================
*/
func TestMonsterStuckWarpToBattleTargetMonster(t *testing.T) {
	ops, instance, mover, _ := pursuitControlsFixture(t)
	instance.Nest.Controls.TraceBoundary = 2
	ops.Monsters.mu.Lock()
	ops.Monsters.division(monsterTestDivision).instances.set(instance.Gid, instance)
	ops.Monsters.mu.Unlock()

	var other monster.Instance
	found := false
	for _, inst := range ops.Monsters.InstancesInRegions(monsterTestDivision, []uint16{monsterTestRegion}) {
		if inst.Ref.RefObjID == 2000 {
			other, found = inst, true
		}
	}
	if !found {
		t.Fatal("target monster instance not materialized")
	}
	otherMover, ok := ops.Monsters.Mover(monsterTestDivision, other.Gid)
	if !ok {
		t.Fatal("target monster has no mover")
	}
	if !ops.Monsters.ArmRetaliation(monsterTestDivision, instance.Gid, other.Gid) {
		t.Fatal("retaliation onto monster target was not armed")
	}
	mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	origin := mover.Pose
	for i := 0; i < testStuckFailureLimit; i++ {
		mover.NoteMoveIssued(origin, false)
		mover.NoteMoveStopped(origin)
	}
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)

	frames := ops.resolveStuck(monsterTestDivision, instance.Gid, nil, 40_000, nil)
	if len(frames) != 1 || frames[0].Opcode != wire.OpObjectSourceCorrection {
		t.Fatalf("expected 1 OpObjectSourceCorrection frame for monster battle warp, got %d", len(frames))
	}
	after, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if after.Mode() != monster.MoverChasing || after.TargetGID() != other.Gid {
		t.Fatalf("expected MoverChasing on monster %d after battle warp, got mode=%v target=%d",
			other.Gid, after.Mode(), after.TargetGID())
	}
	want := otherMover.LivePoseAt(40_000, ops.GroundHeight)
	if after.Pose.X != want.X || after.Pose.Z != want.Z {
		t.Fatalf("warped pose = %+v, want target monster XZ (%v,%v)", after.Pose, want.X, want.Z)
	}
}

// A stuck relocation may move an actor anywhere on its own plane and out
// of a cave, but never carries an outdoor actor into a cave (#1646 keeps
// the rule; #1658 changed how often cave actors reach the stuck counter).
func TestStuckRelocationNeverEntersACaveFromOutdoors(t *testing.T) {
	const outdoor, otherOutdoor, cave, otherCave uint16 = 0x62a8, 0x62a9, 0x8001, 0x8003
	for _, test := range []struct {
		from, to uint16
		enters   bool
	}{
		{outdoor, otherOutdoor, false},
		{outdoor, cave, true},
		{cave, outdoor, false},
		{cave, otherCave, false},
	} {
		if got := relocationEntersCave(test.from, test.to); got != test.enters {
			t.Errorf("%#x -> %#x: enters cave %t, want %t", test.from, test.to, got, test.enters)
		}
	}
}
