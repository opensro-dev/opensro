/*
===========================================================================

register.go - authenticated quest requests and private result publication

The transport binds the character before entering a quest operation. This
owner publishes its receipt and only the explicitly public reward effects.
Client-supplied quest identities never select another character.

===========================================================================
*/
package quest

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

/*
================
Register

One owner registers each opcode. Quest operations share the same character
authority used by inventory and combat; gathering cancellation is transient.
================
*/
func Register(hub *transport.Hub, rt *Runtime) {
	hub.Handle(OpQuestGiveUpRequest, questHubHandler(hub, rt, "0x71EB give-up", rt.HandleGiveUp))
	hub.Handle(OpQuestRewardRequest, questHubHandler(hub, rt, "0x729A reward-select", rt.HandleRewardSelect))
	hub.Handle(OpQuestGatherCancel, questHubHandler(hub, rt, "0x775D gathering cancel", rt.HandleGatherCancel))
}

/*
================
questHubHandler

Bind the authority character, answer its private result and fan out only
the public projection. Refusals settle the matching native transaction.
================
*/
func questHubHandler(hub *transport.Hub, rt *Runtime, label string, op func(*enterworld.Character, []byte) (OpResult, error)) transport.HandlerFunc {
	return func(s *transport.Session, opcode uint16, payload []byte) {
		character, divisionID, bound := enterworld.SessionCharacter(rt.deps, s)
		if !bound {
			log.Debugf("quest: 0x%04X from unbound session %d discarded", opcode, s.ID)
			return
		}
		result, err := op(character, payload)
		if err != nil {
			log.Warnf("quest: %s refused for %s: %v", label, character.Name, err)
			// Native 75c370 / 75c3d0 consume [result=2, error:u8]. Zero is
			// our unspecified refusal policy, NOT a recovered retail reason
			// mapping. B1EB error 4 has a specific not-allowed message; do not
			// assign it to unrelated inventory/persistence/validation failures.
			ack := uint16(0xB29A)
			if opcode == OpQuestGiveUpRequest {
				ack = 0xB1EB
			}
			if opcode == OpQuestGatherCancel {
				ack = OpQuestGatherCancelReply
			}
			sendFrames(s, []wire.Frame{{Opcode: ack, Payload: []byte{2, 0}}})
			return
		}
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

Reliable ordering ends at the first failed write; a later receipt must not
overtake a missing inventory or quest delta.
================
*/
func sendFrames(s *transport.Session, frames []wire.Frame) {
	for _, frame := range frames {
		if err := s.Send(frame.Opcode, frame.Payload); err != nil {
			log.Debugf("quest: send 0x%04X to session %d failed: %v", frame.Opcode, s.ID, err)
			return
		}
	}
}
