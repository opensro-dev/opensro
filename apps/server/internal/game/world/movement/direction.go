/*
===========================================================================

direction.go - the direction walk: legs, blocking and continuation

A 0x7738 mode-0 GO command walks the mover along a heading until the ground
move test blocks it (simulation/direction.go has the native chain). The
native server steps its command mover every tick with no distance cap; the
port's live plane needs a goal, so the walk runs as consecutive legs of
simulation.DirectionLegUnits. This file owns the walk's lifetime:

  - the first leg is committed by the 0x7738 handler (handleMove);
  - DirectionTickHook plans the next leg shortly before one matures, so the
    tick never publishes a settle between legs and peers see one walk;
  - a leg that ends on a blocking contact ends the walk: when it matures
    the mover gets the 0xB2F5 correction (observers get the tick's own
    settle), the server authority halting a client that walked on;
  - 0x72CF steers and 0x72F5 stops it (steer.go).

The mover's own client walks the direction by itself (native nav state 2),
so continuation legs send it nothing; only the tick's destination acks
carry them to observers.

A walk is only live while the world plane still holds its leg: any other
writer (a new click, pickup or attack approach, sit, stun, teleport, death,
rebirth) replaces the goal and the walk ends silently at its next check.

===========================================================================
*/
package movement

import (
	"sync"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

// directionAccessBisections bounds the search for the last enterable point
// of a leg that runs into a restricted region: 1000 / 2^10 is about one
// world unit.
const directionAccessBisections = 10

/*
================
directionWalk

One walking mover. goal and moving identify the leg the walk committed:
the walk is current only while the world plane still holds that goal. A
gait or speed change retimes the segment toward the same goal
(WorldState.UpdateMovementSpeeds, applyMotionCode) and keeps the walk.
================
*/
type directionWalk struct {
	divisionID string
	character  *enterworld.Character
	cosGID     uint32
	request    simulation.MovementRequest
	goal       simulation.Spawn
	moving     bool
	blocked    bool
}

/*
================
directionWalk.current
================
*/
func (walk directionWalk) current(world simulation.WorldState) bool {
	return world.Spawn == walk.goal && (world.MoveSegment != nil) == walk.moving
}

/*
================
directionWalks

The registry of walking movers, keyed by simulation.WorldKey. It has its
own lock because the tick hook lists it without a character lock; every
change to one walk happens under that character's operation lock.
================
*/
type directionWalks struct {
	mu    sync.Mutex
	walks map[string]directionWalk
}

/*
================
directionWalks.get
================
*/
func (d *directionWalks) get(key string) (directionWalk, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	walk, ok := d.walks[key]
	return walk, ok
}

/*
================
directionWalks.set
================
*/
func (d *directionWalks) set(key string, walk directionWalk) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.walks == nil {
		d.walks = make(map[string]directionWalk)
	}
	d.walks[key] = walk
}

/*
================
directionWalks.clear
================
*/
func (d *directionWalks) clear(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.walks, key)
}

/*
================
directionWalks.keys
================
*/
func (d *directionWalks) keys() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	keys := make([]string, 0, len(d.walks))
	for key := range d.walks {
		keys = append(keys, key)
	}
	return keys
}

//============================================================================

/*
================
directionLeg

One constrained leg: where it ends, the surface ownership walked to get
there, and whether a blocking contact (or the end of walkable coverage, or
an area the character may not enter) ended it short of the full leg.
================
*/
type directionLeg struct {
	goal    simulation.Spawn
	walk    simulation.NavWalk
	blocked bool
}

