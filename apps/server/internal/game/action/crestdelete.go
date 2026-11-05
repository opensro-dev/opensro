/*
===========================================================================

crestdelete.go - the crest delete items: erase a guild or union crest

#2282 (delete half): ITEM_ETC_GUILD_CREST_DELETE and
ITEM_ETC_UNION_CREST_DELETE (type 3/3/12/2; param1 1 guild / 2 union,
param2 2 delete) erase the crest their kind names. The client asks
first (confirm box 0x16/0x19) and refuses locally when no crest
stands; the server judges the same truth again here, behind gates:
the guild item wants a guild of level 4 or more and its master for a
user, the union item a guild of level 5 or more and its master in a
guild that leads the union, and each wants a crest standing. The
gates refuse in that order - no guild, level, master, then the
union's own (no union, not its master guild, no crest) - and a
refusal spends nothing. The crest clear commits through the guild or
union door, then the item is spent and the ordinary 0xB5BD success
answers.

The 0x34F3 crest update behind the answer is for more than the user:
its audience is inferred from the client's crest handler, which
re-skins every loaded character of the named guild, member or not.
So the update goes to every client that can see a member of the
guild (of the union's guilds, for the union item): the members
wherever they stand plus everyone in view of any of them. The port
has no per-member observer set, so it sends division-wide (a few
bytes; crest changes are rare), falling back to the user's observers
plus a named push to each member where that seam is unwired.

The register twins (param2 1) stay unsupported: their flow uploads a
bitmap the port has no source for yet (#2272).

===========================================================================
*/
package action

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/social/union"
)

// The crest a delete item erases, named by itemdata param1.
const (
	crestDeleteGuild = "guild"
	crestDeleteUnion = "union"
)

// The guild levels a crest delete requires: the guild item at 4, the
// union item at 5.
const (
	crestDeleteGuildMinLevel = 4
	crestDeleteUnionMinLevel = 5
)

/*
================
admitCrestDeleteUse

The item-use closure's crest arm: name the crest the finish should
clear, or "" with the refusal already in result. Nothing is spent
here: the guild and union doors take their own locks and must run
outside this character door, so HandleItemUse clears first and spends
second (finishCrestDeleteUse) - the name-change scroll's shape.
================
*/
func (rt *Runtime) admitCrestDeleteUse(character *enterworld.Character, ref *enterworld.ItemRef, tail []byte, result *OpResult) string {
	if len(tail) != 0 {
		return ""
	}
	param1, _ := ref.NativeFields.Lookup("itemParam1_29c")
	kind := ""
	switch param1 {
	case 1:
		kind = crestDeleteGuild
	case 2:
		kind = crestDeleteUnion
	}
	if kind == "" || character.GuildID == nil {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		result.DiagnosticRefusal = "item-use: crest delete names no crest for " + character.Name
		return ""
	}
	return kind
}

/*
================
crestGateCode

The two guild-door crest gates in their refusal order - guild level,
then mastership - judged against one coherent guild view. Answers
the wire notice byte the refusal carries, or 0 when the guild admits
the delete. A user with no member row never reaches this (the door
refuses first); the generic byte covers that impossible case.
================
*/
func crestGateCode(record domain.GuildRecord, members []domain.GuildMemberRecord, characterID int64, minLevel uint8) uint8 {
	if record.Level < minLevel {
		return wire.ErrCodeCrestGuildLevelTooLow
	}
	for _, member := range members {
		if member.CharID != characterID {
			continue
		}
		if member.Grade != domain.GuildLeaderGrade {
			return wire.ErrCodeCrestNotMaster
		}
		return 0
	}
	return wire.ErrCodeInvalidRequest
}

