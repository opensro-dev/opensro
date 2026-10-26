/*
===========================================================================

monstersquad.go - target squads and the fixed-query uniques' target switch

Every tactics owner that takes a target joins that target's squad.
An unforced join is refused once the squad holds SquadMemberLimit
actors, and the refusal fails the target set: sight acquisition
then takes no
target. The squad is the set of actors whose +0xBC names the target, which is
what a mover's TargetGID records here.

===========================================================================
*/

package simulation

import (
	"opensro.online/server/internal/game/world/monster"
	"sort"
)

/*
================
TargetSquad

The native healer snapshots the healed target's squad before
publishing healing threat.
Use the existing mover target owner, across live populations in this division.
================
*/
func (s *MonsterState) TargetSquad(division string, target uint32) []uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []uint32
	collect := func(state *divisionMonsterState) {
		if state == nil {
			return
		}
		for gid, record := range state.movers.records() {
			if record.live != nil && record.live.TargetGID() == target && state.instances.get(gid).CurrentHP != 0 {
				out = append(out, gid)
			}
		}
	}
	collect(s.divs[division])
	for key, state := range s.worldPopulations {
		if key.division == division {
			collect(state)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

/*
==================
squadAdmits

Whether gid may take target without forcing it: fewer than SquadMemberLimit
other actors of its population hold that target. A forced join (retaliation,
help, assist, the fixed-query uniques, flag 4/0x80 rows) is never refused.
==================
*/
func (s *MonsterState) squadAdmits(division string, gid, target uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	members := 0
	for other, record := range state.movers.records() {
		if other == gid || record.live == nil || record.live.TargetGID() != target {
			continue
		}
		members++
		if members >= monster.SquadMemberLimit {
			return false
		}
	}
	return true
}

/*
==================
acquireSightTarget

The +0xF8 acquisition callback. Both native acquisition forms (the
tactics query and the fixed query) take the first
query candidate and fail when the target set refuses it; neither falls back
to a farther candidate. The fixed query always uses selectors 1/2, so a
fixed-query row's 0x100 bit does not select the 0x11/5 query.
==================
*/
func (ops *MonsterMoverOps) acquireSightTarget(division string, instance monster.Instance, from monster.Pose, players []playerPose, tactics monster.Tactics) (playerPose, bool) {
	query := instance
	if instance.Nest.HasControls && instance.Nest.Controls.FixedQuery() {
		query.Nest.NativeTacticsFlags &^= 0x100
	}
	target, found := nearestEligiblePlayer(query, from, players, tactics.SightRange)
	if !found {
		return playerPose{}, false
	}
	if !instance.AcquisitionForced() && ops.Monsters != nil && !ops.Monsters.squadAdmits(division, instance.Gid, target.Gid) {
		return playerPose{}, false
	}
	return target, true
}

/*
==================
switchToSecondaryOpponent

The fixed-query uniques' +0x100 target check. It replaces the
home-trace abandonment, so these uniques never
abandon through the home-trace test. When the target stands beyond the
attack reach (+0x160) and the secondary opponent (+0xD0) is valid and inside
it, the forced target set makes it the target.

Native returns 0 on that switch and BATTLE finishes its tick against the old
target pointer; the port commits the switch and lets the next tick plan
against the new target. A forced registration cannot fail here, so the abandon
return is unreachable.
==================
*/
func (ops *MonsterMoverOps) switchToSecondaryOpponent(division string, instance monster.Instance, mover monster.MoverState, target playerPose, players []playerPose, live monster.Pose, now int64) bool {
	if !instance.Nest.HasControls || !instance.Nest.Controls.FixedQuery() {
		return false
	}
	reach := instance.AttackReachRadius()
	if !(reach < tacticsDistance3D(live, spawnToPose(target.Pose))) {
		return false
	}
	secondary := instance.Opponents[1].GID
	if secondary == 0 || secondary == target.Gid {
		return false
	}
	foe, valid := eligiblePlayerByGid(instance, players, secondary)
	if !valid || !(reach > tacticsDistance3D(live, spawnToPose(foe.Pose))) {
		return false
	}
	return ops.Monsters.switchCombatTarget(division, instance.Gid, mover, secondary, now)
}

/*
==================
switchCombatTarget

The native target set with force: +0xBC takes the new target and
+0xCC the current tick;
the rest of the primary record stays as it was.
==================
*/
func (s *MonsterState) switchCombatTarget(division string, gid uint32, mover monster.MoverState, target uint32, now int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	if state.movers.get(gid) != mover {
		return false
	}
	instance, exists := state.instances.lookup(gid)
	if !exists {
		return false
	}
	next := mover
	mustMoverTransition(&next, monster.MoverEventRetaliationArmed, target)
	if !s.commitMoverLocked(state, gid, next) {
		return false
	}
	instance = state.instances.get(gid)
	instance.Opponents[0].GID, instance.Opponents[0].LastHitMs = target, uint32(now)
	state.instances.set(gid, instance)
	return true
}

/*
==================
vehicleRedirectMargin

Both redirect forms retarget only when the new actor is at least
this much closer, after truncating the difference.
==================
*/
const vehicleRedirectMargin = 15

/*
==================
redirectToVehicle

The two target-vehicle redirect forms (job flag 4 and job flag
80). A
player target whose job state the row names gives way to its active
vehicle; a companion target gives way to its owner. The new target must be
valid and at least vehicleRedirectMargin closer, and the target set takes
it (forced by the flag). BATTLE then ends its tick (return 2).
==================
*/
func (ops *MonsterMoverOps) redirectToVehicle(division string, instance monster.Instance, mover monster.MoverState, target playerPose, players []playerPose, live monster.Pose, now int64) bool {
	if !instance.Nest.HasControls {
		return false
	}
	redirects, jobs := instance.Nest.Controls.VehicleRedirect()
	if !redirects {
		return false
	}
	var next playerPose
	found := false
	if target.OwnerGid == 0 {
		if target.JobState == jobs[0] || target.JobState == jobs[1] || target.JobState == jobs[2] {
			next, found = activeVehicle(players, target.Gid)
		}
	} else {
		next, found = eligiblePlayerByGid(instance, players, target.OwnerGid)
	}
	if !found {
		return false
	}
	before := float64(tacticsDistance3D(live, spawnToPose(target.Pose)))
	after := float64(tacticsDistance3D(live, spawnToPose(next.Pose)))
	if int32(before-after) < vehicleRedirectMargin {
		return false
	}
	return ops.Monsters.switchCombatTarget(division, instance.Gid, mover, next.Gid, now)
}

/*
==================
activeVehicle

The active vehicle getter: the owner's summoned transport (COS bands 1
and 2). The companion list carries only unmounted ones; a ridden vehicle
stands under its rider and could never be the margin closer.
==================
*/
func activeVehicle(players []playerPose, owner uint32) (playerPose, bool) {
	for _, p := range players {
		if p.OwnerGid == owner && (p.Band == 1 || p.Band == 2) {
			return p, true
		}
	}
	return playerPose{}, false
}
