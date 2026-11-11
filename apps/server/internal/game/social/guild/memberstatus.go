/*
===========================================================================

memberstatus.go - a member's login and logout reach the guild roster

The 0x32C4 block carries each member's offline byte as read at encode
time, so without a later push the roster freezes at the moment the block
was built. The client keeps it live through the 0x3B29 subOp-6 member
delta's &0x02 leg (client CIFGuildMember_OnMemberUpdated 5E9D20: &1
level, &2 offline at member +0x26), and its announcement block (5F3340,
gated on &2 @5f3392) prints UIIT_MSG_GUILD_CONNECT for another member and
the guild notice for the reader's own row when the byte flips to 0.

The entry seed is built before the world bind publishes the character in
the presence directory, so the entering member reads its own row offline
in the seed; the world-bound push below flips it, which is the flip the
self arm of 5F3340 announces. Native shows no other emitter for the
client's connect texts, so the inference is that SR_GameServer sends this
delta to every online member, the subject included, on login and on
logout.

===========================================================================
*/

package guild

import (
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

const (
	// memberDeltaOfflineBit is the &0x02 leg of the subOp-6 member delta.
	memberDeltaOfflineBit = 0x02

	// memberOnline and memberOffline are the values of the member row's
	// offline byte (0x32C4 row tail, subOp-6 &0x02 leg).
	memberOnline  = 0
	memberOffline = 1
)

/*
================
EncodeMemberOnline3B29

The 0x3B29 subOp-6 member delta carrying ONLY the &0x02 offline leg:
{u8 6}{u32 jid}{u8 0x02}{u8 offline}.
================
*/
func EncodeMemberOnline3B29(jid uint32, offline uint8) []byte {
	writer := wire.NewWriter(7)
	writer.U8(6)
	writer.U32(jid)
	writer.U8(memberDeltaOfflineBit)
	writer.U8(offline)
	return writer.Payload()
}

/*
================
MemberWorldBound

Tells every online member of the character's guild, the character
included, that its row is online. Called once per login from the world
bind, after the exclusive bind is published; a teleport re-entry does not
call it (the character never left, and its new seed reads it bound).
================
*/
func MemberWorldBound(deps Dependencies, presence Presence, divisionID string, character *enterworld.Character) {
	announceMemberStatus(deps, presence, divisionID, character, memberOnline, "")
}

/*
================
MemberSessionClosed

Tells the remaining online members that the closing session's character
went offline. A session that lost its bind to a newer one (a relog that
replaced it) announces nothing: the character never went offline.
================
*/
func MemberSessionClosed(deps Dependencies, presence Presence, session *transport.Session) {
	character, divisionID, bound := enterworld.SessionCharacter(deps, session)
	if !bound || presence == nil {
		return
	}
	if winner, live := presence.SessionByName(divisionID, character.Name); live && winner != session {
		return
	}
	announceMemberStatus(deps, presence, divisionID, character, memberOffline, character.Name)
}

/*
================
announceMemberStatus

Resolves the character's guild row under the authority read door, then
fans the delta to every online member that holds the guild's block
(sendToOnlineMembers), skipping the named member.
================
*/
func announceMemberStatus(
	deps Dependencies,
	presence Presence,
	divisionID string,
	character *enterworld.Character,
	offline uint8,
	skip string,
) {
	if deps == nil || deps.GuildAuthority() == nil || presence == nil || character == nil {
		return
	}
	var guildID int64
	deps.Read(divisionID, func() {
		if character.GuildID != nil {
			guildID = *character.GuildID
		}
	})
	if guildID == 0 {
		return
	}
	_, members, ok := deps.GuildAuthority().Guild(divisionID, guildID)
	if !ok {
		return
	}
	var jid uint32
	found := false
	names := make([]string, 0, len(members))
	for _, member := range members {
		if member.CharID == character.ID {
			jid = member.JID
			found = true
		}
		if skip != "" && strings.EqualFold(member.Name, skip) {
			continue
		}
		names = append(names, member.Name)
	}
	if !found {
		return
	}
	sendToOnlineMembers(presence, divisionID, guildID, names, EncodeMemberOnline3B29(jid, offline))
}
