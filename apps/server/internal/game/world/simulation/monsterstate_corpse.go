/*
===========================================================================

monsterstate_corpse.go - monster death and the corpse pose

===========================================================================
*/

package simulation

import "opensro.online/server/internal/game/world/monster"

/*
==================
settleCorpseLocked

settleCorpseLocked is part of the positive-HP -> zero-HP transaction.
Removal/respawn remains an explicit later lifecycle operation. Until then,
combat, interest and bootstrap must all see the same frozen death pose.
A retained NavigationPath supplies the authoritative surface; it is sampled
before discarding movement ownership. Without one the in-flight pose is a
chord between two terrain heights, below the ground over a rise, so the
corpse is re-grounded (native CRTNavMeshTerrain_Move samples the surface on
every step: a monster never dies inside a hill).
==================
*/
func (state *divisionMonsterState) settleCorpseLocked(instance *monster.Instance, nowMs int64, ground MonsterSpawnGroundResolver) {
	gid := instance.Gid
	if mover, ok := state.movers.lookup(gid); ok {
		mover.Pose = groundedLivePose(mover, nowMs, ground)
		mover.From, mover.To = monster.Pose{}, monster.Pose{}
		mover.DepartMs, mover.ArriveMs = 0, 0
		mover.CancelNavigation()
		mover.AdoptNavigation(nil)
		state.movers.set(gid, mover)
	}
	state.releaseApproachActor(gid)
	// Death retires every slot (4A59F0) without running start/tick effects;
	// the corpse keeps no parameter writes, motion hold or tactics events.
	instance.Abnormal = nil
	instance.Motion = monster.MotionHold{}
	delete(state.abnormalActive, gid)
	delete(state.aiEvents, gid)
}

/*
==================
groundedLivePose

The live pose with its height taken from the surface under it (the chord
height is only the reference for picking that surface). A retained
navigation path already answers from its own surface.
==================
*/
func groundedLivePose(mover monster.MoverState, nowMs int64, ground MonsterSpawnGroundResolver) monster.Pose {
	if ground == nil {
		return mover.LivePoseAt(nowMs, nil)
	}
	return mover.LivePoseAt(nowMs, func(regionID uint16, x, z float64) (float64, bool) {
		chord := mover.LivePoseAt(nowMs, nil)
		return ground(regionID, x, chord.Y, z)
	})
}
