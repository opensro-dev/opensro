/*
===========================================================================

monsterstuck.go - native stuck counting and event-0x28 dispatch for monsters

Monster movers share the native tactics stuck counter
(+0x184/+0x19C/+0x148) and the event-0x28 dispatch (the
move-stopped counter through the extended-event dispatch):

  - The BATTLE state:
    TraceBoundary == 2 relocates onto the live combat target via
    the relocation request (0x7022 -> move mode 7, refusing
    outdoor -> dungeon) and stays in BATTLE; any other
    TraceBoundary retains BATTLE without relocating.
  - Non-BATTLE states:
    COS actors (TypeID kind 3 with TypeID4 in 1..5) relocate onto
    their live controller and enter IDLE; non-COS actors release
    their nest command (preserving controllerGID at +0x14C) and
    enter IDLE.

===========================================================================
*/

package simulation

import (
	"time"

	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
settleMaturedSegment

The port's movement segment matures by wall clock without an explicit per-tick
mover callback. When a planner path observes a matured segment before
monsterbehavior.go's end-of-tick settle, count that stop at To first so every
finished leg feeds the move-stopped counter before a successor command is issued.
================
*/
func settleMaturedSegment(gid uint32, mover *monster.MoverState, nowMs int64) ([]Frame, bool) {
	if mover.ArriveMs <= mover.DepartMs || mover.InFlight(nowMs) {
		return nil, false
	}
	mover.Pose = mover.To
	mover.From, mover.To = monster.Pose{}, monster.Pose{}
	mover.DepartMs, mover.ArriveMs = 0, 0
	stuck := mover.NoteMoveStopped(mover.Pose)
	return []Frame{correctionFrame(gid, mover.Pose)}, stuck
}

/*
================
resolveStuck

Answers a latched event-0x28 (mover.Stuck) at the end of advanceInstance using
monster.NativeStuckResponse. The non-stuck fast path returns without acquiring
a second population lock when no stop crossed nativeStuckFailureLimit.
================
*/
func (ops *MonsterMoverOps) resolveStuck(
	divisionID string,
	gid uint32,
	divisionPlayers []playerPose,
	nowMs int64,
	frames []Frame,
) []Frame {
	mover, ok := ops.Monsters.Mover(divisionID, gid)
	if !ok || !mover.Stuck() {
		return frames
	}
	current, exists := ops.Monsters.Get(divisionID, gid)
	if !exists || current.Motion.StateAt(nowMs) != 0 || (current.Ref.MaxHP > 0 && current.CurrentHP == 0) {
		return frames
	}

	response := monster.NativeStuckResponse(
		mover.InNativeBattle(),
		current.Nest.Controls.TraceBoundary,
		current.Ref.TidWord,
	)
	live := mover.LivePoseAt(nowMs, ops.GroundHeight)
	hadSegment := mover.ArriveMs > mover.DepartMs

	switch response {
	case monster.StuckRetainBattle:
		next := mover
		next.ClearStuck()
		ops.Monsters.commitStuckMover(divisionID, gid, current, mover, next, false, nowMs)
		return frames

	case monster.StuckWarpToBattleTarget:
		next := mover
		next.ClearStuck()
		warped := false
		if dest, found := ops.resolveBattleTargetPose(divisionID, mover.TargetGID(), divisionPlayers, nowMs); found {
			// The server refuses outdoor -> dungeon relocation.
			if !relocationEntersCave(live.RegionID, dest.RegionID) {
				relocated := ops.resolveRelocationPose(live, dest)
				next.Pose = relocated
				next.From, next.To = monster.Pose{}, monster.Pose{}
				next.DepartMs, next.ArriveMs = 0, 0
				next.CancelNavigation()
				warped = true
			}
		}
		if ops.Monsters.commitStuckMover(divisionID, gid, current, mover, next, false, nowMs) && warped {
			frames = appendOrReplaceCorrectionFrame(frames, gid, next.Pose)
		}
		return frames

	case monster.StuckDetachAndIdle:
		next := mover
		next.Pose = live
		next.From, next.To = monster.Pose{}, monster.Pose{}
		next.DepartMs, next.ArriveMs = 0, 0
		next.CancelNavigation()
		mustMoverTransition(&next, monster.MoverEventStuckIdled, 0)
		next.BehaviorDeadlineMs = nowMs + ops.idleDelayMs()
		detached := current
		detached.NestDetached = true
		if !needsHoming(detached, live) {
			next.TakeIdleEntry()
		}
		if ops.Monsters.commitStuckMover(divisionID, gid, current, mover, next, true, nowMs) && hadSegment {
			frames = appendOrReplaceCorrectionFrame(frames, gid, live)
		}
		return frames

	case monster.StuckWarpToControllerAndIdle:
		relocated := live
		warped := false
		if ctrlPose, found := ops.resolveControllerPose(divisionID, mover.ControllerGID(), divisionPlayers, nowMs); found {
			// The server refuses outdoor -> dungeon relocation.
			if !relocationEntersCave(live.RegionID, ctrlPose.RegionID) {
				relocated = ops.resolveRelocationPose(live, ctrlPose)
				warped = true
			}
		}
		next := mover
		next.Pose = relocated
		next.From, next.To = monster.Pose{}, monster.Pose{}
		next.DepartMs, next.ArriveMs = 0, 0
		next.CancelNavigation()
		mustMoverTransition(&next, monster.MoverEventStuckIdled, 0)
		next.TakeIdleEntry()
		next.BehaviorDeadlineMs = nowMs + ops.idleDelayMs()
		if ops.Monsters.commitStuckMover(divisionID, gid, current, mover, next, false, nowMs) && (warped || hadSegment) {
			frames = appendOrReplaceCorrectionFrame(frames, gid, relocated)
		}
		return frames
	}
	return frames
}

/*
================
resolveRelocationPose

The native move (mode 7) resolves the destination surface height
through the region manager before writing the relocated
coordinates.
================
*/
func (ops *MonsterMoverOps) resolveRelocationPose(from, target monster.Pose) monster.Pose {
	relocated := monster.Pose{
		RegionID: target.RegionID,
		X:        target.X,
		Y:        target.Y,
		Z:        target.Z,
		Heading:  from.Heading,
	}
	// A relocation is a teleport: the destination surface resolves afresh,
	// nearest the target's own height (#1695).
	if ops.GroundHeight != nil {
		if y, ok := ops.GroundHeight(relocated); ok {
			relocated.Y = y
		}
	}
	return relocated
}

/*
================
resolveBattleTargetPose

Resolves the live position of the mover's combat target (the tactics record's target id).
The native battle warp finds that object through the global object manager,
so the target may be a player, a session companion, a summon or another
monster; there is no tactics status re-admission, because the object is
already the owned combat target and only liveness gates the warp. A live
population object answers through its own mover first, then the tick's
eligible list (players with their companions appended).
================
*/
func (ops *MonsterMoverOps) resolveBattleTargetPose(
	divisionID string,
	targetGID uint32,
	divisionPlayers []playerPose,
	nowMs int64,
) (monster.Pose, bool) {
	if targetGID == 0 {
		return monster.Pose{}, false
	}
	if inst, ok := ops.Monsters.Get(divisionID, targetGID); ok && (inst.Ref.MaxHP == 0 || inst.CurrentHP > 0) {
		if targetMover, ok := ops.Monsters.Mover(divisionID, targetGID); ok {
			return targetMover.LivePoseAt(nowMs, ops.GroundHeight), true
		}
	}
	if player, exists := playerByGid(divisionPlayers, targetGID); exists {
		return spawnToPose(player.Pose), true
	}
	return monster.Pose{}, false
}

/*
================
resolveControllerPose

Resolves the live position of the tactics record's +0x14C
(controllerGID), which may name
either a live monster summoner or a live player controller in the division.
================
*/
func (ops *MonsterMoverOps) resolveControllerPose(
	divisionID string,
	controllerGID uint32,
	divisionPlayers []playerPose,
	nowMs int64,
) (monster.Pose, bool) {
	if controllerGID == 0 {
		return monster.Pose{}, false
	}
	if inst, ok := ops.Monsters.Get(divisionID, controllerGID); ok && (inst.Ref.MaxHP == 0 || inst.CurrentHP > 0) {
		if ctrlMover, ok := ops.Monsters.Mover(divisionID, controllerGID); ok {
			return ctrlMover.LivePoseAt(nowMs, ops.GroundHeight), true
		}
	}
	for _, player := range divisionPlayers {
		if player.Gid == controllerGID {
			return spawnToPose(player.Pose), true
		}
	}
	return monster.Pose{}, false
}

/*
================
commitStuckMover

Commits the event-0x28 state transition and optional nest
detachment (the native command release and its nest death
handling) atomically
under the division population lock.
================
*/
func (s *MonsterState) commitStuckMover(
	division string,
	gid uint32,
	expectedInstance monster.Instance,
	expectedMover monster.MoverState,
	next monster.MoverState,
	detachNest bool,
	now int64,
) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	current, exists := state.instances.lookup(gid)
	if !exists || current != expectedInstance || state.movers.get(gid) != expectedMover {
		return false
	}
	if current.Ref.MaxHP > 0 && current.CurrentHP == 0 {
		return false
	}
	if detachNest {
		s.detachNestLocked(state, gid, now)
	}
	if !s.commitMoverLocked(state, gid, next) {
		return false
	}
	s.scheduleBehavior(state, gid, now)
	return true
}

