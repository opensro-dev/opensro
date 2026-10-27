/*
===========================================================================

monsterapproach.go - native +0x114 squad approach slots and pursuit goals

Manages the per-target 8-slot approach circle and endpoint construction
for the retail server's two pursuit callbacks (the direction-gated one
outdoors, the region-routed one in dungeons).

===========================================================================
*/
package simulation

import (
	"math"
	"sort"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
approachMember
================
*/
type approachMember struct {
	order uint64
	slot  int // -2: enrolled but not allocated; -1: navigation released direction
}

/*
================
monsterApproachSquad
================
*/
type monsterApproachSquad struct {
	revision uint64
	serial   uint64
	slots    monster.ApproachSlots
	members  map[uint32]approachMember
}

/*
================
releaseApproachActor

Groups belong to a population lifetime, not a process-global player GID.
================
*/
func (state *divisionMonsterState) releaseApproachActor(gid uint32) {
	target := state.approachActors[gid]
	if target == 0 {
		return
	}
	delete(state.approachActors, gid)
	if group := state.approachTargets[target]; group != nil {
		group.slots.Release(gid)
		delete(group.members, gid)
		group.revision++
		if len(group.members) == 0 {
			delete(state.approachTargets, target)
		}
	}
}

/*
================
ordinarySquadApproach

Whether the actor runs the native +0x114 pursuit callback (the
retail server's direction-gated pursuit outdoors, its region-routed
pursuit in dungeons). The tactics installer stores the direction-gated
callback into slot +0x114 in an unconditional run at function entry: the
installer has no FleeType (+0x1d) or BattleStyle (+0x20) gate, and the
dungeon variant is picked by the region test. Flags bits 0x04 and 0x80
replace only slot +0x100 (target check), not +0x114.

Native resolves a monster's tactics once, when it is spawned
(its spawn-tactics selection, called with dwTacticsID == 0), so a spawn
without a tactics id reads its spawn table row alone. Such a row carries no control block, and the pursuit callback
belongs to that spawn as well.

The port has to recognise those spawns without inventing a gate native does
not have. A spawn that carries a TacticsControls row keeps the row's own
FleeType and BattleStyle. A spawn without one is admitted when it came
from a summoner (SummonerGID != 0), or when it is a dynamic spawn that is
detached and policy-pinned: the retail server's quest scripts and
/LOADMONSTER build exactly that, with a detached, policy-pinned nest.

A resource-authored area nest is policy-pinned and holds its slot, so it
stays out while it holds it. Releasing that slot (through
detachCrossPlaneNest) leaves the actor detached and policy-pinned, and the
predicate then admits it. Native installs the callback for that actor at
its spawn, so admitting it after the release is the closer of the two
approximations, and the flip is deliberate rather than a fallthrough.
================
*/
func ordinarySquadApproach(actor monster.Instance) bool {
	if actor.Nest.HasControls {
		return actor.Nest.Controls.FleeType == 0 && actor.Nest.Controls.BattleStyle == 0
	}
	if actor.SummonerGID != 0 {
		return true
	}
	return actor.NestDetached && actor.Nest.PolicyPinned
}

/*
================
syncApproachActor

Caller holds MonsterState.mu; only admitted state edges change membership.
================
*/
func (state *divisionMonsterState) syncApproachActor(gid uint32, mover monster.MoverState) {
	target := mover.TargetGID()
	if !ordinarySquadApproach(state.instances.get(gid)) ||
		(mover.Mode() != monster.MoverChasing && mover.Mode() != monster.MoverAttacking) {
		target = 0
	}
	if state.approachActors[gid] == target {
		return
	}
	state.releaseApproachActor(gid)
	if target == 0 {
		return
	}
	if state.approachActors == nil {
		state.approachActors = make(map[uint32]uint32)
	}
	if state.approachTargets == nil {
		state.approachTargets = make(map[uint32]*monsterApproachSquad)
	}
	group := state.approachTargets[target]
	if group == nil {
		group = &monsterApproachSquad{members: make(map[uint32]approachMember)}
		state.approachTargets[target] = group
	}
	group.serial++
	group.members[gid] = approachMember{order: group.serial, slot: -2}
	group.revision++
	state.approachActors[gid] = target
}

/*
================
approachNavigation
================
*/
type approachNavigation struct {
	target   uint32
	existed  bool
	revision uint64
	group    monsterApproachSquad
	inputs   map[uint32]monster.MoverState
	slot     int
}

/*
================
prepareApproachNavigation

Snapshot acquisition alone holds the population lock. Allocation and surface
sampling run on immutable copies outside it: failed or stale navigation must
not steal a peer's reservation. commitNavigation checks the entire read set.
================
*/
func (s *MonsterState) prepareApproachNavigation(division string, actor monster.Instance, next monster.MoverState, target playerPose, now int64) navigationAdmission {
	s.mu.Lock()
	state := s.populationForObject(division, actor.Gid)
	current, exists := state.instances.lookup(actor.Gid)
	before := navigationAdmission{instance: current, mover: state.movers.get(actor.Gid), exists: exists}
	if !exists || !ordinarySquadApproach(current) || next.TargetGID() != target.Gid || !monster.FollowLocationCompatible(next.Pose.RegionID, target.Pose.RegionID) {
		s.mu.Unlock()
		return before
	}
	p := &approachNavigation{target: target.Gid, inputs: make(map[uint32]monster.MoverState)}
	p.group.members = make(map[uint32]approachMember)
	if group := state.approachTargets[target.Gid]; group != nil {
		p.existed, p.revision = true, group.revision
		p.group.revision, p.group.serial, p.group.slots = group.revision, group.serial, group.slots
		for gid, member := range group.members {
			p.group.members[gid] = member
			p.inputs[gid] = state.movers.get(gid)
		}
	}
	if _, exists := p.group.members[actor.Gid]; !exists {
		p.group.serial++
		p.group.members[actor.Gid] = approachMember{order: p.group.serial, slot: -2}
	}
	s.mu.Unlock()
	poses := make(map[uint32]monster.Pose, len(p.group.members))
	moving := make(map[uint32]bool, len(p.group.members))
	for gid := range p.group.members {
		mover := p.inputs[gid]
		if gid == actor.Gid {
			mover = next
		}
		poses[gid], moving[gid] = mover.LivePoseAt(now, nil), mover.InFlight(now)
	}
	targetPose := monster.Pose{RegionID: target.Pose.RegionID, X: target.Pose.X, Y: target.Pose.Y, Z: target.Pose.Z}
	assign := func(gid uint32) {
		candidate := poses[gid]
		preferred := monster.NativeApproachPreferredSlot(candidate, targetPose)
		member := p.group.members[gid]
		member.slot = p.group.slots.Assign(gid, preferred, func(incumbent uint32) bool {
			return moving[incumbent] && tacticsDistance3D(candidate, targetPose) < tacticsDistance3D(poses[incumbent], targetPose)
		})
		p.group.members[gid] = member
	}
	// Initialize new members in accepted acquisition order, including attackers
	// admitted immediately at short range. Map iteration must not choose winners.
	var unassigned []uint32
	for gid, member := range p.group.members {
		if member.slot == -2 {
			unassigned = append(unassigned, gid)
		}
	}
	sort.Slice(unassigned, func(i, j int) bool {
		return p.group.members[unassigned[i]].order < p.group.members[unassigned[j]].order
	})
	for _, gid := range unassigned {
		assign(gid)
	}
	member := p.group.members[actor.Gid]
	// 545620 releases only the actor's own reservation. A remembered direction
	// that has been preempted must not clear the replacement owner's slot.
	if member.slot != -1 || len(p.group.members) < 8 {
		p.group.slots.Release(actor.Gid)
		assign(actor.Gid)
	}
	p.slot = p.group.members[actor.Gid].slot
	before.approach = p
	return before
}

/*
================
approachInputsMatch
================
*/
func (state *divisionMonsterState) approachInputsMatch(p *approachNavigation) bool {
	if p == nil {
		return true
	}
	group := state.approachTargets[p.target]
	if (group != nil) != p.existed || group != nil && group.revision != p.revision {
		return false
	}
	for gid, input := range p.inputs {
		if state.movers.get(gid) != input {
			return false
		}
	}
	return true
}

/*
================
commitApproach
================
*/
func (state *divisionMonsterState) commitApproach(p *approachNavigation, gid uint32, next monster.MoverState) {
	if p == nil || state.approachActors[gid] != p.target {
		return
	}
	// A failed corridor must not reserve an unusable slot indefinitely. Retry
	// timing, clipping, routing and wire publication remain navigation-owned.
	if !next.InFlight(next.DepartMs) {
		p.group.slots.Release(gid)
		member := p.group.members[gid]
		member.slot = -1
		p.group.members[gid] = member
	}
	if current := state.approachTargets[p.target]; current != nil {
		p.group.revision = current.revision + 1
	}
	state.approachTargets[p.target] = &p.group
}

/*
================
squadApproachGoal

Direction/end-point geometry only. planSegment remains the sole routing,
surface, quantization, speed, time and packet owner; no second integrator.
================
*/
func squadApproachGoal(live monster.Pose, target playerPose, spacing CombatSpacing, slot int) Spawn {
	if slot < 0 {
		return NormalizeSpawnFrame(target.Pose)
	}
	reach := float32(spacing.AdmissionRadius())
	x, z := monster.NativeApproachOffset(slot, reach)
	goal := target.Pose
	goal.X = float64(float32(float32(goal.X) + x))
	goal.Z = float64(float32(float32(goal.Z) + z))
	goal = NormalizeSpawnFrame(goal)
	// Dungeon actors run the region-routed pursuit (installed by the region
	// test), which steers to the slot point whether or not the target moves;
	// only the outdoor direction-gated pursuit extends
	// (tactics_plane.go).
	if !pursuitAt(live.RegionID).ExtendsTowardMovingTarget() || !target.chaseGuidance().TargetMoving() {
		return goal
	}
	// The pursuit endpoint uses actor-to-target center distance, not distance to the slot.
	candidate := monster.Pose{RegionID: goal.RegionID, X: goal.X, Y: goal.Y, Z: goal.Z}
	dx, _, dz := monster.NativeTacticsRelative(live, candidate)
	dy := float32(candidate.Y) - float32(live.Y)
	length := float32(math.Sqrt(float64(float32(float64(dx)*float64(dx) + float64(dy)*float64(dy) + float64(dz)*float64(dz)))))
	if !(length > 0) {
		return NormalizeSpawnFrame(target.Pose)
	}
	distance := tacticsDistance3D(live, monster.Pose{RegionID: target.Pose.RegionID, X: target.Pose.X, Y: target.Pose.Y, Z: target.Pose.Z})
	goal = poseToSpawn(live)
	goal.X = float64(float32(float32(live.X) + float32(float32(dx/length)*distance)))
	goal.Y = float64(float32(float32(live.Y) + float32(float32(dy/length)*distance)))
	goal.Z = float64(float32(float32(live.Z) + float32(float32(dz/length)*distance)))
	return NormalizeSpawnFrame(goal)
}
