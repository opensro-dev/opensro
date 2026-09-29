/*
===========================================================================

steer.go - steering and stopping a direction walk

The walking client owns its heading. Two commands reach the server while it
walks (both carry [u16 heading] in the 0x7738 heading unit):

  - 0x72CF steer, from CNavigationDeadreckon_SendSteeringUpdate (0x877540):
    the drift correction of UpdateMovementHeading (0x8779B0) every 500 ms,
    and the Left/Right arrow keys. The server's SetAngle (0x48C0E0) turns
    the command mover without dropping GO; observers get 0xB2CF.
  - 0x72F5 stop, from CNavigationDeadreckon_SendAngleUpdatePacket
    (0x8777B0) on the Up-arrow release. The server's Cancel (0x48C110)
    stops the mover; everyone, the mover included, gets the 0xB2F5
    correction at the point where it stopped.

A mounted walker sends the same pair as 0x769E tags 0x04 and 0x03 for its
vehicle (the action lane decodes them and calls HandleCOSSteer and
HandleCOSStop).

===========================================================================
*/
package movement

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

/*
==================
HeadingOutcome

One handled steer or stop: frames for the acting session, frames for the
observers of the mover, and the refusal reason for logging and tests.
==================
*/
type HeadingOutcome struct {
	Frames    []wire.Frame
	Broadcast []wire.Frame
	Refusal   string
}

/*
==================
registerDirectionCommands

Wires 0x72CF and 0x72F5 onto the hub. Refusals are silent on the wire,
like every movement refusal.
==================
*/
func (rt *Runtime) registerDirectionCommands(hub *transport.Hub) {
	handlers := map[uint16]func(string, *enterworld.Character, []byte) HeadingOutcome{
		simulation.OpClientSteerRequest:         rt.HandleSteer,
		simulation.OpClientDirectionStopRequest: rt.HandleDirectionStop,
	}
	for opcode, handle := range handlers {
		hub.Handle(opcode, func(s *transport.Session, opcode uint16, payload []byte) {
			character, divisionID, bound := enterworld.SessionCharacter(rt.deps, s)
			if !bound {
				log.Debugf("movement: 0x%04X before enter-world bind ignored", opcode)
				return
			}
			outcome := handle(divisionID, character, payload)
			if outcome.Refusal != "" {
				log.Debugf("movement: 0x%04X refused for %s: %s", opcode, character.Name, outcome.Refusal)
				return
			}
			for _, frame := range outcome.Frames {
				if err := s.Send(frame.Opcode, frame.Payload); err != nil {
					return
				}
			}
			if len(outcome.Broadcast) != 0 {
				broadcastObservedMotion(hub, divisionID, s.ID, enterworld.ObjectIDForCharacter(character), outcome.Broadcast)
			}
		})
	}
}

/*
==================
HandleSteer

The 0x72CF handler.
==================
*/
func (rt *Runtime) HandleSteer(divisionID string, character *enterworld.Character, payload []byte) HeadingOutcome {
	heading, refusal := simulation.DecodeClientHeadingRequest(payload)
	if refusal != nil {
		return HeadingOutcome{Refusal: refusal.Reason}
	}
	return rt.steer(divisionID, character, 0, heading)
}

/*
==================
HandleDirectionStop

The 0x72F5 handler.
==================
*/
func (rt *Runtime) HandleDirectionStop(divisionID string, character *enterworld.Character, payload []byte) HeadingOutcome {
	heading, refusal := simulation.DecodeClientHeadingRequest(payload)
	if refusal != nil {
		return HeadingOutcome{Refusal: refusal.Reason}
	}
	return rt.stop(divisionID, character, 0, heading)
}

/*
==================
HandleCOSSteer

The 0x769E tag-0x04 twin of HandleSteer for the mount gid the action lane
already bound to the character's active COS. Returns the acting session's
frames and the observers' frames (action.Runtime.SteerCOS).
==================
*/
func (rt *Runtime) HandleCOSSteer(divisionID string, character *enterworld.Character, gid uint32, heading uint16) ([]wire.Frame, []wire.Frame) {
	if gid == 0 {
		return nil, nil
	}
	outcome := rt.steer(divisionID, character, gid, heading)
	return outcome.Frames, outcome.Broadcast
}

