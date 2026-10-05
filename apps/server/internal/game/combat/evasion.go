/*
===========================================================================

evasion.go - a defender's critical evasion and incoming-damage reduction

Two keeper contributions a defender owns: parameter 0x39 (the evcr magic
option and the Warrior's shield-mastery passive dcri) shrinks the
attacker's critical chance, and odar's incoming reductions (Earth Barrier,
the Rogue's bow-absorb passive) shrink the damage it takes. Both buffs and
passives install them through 594AC0, so the writes have one owner here.

===========================================================================
*/

package combat

import (
	"math"

	"opensro.online/server/internal/game/paramkeeper"
)

// criticalEvasionParameter is the defender's evade-critical keeper (0x39).
const criticalEvasionParameter = 0x39

/*
================
EvadedCriticalRate

SkillCombat_CalculateHitOutcome 58EB64..58EBAF: the attacker's critical
byte over (1 + the defender's parameter 0x39 / 100), truncated to a byte,
as the block chance is cut by the attacker's 0x38 (BlockChance).
================
*/
func EvadedCriticalRate(rate uint8, defender Stats) uint8 {
	evade, _ := defender.Param(criticalEvasionParameter)
	return uint8(int32(math.Trunc(float64(rate) / (float64(evade)/100 + 1))))
}

/*
================
CriticalEvasionWrite

594AC0 0x595DCA..0x595DF4: dcri adds its word to parameter 0x39's flat
channel.
================
*/
func CriticalEvasionWrite(value uint32, source uint32) paramkeeper.Write {
	return paramkeeper.Write{Parameter: criticalEvasionParameter, Channel: paramkeeper.Flat, Source: source, Value: float32(value)}
}

/*
================
IncomingReductionWrites

596004: odar {bits, word} lowers each incoming-damage keeper its bits
select (physical 4 / magical 8, crossed with basic 1 / skill 2:
0xAE..0xB1) by word percent, on the percent-product channel.
================
*/
func IncomingReductionWrites(bits, word uint32, source uint32) []paramkeeper.Write {
	var writes []paramkeeper.Write
	value := float32(-float64(word))
	for _, slot := range [...]struct {
		bits  uint32
		param uint16
	}{{4 | 1, 0xae}, {4 | 2, 0xaf}, {8 | 1, 0xb0}, {8 | 2, 0xb1}} {
		if bits&slot.bits == slot.bits {
			writes = append(writes, paramkeeper.Write{Parameter: slot.param, Channel: paramkeeper.PercentProduct, Source: source, Value: value})
		}
	}
	return writes
}
