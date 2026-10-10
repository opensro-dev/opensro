/*
===========================================================================

petfollow.go - independent summoned-pet movement and status speed changes

===========================================================================
*/

package simulation

import (
	"math"

	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
)

// PetFollower owns the unmounted COS motion plane. It is stepped only by the
// existing simulation clock, under its division's action lock. No player world
// state is borrowed or mutated to move a pet.
/*
================
PetFollower
================
*/
type PetFollower struct {
	gid          uint32
	world        WorldState
	lastTick     int64
	clockStarted bool
	runPublished bool
	revision     uint64
	planRoute    PetRoutePlanner
	detour       petDetour
}

// PetRoutePlanner plans a pet's way around what its straight path cannot
// cross: waypoints that end at goal, or nil when no route is known.
type PetRoutePlanner func(from, goal Spawn) []Spawn

/*
================
petDetour

The route a blocked pet is following: its waypoints, the goal they lead
to, and when a failed plan may be tried again.
================
*/
type petDetour struct {
	points  []Spawn
	goal    Spawn
	retryAt int64
}

/*
================
petClip

One constrained segment: where it stops, or why it was refused.
================
*/
type petClip struct {
	reached Spawn
	fault   *MoveError
}

const (
	// petDetourGoalSlack keeps a route while its goal moves this little, as
	// monsters keep their detour corridor (monsternavigation.go).
	petDetourGoalSlack = 32.0
	// petDetourArrived is where a waypoint counts as reached.
	petDetourArrived = 1.0
	// petDetourRetryMs paces a failed plan, as monsterNavigationRetryMs does.
	petDetourRetryMs = 1000
)

/*
================
NewPetFollower
================
*/
func NewPetFollower(gid uint32, spawn Spawn) *PetFollower {
	return &PetFollower{gid: gid, world: WorldState{Spawn: spawn, MovementMode: RunMode}}
}

/*
================
SetRoutePlanner

INFERENCE: native COS movement runs the same CTactics mover as monsters, so
a pet whose straight path is blocked (a raised room's doorway, a wall) takes
the detour the monster AI takes (PlanMonsterRoute) instead of standing at
the wall. A nil planner keeps the straight, clipped segment.
================
*/
func (p *PetFollower) SetRoutePlanner(plan PetRoutePlanner) { p.planRoute = plan }

/*
================
GID
================
*/
func (p *PetFollower) GID() uint32 { return p.gid }

/*
================
Position
================
*/
func (p *PetFollower) Position(nowMs int64) Spawn { return p.world.LiveSpawnAt(nowMs) }

// Presentation returns detached motion state. The action owner holds its
// division lock while copying; visibility never reads a mutable follower.
/*
================
Presentation
================
*/
func (p *PetFollower) Presentation() (WorldState, uint64) {
	return CloneWorldState(p.world), p.revision
}

/*
================
SetMovementSpeeds

Use the shared mover rescaling so a speed change cannot jump the pet or leave
the previous arrival deadline active. The division owner serializes updates.
================
*/
func (p *PetFollower) SetMovementSpeeds(walk, run float32, nowMs int64) {
	if p.world.UpdateMovementSpeeds(walk, run, nowMs) {
		p.revision++
	}
}

// Follow distance is a bounded policy based on the later server's 549F80
// distance comparison. Exact formation offsets and steering parity remain
// separate; this is not an exact reconstruction of that function.
const PetFollowDistance = 60.0

// Advance emits changed destinations and one stop at arrival, never a packet
// for each idle tick. Geometry refusal stops at the current live position.
// A missing collision owner refuses motion; it never enables noclip.
/*
================
Advance
================
*/
func (p *PetFollower) Advance(owner Spawn, speed float64, nowMs int64, constrain func(Spawn, Spawn) (Spawn, *MoveError)) []Frame {
	return p.Approach(owner, speed, nowMs, PetFollowDistance, constrain)
}

// Approach shares collision and motion ownership with following.
/*
================
Approach
================
*/
func (p *PetFollower) Approach(owner Spawn, speed float64, nowMs int64, stopDistance float64, constrain func(Spawn, Spawn) (Spawn, *MoveError)) []Frame {
	if !(stopDistance > 0) || math.IsInf(stopDistance, 0) {
		return p.Stop(nowMs)
	}
	if p.gid == 0 || nowMs < 0 || p.clockStarted && nowMs <= p.lastTick {
		return nil
	}
	p.clockStarted, p.lastTick = true, nowMs
	from := p.Position(nowMs)
	if !finitePetSpawn(from) || !finitePetSpawn(owner) || !petSamePlane(from.RegionID, owner.RegionID) || !(speed > 0) || math.IsInf(speed, 0) || constrain == nil {
		return p.Stop(nowMs)
	}
	distance := WorldDistance2D(from, owner)
	if distance <= stopDistance {
		return p.Stop(nowMs)
	}
	// Reuse the shared region conversion. A pet approaches the owner's live
	// position; it never teleports to an unvalidated owner destination.
	point := worldgeom.Interpolate(worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z}, worldgeom.RegionXZ{RegionID: owner.RegionID, X: owner.X, Z: owner.Z}, (distance-stopDistance/2)/distance)
	goal := Spawn{RegionID: point.RegionID, X: math.Round(point.X), Y: math.Round(owner.Y), Z: math.Round(point.Z)}
	if p.world.MoveSegment.Valid() && nowMs < p.world.MoveSegment.ArrivesAtMs && samePetGoal(p.world.Spawn, goal) {
		return nil
	}
	return p.moveTo(goal, speed, nowMs, constrain)
}