/*
==================
HandleCOSStop

The 0x769E tag-0x03 twin of HandleDirectionStop (action.Runtime.StopCOS).
==================
*/
func (rt *Runtime) HandleCOSStop(divisionID string, character *enterworld.Character, gid uint32, heading uint16) ([]wire.Frame, []wire.Frame) {
	if gid == 0 {
		return nil, nil
	}
	outcome := rt.stop(divisionID, character, gid, heading)
	return outcome.Frames, outcome.Broadcast
}

/*
==================
steer

A walking mover takes a new leg from its live point along the new heading.
A standing mover turns where it stands; observers see either as 0xB2CF,
which sets the yaw and leaves the entity's movement as it is. The mover's
own client already turned, and ignores 0xB2CF for its own gid.

INFERENCE: a mover on a destination walk has no command mover to steer; the
steer is dropped rather than bending a walk the client did not start.
==================
*/
func (rt *Runtime) steer(divisionID string, character *enterworld.Character, cosGID uint32, heading uint16) HeadingOutcome {
	if character == nil {
		return HeadingOutcome{Refusal: "characterNotFound"}
	}
	unlock := rt.lockCharacter(divisionID, character.Name)
	defer unlock()

	admission, refusal := rt.admitMove(divisionID, character, cosGID)
	if refusal != nil {
		return HeadingOutcome{Refusal: refusal.Reason}
	}
	nowMs := rt.Now().UnixMilli()

	walk, walking := rt.directions.get(admission.worldKey)
	if walking && walk.cosGID == cosGID && walk.current(admission.world) {
		walk.request.HeadingWord = heading
		if _, _, refusal := rt.commitDirectionLeg(walk, admission, nowMs); refusal != nil {
			return HeadingOutcome{Refusal: refusal.Reason}
		}
		return HeadingOutcome{Broadcast: []wire.Frame{objectSteerFrame(character, heading)}}
	}

	turned := false
	if _, refusal := rt.commitMove(character, admission, func(world *simulation.WorldState) {
		turned = simulation.ApplyStandingTurn(world, heading, nowMs)
	}); refusal != nil {
		return HeadingOutcome{Refusal: refusal.Reason}
	}
	if !turned {
		return HeadingOutcome{Refusal: "destinationWalk"}
	}
	return HeadingOutcome{Broadcast: []wire.Frame{objectSteerFrame(character, heading)}}
}

/*
==================
stop

The walker settles at its live point facing the heading it stopped with,
and the 0xB2F5 correction goes to the mover and its observers: the tick
publishes nothing for a mover without a segment, so without it observers
would walk on to the abandoned leg's goal.

INFERENCE: Cancel acts on the command mover only; a stop for a mover that
is not walking a direction (it already ended blocked, or never started) is
dropped.
==================
*/
func (rt *Runtime) stop(divisionID string, character *enterworld.Character, cosGID uint32, heading uint16) HeadingOutcome {
	if character == nil {
		return HeadingOutcome{Refusal: "characterNotFound"}
	}
	unlock := rt.lockCharacter(divisionID, character.Name)
	defer unlock()

	admission, refusal := rt.admitMove(divisionID, character, cosGID)
	if refusal != nil {
		return HeadingOutcome{Refusal: refusal.Reason}
	}
	walk, walking := rt.directions.get(admission.worldKey)
	if !walking || walk.cosGID != cosGID || !walk.current(admission.world) {
		return HeadingOutcome{Refusal: "notWalking"}
	}
	nowMs := rt.Now().UnixMilli()

	var rest simulation.Spawn
	if _, refusal := rt.commitMove(character, admission, func(world *simulation.WorldState) {
		rest = simulation.ApplyDirectionStop(world, heading, nowMs)
	}); refusal != nil {
		rt.directions.clear(admission.worldKey)
		return HeadingOutcome{Refusal: refusal.Reason}
	}
	rt.directions.clear(admission.worldKey)

	correction := directionCorrectionFrame(enterworld.ObjectIDForCharacter(character), rest)
	return HeadingOutcome{Frames: []wire.Frame{correction}, Broadcast: []wire.Frame{correction}}
}

/*
==================
objectSteerFrame

The 0xB2CF [u32 gid][u16 heading] observers receive for a steer.
==================
*/
func objectSteerFrame(character *enterworld.Character, heading uint16) wire.Frame {
	return wire.Frame{
		Opcode:  simulation.OpObjectSteer,
		Payload: wire.NewWriter(6).U32(enterworld.ObjectIDForCharacter(character)).U16(heading).Payload(),
	}
}
