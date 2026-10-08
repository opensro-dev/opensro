/*
===========================================================================

motionstate.go - movement command admission and state transitions

===========================================================================
*/
// The 0x7017 motion-state lane: the Action toolbar's run/walk and
// sit/stand requests.
//
// WIRE CONTRACT (the client folds are the surviving contract):
//
//   - C->S 0x7017 (sub_6ff740 NetSend_MotionState7017): exactly ONE code
//     byte. 2 = request walk / 3 = request run (sub_862850 sends the
//     opposite of the +0x255 mode byte, which sub_858450 writes as
//     0=walk/1=run) and 4 = sit/stand toggle (sub_862870 always sends 4;
//     sit-vs-stand is server-authoritative).
//   - S->C 0xB017 (sub_775db0): [u8 result], result 1 = silent success.
//   - S->C 0x3122 stateType 1 (sub_777b60 -> sub_858450 SetRunWalkMode +
//     sub_58bbc0 SetupActionButton + sub_573b70 HUD icons):
//     [u32 gid][u8 1][u8 value] with value 0 stand / 2 walk / 3 run /
//     4 sit - one shared motion enum in both directions.
//
// Malformed or unknown input is discarded SILENTLY (log only): the retail
// client can only compose codes 2/3/4, so any other shape is impossible
// from a real client, the same refusal posture the move lane (0x7738) and
// item lane keep for shapes the native client never sends.
package movement

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

/*
================
MotionOutcome

MotionOutcome is one handled 0x7017: the frames for the acting session
(the 0xB017 ack + the 0x3122 state push), the same 0x3122 fanned to
division peers, and the refusal reason when the frame was discarded
(empty on success; a refusal ships NO packets, the silent-wire posture).
================
*/
type MotionOutcome struct {
	Frames    []wire.Frame
	Broadcast []wire.Frame
	Refusal   string
}

/*
================
refusedMotion
================
*/
func refusedMotion(reason string) MotionOutcome {
	return MotionOutcome{Refusal: reason}
}

// PostureTransitionMs is the posture lockout; simulation owns it with the
// posture state it times.
const PostureTransitionMs = simulation.PostureTransitionMs

