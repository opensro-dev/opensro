/*
===========================================================================

weaponthreat.go - the physical weapon term of a damage-free taunt

The reference adds this term to aggression only. HP, contribution rewards and
the transmitted damage remain zero.

===========================================================================
*/

package combat

import (
	"math"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
PhysicalAbsorptionRatio

410A00 reads parameter one and the actor's level. Its denominator constant
is the double representation of float32(6/7), not an exact rational.
================
*/
func PhysicalAbsorptionRatio(level uint8, strength float32) float32 {
	offset := float64(level) - 1
	ratio := float32((float64(strength) + 16 + 2*offset) / ((7*offset + 56) * float64(float32(6.0/7.0))))
	if ratio < 0 {
		return 0
	}
	if !(ratio < float32(1.2)) {
		return float32(1.2)
	}
	return ratio
}

/*
================
TauntAggression

5903EC..590589: tnt2, then pwtt (411000), then keeper BB. The signed weapon
conversion and final unsigned low dword are separate native truncations.
================
*/
func TauntAggression(stats Stats, minimum, maximum float32, threat enterworld.SkillThreat) uint32 {
	strength, _ := stats.Param(1)
	ratio := PhysicalAbsorptionRatio(stats.Level, strength)
	average := float32((float64(minimum) + float64(maximum)) * 0.5)
	bonus := float64(average) * float64(ratio) * (float64(threat.WeaponPercent) / 100)
	weapon := int32(math.MinInt32)
	if !math.IsNaN(bonus) && bonus >= math.MinInt32 && bonus < 1<<31 {
		weapon = int32(bonus)
	}
	aggression := AccumulateThreat(0, 0, threat) + uint32(weapon)
	percent, _ := stats.Param(0xbb)
	return uint32(uint64(float64(aggression) * (float64(percent)/100 + 1)))
}
