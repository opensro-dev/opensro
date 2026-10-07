/*
===========================================================================

register.go - bind native chat requests to the shard's delivery owners

Routing produces acknowledgements and public or private delivery. The beta
runtime orders public history with live messages; presence owns private peers.

===========================================================================
*/
package chat

import (
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/restriction"
	"opensro.online/server/internal/transport"
)

/*
================
DeliveryPresence

Private delivery resolves live recipients through the shared presence owner.
================
*/
type DeliveryPresence interface {
	PresenceView
	SessionByName(divisionID, name string) (*transport.Session, bool)
}

/*
================
Register
================
*/
func Register(hub *transport.Hub, deps Dependencies, presence DeliveryPresence, parties PartyView) *Runtime {
	rt := &Runtime{history: make(map[string][]publicLine), members: make(map[uint64]publicMember), now: time.Now}
	hub.Handle(OpChatRequest, rt.chatHubHandler(hub, deps, presence, parties))
	hub.OnSessionClose(rt.sessionClosed)
	return rt
}

/*
================
chatHubHandler

Resolve the bound character through session identity, then route the request.
Beta public delivery precedes the native receipt so its channel owns sender
presentation. Private recipients resolve through presence at delivery time.
Decode refusals without an acknowledgement remain silent.
================
*/
func (rt *Runtime) chatHubHandler(hub *transport.Hub, deps Dependencies, presence DeliveryPresence, parties PartyView) transport.HandlerFunc {
	return func(s *transport.Session, opcode uint16, payload []byte) {
		character, divisionID, bound := enterworld.SessionCharacter(deps, s)
		if !bound {
			log.Debugf("chat: 0x%04X from unbound session %d discarded", opcode, s.ID)
			return
		}
		// Native admission precedes decoding and all publication/mutation.
		if restriction.Report(s, transport.CommandRestrictionChat) {
			return
		}
		outcome := HandleChat(deps, Views{Presence: presence, Parties: parties, Unions: rt.Unions, Stalls: rt.Stalls}, divisionID, character, payload)
		if outcome.Refusal == "" && outcome.Broadcast != nil && ClosedBetaGlobalChat {
			// The author's echo precedes the keyed receipt on the same reliable
			// stream, so presentation uses the server's channel exactly once.
			rt.publish(divisionID, outcome.Broadcast)
		}
		if outcome.Ack != nil {
			_ = s.Send(OpChatAck, outcome.Ack)
		}
		if outcome.Refusal != "" {
			log.Debugf("chat: 0x7367 refused for %s: %s", character.Name, outcome.Refusal)
			return
		}
		if outcome.Broadcast != nil && !ClosedBetaGlobalChat {
			hub.BroadcastObserved(divisionID, s.ID, enterworld.ObjectIDForCharacter(character), []transport.Frame{{Opcode: OpChatBroadcast, Payload: outcome.Broadcast}})
		}
		if presence == nil {
			return
		}
		for _, delivery := range outcome.Deliveries {
			if peer, online := presence.SessionByName(divisionID, delivery.TargetName); online {
				_ = peer.Send(OpChatBroadcast, delivery.Payload)
			}
		}
	}
}
