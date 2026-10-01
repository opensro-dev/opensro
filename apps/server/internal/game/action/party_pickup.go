/*
===========================================================================

party_pickup.go - native shared-loot recipient selection and private receipts

The party owns its rotation. Action resolves eligibility against detached
live poses, then commits the grant through the chosen character authority.

===========================================================================
*/
package action

import (
	"math"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

const sharedLootRange = 1000
const sharedLootOption uint8 = 2
const (
	questNonItemMask  uint16 = 2
	questClassMask    uint16 = 0x1c
	questClass        uint16 = 0xc
	questCategoryMask uint16 = 0x60
	questCategory     uint16 = 0x60
	questGroupMask    uint16 = 0x780
	questItemBand     uint16 = 0x480
)
const questAlchemyPrefix = "ITEM_QNO_RM_ARCHEMY"

/*
================
partyPickupRecipient

Server 525F5F..52606F: item-share bit, rotating member alive (life byte 1,
525FAE), same packed world, 3D distance <= 1000; fallback is the picker. Quest items stay personal,
except the native alchemy prefix. Rotation advances even on a later refusal.
================
*/
func (rt *Runtime) partyPickupRecipient(division string, picker *enterworld.Character, item grounditem.Item, nowMs int64) *enterworld.Character {
	if rt.NextPartyLootMember == nil {
		return picker
	}
	flags := item.TypeFlags
	quest := flags&questNonItemMask == 0 && flags&questClassMask == questClass && flags&questCategoryMask == questCategory && flags&questGroupMask == questItemBand
	if quest && !strings.HasPrefix(item.Codename, questAlchemyPrefix) {
		return picker
	}
	roster := rt.monsterRewardRoster(division, picker, nowMs)
	origin, found := roster.actors[enterworld.ObjectIDForCharacter(picker)]
	if !found || origin.party == nil || origin.party.Options&sharedLootOption == 0 {
		return picker
	}
	from := monster.Pose{RegionID: origin.pose.RegionID, X: origin.pose.X, Y: origin.pose.Y, Z: origin.pose.Z}
	for range origin.party.Members {
		gid := rt.NextPartyLootMember(division, picker.Name)
		candidate, found := roster.actors[gid]
		if !found || candidate.party == nil || candidate.party.Order != origin.party.Order || candidate.world != origin.world ||
			!enterworld.CharacterAlive(candidate.character) {
			continue
		}
		to := monster.Pose{RegionID: candidate.pose.RegionID, X: candidate.pose.X, Y: candidate.pose.Y, Z: candidate.pose.Z}
		x, y, z := monster.NativeActorRelative(from, to)
		squared := float32(float64(x)*float64(x) + float64(y)*float64(y) + float64(z)*float64(z))
		if math.Sqrt(float64(squared)) <= sharedLootRange {
			return candidate.character
		}
	}
	return picker
}

/*
================
goldShare
================
*/
type goldShare struct {
	character *enterworld.Character
	amount    uint32
}

/*
================
partyGoldShares

CGObjPC_CreditPickedUpGold (4EACA0) and CParty_DistributeGold (5BC7F0): with
the item-share option set, the heap the rotation handed to recipient is split
among the recipient and every party member, in party-list order, who is
online, in the recipient's world and within 1000 of the recipient
(CParty_IsWithinShareRange: 3D, float32 sum of squares, <= 1000.0). Life is
not checked. share = amount / n and the recipient also takes amount % n;
when share is 0 the recipient takes the whole heap. Returns nil when no
split applies (no party, option clear).
================
*/
func (rt *Runtime) partyGoldShares(division string, recipient *enterworld.Character, amount uint32, nowMs int64) []goldShare {
	roster := rt.monsterRewardRoster(division, recipient, nowMs)
	self := enterworld.ObjectIDForCharacter(recipient)
	origin, found := roster.actors[self]
	if !found || origin.party == nil || origin.party.Options&sharedLootOption == 0 {
		return nil
	}
	from := monster.Pose{RegionID: origin.pose.RegionID, X: origin.pose.X, Y: origin.pose.Y, Z: origin.pose.Z}
	var eligible []*enterworld.Character
	for _, gid := range origin.party.Members {
		member, ok := roster.actors[gid]
		if !ok || member.character == nil {
			continue
		}
		if gid != self {
			to := monster.Pose{RegionID: member.pose.RegionID, X: member.pose.X, Y: member.pose.Y, Z: member.pose.Z}
			if member.world != origin.world || !withinGoldShareRange(from, to) {
				continue
			}
		}
		eligible = append(eligible, member.character)
	}
	n := uint32(len(eligible))
	if n == 0 {
		return []goldShare{{recipient, amount}}
	}
	share, remainder := amount/n, amount%n
	if share == 0 {
		return []goldShare{{recipient, amount}}
	}
	out := make([]goldShare, 0, n)
	for _, c := range eligible {
		credit := share
		if c == recipient {
			credit += remainder
		}
		out = append(out, goldShare{c, credit})
	}
	return out
}

/*
================
withinGoldShareRange

CParty_IsWithinShareRange (5BDB30): the squared distance is stored as
float32 before the square root, and an unordered result is refused.
================
*/
func withinGoldShareRange(from, to monster.Pose) bool {
	x, y, z := monster.NativeActorRelative(from, to)
	squared := float32(float64(x)*float64(x) + float64(y)*float64(y) + float64(z)*float64(z))
	return math.Sqrt(float64(squared)) <= sharedLootRange
}

/*
================
routeSharedPickup

Movement/action completion belongs to the picker; inventory, currency and
quest receipts belong to the awarded member. Public scoop/despawn is shared.
================
*/
func routeSharedPickup(result OpResult, picker, recipient *enterworld.Character) OpResult {
	if picker == recipient || len(result.Broadcast) == 0 {
		return result
	}
	var local, private []wire.Frame
	for _, frame := range result.Frames {
		switch frame.Opcode {
		case wire.OpActionState, wire.OpPickupAnim, wire.OpObjectDespawn:
			local = append(local, frame)
		default:
			private = append(private, frame)
		}
	}
	result.Frames = local
	if len(private) > 0 {
		result.Recipients = append(result.Recipients, RecipientFrames{CharacterID: recipient.ID, Frames: private})
	}
	return result
}

/*
================
partyLootNotice

CParty_BroadcastLootNotice (5BDC80): after a granted pickup in a party
whose item-share option is set, every member learns what the recipient got.
Gold never announces: CPlayer_ExecuteGroundPickup keeps the object's
is-gold slot (vtable +0x7C, stored at 5262AA) and skips the call when it
is set (5263C8).
================
*/
func (rt *Runtime) partyLootNotice(division string, picker, recipient *enterworld.Character, item grounditem.Item, nowMs int64) []RecipientFrames {
	if item.IsGold() {
		return nil
	}
	roster := rt.monsterRewardRoster(division, picker, nowMs)
	origin, found := roster.actors[enterworld.ObjectIDForCharacter(picker)]
	if !found || origin.party == nil || origin.party.Options&sharedLootOption == 0 {
		return nil
	}
	amount := uint32(max(item.StackCount, 1))
	// A member that never saw the drop has no reference for it; the native
	// client reads its own item table, so send the reference first.
	frames := append(rt.groundReferences([]grounditem.Item{item}), wire.Frame{
		Opcode:  wire.OpPartyLootNotice,
		Payload: wire.EncodePartyLootNotice(enterworld.ObjectIDForCharacter(recipient), item.RefObjID, item.TypeFlags, amount),
	})
	var out []RecipientFrames
	for _, gid := range origin.party.Members {
		member, ok := roster.actors[gid]
		if !ok || member.character == nil {
			continue
		}
		out = append(out, RecipientFrames{CharacterID: member.character.ID, Frames: frames})
	}
	return out
}