/*
================
constrainDirectionLeg

The direction twin of ConstrainMovementFrom. The clip finds the first
blocking contact along the full leg (the native move test that stops nav
state 2), and the path guard
then inspects only the chord the walker will really cover. A chord the
guard still refuses (no walkable coverage ahead) leaves the walker standing
where it is, blocked.
================
*/
func (rt *Runtime) constrainDirectionLeg(character *enterworld.Character, from simulation.Spawn, fromOwner simulation.NavOwner, heading uint16) directionLeg {
	full := simulation.DirectionLegGoal(from, heading, simulation.DirectionLegUnits)
	end := full
	if rt.ClientClip != nil {
		end = rt.ClientClip.ProcessMoveFrom(character.Name, from, fromOwner, full)
	}
	blocked := end.RegionID != full.RegionID || end.X != full.X || end.Z != full.Z

	if rt.PathGuard != nil && rt.PathGuard.InspectMoveFrom(character.Name, from, fromOwner, end) != nil {
		return standingLeg(from, fromOwner)
	}
	if rt.CanEnterRegion != nil && !rt.CanEnterRegion(character, end.RegionID) {
		end = rt.enterableLegEnd(character, from, end)
		blocked = true
	}
	if end.RegionID == from.RegionID && end.X == from.X && end.Z == from.Z {
		return standingLeg(from, fromOwner)
	}

	end, walk := rt.walkOwners(from, fromOwner, end)
	return directionLeg{goal: end, walk: walk, blocked: blocked}
}

/*
================
standingLeg

A blocked leg of zero length: the walker keeps its position and its
retained surface owner.
================
*/
func standingLeg(from simulation.Spawn, fromOwner simulation.NavOwner) directionLeg {
	return directionLeg{goal: from, walk: simulation.NavWalk{Rest: fromOwner}, blocked: true}
}

/*
================
enterableLegEnd

The furthest point of the chord from -> to that stands in a region the
character may enter. The world-area policy is the same gate a destination
click meets at its endpoint; a walk that reaches a closed area stops at its
border instead of being refused outright, since the client is already
walking. Assumes from is enterable (the character stands there).
================
*/
func (rt *Runtime) enterableLegEnd(character *enterworld.Character, from, to simulation.Spawn) simulation.Spawn {
	best := from
	low, high := 0.0, 1.0
	for i := 0; i < directionAccessBisections; i++ {
		mid := (low + high) / 2
		planar := worldgeom.Interpolate(
			worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z},
			worldgeom.RegionXZ{RegionID: to.RegionID, X: to.X, Z: to.Z},
			mid,
		)
		candidate := simulation.Spawn{RegionID: planar.RegionID, X: planar.X, Y: from.Y + (to.Y-from.Y)*mid, Z: planar.Z, Angle: to.Angle}
		if rt.CanEnterRegion(character, candidate.RegionID) {
			best, low = candidate, mid
		} else {
			high = mid
		}
	}
	return best
}

/*
================
commitDirectionLeg

Plans the leg from the live point along request.HeadingWord, commits it
through the same fences as every move, and records the walk. The mover's
0xB738 ack comes back in the result; the continuation drops it.
================
*/
func (rt *Runtime) commitDirectionLeg(walk directionWalk, admission moveAdmission, nowMs int64) (simulation.MoveResult, simulation.WorldState, *simulation.MoveError) {
	character := walk.character
	live := admission.world.LiveSpawnAt(nowMs)
	leg := rt.constrainDirectionLeg(character, live, admission.world.LiveOwnerAt(nowMs), walk.request.HeadingWord)

	var result simulation.MoveResult
	committed, refusal := rt.commitMove(character, admission, func(world *simulation.WorldState) {
		result = simulation.ApplyDirectionLeg(world, enterworld.ObjectIDForCharacter(character), walk.request, leg.goal, nowMs)
		// Same rule as a destination commit: the spans belong to the chord
		// walked from this live point and nothing else.
		if result.LiveBefore == live {
			world.CommitWalk(leg.walk.Spans, leg.walk.Rest)
		}
	})
	if refusal != nil {
		rt.directions.clear(admission.worldKey)
		return result, committed, refusal
	}

	walk.goal = committed.Spawn
	walk.moving = committed.MoveSegment != nil
	walk.blocked = leg.blocked
	rt.directions.set(admission.worldKey, walk)
	return result, committed, nil
}

