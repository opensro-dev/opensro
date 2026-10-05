/*
===========================================================================

skillweapon.go - which weapon a skill admits

The weapon half of TargetValidation's equipment check (58D480): a racial
base attack is recognized from the seed catalog, a skill without reqi
compares its two authored weapon kinds against the equipped loadout, and
the bow and crossbow are the weapons that spend ammunition.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
isPinnedBaseAttack

Resolve the racial seed catalog instead of inferring base attacks from IDs.
================
*/
func isPinnedBaseAttack(character *enterworld.Character, codename string) bool {
	for _, candidate := range enterworld.DefaultSkillCodenames(
		enterworld.ResolveCharacterRaceKey(character),
	) {
		if candidate == codename {
			return true
		}
	}
	return false
}

/*
==================
skillWeaponAdmitted

skillWeaponAdmitted is the weapon half of 58D480: a skill with reqi pairs
is judged by reqiRefusal alone and never reads its weapon kinds
(RefSkill+0xC7/+0xC8); only a skill without reqi compares them.
==================
*/
func skillWeaponAdmitted(loadout combat.Loadout, skill enterworld.SkillRow) bool {
	return skill.Reqi.Present || loadoutMatchesSkill(loadout, skill.RequiredWeaponKinds)
}

/*
================
loadoutMatchesSkill

The authored two-slot weapon requirement includes the bare-hand sentinel.
================
*/
func loadoutMatchesSkill(loadout combat.Loadout, kinds [2]uint8) bool {
	if kinds == [2]uint8{0xff, 0xff} {
		return true
	}
	for _, kind := range kinds {
		switch {
		case kind == 0xff:
			continue
		case kind == 1 && !loadout.HasWeapon:
			return true
		case loadout.HasWeapon && kind == loadout.WeaponKind:
			return true
		}
	}
	return false
}

/*
================
weaponRequiresAmmunition

Only bows and crossbows consume the shared secondary-equipment ammunition.
================
*/
func weaponRequiresAmmunition(kind uint8) bool {
	// RefItemData TID4 6 is the Chinese bow family; 12 is the European
	// crossbow family. Inventory's native socket map places TID 3.3.4
	// arrows/bolts in the shared secondary-equipment socket for those two.
	return kind == 6 || kind == 12
}
