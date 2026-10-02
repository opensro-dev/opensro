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
pmhp (Life Control) takes pmdg's form on the maximum-HP parameter.
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
		// pmhp mirrors pmdg's negated percentage product on the maximum.
		writes = append(writes,
			paramkeeper.Write{Parameter: attributeMaxHP, Channel: paramkeeper.PercentProduct, Value: float32(-float64(a.HPPenaltyPercent))})
	}
	if a.DamagePenalty {
		writes = append(writes,
			paramkeeper.Write{Parameter: attributePhysicalOutgoing, Channel: paramkeeper.PercentProduct, Value: float32(-float64(a.PhysicalPenalty))},
			paramkeeper.Write{Parameter: attributeMagicalOutgoing, Channel: paramkeeper.PercentProduct, Value: float32(-float64(a.MagicalPenalty))})
	}
	return writes
}