/*
================
HandleMotionState

HandleMotionState applies one 0x7017 motion-state request to the
authoritative world plane and answers with the client-contract frames.

Codes 2/3 set WorldState.MovementMode (persisted through the same
write-back the move lane owns) and, when a move is in flight and the
gait actually changed, re-time the live segment from the interpolated
point at the new wire speed - the server mirror of sub_858450 restarting
an active path-follow state on the new speed channel. Code 4 toggles the
runtime-only sitting posture; the answered 0x3122 value is 4 (sit) or
0 (stand), the two SetRunWalkMode motion-state arms.

Transport-free so tests drive it without a Hub; registerMotionState is
the only glue.
================
*/
func (rt *Runtime) HandleMotionState(divisionID string, character *enterworld.Character, payload []byte) MotionOutcome {
	if character == nil {
		return refusedMotion("characterNotFound")
	}
	if rt.AdvanceResidentRegion != nil {
		rt.AdvanceResidentRegion(divisionID, character.Name, rt.Now().UnixMilli())
	}

	unlock := rt.lockCharacter(divisionID, character.Name)
	defer unlock()

	characterSnapshot := rt.characterSnapshot(divisionID, character)
	if characterSnapshot == nil || characterSnapshot.DeletePending {
		return refusedMotion("deletePending")
	}

	// STRICT decode: the native sender appends exactly one byte
	// (AppendBytes(&code, 1) @0x6ff781); anything else never left a real
	// client.
	if len(payload) != 1 {
		return refusedMotion(fmt.Sprintf("payloadLength %d, want 1", len(payload)))
	}
	code := payload[0]
	if code != simulation.WalkMode && code != simulation.RunMode && code != simulation.MotionToggleSitStand {
		return refusedMotion(fmt.Sprintf("unknownCode 0x%02X", code))
	}

	// CGObjChar_RequestMotionChange (4B14B2): a masked player (msch 1)
	// cannot sit, stand or change gait.
	if characterSnapshot.TransformMode == 1 {
		return refusedMotion("transformed")
	}

	worldKey := simulation.WorldKey(divisionID, character.Name)
	nowMs := rt.Now().UnixMilli()

	// POSTURE-TRANSITION LOCKOUT. While a sit/stand transition is in flight
	// the request is DROPPED - no ack, no state push, nothing on the wire.
	// This is the whole handler, not just the sit arm: the later retail
	// build wraps its entire body in "current motion state is not the
	// in-progress one" (v1.188 LEAD, seq: MotionStateChangeReq folder), so
	// a gait request lands in the same lockout as a second posture request.
	// Reading before the Mutate keeps a dropped request off the persistence
	// lane entirely; the character operation lock makes the peek-then-update
	// pair atomic with the move lane.
	if snapshot := rt.Worlds.Snapshot(worldKey, func() simulation.WorldState {
		return simulation.SeedWorldState(characterSnapshot)
	}); nowMs < snapshot.PostureTransitionUntilMs {
		return refusedMotion("postureTransitionInFlight")
	}

	// The mode write and the record's movement-mode write-back commit as
	// one unit, like the move lane (the sit toggle re-writes the same
	// persisted values, which is idempotent - posture itself is
	// runtime-only by the WorldState.Sitting contract). A sit-down that
	// truncates an in-flight move (see applyMotionCode) also persists the
	// live stop point through the same write-back, so it survives re-enter.
	var value uint8
	var truncated bool
	var rest simulation.Spawn
	if !rt.deps.Update(character, "motionState", func() bool {
		if character.DeletePending {
			return false
		}
		state := rt.Worlds.Update(worldKey,
			func() simulation.WorldState { return simulation.SeedWorldState(character) },
			func(world *simulation.WorldState) {
				value, truncated = applyMotionCode(world, code, nowMs)
			})
		writeBackWorld(character, state)
		rest = state.Spawn
		return true
	}) {
		return refusedMotion("deletePending")
	}

	// (Ruling 44 must-have F) a sit that truncates an in-flight move releases
	// the pickup-approach latch, exactly as a fresh ground move does
	// (runtime.go wires ClearPendingPickup on 0x7738 but NOT on 0x7017);
	// otherwise a mid-approach sit leaves the action pickup ETA armed while
	// the character has stopped short of the item.
	if truncated && rt.ClearPendingPickup != nil {
		rt.ClearPendingPickup(divisionID, character.Name)
	}

	gid := enterworld.ObjectIDForCharacter(character)
	statePush := wire.Frame{
		Opcode: wire.OpObjectStateRefresh,
		Payload: wire.ObjectStateRefresh{
			Gid:       gid,
			StateType: wire.StateChannelMove,
			Value:     value,
		}.Encode(),
	}
	frames := []wire.Frame{
		{Opcode: simulation.OpMotionStateAck, Payload: []byte{simulation.MotionStateAckSuccess}},
		statePush,
	}
	broadcast := []wire.Frame{statePush}

	// (Ruling 44 must-haves B / B') the truncation correction rides the
	// MOVEMENT channel (0xB2F5 OpObjectSourceCorrection), NOT the sit ack
	// (0xB017): the v1.150 client's sit-ack handler never touches the mover
	// (sub_775db0, binary-certified), so a stop sent there is silently
	// swallowed. And the tick's own 0xB2F5 settle EXCLUDES the acting session
	// (tick.go), so the correction must go to SELF as well as peers or the
	// sitting player keeps sliding on its own uncleared move goal (a
	// self-vs-world divergence of latency*speed) while peers stop.
	if truncated {
		correction := wire.Frame{
			Opcode: wire.OpObjectSourceCorrection,
			Payload: wire.ObjectSourceCorrection{
				Gid: gid,
				Position: wire.Position{
					RegionID: rest.RegionID,
					X:        float32(rest.X),
					Y:        float32(rest.Y),
					Z:        float32(rest.Z),
					Heading:  rest.Angle,
				},
			}.Encode(),
		}
		frames = append(frames, correction)
		broadcast = append(broadcast, correction)
	}

	return MotionOutcome{Frames: frames, Broadcast: broadcast}
}

