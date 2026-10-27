/*
===========================================================================

namechange.go - the name-change scroll: queue a rename for the next login

Port-only, not native (#1689, owner decision 2026-10-27): v1.150 has no
character rename at all. Behind SRO_NAME_CHANGE_SCROLL (default off) the
Item Mall sells one scroll for 500 silk; using it in the world queues the
name typed into the client's entry box, and the authority store applies
it before the character list is answered at the next login
(store character_rename.go owns the apply, the 30-day cooldown and the
90-day hold on the old name). Nothing here renames mid-session: identity
is ID-keyed end to end, and the live session keeps the name it entered
with.

The scroll reuses a shipped item, ITEM_MALL_REINFORCE_WEAPON_INC_1_SCROLL
(ref 3789, TypeIDs 3/3/13/11): no rename item exists in the v1.150 data,
a synthetic item could never render in the client, and this one is named
by no family, sold by no shipped listing and dropped nowhere, so the
flag alone decides whether it can be obtained (the authored mall
package) and used (this file). Off is native: the item keeps its
unsupported-use answer and the mall lists nothing extra.

===========================================================================
*/

package action

import (
	"os"
	"strings"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// EnvNameChangeScroll gates the whole feature: the mall listing and the
// scroll's use. Off (the default) is the native game.
const EnvNameChangeScroll = "SRO_NAME_CHANGE_SCROLL"

// The mall row and the donor item (see the file banner). The price is
// the owner's 500 silk; the tab is the shipped mall's scroll tab.
const (
	NameChangeScrollItem  = "ITEM_MALL_REINFORCE_WEAPON_INC_1_SCROLL"
	NameChangeMallPackage = "PACKAGE_ITEM_MALL_CHARACTER_RENAME"
	NameChangeMallTab     = "MALL_CONSUME_SCROLL"
	NameChangePriceSilk   = 500
)

// 0xB5BD failure bytes for the scroll's own refusals (port-only: the
// native client maps no text to these; the browser client renders its
// own lines for them). 0xF5..0xFD is a run the native item-error table
// leaves entirely unmapped (0x92..0x95, by contrast, are native texts),
// so the bytes mean the scroll's refusals and nothing else. The state
// refusals precede the queue door's, so a player in a refused state is
// told before any name is judged.
const (
	errCodeRenameInvalidName   uint8 = 0xF5
	errCodeRenameTaken         uint8 = 0xF6
	errCodeRenameReserved      uint8 = 0xF7
	errCodeRenameAlreadyQueued uint8 = 0xF8
	errCodeRenameCooldown      uint8 = 0xF9
	errCodeRenameGuildWar      uint8 = 0xFA
	errCodeRenameFortressRole  uint8 = 0xFB
	errCodeRenameJobSuit       uint8 = 0xFC
	errCodeRenameInParty       uint8 = 0xFD
)

/*
================
NameChangeScrollFromEnv

The flag accepts "1", "on" and "true" (PharaohTombEnabled's idiom);
anything else, including unset, is off.
================
*/
func NameChangeScrollFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvNameChangeScroll))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
Runtime.nameChangeStateRefusal

The states the owner refused a queue from: dressed for a job, in a
party, in a guild at war, or holding a fortress position. Read live at
use; the apply at login re-checks the guild war, the one state that can
begin while the character is offline (store character_rename.go).
Zero means the queue may proceed.
================
*/
func (rt *Runtime) nameChangeStateRefusal(divisionID string, character *enterworld.Character, nowMs int64) uint8 {
	if enterworld.DressedJob(character) != 0 {
		return errCodeRenameJobSuit
	}
	if rt.rewardPartyOf(divisionID, enterworld.ObjectIDForCharacter(character)) != nil {
		return errCodeRenameInParty
	}
	if character.GuildID == nil {
		return 0
	}
	guildID := *character.GuildID
	if rt.GuildWars != nil {
		for _, war := range rt.GuildWars.Wars(divisionID, guildID) {
			if war.EndMs == 0 || war.EndMs > nowMs {
				return errCodeRenameGuildWar
			}
		}
	}
	if authority := rt.deps.GuildAuthority(); authority != nil {
		if _, members, ok := authority.Guild(divisionID, guildID); ok {
			for _, member := range members {
				if member.CharID == character.ID && member.FortressRole != 0 {
					return errCodeRenameFortressRole
				}
			}
		}
	}
	return 0
}

/*
================
Runtime.admitNameChangeUse

The item-use closure's scroll arm: judge the request and answer with
the name the queue door should hold, or "" with the refusal already in
result. The tail is the raw name bytes the entry box sent; the store
door revalidates the native 2..12 ASCII shape, so anything untoward
that survives the length screen still refuses as an invalid name.

Nothing is spent here: the queue door takes the store's own lock and
must run outside this character door, so HandleItemUse queues first and
spends second (finishNameChangeUse). Both halves run inside the one
request under the division lock, so the inventory cannot move between
them.
================
*/
func (rt *Runtime) admitNameChangeUse(divisionID string, character *enterworld.Character, tail []byte, nowMs int64, result *OpResult) string {
	if len(tail) < 1 || len(tail) > domain.CharacterNameMaxBytes {
		*result = itemUseFailure(errCodeRenameInvalidName)
		return ""
	}
	// Player-chosen names pass the native name filter (namefilter.go),
	// here as at creation and for job aliases and pet names; the apply
	// at login screens again through the same list (characters.go).
	if !rt.nameAllowed(string(tail)) {
		*result = itemUseFailure(errCodeRenameInvalidName)
		return ""
	}
	if code := rt.nameChangeStateRefusal(divisionID, character, nowMs); code != 0 {
		*result = itemUseFailure(code)
		return ""
	}
	return string(tail)
}

/*
================
Runtime.finishNameChangeUse

Queue the rename, then spend the scroll. A queue refusal spends nothing
and answers its own byte. The spend re-finds the row by slot and
codename under a fresh character door; if it somehow vanished (the
request is one session under the division lock, so nothing else can
move it) the rename stays queued and the scroll stays unspent - the
player keeps both, and the loud refusal says so.
================
*/
func (rt *Runtime) finishNameChangeUse(divisionID string, character *enterworld.Character, request wire.ItemUseRequest, ref *enterworld.ItemRef, name string, nowMs int64, result *OpResult) {
	outcome, err := rt.deps.QueueCharacterRename(divisionID, character, name, nowMs)
	if err != nil {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		result.DiagnosticRefusal = "item-use: rename queue door failed: " + err.Error()
		return
	}
	switch outcome {
	case enterworld.RenameRefusedInvalidName:
		*result = itemUseFailure(errCodeRenameInvalidName)
		return
	case enterworld.RenameRefusedNameTaken:
		*result = itemUseFailure(errCodeRenameTaken)
		return
	case enterworld.RenameRefusedNameReserved:
		*result = itemUseFailure(errCodeRenameReserved)
		return
	case enterworld.RenameRefusedAlreadyQueued:
		*result = itemUseFailure(errCodeRenameAlreadyQueued)
		return
	case enterworld.RenameRefusedCooldown:
		*result = itemUseFailure(errCodeRenameCooldown)
		return
	}
	consumed := rt.deps.Update(character, "item-use-rename", func() bool {
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
		*result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse,
			Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)}}}
		result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
		return true
	})
	if !consumed {
		log.Warnf("action: rename for %q in %s queued but its scroll row is gone; left unspent", character.Name, divisionID)
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		result.DiagnosticRefusal = "item-use: rename queued but the scroll row vanished before it could be spent"
		return
	}
	rt.publishItemUseVisual(character, ref, 0, result)
}
