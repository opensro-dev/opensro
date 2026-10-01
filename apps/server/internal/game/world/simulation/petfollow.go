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
}

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
	goal, fault := constrain(from, goal)
	if fault != nil || !finitePetSpawn(goal) || !petSamePlane(from.RegionID, goal.RegionID) {
		return p.Stop(nowMs)
	}
	// Destination packets carry integer coordinates: simulate that same goal.
	goal.X, goal.Y, goal.Z = math.Round(goal.X), math.Round(goal.Y), math.Round(goal.Z)
	// Rounding can cross a collision boundary. Require the rounded endpoint to
	// be admitted too; never restore a rejected fractional endpoint downstream.
	checked, fault := constrain(from, goal)
	if fault != nil || !samePetGoal(checked, goal) {
		return p.Stop(nowMs)
	}
	if p.world.MoveSegment.Valid() && nowMs < p.world.MoveSegment.ArrivesAtMs && samePetGoal(p.world.Spawn, goal) {
		return nil
	}
	distance = WorldDistance2D(from, goal)
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
Stop
================
*/
func (p *PetFollower) Stop(nowMs int64) []Frame {
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