/*
================
moveTo

Both approach and native formation submit through the same collision owner.
================
*/
func (p *PetFollower) moveTo(goal Spawn, speed float64, nowMs int64, constrain func(Spawn, Spawn) (Spawn, *MoveError)) []Frame {
	from := p.Position(nowMs)
	target := goal
	goal, fault := constrain(from, target)
	if waypoint, ok := p.detourWaypoint(from, target, petClip{reached: goal, fault: fault}, nowMs); ok {
		goal, fault = constrain(from, waypoint)
	}
	if fault != nil || !finitePetSpawn(goal) || !petSamePlane(from.RegionID, goal.RegionID) {
		return p.Stop(nowMs)
	}
	// Destination packets carry integer coordinates: simulate that same goal.
	goal.X, goal.Y, goal.Z = math.Round(goal.X), math.Round(goal.Y), math.Round(goal.Z)
	// Rounding can cross a collision boundary. Require the rounded endpoint to
	// be admitted too; never restore a rejected fractional endpoint downstream.
	checked, fault := constrain(from, goal)
	// Terrain resolves a float height; the destination wire stores an integer.
	// Compare that same quantized height instead of rejecting every slope.
	checked.Y = math.Round(checked.Y)
	if fault != nil || !samePetGoal(checked, goal) {
		return p.Stop(nowMs)
	}
	if p.world.MoveSegment.Valid() && nowMs < p.world.MoveSegment.ArrivesAtMs && samePetGoal(p.world.Spawn, goal) {
		return nil
	}
	distance := WorldDistance2D(from, goal)
	if !(distance > 0) || math.IsInf(distance, 0) {
		return p.Stop(nowMs)
	}
	goal.Angle, _ = HeadingFromMovement(from, goal)
	duration := math.Ceil(distance / speed * 1000)
	if duration > float64(math.MaxInt64-nowMs) || duration < 1 {
		return p.Stop(nowMs)
	}
	p.world.Spawn = goal
	p.world.MoveSegment = &MoveSegment{From: from, StartedAtMs: nowMs, ArrivesAtMs: nowMs + int64(duration)}
	p.revision++
	source := MovementSourceFromSpawn(from)
	state := wire.ObjectStateRefresh{Gid: p.gid, StateType: wire.StateChannelMove, Value: RunMode}
	var frames []Frame
	if !p.runPublished {
		frames = append(frames, Frame{Opcode: wire.OpObjectStateRefresh, Payload: state.Encode()})
		p.runPublished = true
	}
	return append(frames, Frame{Opcode: OpMovementAck, Payload: BuildMovementAckPayload(p.gid, MovementRequest{Mode: MovementAckDestinationMode, RegionID: goal.RegionID, X: goal.X, Y: goal.Y, Z: goal.Z}, &source)})
}

/*
================
detourWaypoint

The next waypoint toward goal when the straight segment (straight, the
caller's constrained result) cannot reach it. A kept route serves while its
goal stays within petDetourGoalSlack; reached waypoints are dropped.
Otherwise a clipped straight segment asks the planner, at most once per
petDetourRetryMs while no route is found.
================
*/
func (p *PetFollower) detourWaypoint(from, goal Spawn, straight petClip, nowMs int64) (Spawn, bool) {
	if p.planRoute == nil {
		return Spawn{}, false
	}
	route := &p.detour
	if len(route.points) > 0 && WorldDistance2D(route.goal, goal) <= petDetourGoalSlack {
		for len(route.points) > 1 && WorldDistance2D(from, route.points[0]) < petDetourArrived {
			route.points = route.points[1:]
		}
		if WorldDistance2D(from, route.points[0]) >= petDetourArrived {
			return route.points[0], true
		}
	}
	route.points = nil
	if straight.fault == nil && WorldDistance2D(straight.reached, goal) < petDetourArrived {
		return Spawn{}, false
	}
	if nowMs < route.retryAt {
		return Spawn{}, false
	}
	points := p.planRoute(from, goal)
	if len(points) < 2 {
		route.retryAt = nowMs + petDetourRetryMs
		return Spawn{}, false
	}
	route.points, route.goal, route.retryAt = points, goal, 0
	return points[0], true
}

/*
================
Stop
================
*/
func (p *PetFollower) Stop(nowMs int64) []Frame {
	p.detour.points = nil
	if !p.world.MoveSegment.Valid() {
		return nil
	}
	pose := p.Position(nowMs)
	p.world.Spawn, p.world.MoveSegment = pose, nil
	p.revision++
	correction := wire.ObjectSourceCorrection{Gid: p.gid, Position: wire.Position{RegionID: pose.RegionID, X: float32(pose.X), Y: float32(pose.Y), Z: float32(pose.Z), Heading: pose.Angle}}
	return []Frame{{Opcode: wire.OpObjectSourceCorrection, Payload: correction.Encode()}}
}

/*
================
finitePetSpawn
================
*/
func finitePetSpawn(p Spawn) bool {
	return !math.IsNaN(p.X) && !math.IsNaN(p.Y) && !math.IsNaN(p.Z) && !math.IsInf(p.X, 0) && !math.IsInf(p.Y, 0) && !math.IsInf(p.Z, 0)
}

/*
================
samePetGoal
================
*/
func samePetGoal(a, b Spawn) bool {
	return a.RegionID == b.RegionID && a.X == b.X && a.Y == b.Y && a.Z == b.Z
}

/*
================
petSamePlane
================
*/
func petSamePlane(a, b uint16) bool {
	return worldgeom.SamePlane(a, b) && (!worldgeom.IsDungeonRegion(a) || a == b)
}

/*
================
Displace

A committed hit relocates the same mover and retires its previous segment.
The cast result carries the position to observers.
================
*/
func (p *PetFollower) Displace(pose Spawn, now int64) {
	p.world.Spawn = pose
	p.world.MoveSegment = nil
	p.lastTick, p.clockStarted = now, true
	p.revision++
}
