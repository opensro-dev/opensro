/*
===========================================================================

direction.go - the angular (walk-in-direction) movement plane

The v1.150 client has two ways to move: a ground destination (0x7738 mode
1) and a direction walk (0x7738 mode 0). The direction walk is what a click
on the sky, or on anything the ground pick misses, produces:
CGInterface_MoveToWorldPoint (0x6932A0) falls through to
CNavigationDeadreckon_SendAngleMoveCommand (0x877F30), which ships the
camera ray's yaw with angular flag 1 (GO). The ack then starts
CNavigationController_StartDirectMove (0x86D150): nav state 2, which has
no arrival and walks until the region manager's ground move test blocks it.

The v1.188 server mirrors it with CGObjMoverByCmd: SetCommand (0x48C140)
latches the angle and flags, bit 0 makes it move, ComputeStep (0x48BFF0)
steps speed * dt with no distance cap, SetAngle (0x48C0E0) steers without
dropping GO and Cancel (0x48C110) stops it.

This file owns the wire flags, the opcodes of the steer/stop pair, and the
pure state transition of one direction leg. The movement runtime owns the
walk's lifetime (movement/direction.go).

===========================================================================
*/
package simulation

import "math"

// Angular flag bits of the 0x7738/0xB738 mode-0 body (record +0x0E, the
// server's CGObjMoverByCmd +0x14 flags).
const (
	// AngularFlagGo is bit 0: walk along the heading (SetCommand sets the
	// mover's moving latch only when it is present).
	AngularFlagGo uint8 = 0x01
	// AngularFlagSteerLeft and AngularFlagSteerRight are the arrow-key
	// steering bits MovementController_SetDirectionVector (0x877FB0) keeps
	// client-side; CGObjMoverByCmd_TickTurn (0x48C1E0) turns pi rad/s while
	// one is held. The v1.150 client never puts them on 0x7738 (0x877F30
	// always writes flag 1), so the port reads only AngularFlagGo.
	AngularFlagSteerLeft  uint8 = 0x10
	AngularFlagSteerRight uint8 = 0x20
)

// Steer and stop opcodes of the direction walk.
const (
	// OpClientSteerRequest is 0x72CF [u16 heading]: a new heading for a
	// walking mover (CNavigationDeadreckon_SendSteeringUpdate 0x877540).
	OpClientSteerRequest uint16 = 0x72CF
	// OpObjectSteer is 0xB2CF [u32 gid][u16 heading]
	// (CPSMission_OnEntityUpdateAngle0xB2CF 0x775A90): the client sets the
	// target yaw of another mover and keeps it walking; its own gid is
	// ignored.
	OpObjectSteer uint16 = 0xB2CF
	// OpClientDirectionStopRequest is 0x72F5 [u16 heading]: the Up-arrow
	// release (CNavigationDeadreckon_SendAngleUpdatePacket 0x8777B0). The
	// answer is the 0xB2F5 source correction (wire.OpObjectSourceCorrection).
	OpClientDirectionStopRequest uint16 = 0x72F5
	// ClientHeadingRequestSize is the body size of 0x72CF and 0x72F5.
	ClientHeadingRequestSize = 2
)

// DirectionLegUnits is how far ahead one direction leg is planned.
//
// INFERENCE: the native server has no legs - ComputeStep advances the mover
// every tick until the move test blocks it. The port's live plane needs a
// goal for its segment, so the walk is planned as consecutive legs. 1000
// units is the ground pick's own ray cap (SWorld_PickGroundTerrainAndNavigation
// 0x88D340): a click that misses the ground within it is what starts a
// direction walk, so a leg covers the distance a destination click could not.
const DirectionLegUnits = 1000.0

// DirectionLegLookaheadMs is how long before a leg matures the next one is
// planned. INFERENCE: 500 ms is the client's own direction cadence
// (CIObject_ScheduleStateTimer(this, 7, 500) behind
// CNavigationDeadreckon_UpdateMovementHeading 0x8779B0). Planning inside
// the leg keeps the tick from publishing a settle between legs.
const DirectionLegLookaheadMs int64 = 500

