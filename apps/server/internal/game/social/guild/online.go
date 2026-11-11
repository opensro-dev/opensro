/*
===========================================================================

online.go - guild member presence: the roster's offline byte follows bind and close

A member row's offline flag is derived only when a 0x32C4 block is
encoded (encode32c4.go): the entry seed is composed before the binder
joins the presence directory, so the seed reads the binder's own row
offline, and no roster corrects itself afterwards (#3208). The
0x3B29 subOp-6 &0x02 leg is the correction the client already applies
(social-guild-union.ts, native 5E9D20): WorldBound below sends the
binder's online delta to every online member including the binder,
and SessionClosed sends the offline delta to the others. The member's
wire jid comes from the stored member row, never from the character
id (the two spaces differ: domain.GuildMemberRecord).

===========================================================================
*/

package guild

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/transport"
)

/*
================
WorldBound

The world-bind presence fan-out, called from the server's
OnWorldBound tail after the exclusive bind wins (the friend lane's
FriendWorldBound is the model): every online member of the binder's
guild - the binder included - receives the binder's online delta
{6, jid, 0x02, 0}. The binder's own row needs the self leg because
their seed was encoded before the bind made them present; the other
members' blocks were encoded while the binder was offline. A rebind
after an eviction fans out again; the client's flip write is
idempotent.
================
*/
func WorldBound(deps Dependencies, presence Presence, divisionID string, character *domain.Character) {
	guildID, jid, names, ok := memberPresence(deps, divisionID, character)
	if !ok {
		return
	}
	sendToOnlineMembers(presence, divisionID, guildID, names, EncodeMemberPresence3B29(jid, 0))
}

/*
================
SessionClosed

The session-close presence fan-out, called from the hub's
OnSessionClose hook (the friend lane's FriendSessionClosed guard):
resolve the closing session's bound character (an evicted loser had
its name key cleared at bind time and resolves to nothing), skip
when ANOTHER session already holds the bind key (the relog winner is
live - the character never went offline), then send the offline
delta {6, jid, 0x02, 1} to every other online member.
================
*/
func SessionClosed(deps Dependencies, presence Presence, s *transport.Session) {
	if presence == nil {
		return
	}
	character, divisionID, bound := enterworld.SessionCharacter(deps, s)
	if !bound {
		return
	}
	if winner, live := presence.SessionByName(divisionID, character.Name); live && winner != s {
		return
	}
	guildID, jid, names, ok := memberPresence(deps, divisionID, character)
	if !ok {
		return
	}
	others := make([]string, 0, len(names))
	for _, name := range names {
		if name != character.Name {
			others = append(others, name)
		}
	}
	sendToOnlineMembers(presence, divisionID, guildID, others, EncodeMemberPresence3B29(jid, 1))
}

/*
================
memberPresence

The binder's guild id, their stored member-row jid and every member
name, read under the authority read door (sends happen outside it,
in sendToOnlineMembers). ok=false when the character is guildless,
the guild row is gone, or the character holds no member row - each
means no roster anywhere carries a row this delta could flip.
================
*/
func memberPresence(deps Dependencies, divisionID string, character *domain.Character) (int64, uint32, []string, bool) {
	if deps == nil || character == nil || character.GuildID == nil {
		return 0, 0, nil, false
	}
	guildID := *character.GuildID
	var jid uint32
	var names []string
	deps.Read(divisionID, func() {
		_, members, ok := deps.GuildAuthority().Guild(divisionID, guildID)
		if !ok {
			return
		}
		for _, member := range members {
			names = append(names, member.Name)
			if member.CharID == character.ID {
				jid = member.JID
			}
		}
	})
	if jid == 0 || len(names) == 0 {
		return 0, 0, nil, false
	}
	return guildID, jid, names, true
}
