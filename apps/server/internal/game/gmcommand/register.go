package gmcommand

import (
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/transport"
)

// Register wires the GM command lane's 0x75B6 handler onto the hub. Called
// from wiring.go with the SAME deps/presence instances every other lane
// captures, after the store collaborators are assigned. 0x75B6 registers
// exactly once and no other lane touches 0x75B6/0xB5B6 (survey date
// 2026-07-29).
func Register(hub *transport.Hub, deps Dependencies, presence PresenceView, status ...BodyStatusCommands) {
	hub.Handle(OpGmCommand, gmCommandHubHandler(deps, presence, status...))
}

// gmCommandHubHandler adapts HandleGmCommand onto the hub: resolve the bound
// character through the session identity keys (never a client-supplied
// name), gate on GMPrivilege inside HandleGmCommand, and answer the sender
// with the 0xB5B6 ack. A nil Ack stays silent on the wire (an unprivileged
// sender, a decode refusal, or a bound-character miss).
func gmCommandHubHandler(deps Dependencies, presence PresenceView, status ...BodyStatusCommands) transport.HandlerFunc {
	audit := newPrivilegeAudit()
	return func(s *transport.Session, opcode uint16, payload []byte) {
		character, divisionID, bound := enterworld.SessionCharacter(deps, s)
		if !bound {
			log.Debugf("gmcommand: 0x%04X from unbound session %d discarded", opcode, s.ID)
			return
		}
		outcome := HandleGmCommand(deps, presence, divisionID, character, payload, status...)
		if outcome.PrivilegeDenied {
			audit.deny(divisionID, character.Name, s.ID, len(payload), time.Now())
		} else if outcome.Refusal != "" {
			log.Debugf("gmcommand: 0x75B6 refused for %s: %s", character.Name, outcome.Refusal)
		}
		if outcome.Ack != nil {
			_ = s.Send(OpGmCommandAck, outcome.Ack)
		}
	}
}
