/*
===========================================================================

timeditemmodifier.go - timed item stat blocks become instance-owned writes

594AC0 installs hpi/mpi on parameters 3/4, er/hr on 9/11, and stri/inti on
1/2. The common registry removes the same writes at cancellation and expiry.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
)

const (
	itemParamStrength  = 1
	itemParamIntellect = 2
	itemParamMaxHP     = 3
	itemParamMaxMP     = 4
	itemParamEvasion   = 9
	itemParamAccuracy  = 11
)

/*
================
timedItemModifierWrites

Each authored word is unsigned and rounded to float32 at the native call
boundary. Percent-sum and flat are separate channels, not a precomputed bonus.
================
*/
func (rt *Runtime) timedItemModifierWrites(division string, character *enterworld.Character, effect enterworld.SkillTimedEffect) ([]paramkeeper.Write, error) {
	if !effect.Pinned || !effect.ItemProgram {
		return nil, nil
	}
	var writes []paramkeeper.Write
	for _, block := range [...]struct {
		parameter uint16
		value     enterworld.SkillFlatRate
	}{
		{itemParamMaxHP, effect.HP}, {itemParamMaxMP, effect.MP},
		{itemParamEvasion, effect.Evasion}, {itemParamAccuracy, effect.Accuracy},
	} {
		if !block.value.Present {
			continue
		}
		writes = append(writes,
			paramkeeper.Write{Parameter: block.parameter, Channel: paramkeeper.PercentSum, Value: float32(block.value.Percent)},
			paramkeeper.Write{Parameter: block.parameter, Channel: paramkeeper.Flat, Value: float32(block.value.Flat)},
		)
	}
	if effect.Strength.Present || effect.Intellect.Present {
		stats, _, err := rt.playerCombatStats(division, character)
		if err != nil {
			return nil, err
		}
		strength, _ := stats.Param(itemParamStrength)
		intellect, _ := stats.Param(itemParamIntellect)
		boosts, err := combat.StatBoostWrites(
			combat.StatBoost{Present: effect.Strength.Present, Value: effect.Strength.Value, CapPercent: effect.Strength.CapPercent, Current: strength},
			combat.StatBoost{Present: effect.Intellect.Present, Value: effect.Intellect.Value, CapPercent: effect.Intellect.CapPercent, Current: intellect},
		)
		if err != nil {
			return nil, err
		}
		writes = append(writes, boosts...)
	}
	return writes, nil
}
