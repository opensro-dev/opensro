/*
===========================================================================

register.go - authenticated progression transport dispatch

All handlers resolve the character bound to the session. The runtime owns
validation and mutation; this adapter delivers its committed private and
public frames without trusting a client-supplied character identity.

===========================================================================
*/
package progression

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

/*
================
Register

The dependencies must resolve the same character records as enter-world.
Independent record copies would let the two owners persist divergent state.
================
*/
func Register(hub *transport.Hub, deps Dependencies) *Runtime {
	rt := NewRuntime(deps)
	rt.Register(hub)
	return rt
}

/*
================
Register

Install composition hooks before exposing the runtime to authenticated
traffic. Withdrawal refuses if its inventory/effect owners are absent.
================
*/
func (rt *Runtime) Register(hub *transport.Hub) {
	hub.Handle(wire.OpAllocStrRequest, rt.hubHandler(hub, rt.HandleAllocStr))
	hub.Handle(wire.OpAllocIntRequest, rt.hubHandler(hub, rt.HandleAllocInt))
	hub.Handle(wire.OpMasteryLevelUpRequest, rt.hubHandler(hub, rt.HandleMasteryLevelUp))
	hub.Handle(wire.OpSkillLearnRequest, rt.hubHandler(hub, rt.HandleSkillLearn))
	hub.Handle(wire.OpSkillWithdrawalRequest, rt.hubHandler(hub, rt.HandleSkillWithdrawal))
	hub.Handle(wire.OpMasteryWithdrawalRequest, rt.hubHandler(hub, rt.HandleMasteryWithdrawal))
	// The dev exp-grant trigger (0xDE01, NOT a retail opcode - see the
	// OpDevGrantExp doc) only exists on the hub when explicitly enabled;
	// when disabled the opcode is simply unregistered and the hub drops
	// the frame like any other unknown opcode.
	if DevExpGrantEnabled() {
		hub.Handle(OpDevGrantExp, rt.hubHandler(hub, rt.HandleDevGrantExp))
		log.Warnf("progression: GM-only DEV exp-grant trigger ENABLED (opcode 0x%04X, %s=1); leave disabled outside diagnostics", OpDevGrantExp, EnvDevExpGrant)
	}
}

/*
================
opFunc

An operation returns committed frames without depending on a socket.
================
*/
type opFunc func(divisionID string, character *enterworld.Character, payload []byte) OpResult

/*
================
hubHandler

Unbound sessions have no progression conversation. Bound sessions receive
private state first; public presentation excludes the sender and every
other division. Only operations that return Broadcast participate in it.
================
*/
func (rt *Runtime) hubHandler(hub *transport.Hub, op opFunc) transport.HandlerFunc {
	return func(s *transport.Session, opcode uint16, payload []byte) {
		character, divisionID, bound := enterworld.SessionCharacter(rt.deps, s)
		if !bound {
			log.Debugf("progression: 0x%04X from unbound session %d discarded", opcode, s.ID)
			return
		}
		result := op(divisionID, character, payload)
		sendFrames(s, result.Frames)
		if len(result.Broadcast) == 0 {
			return
		}
		accept := func(peer *transport.Session) bool {
			if peer.ID == s.ID {
				return false
			}
			peerDivision, ok := peer.DivisionID()
			return ok && peerDivision == divisionID
		}
		for _, frame := range result.Broadcast {
			hub.BroadcastFunc(accept, frame.Opcode, frame.Payload)
		}
	}
}

/*
================
sendFrames

Stop at the first delivery failure so a later receipt cannot overtake the
state update it acknowledges on this session.
================
*/
func sendFrames(s *transport.Session, frames []wire.Frame) {
	for _, frame := range frames {
		if err := s.Send(frame.Opcode, frame.Payload); err != nil {
			log.Debugf("progression: send 0x%04X to session %d failed: %v", frame.Opcode, s.ID, err)
			return
		}
	}
}