/*
================
applyMotionCode

applyMotionCode mutates the world plane for one validated 0x7017 code and
returns the 0x3122 stateType-1 value that describes the resulting state and
whether it truncated an in-flight move (the caller emits the position
correction and clears the pickup latch for a truncation).

SIT CANCELS MOVEMENT (Ruling 44/48): when the player sits DOWN while
travelling, the walk stops at the LIVE interpolated point. It is
unconditional - there is no flag; a sit either cancels the walk or it does
not, no partial-coverage failure mode, so the human's answer was "just fix
it" (Ruling 48, flag removed as cargo-cult inherited from the clip shape).
The v1.150 client CANNOT do this itself - the posture state (char+0x644)
and the mover (char+0x650) are disjoint sub-objects, and the ONLY writer of
the move-goal flags +0x84/+0x80 is the integrator's own arrival/blocked
path (binary+dump two-source, clipreplicate-wave seq431/483/514) - so a
server that keeps interpolating leaves a seated character sliding to the
goal, which is the reported defect. The later retail build performed this
stop server-side (v1.188 LEAD, survived independent re-derive, seq686/698);
we CANNOT prove v1.150's retail server sent such a stop - only that our
client can RECEIVE a server position correction (0xB2F5 -> mover goal clear,
binary-certified) and cannot self-cancel. So this is server position
authority replicating the client's own stop, honest-limit included.
================
*/
func applyMotionCode(world *simulation.WorldState, code uint8, nowMs int64) (value uint8, truncated bool) {
	if code == simulation.MotionToggleSitStand {
		world.Sitting = !world.Sitting
		// Arm the transition-in-progress window. The final posture is published
		// IMMEDIATELY (below) and the window is invisible to the client:
		// the later retail build's notifier writes the REQUESTED posture
		// byte, never the in-progress state it just installed, which is
		// why neither build's client has a wire value for one. So this
		// window is an action lockout, not a pose - it must not gate
		// movement, because that build's movement gate reads only life
		// state and never consults posture at all.
		world.PostureTransitionUntilMs = nowMs + PostureTransitionMs
		// (must-have A) sitting DOWN mid-travel truncates the move at the live
		// point: Spawn := LiveSpawnAt AND MoveSegment := nil, PAIRED. Unpaired
		// is a silent teleport - LiveSpawnAt returns Spawn once the segment is
		// nil, so a stale goal Spawn would snap the character to the
		// destination the instant the segment clears (the gait-retime path
		// keeps the old goal Spawn on purpose; this MUST NOT).
		if world.Sitting && world.MovingAt(nowMs) {
			world.SettleLive(nowMs)
			truncated = true
		}
		if world.Sitting {
			return wire.MoveStateSit, truncated
		}
		return wire.MoveStateStand, truncated
	}

	changed := world.MovementMode != code
	world.MovementMode = code
	// Re-time an in-flight segment at the new wire speed, departing from
	// the live interpolated point (never the goal - the bug-D plane).
	// MoveSegmentForTravel returning nil (zero-length remainder) clears the
	// segment, the same pairing rule every Spawn write keeps.
	if changed && world.MovingAt(nowMs) {
		live := world.LiveSpawnAt(nowMs)
		angular := world.MoveSegment.GroundAngular
		world.MoveSegment = world.TravelSegment(live, world.Spawn, code, nowMs)
		if world.MoveSegment != nil {
			world.MoveSegment.GroundAngular = angular
			world.MoveSegment.GroundContinuation = true
		}
	}
	return code, false
}

/*
================
registerMotionState

registerMotionState wires the 0x7017 handler onto the hub: resolve the
bound character (never client-supplied identity), run the op, answer the
acting session, fan the 0x3122 to division peers - the origin excluded,
it already holds its own copy (the action broadcast convention).
================
*/
func (rt *Runtime) registerMotionState(hub *transport.Hub) {
	hub.Handle(simulation.OpMotionStateRequest, func(s *transport.Session, _ uint16, payload []byte) {
		character, divisionID, bound := enterworld.SessionCharacter(rt.deps, s)
		if !bound {
			log.Debug("movement: 0x7017 before enter-world bind ignored")
			return
		}
		outcome := rt.HandleMotionState(divisionID, character, payload)
		if outcome.Refusal != "" {
			log.Debugf("movement: 0x7017 refused for %s: %s", character.Name, outcome.Refusal)
			return
		}
		for _, frame := range outcome.Frames {
			if err := s.Send(frame.Opcode, frame.Payload); err != nil {
				return
			}
		}
		if len(outcome.Broadcast) == 0 {
			return
		}
		broadcastObservedMotion(hub, divisionID, s.ID, enterworld.ObjectIDForCharacter(character), outcome.Broadcast)
	})
}
