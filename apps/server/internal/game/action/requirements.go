package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

// equipRequirements builds the per-operation character-vs-itemdata equip
// gate (inventory.EquipRequirements): the server-authority mirror of the
// native client's full-mask requirement bits (CSOItem_Query_789C60). Data sources:
//
//   - the character's level/gender/race off the bound enterworld.Character
//     (the authority store rows);
//   - the item's requirement columns off the typed ItemRef fields
//     (itemdata media columns 14/33/58/59/60).
//
// A nil item source or a missing itemdata row passes - the documented
// degraded no-textdata boot, same as the socket resolver's behavior.
func equipRequirements(
	items enterworld.ItemRefSource,
	character *enterworld.Character,
	fortressRole FortressGuildRoleResolver,
) inventory.EquipRequirements {
	if items == nil || character == nil {
		return nil
	}
	return &characterEquipRequirements{
		items:        items,
		character:    character,
		fortressRole: fortressRole,
	}
}

type characterEquipRequirements struct {
	items        enterworld.ItemRefSource
	character    *enterworld.Character
	fortressRole FortressGuildRoleResolver
}

// characterLevel coerces the pointer stat with the same fallback the
// bootstrap snapshot uses: an absent persisted level reads as 1. The
// result is clamped to the native storage width: the client keeps the
// player level in ONE byte (client 789C60 reads it as a u8 off player
// +0x820), so a hypothetical persisted level > 255 must compare as the
// u8 ceiling here, never as a wider value no native client could hold.
func (r *characterEquipRequirements) characterLevel() int64 {
	if r.character.Level == nil || *r.character.Level < 1 {
		return 1
	}
	return clampToU8(*r.character.Level)
}

// clampToU8 pins a compare input to the native u8 storage width (0..255).
// Only the CHARACTER-side levels are clamped; the quad requirement VALUES
// stay unclamped because native reads those as u32 dwords (+0xd8..+0xe4).
func clampToU8(v int64) int64 {
	if v < 0 {
		return 0
	}
	if v > 0xff {
		return 0xff
	}
	return v
}

// The native selector/country mappings are the shared bootstrap helpers
// (NativeSexSelector1AC / NativeCountryByte9C) - the same values
// ResolveLocalPlayerEntry publishes to the client entry, so the equip gates
// can never drift from what the character visually IS.

// quadWalk mirrors the native ANY-of typed-quad walk shared by the level
// (bit 0x020) and mastery (bit 0x100) legs of client 789C60: the flag starts
// set; a participating slot whose compared level >= the slot's value
// passes the WHOLE bit immediately (the native early exit), a
// participating slot below its value clears the flag and the walk
// continues, and non-participating slots (wrong type - or a missing
// mastery record, native's null-continue) never touch the flag.
// levelFor answers (comparableLevel, participates) per slot type.
// Returns true when the bit REFUSES.
func quadWalk(ref *enterworld.ItemRef, levelFor func(quadType int64) (int64, bool)) bool {
	pass := true
	for slot := 0; slot < 4; slot++ {
		level, participates := levelFor(ref.ReqQuadTypes[slot])
		if !participates {
			continue
		}
		if level >= ref.ReqQuadValues[slot] {
			return false // native early exit: the whole bit passes
		}
		pass = false
	}
	return !pass
}

