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
	"opensro.online/server/internal/game/world/monster"
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
	// progress is the native stuck count (the tactics record's
	// +184/+19C/+148): each
	// stop that left the pet within 20 of where its move began counts, and
	// from the tenth the stuck event is raised.
	// stopCounted marks the current segment's stop as already counted; stuck
	// holds the event for the action owner (TakeStuck).
	progress    monster.MovementProgress
	stopCounted bool
	stuck       bool
	// trail is where the owner walked, oldest first, a point every
	// petTrailSpacing, kept only while ownerTrail is on (SetOwnerTrail);
	// following is set while Advance steps, so the detour may take the
	// trail (trailRoute).
	trail      []Spawn
	ownerTrail bool
	following  bool
	// reach is the last move's reach verdict (Reach).
	reach PetReach
}

/*
================
PetReach

A move's reach verdict: Blocked when the straight segment toward Goal was
refused or cut short and no detour leads there; Reached is where that
segment stops. Goal tells its readers which move the verdict belongs to.
================
*/
type PetReach struct {
	Goal, Reached Spawn
	Blocked       bool
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
	// trail marks a route taken from the owner's trail (trailRoute):
	// its later points may come into straight reach (skipAhead).
	trail bool
	// searched/found are the last search's outcome for goal: a route
	// (planned or trail) was known. They outlive a refused leg and the
	// retry pause after it, so a pause between plans never reads as "no
	// way"; they speak only for the goal searched (noteReach).
	searched bool
	found    bool
}

/*
================
petDetour.failedFor

True when the last search was for goal (within the slack a route serves)
and found nothing. A goal never searched is unknown, not unreachable.
================
*/
func (d *petDetour) failedFor(goal Spawn) bool {
	return d.searched && !d.found && WorldDistance2D(d.goal, goal) <= petDetourGoalSlack
}

/*
================
petDetour.noteSearch
================
*/
func (d *petDetour) noteSearch(goal Spawn, found bool) {
	d.searched, d.found, d.goal = true, found, goal
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
	// petTrailSpacing is how far the owner walks between two trail points,
	// and petTrailMax how many the follower keeps (about a minute's run).
	petTrailSpacing = 20.0
	petTrailMax     = 96
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

INFERENCE: native COS movement runs the same tactics mover as
monsters, so
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
Moving

Whether a segment is still under way at nowMs.
================
*/
func (p *PetFollower) Moving(nowMs int64) bool {
	return p.world.MoveSegment.Valid() && nowMs < p.world.MoveSegment.ArrivesAtMs
}

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

// Follow distance is a bounded policy based on the reference
// server's distance comparison. Exact formation offsets and steering parity remain
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
	if p.ownerTrail {
		p.noteOwner(owner)
	}
	p.following = true
	defer func() { p.following = false }()
	return p.Approach(owner, speed, nowMs, PetFollowDistance, constrain)
}

/*
================
SetOwnerTrail

Port-only, not native: a following trade transport (SRO_TRANSPORT_FOLLOW,
action transportfollow.go) walks its owner's trail past a wall
(trailRoute). Natively a transport does not follow at all, and a walled-in
pet raises the stuck event (action petstuck.go), which these followers
keep: off, the default, records and takes no trail.
================
*/
func (p *PetFollower) SetOwnerTrail(on bool) {
	p.ownerTrail = on
	if !on {
		p.trail = nil
	}
}

