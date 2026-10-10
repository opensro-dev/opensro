/*
===========================================================================

shieldattack.go - spda: a buff that trades the shield's defense for attack

Flying Heaven Art (SKILL_CH_SWORD_SHIELDPD_*) authors spda {defense %,
attack %}. 594AC0 (5950DD..5951CE) reads the shield's own physical defense
when the buff installs and turns both percents of it into flat writes, so
the trade is fixed for the instance's lifetime.

===========================================================================
*/

package combat

import (
	"math"
	"strconv"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/durability"
	"opensro.online/server/internal/game/paramkeeper"
)

// shieldSocket is equipment socket 7, which 5950FD asks the actor for
// (vtable +0x578). Arrows share it; they carry no defense.
const shieldSocket = 7

/*
================
ShieldPhysicalDefense

The equipped shield's item +0x198: its own physical defense rolled from
variance slot 4 and the per-plus term (CGItemEquip_CalculateBaseStats,
49689B), before magic options, which scale only HR and ER. Reports false
when socket 7 holds no intact shield. 5950FD itself does not test
durability; the broken test only repeats the cast's reqi (58D480 refuses a
shield marked depleted at item +0x190), so it never differs from native.
================
*/
func ShieldPhysicalDefense(character *domain.Character, items enterworld.ItemRefSource) (float64, bool) {
	if character == nil || items == nil {
		return 0, false
	}
	for _, row := range character.MissionInventory {
		if row.Slot != shieldSocket {
			continue
		}
		ref, ok := items.ItemRefByCodename(row.Codename)
		if !ok || ref == nil || ref.Combat == nil || !isBodyProtectorFamily(ref.TypeFlags()) ||
			durability.Broken(ref.TypeFlags(), row.Durability == 0) {
			return 0, false
		}
		varianceBits, err := strconv.ParseUint(row.VarianceBits, 10, 64)
		if err != nil {
			return 0, false
		}
		stats, _, err := deriveItemStats(ref, varianceBits, uint8(clampInt64(row.Plus, 0, math.MaxUint8)))
		if err != nil {
			return 0, false
		}
		return stats.PhysicalDefense, true
	}
	return 0, false
}

/*
================
ShieldAttackWrites

5950DD..5951CE: a = trunc(defense% x PD / 100), b = trunc(attack% x PD / 100)
(the x87 product through _ftol). A positive a lowers physical defense
(parameter 5) by a; a positive b raises physical attack minimum and maximum
(0xD, 0xE) by b. All three go through 4B3510 on channel 0, the flat one.
================
*/
func ShieldAttackWrites(spda enterworld.SkillShieldAttack, shieldDefense float64) []paramkeeper.Write {
	if !spda.Present {
		return nil
	}
	lowered := int64(math.Trunc(float64(spda.DefensePercent) * float64(float32(shieldDefense)) / 100))
	raised := int64(math.Trunc(float64(spda.AttackPercent) * float64(float32(shieldDefense)) / 100))
	var writes []paramkeeper.Write
	if lowered > 0 {
		writes = append(writes, paramkeeper.Write{Parameter: attributePhysicalDefense, Channel: paramkeeper.Flat, Value: -float32(lowered)})
	}
	if raised > 0 {
		writes = append(writes,
			paramkeeper.Write{Parameter: attributePhysicalMin, Channel: paramkeeper.Flat, Value: float32(raised)},
			paramkeeper.Write{Parameter: attributePhysicalMax, Channel: paramkeeper.Flat, Value: float32(raised)})
	}
	return writes
}