/*
================
clearGuildCrest

The guild arm: level, master and a standing crest judged inside one
guild-door update, the clear itself their reward. On refusal result
carries the named code and nothing changed; on success the 0x34F3
guild row and the committed member list come back.
================
*/
func (rt *Runtime) clearGuildCrest(divisionID string, character *enterworld.Character, result *OpResult) (wire.Frame, []domain.GuildMemberRecord, bool) {
	authority := rt.deps.GuildAuthority()
	if authority == nil {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		result.DiagnosticRefusal = "item-use: no guild store wired"
		return wire.Frame{}, nil, false
	}
	gate := uint8(0)
	snapshot, refusal := authority.UpdateGuildAs(divisionID, character.ID, "crest-delete",
		domain.GuildAuthorization{},
		func(record domain.GuildRecord, members []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
			if code := crestGateCode(record, members, character.ID, crestDeleteGuildMinLevel); code != 0 {
				gate = code
				return record, members, false
			}
			if record.CrestParam == 0 {
				gate = wire.ErrCodeInvalidRequest
				return record, members, false
			}
			record.CrestParam = 0
			return record, members, true
		})
	if refusal.Refused() {
		code := gate
		if code == 0 {
			code = wire.ErrCodeInvalidRequest
		}
		*result = itemUseFailure(code)
		result.DiagnosticRefusal = "item-use: guild crest delete refused"
		return wire.Frame{}, nil, false
	}
	update := wire.Frame{Opcode: guild.OpCrestUpdate,
		Payload: guild.EncodeGuildCrestUpdate(uint32(snapshot.Guild.ID), snapshot.Guild.Name, 0)}
	return update, snapshot.Members, true
}

/*
================
probeCrestGuildGate

Runs the guild-side crest gates inside the guild door without
changing anything (the union arm's pre-clear: its own clear belongs
to the union door). Answers the first gate's wire code, or 0 when
the guild admits the delete. A guild the door will not even open
for this user reads as the generic code - the no-guild refusal.
================
*/
func (rt *Runtime) probeCrestGuildGate(divisionID string, character *enterworld.Character, minLevel uint8) uint8 {
	authority := rt.deps.GuildAuthority()
	if authority == nil {
		return wire.ErrCodeInvalidRequest
	}
	gate := uint8(0)
	probed := false
	authority.UpdateGuildAs(divisionID, character.ID, "crest-delete",
		domain.GuildAuthorization{},
		func(record domain.GuildRecord, members []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
			probed = true
			gate = crestGateCode(record, members, character.ID, minLevel)
			return record, members, false
		})
	if !probed {
		return wire.ErrCodeInvalidRequest
	}
	return gate
}

/*
================
clearUnionCrest

The union arm: the guild gates first (inside the guild door, before
the union door is touched), then the union door's own - no union,
not the union's master guild, no crest - around its clear. On
refusal result carries the named code and nothing changed; on
success the 0x34F3 union row and the member list of every guild in
the union come back.
================
*/
func (rt *Runtime) clearUnionCrest(divisionID string, character *enterworld.Character, result *OpResult) (wire.Frame, []domain.GuildMemberRecord, bool) {
	if rt.Unions == nil {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		result.DiagnosticRefusal = "item-use: no union authority wired"
		return wire.Frame{}, nil, false
	}
	if code := rt.probeCrestGuildGate(divisionID, character, crestDeleteUnionMinLevel); code != 0 {
		*result = itemUseFailure(code)
		result.DiagnosticRefusal = "item-use: union crest delete refused at the guild gate"
		return wire.Frame{}, nil, false
	}
	record, refusal, err := rt.Unions.ClearCrest(divisionID, *character.GuildID)
	if err != nil {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		result.DiagnosticRefusal = "item-use: union crest delete refused"
		return wire.Frame{}, nil, false
	}
	switch refusal {
	case union.CrestRefusalNone:
	case union.CrestRefusalNoUnion:
		*result = itemUseFailure(wire.ErrCodeCrestNoUnion)
		result.DiagnosticRefusal = "item-use: union crest delete without a union"
		return wire.Frame{}, nil, false
	case union.CrestRefusalNotMasterGuild:
		*result = itemUseFailure(wire.ErrCodeCrestNotMaster)
		result.DiagnosticRefusal = "item-use: union crest delete by a guild that does not lead the union"
		return wire.Frame{}, nil, false
	default:
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		result.DiagnosticRefusal = "item-use: union crest delete without a crest"
		return wire.Frame{}, nil, false
	}
	names := []string{}
	members := []domain.GuildMemberRecord{}
	if authority := rt.deps.GuildAuthority(); authority != nil {
		for _, guildID := range record.Guilds {
			if guildID == 0 {
				continue
			}
			if guildRecord, guildMembers, ok := authority.Guild(divisionID, guildID); ok {
				names = append(names, guildRecord.Name)
				members = append(members, guildMembers...)
			}
		}
	}
	update := wire.Frame{Opcode: guild.OpCrestUpdate,
		Payload: guild.EncodeAllianceCrestUpdate(uint32(record.AllianceID), names, 0)}
	return update, members, true
}

