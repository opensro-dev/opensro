/*
===========================================================================

rename_notice.go - "X is now known as Y" for friends and guild members

Port-only, not native (#1689, owner decision 2026-10-27): when a queued
name change applies at login, the old name rides the renamed character
(domain.Character.RenameNoticeFrom) until the character next binds to
the world. This fanout then tells exactly the people the owner named -
the character's online friends and online guild members, nobody else -
and clears the debt, so the notice fires once.

The recipients resolve under the authority read door (friend edges by
ID, guild members by the roster's guild field) but the sends happen
after it, and the clear runs through the character door: no door is
held across a network send, and once the clear commits no later bind
can repeat the notice.

===========================================================================
*/

package community

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
RenameNoticeWorldBound

The world-bind fanout for a character whose queued rename applied at
login (character.RenameNoticeFrom holds the pre-rename name). Called
from the same bind hook as FriendWorldBound. A character with no debt
returns at once.
================
*/
func RenameNoticeWorldBound(deps Dependencies, presence Presence, divisionID string, character *enterworld.Character) {
	if deps == nil || character == nil || character.RenameNoticeFrom == "" {
		return
	}
	from := character.RenameNoticeFrom
	frame := wire.RenameNotificationFrame(from + " is now known as " + character.Name)

	// Resolve the recipient names under the read door, never holding it
	// across a presence lookup or a network send. The renamed character
	// never receives its own notice.
	var recipients []string
	seen := map[string]bool{character.Name: true}
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			recipients = append(recipients, name)
		}
	}
	deps.Read(divisionID, func() {
		for _, edge := range enterworld.FriendsView(character) {
			add(edge.Name)
		}
		if character.GuildID != nil {
			for _, member := range deps.CharactersForDivision(divisionID) {
				if member != nil && member.GuildID != nil && *member.GuildID == *character.GuildID {
					add(member.Name)
				}
			}
		}
	})
	for _, name := range recipients {
		if peer, ok := presenceSession(presence, divisionID, name); ok {
			_ = peer.Send(frame.Opcode, frame.Payload)
		}
	}

	// The notice is spent even when nobody was online to hear it: the
	// debt records that the rename happened, not who still needs word.
	deps.Update(character, "rename-notice-shown", func() bool {
		if character.RenameNoticeFrom != from {
			return false
		}
		character.RenameNoticeFrom = ""
		return true
	})
}
