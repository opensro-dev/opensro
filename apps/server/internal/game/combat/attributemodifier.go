/*
===========================================================================

attributemodifier.go - timed maximum-health, attack and outgoing-damage writes

The effect registry owns these contributions and removes them together. Keep
native keeper channels explicit: a flat attack gain is not a percentage.

===========================================================================
*/

package combat

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
)

const (
	attributeMaxHP            = 3
	attributePhysicalDefense  = 5
	attributeMagicalDefense   = 6
	attributePhysicalMin      = 0x0d
	attributePhysicalMax      = 0x0e
	attributeMagicalMin       = 0x0f
	attributeMagicalMax       = 0x10
	attributePhysicalOutgoing = 0xb2
	attributeMagicalOutgoing  = 0xb3
)

/*
================
AttributeEffectWrites

595481 installs hpi; 595C4E installs apau; 5961E3 installs pmdg mode two.
59613F installs pmhp's percentage on maximum HP through channel 1.
Convert the unsigned operand before negation, matching the x87 path.
================
*/
func AttributeEffectWrites(a enterworld.SkillAttributeBoost) []paramkeeper.Write {
	var writes []paramkeeper.Write
	if a.MaxHP {
		writes = append(writes,
			paramkeeper.Write{Parameter: attributeMaxHP, Channel: paramkeeper.Flat, Value: float32(a.HPFlat)},
			paramkeeper.Write{Parameter: attributeMaxHP, Channel: paramkeeper.PercentSum, Value: float32(a.HPPercent)})
	}
	if a.Attack {
		for _, lane := range [...]struct {
			parameter uint16
			value     uint32
		}{
			{attributePhysicalMin, a.PhysicalAttack}, {attributePhysicalMax, a.PhysicalAttack},
			{attributeMagicalMin, a.MagicalAttack}, {attributeMagicalMax, a.MagicalAttack},
		} {
			writes = append(writes, paramkeeper.Write{Parameter: lane.parameter, Channel: paramkeeper.Flat, Value: float32(lane.value)})
		}
	}
	if a.MaxHPPenalty {
		// 59615D pushes channel 1, not pmdg's channel 2. Other HP
		// percentage bonuses must add to this penalty before multiplication.
		writes = append(writes,
			paramkeeper.Write{Parameter: attributeMaxHP, Channel: paramkeeper.PercentSum, Value: float32(-float64(a.HPPenaltyPercent))})
	}
	if a.DefensePenalty {
		// 0x596279 / 0x5962AC push channel 1 with the negated percents.
		writes = append(writes,
			paramkeeper.Write{Parameter: attributePhysicalDefense, Channel: paramkeeper.PercentSum, Value: float32(-float64(a.PhysicalDefensePenalty))},
			paramkeeper.Write{Parameter: attributeMagicalDefense, Channel: paramkeeper.PercentSum, Value: float32(-float64(a.MagicalDefensePenalty))})
	}
	if a.DamagePenalty {
		writes = append(writes,
			paramkeeper.Write{Parameter: attributePhysicalOutgoing, Channel: paramkeeper.PercentProduct, Value: float32(-float64(a.PhysicalPenalty))},
			paramkeeper.Write{Parameter: attributeMagicalOutgoing, Channel: paramkeeper.PercentProduct, Value: float32(-float64(a.MagicalPenalty))})
	}
	return writes
}

// The evasion (ER) and hit rate (HR) parameters a hostile debuff lowers.
const (
	attributeEvasionRate = 9
	attributeHitRate     = 0x0b
)

/*
================
HostileDebuffWrites

594AC0 59591A..5959A0: an enemy-cast buff's terd writes minus its word to
evasion (9) and thrd to hit rate (0xB), each on the flat channel.
================
*/
func HostileDebuffWrites(evasion, hitRate uint32) []paramkeeper.Write {
	var writes []paramkeeper.Write
	if evasion != 0 {
		writes = append(writes, paramkeeper.Write{Parameter: attributeEvasionRate, Channel: paramkeeper.Flat, Value: -float32(evasion)})
	}
	if hitRate != 0 {
		writes = append(writes, paramkeeper.Write{Parameter: attributeHitRate, Channel: paramkeeper.Flat, Value: -float32(hitRate)})
	}
	return writes
}
