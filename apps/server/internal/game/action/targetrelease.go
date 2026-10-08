package action

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

// TargetReleaseOutcome is one handled 0x74B3. The release is accepted only
// for the exact gid previously granted by 0x745A; the sole success frame is
// retail's 0xB4B3 mode-1 close acknowledgement.
type TargetReleaseOutcome struct {
	Released uint32
	Frames   []wire.Frame
	Refusal  string
}

func refusedTargetRelease(reason string) TargetReleaseOutcome {
	return TargetReleaseOutcome{Refusal: reason}
}

/*
================
answeredTargetRelease

A well-formed release the server refuses (no selection, or another gid)
still gets its 0xB4B3 answer, mode 2: the client's release is an untagged
barrier, and a silent refusal left it waiting until it dropped the session
("Target release timed out", BR-261007-0624). 761820 parses mode 2's extra
byte, so the reply exists in v1.150; the selection itself is kept.
================
*/
func answeredTargetRelease(reason string) TargetReleaseOutcome {
	return TargetReleaseOutcome{Refusal: reason, Frames: []wire.Frame{{
		Opcode:  wire.OpTalkCloseResult,
		Payload: wire.EncodeTalkCloseRefusal(),
	}}}
}

// registerTargetRelease wires the selected-target release conversation.
// A malformed body is dropped silently (no client sends one); every other
// refusal is answered with 0xB4B3 mode 2 (answeredTargetRelease).
func (rt *Runtime) registerTargetRelease(hub *transport.Hub) {
	hub.Handle(wire.OpTargetReleaseRequest, func(
		session *transport.Session,
		opcode uint16,
		payload []byte,
	) {
		character, divisionID, bound := enterworld.SessionCharacter(rt.deps, session)
		if !bound {
			log.Debugf("action: 0x%04X from unbound session %d discarded", opcode, session.ID)
			return
		}
		outcome := rt.HandleTargetRelease(divisionID, character, payload)
		if outcome.Refusal != "" {
			log.Debugf(
				"action: 0x74B3 refused for %s: %s",
				character.Name,
				outcome.Refusal,
			)
			sendFrames(session, outcome.Frames)
			return
		}
		sendFrames(session, outcome.Frames)
		log.Debugf(
			"action: 0x74B3 %s released gid %d",
			character.Name,
			outcome.Released,
		)
	})
}

// HandleTargetRelease validates the exact request body and binds it to the
// per-character selection established by 0x745A. Matching releases clear
// that selection atomically under the division operation lock, then answer
// 0xB4B3 [mode=1]. Malformed, stale, or attacker-chosen gids do not disturb
// the current selection.
func (rt *Runtime) HandleTargetRelease(
	divisionID string,
	character *enterworld.Character,
	payload []byte,
) TargetReleaseOutcome {
	if character == nil {
		return refusedTargetRelease("characterNotFound")
	}
	gid, err := wire.DecodeTargetReleaseRequest(payload)
	if err != nil {
		return refusedTargetRelease(err.Error())
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()

	selected, ok := rt.Selected.Get(divisionID, character.Name)
	if !ok {
		return answeredTargetRelease("no selected object")
	}
	if selected != gid {
		return answeredTargetRelease(
			fmt.Sprintf("gid %d is not current selection %d", gid, selected),
		)
	}

	rt.Selected.Clear(divisionID, character.Name)
	rt.NpcDialogs.Clear(divisionID, character.Name)
	return TargetReleaseOutcome{
		Released: gid,
		Frames: []wire.Frame{{
			Opcode:  wire.OpTalkCloseResult,
			Payload: wire.EncodeTalkCloseResult(),
		}},
	}
}