/*
================
noteOwner

Adds the owner's live pose to the trail once it has walked petTrailSpacing
from the last point; the oldest point goes past petTrailMax. A pose on
another plane starts the trail again.
================
*/
func (p *PetFollower) noteOwner(owner Spawn) {
	if !finitePetSpawn(owner) {
		return
	}
	if n := len(p.trail); n > 0 {
		last := p.trail[n-1]
		if petSamePlane(last.RegionID, owner.RegionID) && WorldDistance2D(last, owner) < petTrailSpacing {
			return
		}
		if !petSamePlane(last.RegionID, owner.RegionID) {
			p.trail = p.trail[:0]
		}
	}
	p.trail = append(p.trail, owner)
	if len(p.trail) > petTrailMax {
		p.trail = append(p.trail[:0], p.trail[len(p.trail)-petTrailMax:]...)
	}
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
	p.settleArrival(nowMs)
	from := p.Position(nowMs)
	if !finitePetSpawn(from) || !finitePetSpawn(owner) || !petSamePlane(from.RegionID, owner.RegionID) || !(speed > 0) || math.IsInf(speed, 0) || constrain == nil {
		return p.Stop(nowMs)
	}
	distance := WorldDistance2D(from, owner)
	if distance <= stopDistance {
		// In reach: the move is done and the AI leaves its moving state
		// (a state change clears the stuck count).
		frames := p.Stop(nowMs)
		p.ResetStuck()
		p.reach = PetReach{}
		return frames
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
	straight := petClip{reached: goal, fault: fault}
	waypoint, detouring := p.detourWaypoint(from, target, straight, nowMs, constrain)
	p.noteReach(from, target, straight, detouring)
	if detouring {
		goal, fault = constrain(from, waypoint)
	}
	// A refused leg ends its route; the next plan waits out the retry pace,
	// as a failed plan does, rather than searching again every tick.
	refuse := func() []Frame {
		// The native step records the command before the mover refuses
		// it; the refusal is a stop (event 8) where the pet stands.
		p.noteIssued(from, nowMs)
		if !p.world.MoveSegment.Valid() {
			p.noteStopped(from)
		}
		frames := p.Stop(nowMs)
		if detouring {
			p.detour.retryAt = nowMs + petDetourRetryMs
		}
		return frames
	}
	if fault != nil || !finitePetSpawn(goal) || !petSamePlane(from.RegionID, goal.RegionID) {
		return refuse()
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
		return refuse()
	}
	if p.world.MoveSegment.Valid() && nowMs < p.world.MoveSegment.ArrivesAtMs && samePetGoal(p.world.Spawn, goal) {
		return nil
	}
	distance := WorldDistance2D(from, goal)
	if !(distance > 0) || math.IsInf(distance, 0) {
		return refuse()
	}
	goal.Angle, _ = HeadingFromMovement(from, goal)
	duration := math.Ceil(distance / speed * 1000)
	if duration > float64(math.MaxInt64-nowMs) || duration < 1 {
		return refuse()
	}
	p.noteIssued(from, nowMs)
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
noteReach

Records whether the goal this move asked for can be reached: blocked when
the straight segment was refused or stopped short of it and the last search,
for this goal, found no detour (planned route or owner trail). A pause
between searches after a refused leg keeps the route it found, and a goal
no search has served yet is unknown, never "no way" (petDetour.failedFor). The follower only records the
fact; its owners judge it (action petrecovery.go), because "close enough"
differs between a strike and a follow.
================
*/
func (p *PetFollower) noteReach(from, goal Spawn, straight petClip, detouring bool) {
	p.reach = PetReach{
		Goal:    goal,
		Reached: straight.reached,
		Blocked: !detouring && p.detour.failedFor(goal) && (straight.fault != nil || WorldDistance2D(straight.reached, goal) >= petDetourArrived),
	}
	if straight.fault != nil {
		p.reach.Reached = from
	}
}

/*
================
Reach

The last move's reach verdict (noteReach). A pet in reach of its goal, or
relocated, is never blocked.
================
*/
func (p *PetFollower) Reach() PetReach {
	return p.reach
}

/*
================
detourWaypoint

The next waypoint toward goal when the straight segment (straight, the
caller's constrained result) cannot reach it; a clear one ends any route
at once. A kept route serves while its
goal stays within petDetourGoalSlack, or for one petDetourRetryMs after it was
planned; reached waypoints are dropped. Otherwise a clipped straight segment
asks the planner, at most once per petDetourRetryMs: after a plan, a failed
plan, or a refused leg (moveTo).
================
*/
func (p *PetFollower) detourWaypoint(from, goal Spawn, straight petClip, nowMs int64, constrain func(Spawn, Spawn) (Spawn, *MoveError)) (Spawn, bool) {
	if p.planRoute == nil && !(p.following && p.ownerTrail) {
		p.detour.noteSearch(goal, false)
		return Spawn{}, false
	}
	route := &p.detour
	// A clear straight segment ends any detour: the route was only ever a way
	// around what that segment could not cross.
	if straight.fault == nil && WorldDistance2D(straight.reached, goal) < petDetourArrived {
		route.points = nil
		return Spawn{}, false
	}
	// A route serves while its goal stays within the slack, and for one retry
	// window after it was planned however far the goal runs: a fleeing goal
	// re-plans at most once per petDetourRetryMs.
	if len(route.points) > 0 && (WorldDistance2D(route.goal, goal) <= petDetourGoalSlack || nowMs < route.retryAt) {
		reached := false
		for len(route.points) > 1 && WorldDistance2D(from, route.points[0]) < petDetourArrived {
			route.points = route.points[1:]
			reached = true
		}
		if reached && route.trail {
			route.points = p.skipAhead(from, route.points, constrain)
		}
		if WorldDistance2D(from, route.points[0]) >= petDetourArrived {
			return route.points[0], true
		}
	}
	route.points = nil
	if nowMs < route.retryAt {
		return Spawn{}, false
	}
	if points := p.trailRoute(from, goal, constrain); len(points) > 0 {
		route.points, route.retryAt, route.trail = points, nowMs+petDetourRetryMs, true
		route.noteSearch(goal, true)
		return points[0], true
	}
	if p.planRoute == nil {
		route.retryAt = nowMs + petDetourRetryMs
		route.noteSearch(goal, false)
		return Spawn{}, false
	}
	route.trail = false
	points := p.planRoute(from, goal)
	if len(points) < 2 {
		route.retryAt = nowMs + petDetourRetryMs
		route.noteSearch(goal, false)
		return Spawn{}, false
	}
	route.points, route.retryAt = points, nowMs+petDetourRetryMs
	route.noteSearch(goal, true)
	return points[0], true
}

/*
================
trailRoute

While following, a blocked follower walks the owner's own trail: from the
newest trail point one straight clip reaches whole, every later point,
then the goal. The owner walked each step between two points, so the way
on is a walk the owner took (a transport left at a town wall where two
regions meet while its trader went out by the gate; the monster detour
searches only a few cells round it, so it stood). Port-only, with the
following transport itself (SetOwnerTrail): the owner's path is a route
through the gap it used. The older points stay: a follower stopped in
follow reach through a wall needs the one it passed before. Nil when
none is in straight reach.
================
*/
func (p *PetFollower) trailRoute(from, goal Spawn, constrain func(Spawn, Spawn) (Spawn, *MoveError)) []Spawn {
	if !p.following || !p.ownerTrail || constrain == nil {
		return nil
	}
	for i := len(p.trail) - 1; i >= 0; i-- {
		point := p.trail[i]
		if !petSamePlane(from.RegionID, point.RegionID) || WorldDistance2D(from, point) < petDetourArrived {
			continue
		}
		reached, fault := constrain(from, point)
		if fault != nil || WorldDistance2D(reached, point) >= petDetourArrived {
			continue
		}
		return append(append(make([]Spawn, 0, len(p.trail)-i+1), p.trail[i:]...), goal)
	}
	return nil
}

/*
================
skipAhead

On a trail route, at each waypoint reached: the newest later trail point
one straight clip reaches whole, so a follower does not walk a loop the
owner walked when a later point is already in sight. The goal (the last
point) is left to the straight segment, which ends the route at once.
================
*/
func (p *PetFollower) skipAhead(from Spawn, points []Spawn, constrain func(Spawn, Spawn) (Spawn, *MoveError)) []Spawn {
	if constrain == nil {
		return points
	}
	for i := len(points) - 2; i > 0; i-- {
		point := points[i]
		if !petSamePlane(from.RegionID, point.RegionID) {
			continue
		}
		reached, fault := constrain(from, point)
		if fault == nil && WorldDistance2D(reached, point) < petDetourArrived {
			return points[i:]
		}
	}
	return points
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
	if !p.stopCounted {
		p.noteStopped(pose)
	}
	p.world.Spawn, p.world.MoveSegment = pose, nil
	p.revision++
	correction := wire.ObjectSourceCorrection{Gid: p.gid, Position: wire.Position{RegionID: pose.RegionID, X: float32(pose.X), Y: float32(pose.Y), Z: float32(pose.Z), Heading: pose.Angle}}
	return []Frame{{Opcode: wire.OpObjectSourceCorrection, Payload: correction.Encode()}}
}

/*
================
noteIssued

The native tactics step: every admitted move command records
where it was issued, and a command issued while the pet stands still also
starts a new move sequence (monster.MovementProgress.Issued).
================
*/
func (p *PetFollower) noteIssued(from Spawn, nowMs int64) {
	p.progress.Issued(spawnToPose(from), p.Moving(nowMs))
	p.stopCounted = false
}

/*
================
noteStopped

The native move stop raises AI event 8 whenever a non-player's
move stops, arrived or blocked. The move-stopped state counts
the stop when it is within 20 of the sequence origin; from the
tenth such stop the stuck event is raised (event 0x28).
================
*/
func (p *PetFollower) noteStopped(at Spawn) {
	p.stopCounted = true
	if p.progress.Arrived(spawnToPose(at)) {
		p.stuck = true
	}
}

/*
================
settleArrival

The port's segment arrives silently when its time runs out; count that
arrival as the stop native's mover reports.
================
*/
func (p *PetFollower) settleArrival(nowMs int64) {
	if p.world.MoveSegment.Valid() && nowMs >= p.world.MoveSegment.ArrivesAtMs && !p.stopCounted {
		p.noteStopped(p.world.Spawn)
	}
}

/*
================
TakeStuck

Whether the stuck event fired since the last call; each call consumes it.
================
*/
func (p *PetFollower) TakeStuck() bool {
	stuck := p.stuck
	p.stuck = false
	return stuck
}

/*
================
StuckPending

Whether the stuck event is held for the action owner, without
consuming it (TakeStuck does). A caller that must pay for the answer
only when the event fired asks this first (#2139).
================
*/
func (p *PetFollower) StuckPending() bool {
	return p.stuck
}

/*
================
ResetStuck

Every native state change and tactics reset clears the count;
the origins stay.
================
*/
func (p *PetFollower) ResetStuck() {
	p.progress.ResetFailures()
	p.stuck = false
	// A state change ends the move the reach verdict belongs to: a fight's
	// or a pickup's verdict never speaks for the follow that comes next.
	p.reach = PetReach{}
}

/*
================
RelocateTo

The native relocation request: place the pet on the given
point at its surface height. False when that point has no surface.
================
*/
func (p *PetFollower) RelocateTo(point Spawn, now int64, surface func(region uint16, x, y, z float64) (float64, bool)) bool {
	y, valid := surface(point.RegionID, point.X, point.Y, point.Z)
	if !valid {
		return false
	}
	point.Y = y
	p.Displace(point, now)
	return true
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
	return worldgeom.SameFloor(a, b)
}

/*
================
Displace

A committed hit relocates the same mover and retires its previous segment.
The cast result carries the position to observers.
================
*/
func (p *PetFollower) Displace(pose Spawn, now int64) {
	p.reach = PetReach{}
	p.world.Spawn = pose
	p.world.MoveSegment = nil
	p.lastTick, p.clockStarted = now, true
	p.revision++
}
