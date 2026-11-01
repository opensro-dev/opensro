package community

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/transport"
)

// Register wires the community lane's whisper-block mutate handler
// (0x766F) onto the hub. The friend mutators (0x7164 / 0x75DB) and the
// letter mutators (0x7261 send / 0x73F2 read / 0x70CC delete) are LIVE:
// RegisterFriend (friend.go) and RegisterLetter (letter.go) own them,
// wired from server.go alongside this call - nothing here is a
// silent-refuse stub anymore. Called from server.go with the SAME deps
// pointer every other lane retains. Each opcode registers exactly
// once and none is registered by any other lane (hub registration is
// last-write-wins; survey: bootstrap 0x0006/0x3012/0x707B/0x7427,
// movement 0x7738/0x7017/0x324B/0x7025/0x769E, action
// 0x706D/0x72CD/0x745A, progression 0x727A/0x7552/0x7165/0x72CB, community
// friend 0x7164/0x75DB, community letter 0x7261/0x73F2/0x70CC, party
// 0x70D5/0x751A/0x3393/0x704F/0x7664).
func Register(hub *transport.Hub, deps Dependencies) {
	hub.Handle(OpWhisperBlockRequest, whisperBlockHubHandler(deps))
}

// whisperBlockHubHandler adapts HandleWhisperBlock onto the hub: resolve
// the bound character through the session identity keys (never a
// client-supplied name), apply, answer with the 0xB66F ack the outcome
// composed (client CPSMission_OnAliasActionResponse0xB66F - success
// applies the add/remove to the blocking
// panel live, results 1/2/3 msgbox), log. A nil Ack stays silent on the
// wire (decode refusals and the cancel miss); the enter-world chunk-C
// reseed remains the full-list channel (see WhisperBlockOutcome).
func whisperBlockHubHandler(deps Dependencies) transport.HandlerFunc {
	return func(s *transport.Session, opcode uint16, payload []byte) {
		character, divisionID, bound := enterworld.SessionCharacter(deps, s)
		if !bound {
			log.Debugf("community: 0x%04X from unbound session %d discarded", opcode, s.ID)
			return
		}
		outcome := HandleWhisperBlock(deps, divisionID, character, payload)
		if outcome.Ack != nil {
			_ = s.Send(OpWhisperBlockAck, outcome.Ack)
		}
		if outcome.Refusal != "" {
			log.Debugf("community: 0x766F refused for %s: %s", character.Name, outcome.Refusal)
			return
		}
		log.Debugf("community: whisper-block list for %s now %d name(s)", character.Name, outcome.BlockedCount)
	}
}
