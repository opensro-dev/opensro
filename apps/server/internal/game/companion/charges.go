/*
===========================================================================
charges.go - port-only persistent, sequential recharge for stored pet casts.

Projection is pure: reads never reset a running deadline or award offline time
twice. The casting/owner tick transaction commits the returned value.
===========================================================================
*/
package companion

import "opensro.online/server/internal/domain"

const BroodSkill = "Stormbrood"
const BroodLimit = 10
const ChargeKind = "Charges"

/*
================
AdvanceCharges
================
*/
func AdvanceCharges(row domain.PetRowCooldown, now int64) domain.PetRowCooldown {
	if row.Capacity == 0 || row.RechargeMs <= 0 {
		return row
	}
	row.Charges = min(row.Charges, row.Capacity)
	if row.Charges >= row.Capacity {
		row.ReadyAtMs = 0
		return row
	}
	if row.ReadyAtMs <= 0 {
		row.ReadyAtMs = now + row.RechargeMs
		return row
	}
	if now >= row.ReadyAtMs {
		count := min(int64(row.Capacity-row.Charges), 1+(now-row.ReadyAtMs)/row.RechargeMs)
		row.Charges += uint8(count)
		row.ReadyAtMs += count * row.RechargeMs
		if row.Charges == row.Capacity {
			row.ReadyAtMs = 0
		}
	}
	return row
}

/*
================
BroodRow

One initial cast; further charges must actually recharge. Switching books
preserves the running deadline instead of granting new casts.
================
*/
func BroodRow(row domain.PetRowCooldown, skill SkillDef, level uint8, now int64, mods Modifiers) domain.PetRowCooldown {
	capacity := uint8(1)
	if skill.Option >= 2 {
		capacity = 3
	}
	period := int64((600 - float64(min(uint8(10), max(uint8(1), level))-1)*300/9) * 1000 * max(0, 100-mods.CooldownPct) / 100)
	period = max(int64(1), period)
	if row.Capacity == 0 {
		row = domain.PetRowCooldown{Kind: ChargeKind, Capacity: capacity, Charges: 1, RechargeMs: period}
	} else {
		row = AdvanceCharges(row, now)
		row.Capacity = max(row.Capacity, capacity)
		row.RechargeMs = period
	}
	return AdvanceCharges(row, now)
}

/*
================
RecoverPetCooldowns

Only timed reuse is affected; attacks/hits/kills and live summon durations are not.
================
*/
func RecoverPetCooldowns(pet *domain.CharacterCOS, now, amount int64) bool {
	changed := false
	for i, row := range pet.Training.Rows {
		if row.ReadyAtMs <= now {
			continue
		}
		row.ReadyAtMs = max(now, row.ReadyAtMs-amount)
		pet.Training.Rows[i] = AdvanceCharges(row, now)
		changed = true
	}
	return changed
}
