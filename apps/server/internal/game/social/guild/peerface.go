/*
===========================================================================

peerface.go - a player's guild nameplate as the players around them see it

The spawn row carries a player's guild face once, when a viewer first sees
them. A membership change after that (create, join, kick, leave, break, a
grant name) has to reach the viewers who already hold the player, or their
nameplates keep the old guild until the player walks out of sight and back
(#4413, BUG-097).

Native v1.188 sends 0x30FF to the nearby sessions on a bind
(its guild-face broadcast 4EA2F0, called from the join commit
5C8BF0 and the create commit 5C8D40) and 0x3100 {u32 uid} on an unbind
(the dissolve broadcast 5C3D80 and the leave/kick path 5C3EA0).
The v1.150 client registers the same two handlers under its own opcodes
(CPSMission_RegisterPacketHandlers 74E833 / 74E854):

	0x365F CPSMission_OnPeerGuildAppearance 75FE90
	  u32 gid, u32 guildId, ascii guildName; when the name is not empty:
	  ascii grantName, u32 crest, u32 allianceId, u32 allianceCrest,
	  u8 fortressRole -> CICPlayer_BindGuild 869810 + grant + fortress title
	0x37E6 CPSMission_OnPeerGuildCleared 75C890
	  u32 gid -> CICPlayer_UnbindGuild 868800

===========================================================================
*/
package guild

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

// OpPeerGuildAppearance binds a nearby player's guild nameplate (S->C).
const OpPeerGuildAppearance uint16 = 0x365F

// OpPeerGuildCleared removes a nearby player's guild nameplate (S->C).
const OpPeerGuildCleared uint16 = 0x37E6

/*
================
peerGuildFace

The guild half of a player's nameplate: the same fields, in the same
order, as the spawn row's guild sub-block (movement resolvePeerGuild).
================
*/
type peerGuildFace struct {
	Name          string
	GuildID       uint32
	Grant         string
	Crest         uint32
	AllianceID    uint32
	AllianceCrest uint32
	FortressRole  uint8
}

/*
================
EncodePeerGuildAppearance

The 0x365F body. An empty name would make the client read nothing past it
and bind nothing, so a guildless player is sent 0x37E6 instead.
================
*/
func EncodePeerGuildAppearance(gid uint32, face peerGuildFace) []byte {
	writer := wire.NewWriter(32 + len(face.Name) + len(face.Grant))
	writer.U32(gid)
	writer.U32(face.GuildID)
	writer.Str(face.Name)
	writer.Str(face.Grant)
	writer.U32(face.Crest)
	writer.U32(face.AllianceID)
	writer.U32(face.AllianceCrest)
	writer.U8(face.FortressRole)
	return writer.Payload()
}

/*
================
EncodePeerGuildCleared

The 0x37E6 body: the player whose guild nameplate goes.
================
*/
func EncodePeerGuildCleared(gid uint32) []byte {
	writer := wire.NewWriter(4)
	writer.U32(gid)
	return writer.Payload()
}

/*
================
resolvePeerGuildFace

The committed guild face of one character, read after the mutation that
changed it. ok=false is a guildless character.
================
*/
func resolvePeerGuildFace(deps Dependencies, unions *UnionRuntime, divisionID string, characterID int64) (peerGuildFace, bool) {
	guilds := deps.GuildAuthority()
	if guilds == nil {
		return peerGuildFace{}, false
	}
	guildID, ok := guilds.GuildOfCharacter(divisionID, characterID)
	if !ok {
		return peerGuildFace{}, false
	}
	record, ok := guilds.GuildRecord(divisionID, guildID)
	if !ok {
		return peerGuildFace{}, false
	}
	face := peerGuildFace{Name: record.Name, GuildID: uint32(record.ID), Crest: record.CrestParam}
	if member, ok := guilds.GuildMember(divisionID, guildID, characterID); ok {
		face.Grant = member.GrantName
		face.FortressRole = member.FortressRole
	}
	if unions != nil {
		if alliance, ok := unions.unions.Of(divisionID, guildID); ok {
			face.AllianceID, face.AllianceCrest = uint32(alliance.AllianceID), alliance.Crest
		}
	}
	return face, true
}

/*
================
peerFaces

What refreshPeerGuildFaces needs to reach the viewers. One plain struct
because both the lane (register.go) and the invite runtime send it.
================
*/
type peerFaces struct {
	hub      *transport.Hub
	deps     Dependencies
	presence Presence
	unions   *UnionRuntime
}

/*
================
refresh

Sends each named online character's current guild face to every other
session that sees them. Their own client already learned it from the
guild block (0x32C4) or the leave/kick/break ack. Offline names are
skipped; a later spawn carries the stored face.
================
*/
func (p peerFaces) refresh(divisionID string, names []string) {
	if p.hub == nil || p.presence == nil {
		return
	}
	for _, name := range names {
		session, online := p.presence.SessionByName(divisionID, name)
		if !online {
			continue
		}
		gid, ok := session.CharacterObjectID()
		if !ok {
			continue
		}
		character, _, bound := enterworld.SessionCharacter(p.deps, session)
		if !bound {
			continue
		}
		frame := transport.Frame{Opcode: OpPeerGuildCleared, Payload: EncodePeerGuildCleared(gid)}
		if face, ok := resolvePeerGuildFace(p.deps, p.unions, divisionID, character.ID); ok {
			frame = transport.Frame{Opcode: OpPeerGuildAppearance, Payload: EncodePeerGuildAppearance(gid, face)}
		}
		p.hub.BroadcastObserved(divisionID, session.ID, gid, []transport.Frame{frame})
	}
}
