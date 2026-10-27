package enterworld

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/transport"
)

// OpcodeStallNetworkLeave is the C->S stall-network (vendor-browse) leave
// request. The ONLY producer is client 702960 /
// CGInterface_SendStallNetworkClose7427, whose body is EMPTY by asm truth:
// the CanSendOpcode gate (client @0x702987), the CMsgStreamBuffer ctor
// (client @0x702998) and the submit (@0x7029ca) run with NO AppendBytes between
// them. The client sends it from the window-close path only while a browse
// session is active (client 69DB80 -> StallNetwork_IsActive), then clears the
// active flag ITSELF - it does not wait for any answer.
//
// The S->C twin 0xB427 (client 767260) is a 1-byte sink natively (reads one
// byte, no side effects) and is NOT folded or wired in the browser rebuild;
// the byte retail sends is unpinned (analogy-only). So this handler pushes
// NO reply - the eventguide 0x707B posture - and emitting a guessed 0xB427
// would invent a wire contract for a frame nothing consumes.
//
// There is no server-side stall-browse session state to clear yet: the
// stall-network data plane (the 0xB6F9 stall-data family that STARTS a
// browse session) does not exist on this server, so a well-formed leave is
// a no-op by honest necessity, logged for the day that plane lands.
const OpcodeStallNetworkLeave uint16 = 0x7427

// HandleStallNetworkLeave validates one 0x7427 frame: the native body is
// exactly empty, so any payload byte is a shape the retail client cannot
// compose and refuses without touching state. No reply frames either way.
func HandleStallNetworkLeave(character *Character, payload []byte) error {
	if character == nil {
		return fmt.Errorf("bootstrap: stallNetworkLeave with no character")
	}
	if len(payload) != 0 {
		return fmt.Errorf("bootstrap: stallNetworkLeave payload %d bytes, native body is empty (client 702960 appends nothing)", len(payload))
	}
	return nil
}

// RegisterStallNetworkLeave registers the 0x7427 handler: resolve the bound
// character through the session identity keys (never a client-supplied
// name), validate the empty body, push nothing. Unbound sessions and
// malformed payloads drop the frame with a log line, mirroring the
// eventguide/move-lane silent-refusal posture.
func RegisterStallNetworkLeave(hub *transport.Hub, deps *Deps) {
	hub.Handle(OpcodeStallNetworkLeave, func(s *transport.Session, _ uint16, payload []byte) {
		character, _, bound := SessionCharacter(deps.Characters, s)
		if !bound {
			log.Debug("bootstrap: 0x7427 before enter-world bind ignored")
			return
		}
		if err := HandleStallNetworkLeave(character, payload); err != nil {
			log.Debugf("bootstrap: 0x7427 refused for %s: %v", character.Name, err)
			return
		}
		log.Debugf("bootstrap: 0x7427 stall-network leave from %s accepted (no browse-session state to clear yet)", character.Name)
	})
}
