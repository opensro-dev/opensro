/*
===========================================================================

wiring_plusfail.go - the /plusfail announcement switch on this shard

Port-only, not native (#3682; the announcements are SRO_ANNOUNCE_PLUS_FAIL,
action.announcePlusFail). With the shard flag on, the chat handler takes
the speaker's "/plusfail on|off" and the choice is committed through the
character door as the character's opt-out (plusFailOptOut, schema 51) -
the same field the account page sets through the public API
(public_api.go's SetPlusFailNotice). With the flag off nothing is wired
and "/plusfail" stays ordinary chat text, the flag's native posture.

===========================================================================
*/
package main

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
wirePlusFailNotice

Called from register once the chat runtime exists. The setter reports
whether the door took the choice, so a refused save keeps the old one
(chat.PlusFailCommand says so).
================
*/
func wirePlusFailNotice(game *gameplayPlane) {
	if !action.AnnouncementsFromEnv().PlusFail {
		return
	}
	game.chat.PlusFailNotice = func(character *enterworld.Character, on bool) bool {
		return game.deps.Update(character, "plus-fail-notice", func() bool {
			if character.PlusFailOptOut == !on {
				return false
			}
			character.PlusFailOptOut = !on
			return true
		})
	}
	log.Infof("plus-fail announcements: ON (%s); opt out with /plusfail off or on the account page", action.EnvAnnouncePlusFail)
}
