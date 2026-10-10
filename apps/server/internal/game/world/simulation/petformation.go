/*
===========================================================================

petformation.go - native owner-follow decisions on the existing COS mover

The action owner retains reservations and the FOLLOW state timer. This module
projects native destinations into the shared movement and collision lifecycle.

===========================================================================
*/
package simulation

import (
	"math"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
PetFormationStep
================
*/
type PetFormationStep struct {
	Relocated  *bool
	Owner      WorldState
	Slots      *monster.ApproachSlots
	Slot       *int
	BodyRadius float32
	Speed      float32
	Now        int64
	Surface    func(region uint16, x, y, z float64) (float64, bool)
	Constrain  func(Spawn, Spawn) (Spawn, *MoveError)
}

/*
================
PetRouteFromMonster

The monster AI's detour planner (PlanMonsterRoute) as a pet's: a ready
route of two or more points becomes the pet's waypoints. A direct route
(one point) or an unresolved one is no detour.
================
*/
func PetRouteFromMonster(plan func(from, goal monster.Pose) *monster.NavigationRoute) PetRoutePlanner {
	if plan == nil {
		return nil
	}
	return func(from, goal Spawn) []Spawn {
		route := plan(spawnToPose(from), spawnToPose(goal))
		if route == nil || route.Status() != monster.NavigationRouteReady || route.Len() < 2 {
			return nil
		}
		points := make([]Spawn, route.Len())
		for i := range points {
			points[i] = poseToSpawn(route.Point(i))
		}
		return points
	}
}

/*
================
FollowFormation

549F80 / 55E090 / 540F70. Completion releases the slot; it does not replace
the destination with an invented half-radius stand-off point.
================
*/
func (p *PetFollower) FollowFormation(in PetFormationStep) []Frame {
	if p.gid == 0 || in.Now < 0 || p.clockStarted && in.Now <= p.lastTick {
		return nil
	}
	p.clockStarted, p.lastTick = true, in.Now
	from, owner := p.Position(in.Now), in.Owner.LiveSpawnAt(in.Now)
	if !finitePetSpawn(from) || !finitePetSpawn(owner) || in.Speed <= 0 || in.Constrain == nil || in.Surface == nil {
		in.Slots.Release(p.gid)
		return p.Stop(in.Now)
	}
	if !monster.FollowLocationCompatible(from.RegionID, owner.RegionID) {
		in.Slots.Release(p.gid)
		// 541110 refuses outdoor -> dungeon. Entry/teleport already rebinds
		// companions through its own authority; this branch catches stragglers.
		if from.RegionID&0x8000 == 0 && owner.RegionID&0x8000 != 0 {
			return p.Stop(in.Now)
		}
		y, valid := in.Surface(owner.RegionID, owner.X, owner.Y, owner.Z)
		if !valid {
			return p.Stop(in.Now)
		}
		owner.Y = y
		p.Displace(owner, in.Now)
		if in.Relocated != nil {
			*in.Relocated = true
		}
		return nil
	}
	live, leader := spawnToPose(from), spawnToPose(owner)
	if monster.NativeOwnerFollowMotion(monster.OwnerFollowInput{Live: live, Owner: leader}).Satisfied {
		in.Slots.Release(p.gid)
		return p.Stop(in.Now)
	}
	*in.Slot = in.Slots.AssignOwnerFollow(p.gid, *in.Slot, live, leader)
	goal := monster.NativeOwnerFollowGoal(monster.OwnerFormationGoal{Owner: leader, Slot: *in.Slot, BodyRadius: in.BodyRadius,
		Surface: func(point monster.Pose) (monster.Pose, bool) {
			y, valid := in.Surface(point.RegionID, point.X, point.Y, point.Z)
			if valid {
				point.Y = float64(float32(y))
			}
			return point, valid
		}, Normalize: func(point monster.Pose) monster.Pose { return spawnToPose(NormalizeSpawnFrame(poseToSpawn(point))) }})
	if !monster.FollowLocationCompatible(from.RegionID, goal.RegionID) {
		in.Slots.Release(p.gid)
		return p.Stop(in.Now)
	}
	decision := monster.NativeOwnerFollowMotion(monster.OwnerFollowInput{Live: live, Owner: leader, SlotGoal: goal,
		OldGoal: spawnToPose(p.world.Spawn), OwnerGoal: spawnToPose(in.Owner.Spawn),
		Moving:      p.world.MoveSegment.Valid() && in.Now < p.world.MoveSegment.ArrivesAtMs,
		OwnerMoving: in.Owner.MovingAt(in.Now)})
	if !decision.Move || decision.Motion.Distance <= float64(float32(0.01)) {
		return nil
	}
	destination := NormalizeSpawnFrame(poseToSpawn(decision.Motion.Destination(live, decision.Motion.Distance)))
	destination.X, destination.Y, destination.Z = math.Trunc(destination.X), math.Trunc(destination.Y), math.Trunc(destination.Z)
	return p.moveTo(destination, float64(in.Speed), in.Now, in.Constrain)
}