/*
================
appendOrReplaceCorrectionFrame

Coalesces a relocation 0xB2F5 with a trailing settle 0xB2F5 emitted on the
same tick so viewers receive one authoritative position correction.
================
*/
func appendOrReplaceCorrectionFrame(frames []Frame, gid uint32, pose monster.Pose) []Frame {
	correction := correctionFrame(gid, pose)
	if n := len(frames); n > 0 && frames[n-1].Opcode == wire.OpObjectSourceCorrection {
		frames[n-1] = correction
		return frames
	}
	return append(frames, correction)
}

/*
================
vanishInSafeZone

The native region-leaving handler: a monster whose region
changes to one that is not a battlefield (a town,
_RefRegion.IsBattleField 0) is set to life state 3 through the
life-state setter. From alive that state skips the death
broadcast: the monster vanishes without a kill or a reward, and
its nest respawns it. This is why monsters never walk into a
town.
================
*/
func (ops *MonsterMoverOps) vanishInSafeZone(divisionID string, gid uint32, nowMs int64) {
	// Only the region is weighed, so read the pose, not the whole
	// validated mover value the planner steps already paid for.
	pose, ok := ops.Monsters.livePose(divisionID, gid, nowMs)
	if ok && SafeZoneRegion(pose.RegionID) {
		ops.Monsters.Defeat(divisionID, gid, time.UnixMilli(nowMs))
	}
}

/*
================
SafeZoneRegion

A region _RefRegion marks as no battlefield. A region the table does not
know is not one, as the native region test refuses unknown
regions separately.
================
*/
func SafeZoneRegion(region uint16) bool {
	allowed, known := worldgeom.RegionPlayerCombat(region)
	return known && !allowed
}

/*
================
relocationEntersCave

Whether a stuck relocation from one region to another would carry an
outdoor actor into a cave, which the server refuses. A relocation stays
allowed on its own plane and out of a cave.
================
*/
func relocationEntersCave(from, to uint16) bool {
	return PlaneKindOf(from) == PlaneOutdoor && PlaneKindOf(to) == PlaneCave
}
