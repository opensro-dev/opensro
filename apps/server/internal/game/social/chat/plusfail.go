/*
===========================================================================

plusfail.go - the /plusfail announcement switch (port-only, not native)

#3682 (owner decision 2026-11-14): a failed high-plus reinforcement is
announced to the shard unless the character opted out. The account page
carries the same choice; this is its in-game twin, consumed by the chat
handler like /contributor (register.go) and never published. The choice
is the character's stored opt-out (domain.Character.PlusFailOptOut,
schema 51); the announcement itself is action's announcePlusFail, which
keeps #1738's privacy rule (job alias or "a player", never the name).

===========================================================================
*/
package chat

import (
	"strings"

	"opensro.online/server/internal/game/enterworld"
)

// plusFailCommand is the chat line the switch answers to.
const plusFailCommand = "/plusfail"

const (
	answerPlusFailOn    = "Failed reinforcement announcements are on for this character: the world hears when your high-plus reinforcement fails."
	answerPlusFailOff   = "Failed reinforcement announcements are off for this character: your failed reinforcement stays private."
	answerPlusFailUsage = "Usage: /plusfail on or /plusfail off."
	answerPlusFailBusy  = "That choice could not be saved; your old choice stays in force."
)

/*
================
PlusFailCommand

The chat intake: "/plusfail", "/plusfail on" or "/plusfail off" from
the speaker. handled is false for any other line, which stays ordinary
chat. Bare reports the standing choice; on and off store before they
report, and a choice already in force is reported without a save. A
save the store refuses keeps the old choice (answerPlusFailBusy).
================
*/
func PlusFailCommand(character *enterworld.Character, message string, set func(character *enterworld.Character, on bool) bool) (answer string, handled bool) {
	fields := strings.Fields(strings.ToLower(message))
	if len(fields) == 0 || fields[0] != plusFailCommand {
		return "", false
	}
	if len(fields) > 2 || (len(fields) == 2 && fields[1] != "on" && fields[1] != "off") {
		return answerPlusFailUsage, true
	}
	if len(fields) == 1 {
		return plusFailAnswer(character.PlusFailOptOut), true
	}
	on := fields[1] == "on"
	if announced := !character.PlusFailOptOut; on == announced {
		// Already the standing choice: report it, save nothing.
		return plusFailAnswer(character.PlusFailOptOut), true
	}
	if !set(character, on) {
		return answerPlusFailBusy, true
	}
	return plusFailAnswer(!on), true
}

/*
================
plusFailAnswer

The standing choice as the player reads it: announcements on unless
the character opted out.
================
*/
func plusFailAnswer(optedOut bool) string {
	if optedOut {
		return answerPlusFailOff
	}
	return answerPlusFailOn
}
