/*
===========================================================================

crestdelete.go - the crest delete items: erase a guild or union crest

#2282 (delete half): ITEM_ETC_GUILD_CREST_DELETE and
ITEM_ETC_UNION_CREST_DELETE (type 3/3/12/2; param1 1 guild / 2 union,
param2 2 delete) erase the crest their kind names. The client asks
first (confirm box 0x16/0x19) and refuses locally when no crest
stands; the server judges the same truth again here: the crest clear
commits through the guild or union door, then the item is spent and
the ordinary 0xB5BD success answers, with a 0x34F3 crest update
behind it so every client holding the guild restates the crest as
gone. No crest, no guild, no union: refused, nothing spent.

INFERENCE: any member may spend the item. The v1.150 client gates
only on a crest standing (its OK arm reads the crest flag and nothing
else), and no server rule is visible, so membership is the whole
gate - the item itself is the scarce thing.

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
)

// The crest a delete item erases, named by itemdata param1.
const (
	crestDeleteGuild = "guild"
	crestDeleteUnion = "union"
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
finishCrestDeleteUse

Clear the crest, then spend the item. A crest that does not stand
refuses and spends nothing. The spend re-finds the row by slot and
codename under a fresh character door; if it somehow vanished (the
request is one session under the division lock, so nothing else can
move it) the crest stays cleared and the item unspent, and the loud
refusal says so.
================
*/
func (rt *Runtime) finishCrestDeleteUse(divisionID string, character *enterworld.Character, request wire.ItemUseRequest, ref *enterworld.ItemRef, kind string, result *OpResult) {
	var update wire.Frame
	switch kind {
	case crestDeleteGuild:
		authority := rt.deps.GuildAuthority()
		if authority == nil {
			*result = itemUseFailure(wire.ErrCodeInvalidRequest)
			result.DiagnosticRefusal = "item-use: no guild store wired"
			return
		}
		snapshot, refusal := authority.UpdateGuildAs(divisionID, character.ID, "crest-delete",
			domain.GuildAuthorization{},
			func(record domain.GuildRecord, members []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
				if record.CrestParam == 0 {
					return record, members, false
				}
				record.CrestParam = 0
				return record, members, true
			})
		if refusal.Refused() {
			*result = itemUseFailure(wire.ErrCodeInvalidRequest)
			result.DiagnosticRefusal = "item-use: guild crest delete refused"
			return
		}
		update = wire.Frame{Opcode: guild.OpCrestUpdate,
			Payload: guild.EncodeGuildCrestUpdate(uint32(snapshot.Guild.ID), snapshot.Guild.Name, 0)}
	case crestDeleteUnion:
		if rt.Unions == nil {
			*result = itemUseFailure(wire.ErrCodeInvalidRequest)
			result.DiagnosticRefusal = "item-use: no union authority wired"
			return
		}
		record, cleared, err := rt.Unions.ClearCrest(divisionID, *character.GuildID)
		if err != nil || !cleared {
			*result = itemUseFailure(wire.ErrCodeInvalidRequest)
			result.DiagnosticRefusal = "item-use: union crest delete refused"
			return
		}
		names := []string{}
		if authority := rt.deps.GuildAuthority(); authority != nil {
			for _, guildID := range record.Guilds {
				if guildID == 0 {
					continue
				}
				if record, _, ok := authority.Guild(divisionID, guildID); ok {
					names = append(names, record.Name)
				}
			}
		}
		update = wire.Frame{Opcode: guild.OpCrestUpdate,
			Payload: guild.EncodeAllianceCrestUpdate(uint32(record.AllianceID), names, 0)}
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
	rt.publishItemUseVisual(character, ref, 0, result)
}
