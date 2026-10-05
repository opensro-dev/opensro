/*
===========================================================================

fixeddamage.go - the hits whose damage is a word, not an att lane

Formulae_CalculateFixedSkillDamage (40F5F0, pdmg) and
Formulae_CalculateLifeSteal (40F750, lfst). Neither reads defense, parry
or the STR/INT balance; both are cut by the target's level advantage at
2.5 percent a level, never below a tenth of their base.

===========================================================================
*/

package combat

import "math"

const (
	// levelAdvantageCut is the 2.5 percent a level both formulas take.
	levelAdvantageCut = 2.5
	// fixedDamageFloorPercent is 40F5F0's base * 10 / 100.
	fixedDamageFloorPercent = 10
	// lifeStealFloor is 40F750's float32 0.1.
	lifeStealFloor = float32(0.1)
)

/*
================
levelAdvantageCutOf

(1 - gap * 2.5 / 100) * base, truncated, for a defender above the attacker;
false when the defender is not above.
================
*/
func levelAdvantageCutOf(base float64, attackerLevel, defenderLevel uint8) (int64, bool) {
	gap := int32(defenderLevel) - int32(attackerLevel)
	if gap <= 0 {
		return 0, false
	}
	return int64(math.Trunc((1 - float64(gap)*levelAdvantageCut/100) * base)), true
}

/*
================
FixedSkillDamage

40F5F0: the pdmg amount plus the caster's power addend; against a higher
level, the advantage cut held at base * 10 / 100.
================
*/
func FixedSkillDamage(amount, power uint32, attackerLevel, defenderLevel uint8) uint32 {
	base := int64(amount) + int64(power)
	cut, higher := levelAdvantageCutOf(float64(float32(base)), attackerLevel, defenderLevel)
	if !higher {
		return uint32(base)
	}
	floor := base * fixedDamageFloorPercent / 100
	return uint32(min(max(cut, floor), base))
}

/*
================
LifeSteal

40F750: base is lfst + the caster's power addend + the mwhs weapon share.
The advantage cut is floored at trunc(base * 0.1f); a cut or a floor above
the target's HP takes exactly that HP. Otherwise the victim's area percent
scales the result, truncated.
================
*/
func LifeSteal(base int64, attackerLevel, defenderLevel uint8, targetHP uint32, percent uint32) uint32 {
	value := base
	if cut, higher := levelAdvantageCutOf(float64(base), attackerLevel, defenderLevel); higher {
		value = cut
	}
	floor := int64(math.Trunc(float64(base) * float64(lifeStealFloor)))
	if value > int64(targetHP) || floor > int64(targetHP) {
		return targetHP
	}
	value = max(value, floor)
	return uint32(max(0, int64(math.Trunc(float64(int64(percent)*value)/100))))
}