/*
================
publishCrestUpdate

Sends a cleared crest's 0x34F3 past the user (their copy rides the
success burst in result.Frames). The division seam, when wired,
carries it to every other session - the stand-in for a per-member
observer set; otherwise the update joins the result's broadcast
(the user's observers) and goes by name to every member of the
guild - or of the union's guilds - wherever they stand.
================
*/
func (rt *Runtime) publishCrestUpdate(divisionID string, character *enterworld.Character, members []domain.GuildMemberRecord, update wire.Frame, result *OpResult) {
	if rt.PushShardPeerFrames != nil {
		rt.PushShardPeerFrames(divisionID, character.Name, []wire.Frame{update})
		return
	}
	result.Broadcast = append(result.Broadcast, update)
	if rt.PushCharacterFrames == nil {
		return
	}
	for _, member := range members {
		if member.CharID == character.ID || member.Name == "" {
			continue
		}
		rt.PushCharacterFrames(divisionID, member.Name, []wire.Frame{update})
	}
}

/*
================
finishCrestDeleteUse

Clear the crest, then spend the item. A gate that holds refuses and
spends nothing. The spend re-finds the row by slot and codename
under a fresh character door; if it somehow vanished (the request
is one session under the division lock, so nothing else can move
it) the crest stays cleared and the item unspent, and the loud
refusal says so.
================
*/
func (rt *Runtime) finishCrestDeleteUse(divisionID string, character *enterworld.Character, request wire.ItemUseRequest, ref *enterworld.ItemRef, kind string, result *OpResult) {
	var update wire.Frame
	var audience []domain.GuildMemberRecord
	cleared := false
	switch kind {
	case crestDeleteGuild:
		update, audience, cleared = rt.clearGuildCrest(divisionID, character, result)
	case crestDeleteUnion:
		update, audience, cleared = rt.clearUnionCrest(divisionID, character, result)
	}
	if !cleared {
		return
	}

	consumed := rt.deps.Update(character, "item-use-crest-delete", func() bool {
		rowIndex := -1
		for index := range character.MissionInventory {
			if character.MissionInventory[index].Slot == int64(request.Slot) &&
				character.MissionInventory[index].Codename == ref.Codename {
				rowIndex = index
				break
			}
		}
		if rowIndex < 0 {
			return false
		}
		remaining := rt.consumeItemUseRow(character, rowIndex)
		*result = OpResult{Frames: []wire.Frame{
			{Opcode: wire.OpItemUseResponse,
				Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)},
			update,
		}}
		result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
		return true
	})
	if !consumed {
		log.Warnf("action: crest delete for %q in %s cleared but its item row is gone; left unspent", character.Name, divisionID)
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		result.DiagnosticRefusal = "item-use: crest cleared but the delete item row vanished before it could be spent"
		return
	}
	rt.publishCrestUpdate(divisionID, character, audience, update, result)
	rt.publishItemUseVisual(character, ref, 0, result)
}