/*
================
startDirectionWalk

The 0x7738 mode-0 GO arm of handleMove: commit the first leg and answer
with the mode-0 ack that starts the client's own direction walk.
================
*/
func (rt *Runtime) startDirectionWalk(divisionID string, character *enterworld.Character, cosGID uint32, admission moveAdmission, request simulation.MovementRequest) MoveOutcome {
	walk := directionWalk{divisionID: divisionID, character: character, cosGID: cosGID, request: request}
	result, committed, refusal := rt.commitDirectionLeg(walk, admission, rt.Now().UnixMilli())
	if refusal != nil {
		return refusedMove(refusal)
	}
	key := admission.worldKey
	return MoveOutcome{
		Frames: []wire.Frame{
			{Opcode: simulation.OpMovementAck, Payload: result.AckPayload, Current: func() bool { return rt.Worlds.MovementCurrent(key, committed) }},
		},
		Result: &result,
	}
}

//============================================================================

/*
================
DirectionTickHook

The coordinator hook that keeps direction walks going. Wire it into the
mission ticker (cmd/services/sro-gameworld); without it every walk stops
after its first leg.
================
*/
func (rt *Runtime) DirectionTickHook() simulation.TickHook {
	return func(nowMs int64) []simulation.DivisionFrames {
		var out []simulation.DivisionFrames
		for _, key := range rt.directions.keys() {
			if frames, ok := rt.continueDirectionWalk(key, nowMs); ok {
				out = append(out, frames)
			}
		}
		return out
	}
}

/*
================
continueDirectionWalk

One walk's tick, under its character lock:

  - a walk whose leg the world no longer holds, or whose character can no
    longer move, ends silently - whoever replaced the leg published it;
  - an open leg inside DirectionLegLookaheadMs of its arrival is followed
    by the next leg from the live point;
  - a blocked leg ends the walk once it matures, with the 0xB2F5 correction
    for the mover.

================
*/
func (rt *Runtime) continueDirectionWalk(key string, nowMs int64) (simulation.DivisionFrames, bool) {
	walk, ok := rt.directions.get(key)
	if !ok {
		return simulation.DivisionFrames{}, false
	}
	unlock := rt.lockCharacter(walk.divisionID, walk.character.Name)
	defer unlock()

	// A handler may have replaced or ended the walk before the lock.
	if walk, ok = rt.directions.get(key); !ok {
		return simulation.DivisionFrames{}, false
	}
	admission, refusal := rt.admitMove(walk.divisionID, walk.character, walk.cosGID)
	if refusal != nil || !walk.current(admission.world) {
		rt.directions.clear(key)
		return simulation.DivisionFrames{}, false
	}

	arrivesAtMs := nowMs
	if segment := admission.world.MoveSegment; segment.Valid() {
		arrivesAtMs = segment.ArrivesAtMs
	}
	if walk.blocked {
		if nowMs < arrivesAtMs {
			return simulation.DivisionFrames{}, false
		}
		rt.directions.clear(key)
		correction := directionCorrectionFrame(enterworld.ObjectIDForCharacter(walk.character), admission.world.Spawn)
		return simulation.DivisionFrames{
			DivisionID:      walk.divisionID,
			OnlyCharacterID: walk.character.ID,
			Frames:          []simulation.Frame{{Opcode: correction.Opcode, Payload: correction.Payload}},
		}, true
	}
	if nowMs < arrivesAtMs-simulation.DirectionLegLookaheadMs {
		return simulation.DivisionFrames{}, false
	}
	// A refused leg (a death or teleport raced the tick) has already ended
	// the walk; whoever changed the life published the new state.
	if _, _, refusal := rt.commitDirectionLeg(walk, admission, nowMs); refusal != nil {
		log.Debugf("movement: direction walk of %s ended: %s", walk.character.Name, refusal.Reason)
	}
	return simulation.DivisionFrames{}, false
}

/*
================
directionCorrectionFrame

The 0xB2F5 source correction (OnEntityMoveUpdate0xB2F5 0x775B50:
HaltAndSetYaw + ReseedSourcePosition) that stops a mover at spawn.
================
*/
func directionCorrectionFrame(gid uint32, spawn simulation.Spawn) wire.Frame {
	return wire.Frame{
		Opcode: wire.OpObjectSourceCorrection,
		Payload: wire.ObjectSourceCorrection{
			Gid: gid,
			Position: wire.Position{
				RegionID: spawn.RegionID,
				X:        float32(spawn.X),
				Y:        float32(spawn.Y),
				Z:        float32(spawn.Z),
				Heading:  spawn.Angle,
			},
		}.Encode(),
	}
}
