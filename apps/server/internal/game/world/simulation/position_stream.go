/*
===========================================================================

position_stream.go - a moving player's packets to its viewers

Native (the default): a viewer gets one 0xB738 per accepted move of a
player it shows (publishPeerPath), a 0xB2F5 when the server stops the
walk early (sent by the owner that stops it), and nothing on arrival and
nothing per tick. The client walks the peer along the 0xB738 path by
itself. This is how the original server behaves (the v1.188 server, read
for this change and described in words, as #1701 asks):

  - A move request is answered with one movement packet to the viewers
    per accepted move: state checks only, no clock and no throttle.
  - No move-state packet goes with a move. The state packet is sent on a
    run/walk toggle and on motion, life, body, battle and PvP changes,
    never by the move itself.
  - Arrival sends nothing: the walk ends without a broadcast. An
    explicit stop sends the stop packet.
  - A resync position is sent from a timer of 5 when a living character
    stands more than 10 from its last sent position or in another
    region, or is on a direction walk more than 100 from it. The port
    does not run this timer: its unit is not read (seconds by inference
    from its siblings), and the drift it repairs needs a standing player
    moved without a packet, which no port owner is known to do (stops,
    teleports and spawns publish their own). A shard that sees such
    drift turns the stream on.

SRO_POSITION_STREAM=on (port-only, not native) restores the stream the
port inherited from its Node fixture: every tick a live 0x30E3 sample of
each walking player to each viewer, the 0x3122 move state with every
new path, and one 0xB2F5 at the goal when a walk arrives (here and in
the movement lane's ground arrivals). Off is native.

===========================================================================
*/
package simulation

import (
	"os"

	"opensro.online/server/internal/game/item/wire"
)

// EnvPositionStream names the port-only position stream switch.
const EnvPositionStream = "SRO_POSITION_STREAM"

/*
================
PositionStreamFromEnv

The opt-in: anything but an explicit on is off, so a shard that never
sets the variable runs the native fan-out.
================
*/
func PositionStreamFromEnv() bool {
	value := os.Getenv(EnvPositionStream)
	return value == "on" || value == "1"
}

/*
================
Ticker.runPeerMovement

Legs 2 and 3 for one moving player: its new path to every viewer that
has not seen it (native), and with the port-only stream the live sample
and the arrival settle.
================
*/
func (t *Ticker) runPeerMovement(state *divisionTickState, session SessionSnapshot, nowMs int64) {
	segment := session.World.MoveSegment
	if !segment.Valid() {
		delete(state.settled, session.SessionID)
		return
	}
	if !session.World.GroundActive() && nowMs >= segment.ArrivesAtMs {
		// Native arrival sends nothing (StopMove with broadcast 0).
		if t.PositionStream {
			t.pushArrivalSettle(state, session, segment)
		}
		return
	}

	// Destination admission precedes the source sample: retail 30E3 only
	// reseeds the path; it cannot start one for an already visible peer.
	t.publishPeerPath(state, session, nowMs)
	if !t.PositionStream {
		return
	}

	// In flight: the LIVE interpolated position, never the goal (bug D).
	delete(state.settled, session.SessionID)
	liveSpawn := session.World.LiveSpawnAt(nowMs)
	move := wire.ObjectSourceMove{
		Position: wire.Position{
			RegionID: liveSpawn.RegionID,
			X:        float32(liveSpawn.X),
			Y:        float32(liveSpawn.Y),
			Z:        float32(liveSpawn.Z),
			Heading:  liveSpawn.Angle,
		},
		Gid: PlayerObjectID(session.CharacterID),
	}
	t.pushPeerMovement(state, PlayerObjectID(session.CharacterID), []Frame{
		{Opcode: wire.OpObjectSourceMove, Payload: move.Encode(), Current: session.MovementCurrent},
	})
}

/*
================
Ticker.pushArrivalSettle

Port-only, not native: one 0xB2F5 at the goal on the tick a segment
matures, then quiet for that mover.
================
*/
func (t *Ticker) pushArrivalSettle(state *divisionTickState, session SessionSnapshot, segment *MoveSegment) {
	if state.settled[session.SessionID] == segment.ArrivesAtMs {
		return
	}
	state.settled[session.SessionID] = segment.ArrivesAtMs
	goal := session.World.Spawn
	gid := PlayerObjectID(session.CharacterID)
	correction := wire.ObjectSourceCorrection{
		Gid: gid,
		Position: wire.Position{
			RegionID: goal.RegionID,
			X:        float32(goal.X),
			Y:        float32(goal.Y),
			Z:        float32(goal.Z),
			Heading:  goal.Angle,
		},
	}
	t.pushPeerMovement(state, gid, []Frame{
		{Opcode: wire.OpObjectSourceCorrection, Payload: correction.Encode(), Current: session.MovementCurrent},
	})
}