/*
================
MovementRequest.IsDirectionWalk

Mode 0 with GO: walk along HeadingWord until blocked. Mode 0 without GO is
the stationary SetCommand arm, a turn in place (applyTurn).
================
*/
func (m MovementRequest) IsDirectionWalk() bool {
	return m.Mode == MovementAckAngularMode && m.AngularMode&AngularFlagGo != 0
}

/*
================
DirectionLegGoal

The point DirectionLegUnits ahead of from along the wire heading. The wire
bearing is atan2(dz, dx) (headingWordFromDelta), so heading h walks
(cos t, sin t) with t = h / 65535 * 2pi. Height stays at the departure; the
movement runtime resolves the surface the walk reaches. The goal faces the
heading exactly and is folded into its canonical outdoor frame.
================
*/
func DirectionLegGoal(from Spawn, heading uint16, units float64) Spawn {
	theta := float64(heading) / HeadingWordScale * twoPi
	return NormalizeSpawnFrame(Spawn{
		RegionID: from.RegionID,
		X:        from.X + math.Cos(theta)*units,
		Y:        from.Y,
		Z:        from.Z + math.Sin(theta)*units,
		Angle:    heading,
	})
}

/*
================
ApplyDirectionLeg

Commits one direction leg whose geometry the movement runtime already
constrained: goal is where this leg ends (the full leg, or the first
blocking contact).

 1. the departure is the LIVE plane (a steer departs from where the walker
    is, not from the previous leg's goal);
 2. the goal plane takes the leg end facing the requested heading, and the
    live plane gets its segment (nil when blocked where it stands);
 3. the ack echoes the request's mode-0 body and ALWAYS carries the
    departure as the source block.

INFERENCE for 3: the client starts the direction walk only inside the
source-block arm of CPSMission_OnSkillNavResponse0xB738 (0x776200 ->
ReseedSourcePosition -> CNavigationController_StartDirectMove), so a
mode-0 ack without it would leave the mover standing.
================
*/
func ApplyDirectionLeg(world *WorldState, objectID uint32, movement MovementRequest, goal Spawn, nowMs int64) MoveResult {
	liveBefore := world.LiveSpawnAt(nowMs)
	source := MovementSourceFromSpawn(liveBefore)
	goal.Angle = movement.HeadingWord

	world.Spawn = goal
	world.MoveSegment = world.TravelSegment(liveBefore, goal, world.MovementMode, nowMs)
	if world.MoveSegment != nil {
		world.MoveSegment.GroundAngular = true
	}
	world.SpawnSet = true
	world.MovementSourceSeeded = true

	return MoveResult{
		AckPayload:     BuildMovementAckPayload(objectID, movement, &source),
		LiveBefore:     liveBefore,
		NextSpawn:      goal,
		Segment:        world.MoveSegment,
		SourceIncluded: true,
		MovementMode:   world.MovementMode,
	}
}

/*
================
ApplyDirectionStop

The Cancel arm (CGObjMover Cancel 0x48C110): the walker settles at its live
point facing the heading the client stopped with. The caller publishes the
0xB2F5 correction built from the returned spawn. Facing is not part of a
nav position (samePosition), so the settled owner stays valid.
================
*/
func ApplyDirectionStop(world *WorldState, heading uint16, nowMs int64) Spawn {
	world.SettleLive(nowMs)
	world.Spawn.Angle = heading
	return world.Spawn
}

/*
================
ApplyStandingTurn

A heading change for a mover that is not walking: the facing changes where
it stands. Returns false, and changes nothing, while a segment is in flight.

INFERENCE: SetAngle (0x48C0E0) belongs to the command mover. A destination
walk owns its own facing, so a steer that races the end of a direction walk
must not bend it.
================
*/
func ApplyStandingTurn(world *WorldState, heading uint16, nowMs int64) bool {
	if world.MovingAt(nowMs) {
		return false
	}
	world.SettleLive(nowMs)
	world.Spawn.Angle = heading
	return true
}