// PreExclusivity runs the native pre-0x080 requirement bits in evaluation
// order: razed (0x004), STR (0x008), INT (0x010), level (0x020),
// mastery (0x100), gender (0x040).
func (r *characterEquipRequirements) PreExclusivity(item inventory.Item) *inventory.Fault {
	ref, ok := r.items.ItemRefByCodename(item.Codename)
	if !ok || ref == nil {
		return nil
	}

	// Bit 0x004 (client @0x00789cc7): the razed/broken gate. Native refuses when the
	// item instance CARRIES a durability attribute (blob (+0xc0)[5] set)
	// AND its current durability (CSOItem field +0x78) reads 0. The
	// class-level attribute marker is the itemdata Dur_U column (weapons/
	// armor > 0, accessories 0), so a durability-less accessory at
	// Durability 0 passes exactly like native.
	if ref.MaxDurability > 0 && item.Durability == 0 {
		return inventory.NewFault(wire.ErrCodeCantEquipRazed, "razedItem")
	}

	// Bits 0x008/0x010: player STR/INT (native +0x834/+0x836 words) >= ref
	// ReqStr/ReqInt (+0x1b0/+0x1b4). The character stats are authoritative
	// record fields; read helpers use the creation base for detached
	// fixtures. The shipped itemdata carries 0
	// in every ReqStr/ReqInt cell, so these only fire on custom data.
	if ref.RequiredStr > 0 && enterworld.CharacterStrength(r.character) < ref.RequiredStr {
		return inventory.NewFault(wire.ErrCodeStrengthRequired, "strengthRequirement")
	}
	if ref.RequiredInt > 0 && enterworld.CharacterIntellect(r.character) < ref.RequiredInt {
		return inventory.NewFault(wire.ErrCodeIntellectRequired, "intellectRequirement")
	}

	// Bit 0x020: the typed-quad LEVEL walk, native semantics (ida
	// client @0x00789d26..0x00789d6c): flag starts 1; for each quad slot whose
	// ReqLevelTypeN dword == 1 exactly, player level >= ReqLevelN passes
	// the WHOLE bit immediately (ANY-of early exit), a miss clears the
	// flag and the walk continues.
	//
	// History: until the mastery plane landed, the server gated ReqLevel1
	// unconditionally as an approximation, because EU armor carries
	// mastery types (513..518) instead of type 1 and would otherwise have
	// been ungated. With bit 0x100 below now enforcing those mastery
	// tiers (whose values imply the level tier, mastery being
	// level-capped), the level leg returns to the native type==1 walk:
	// CH gear and EU weapons/shields/accessories level-gate here, EU
	// armor mastery-gates below - full native parity, no approximation.
	if refused := quadWalk(ref, func(quadType int64) (int64, bool) {
		if quadType != 1 {
			return 0, false
		}
		return r.characterLevel(), true
	}); refused {
		return inventory.NewFault(wire.ErrCodeLevelRequired, "levelRequirement")
	}

	// Bit 0x100: the typed-quad MASTERY walk (client @0x00789d7c..0x00789e14),
	// same ANY-of shape over slots whose type dword > 0x0a - those dwords
	// ARE masterydata ids (EU 513..518). Native detail mirrored exactly:
	// a MISSING mastery record is a continue (no flag write), not a
	// refusal - characters carry their full racial mastery set from
	// creation, so a miss
	// only happens cross-race, where the country bit refuses anyway.
	// There is no dedicated 01:xx notice for this refusal; the client's
	// own full-mask callers show the generic equip guide, so the server
	// answers ErrCodeCantEquip (0x0e).
	if refused := quadWalk(ref, func(quadType int64) (int64, bool) {
		if quadType <= 0x0a {
			return 0, false
		}
		// Native reads the trained mastery level as a u8 (mastery
		// record byte +4), so the compare input clamps to that storage
		// width like the player level above.
		level, ok := enterworld.MasteryLevel(r.character, uint32(quadType))
		return clampToU8(level), ok
	}); refused {
		return inventory.NewFault(wire.ErrCodeCantEquip, "masteryRequirement")
	}

	// Bit 0x040: RequiredSex 2 never refuses (unisex); otherwise it must
	// equal the character's selector (native charRecord+0x1ac == ref[0x6b]).
	if ref.RequiredSex != 2 && ref.RequiredSex != int64(enterworld.NativeSexSelector1AC(r.character)) {
		return inventory.NewFault(wire.ErrCodeGenderMismatch, "genderMismatch")
	}

	return nil
}

// FortressGuildRoleResolver answers the character's fortress-guild role byte
// (native FortressMgr member record +0x5c: 1 commander, 2 sub-commander,
// 4 battle, 8 production, 0x10 training, 0x20 engineer) and whether a
// member record exists at all. A nil resolver means no member record, which
// is the native pass posture.
type FortressGuildRoleResolver func(character *enterworld.Character) (uint8, bool)

// PostExclusivity runs the native post-0x080 bits in evaluation order:
// country (0x200), fortress-guild role (0x400).
func (r *characterEquipRequirements) PostExclusivity(item inventory.Item) *inventory.Fault {
	ref, ok := r.items.ItemRefByCodename(item.Codename)
	if !ok || ref == nil {
		return nil
	}

	// Bit 0x200: Country 3 never refuses; otherwise it must equal the
	// character's country byte (native ref[0x27] vs charBody+0x9c).
	if ref.Country != 3 && ref.Country != int64(enterworld.NativeCountryByte9C(r.character)) {
		return inventory.NewFault(wire.ErrCodeCountryMismatch, "countryMismatch")
	}

	// Bit 0x400 (client @0x00789fd7): only fortress siege weapons (TID
	// 3.1.6.16, client 5504C0) enter the leg, and the gate is SOFT - a
	// missing fortress-guild member record passes; the refusal fires only
	// when a member record EXISTS and its role byte shares no bit with
	// the item's role mask low byte (ref +0x2ac; the 14 shipped rows all
	// carry -1 = any role). No dedicated notice pair exists; like the
	// mastery bit, refusals answer the generic equip guide.
	if inventory.IsFortressWeaponTypeFlags(item.TypeFlags) {
		role, member := uint8(0), false
		if r.fortressRole != nil {
			role, member = r.fortressRole(r.character)
		}
		if member {
			if role&uint8(ref.FortressRoleMask) == 0 {
				return inventory.NewFault(wire.ErrCodeCantEquip, "fortressRoleMismatch")
			}
		}
	}

	return nil
}
