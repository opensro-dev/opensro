/*
===========================================================================

nameplate.go - the GM's /NAMEPLATE ON and OFF

Port-only, not native (owner, 2026-11-28). Every client draws an animated
glow around a staff member's name (the browser client's staff-nameplate.ts);
/NAMEPLATE OFF turns the GM's own glow off for every player, and
/NAMEPLATE ON turns it back on. The switch is kept in the staff book, so
it holds across relogs and restarts (action.SetGMNameplate).

The v1.150 client has no nameplate word (its GM dispatcher 0x509cf0
matches only the SR_IMPLE_COMMAND_* table), so the port picks subcommand
0xFD (wire.go) and its own reply text. Only a GM whose grant carries
staff.Nameplate can send it (capability.go).

===========================================================================
*/
package gmcommand

import "opensro.online/server/internal/game/enterworld"

/*
================
handleNameplate

Hands the switch to the runtime (status[0]) and answers with what is now
in force, or that the switch was refused.
================
*/
func handleNameplate(divisionID string, sender *enterworld.Character, request Request, status []BodyStatusCommands) Outcome {
	if len(status) > 0 && status[0] != nil {
		if owner, ok := status[0].(interface {
			SetGMNameplate(string, string, bool) bool
		}); ok && owner.SetGMNameplate(divisionID, sender.Name, request.NameplateOn) {
			text := "nameplate glow off"
			if request.NameplateOn {
				text = "nameplate glow on"
			}
			return Outcome{Ack: EncodeModerationReply(AckResultOK, SubNameplate, text)}
		}
	}
	return Outcome{Ack: EncodeModerationReply(AckResultFail, SubNameplate, "nameplate not switched"),
		Refusal: "nameplate-refused"}
}
