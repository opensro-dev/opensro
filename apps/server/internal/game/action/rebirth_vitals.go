/*
===========================================================================

rebirth_vitals.go - the beginner's native present-position recovery

Town reentry restores its full gauges. The level-ten concession instead adds
one HP and forty percent of the keeper maxima, retaining the corpse's mana.
Recovery uses the shared reduction and saturation rules used by skill healing.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
)

// 51019E/5101D4 load this widened float from the double at B460F0.
const presentRebirthRecoveryFraction = float64(float32(0.4))

/*
================
presentRebirthVitals

4DF49F adds one HP through reduced recovery, leaving MP unchanged. The
51019C/5101D2 getters return float32 keeper values; x87 multiplies those by
the widened 0.4f constant and truncates before 4A86A0 reduces and adds them.
Keep the floating maxima for the multiplication, rather than rounding the
maximum down before calculating the recovery.
================
*/
func (rt *Runtime) presentRebirthVitals(division string, character *enterworld.Character) (hp, mp int64) {
	maxHP, maxMP, _, currentMP := rt.playerKeeperVitals(division, character)
	hpMaximum, mpMaximum := float32(maxHP), float32(maxMP)
	var hpReduction, mpReduction float32
	if stats, _, err := rt.playerCombatStats(division, character); err == nil {
		if value, ok := stats.Param(3); ok && value > 0 {
			hpMaximum = value
		}
		if value, ok := stats.Param(4); ok && value > 0 {
			mpMaximum = value
		}
		hpReduction, _ = stats.Param(combat.HPRecoveryReductionParameter)
		mpReduction, _ = stats.Param(combat.MPRecoveryReductionParameter)
	}
	hpAmount := int64(float64(hpMaximum) * presentRebirthRecoveryFraction)
	mpAmount := int64(float64(mpMaximum) * presentRebirthRecoveryFraction)
	hp = combat.RecoverVital(0, maxHP, 1, hpReduction)
	hp = combat.RecoverVital(hp, maxHP, hpAmount, hpReduction)
	mp = combat.RecoverVital(currentMP, maxMP, mpAmount, mpReduction)
	return hp, mp
}
