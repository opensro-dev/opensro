/*
===========================================================================

register.go - bind native chat requests to the shard's delivery owners

Routing produces acknowledgements and public or private delivery. The beta
runtime orders public history with live messages; presence owns private peers.

===========================================================================
*/
package chat

import (
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/restriction"
	"opensro.online/server/internal/game/social/chatlog"
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
		// Port-only, not native: a shadow-muted author's lines reach only
		// the author, who sees the same echo and acknowledgement as ever.
		muted := rt.ShadowMuted != nil && rt.ShadowMuted(character.AccountID)
		rt.recordModerationLine(outcome, muted, character, payload)
		if outcome.Refusal == "" && outcome.Broadcast != nil && ClosedBetaGlobalChat {
			// The author's echo precedes the keyed receipt on the same reliable
			// stream, so presentation uses the server's channel exactly once.
			if muted {
				_ = s.Send(OpChatBroadcast, outcome.Broadcast)
			} else {
				rt.publish(divisionID, character.Name, outcome.Broadcast)
			}
		}
		if outcome.Ack != nil {
			_ = s.Send(OpChatAck, outcome.Ack)
		}
		if outcome.Refusal != "" {
			log.Debugf("chat: 0x7367 refused for %s: %s", character.Name, outcome.Refusal)
			return
		}
		if outcome.Broadcast != nil && !ClosedBetaGlobalChat && !muted {
			hub.BroadcastObserved(divisionID, s.ID, enterworld.ObjectIDForCharacter(character), []transport.Frame{{Opcode: OpChatBroadcast, Payload: outcome.Broadcast}})
		}
		if presence == nil {
			return
		}
		for _, delivery := range outcome.Deliveries {
			if muted && !strings.EqualFold(delivery.TargetName, character.Name) {
				continue
			}
			if peer, online := presence.SessionByName(divisionID, delivery.TargetName); online {
				_ = peer.Send(OpChatBroadcast, delivery.Payload)
			}
		}
	}
}

/*
================
recordModerationLine

Port-only, not native: keep one accepted line for the staff (chatlog).
A line counts as said when routing accepted it and it reached at least
the author's own echo - a public broadcast or one targeted delivery -
so refused lines (offline whisper target, wrong party, ...) are not
logged, and a muted author's lines are logged, marked. Record only
queues the line; the writer owns the disk (chatlog), so this never
stalls the handler. The channel and text come from re-decoding the
request, which HandleChat has already accepted at this point.
================
*/
func (rt *Runtime) recordModerationLine(outcome Outcome, muted bool, character *enterworld.Character, payload []byte) {
	if rt.ModerationLog == nil || outcome.Refusal != "" || (outcome.Broadcast == nil && len(outcome.Deliveries) == 0) {
		return
	}
	request, err := DecodeChatRequest(payload)
	if err != nil {
		return
	}
	rt.ModerationLog.Record(chatlog.Entry{
		At:        rt.now().UTC(),
		AccountID: character.AccountID,
		Character: character.Name,
		Channel:   moderationChannel(request.ChatType),
		Target:    request.TargetName,
		Muted:     muted,
		Text:      request.Message,
	})
}

/*
================
moderationChannel

The staff-facing name of a requested chat lane.
================
*/
func moderationChannel(chatType uint8) string {
	switch chatType {
	case ChatTypeAll:
		return "all"
	case ChatTypeWhisper:
		return "whisper"
	case ChatTypeGM:
		return "gm"
	case ChatTypeParty:
		return "party"
	case ChatTypeGuild:
		return "guild"
	case ChatTypeUnion:
		return "union"
	case ChatTypeStall:
		return "stall"
	}
	return "unknown"
}
