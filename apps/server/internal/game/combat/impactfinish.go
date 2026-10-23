/*
===========================================================================

impactfinish.go - the tail every skill impact shares after its formula

The native hit-outcome computation ends the normal impact with one shared
tail, whatever produced the damage word (att lanes, pdmg, life steal, an
HP ratio): the victim's area percent, the monster attacker's damage
scale, and a floor that keeps a landed att impact from dealing nothing.
Every caller reaches it: the instant and projectile actions, the
persistent pulse and skill objects. So the port's single target, area,
pulse, trap, pet and monster strikes all finish here.

A blocked impact (record type 2) and a ck kill (0x86) keep their zero: the
block's damage is dropped, and a kill has none. Neither does a defender
its wall fully covers: that impact becomes record type 8 and skips the
whole tail.

===========================================================================
*/

package combat

import (
	"math"

	"opensro.online/server/internal/game/item/wire"
)

const (
	// fullImpactPercent is a primary victim's (or a single target's) entry
	// byte +5: the whole result.
	fullImpactPercent = 100

	// populationKindParty is the native population kind (the rarity byte's
	// high nibble) for a party monster.
	populationKindParty = 1
	// partyMonsterDamageScale is the native float32 1.3 party-monster scale.
	partyMonsterDamageScale = float32(1.29999995)

	// The base grade (the rarity byte's low nibble) picks the scale arm.
	gradeNeutralScale = 2 // fld1: exactly 1, the population scale ignored
	gradeGiant        = 4
	gradeTitan        = 5
	gradeElite        = 6
	gradeSeven        = 7
	// largeMonsterDamageFactor is the double 1.5, and
	// gradeSevenDamageFactor the double 1.7999999523.
	largeMonsterDamageFactor = 1.5
	gradeSevenDamageFactor   = 1.7999999523162842
)

/*
================
MonsterDamageScale

The native monster damage scale: the population kind gives 1 (kind 0) or
1.3f (kind 1, a party monster);
any other kind logs "Unknown Monster Type" and takes 1. The [0.1, 10]
clamp that follows cannot move either value. The base grade then keeps
that scale (grades 0, 1, 3 and 8), replaces it with 1 (grade 2),
multiplies it by 1.5 (giants, titans and elites: grades 4 to 6) or by
1.7999999523 (grade 7), each product stored as a float32; any grade
above 8 takes 1.
================
*/
func MonsterDamageScale(rarity uint8) float32 {
	base := float32(1)
	if rarity>>4 == populationKindParty {
		base = partyMonsterDamageScale
	}
	switch rarity & 0x0f {
	case 0, 1, 3, 8:
		return base
	case gradeGiant, gradeTitan, gradeElite:
		return float32(float64(base) * largeMonsterDamageFactor)
	case gradeSeven:
		return float32(float64(base) * gradeSevenDamageFactor)
	case gradeNeutralScale:
		return 1
	}
	return 1
}

/*
================
scaleDamageExact

fild the damage, fmulp by the scale on the x87 stack and fistp under
truncation. A 32-bit damage times a 24-bit mantissa fits the
x87 64-bit mantissa, so the product is exact; the integer product here is
exact too, then truncated by the exponent shift.
================
*/
func scaleDamageExact(damage uint32, scale float32) uint32 {
	bits := math.Float32bits(scale)
	exponent := int(bits >> 23 & 0xff)
	mantissa := uint64(bits & 0x7fffff)
	if exponent == 0 || exponent == 0xff || bits>>31 != 0 {
		// Never produced by the native scale: a scale is positive and normal.
		return damage
	}
	mantissa |= 1 << 23
	shift := exponent - 127 - 23
	product := uint64(damage) * mantissa
	if shift >= 0 {
		product <<= uint(shift)
	} else {
		product >>= uint(-shift)
	}
	return uint32(min(product, uint64(wire.MaxSkillActionDamage)))
}

/*
================
ImpactTail

What the shared tail needs about one impact. Percent is the victim's
entry byte +5 (0 reads as the whole result); PercentApplied marks a life
steal record, whose percent already rode into its amount (the native flow
skips the percent step for it). MonsterAttacker and AttackerRarity
describe the attacker and its rarity byte. Attack marks the skill's att
block: only an att impact takes the floor. Covered marks a defender whose
standing wall covers every lane of the impact
(combat.WallOutcome.Covered): its record skips the tail, so the wall's
absorption leaves it at zero.
================
*/
type ImpactTail struct {
	Percent         uint64
	PercentApplied  bool
	MonsterAttacker bool
	AttackerRarity  uint8
	Attack          bool
	Covered         bool
}

/*
================
FinishImpact

The shared tail on one normal impact: the percent (trunc(percent *
damage / 100)), then a monster attacker's scale, then a zero att damage
becomes 1. A non-att zero stays zero (an effect-only result when the
skill carries a status, otherwise nothing). The lane accumulators keep
their pre-scaling values, as the native records do.
================
*/
func FinishImpact(r Result, tail ImpactTail) Result {
	if r.Blocked || r.Slain || tail.Covered {
		return r
	}
	percent := tail.Percent
	if percent == 0 {
		percent = fullImpactPercent
	}
	if !tail.PercentApplied && percent != fullImpactPercent {
		r.Damage = uint32(uint64(r.Damage) * percent / fullImpactPercent)
	}
	if tail.MonsterAttacker {
		r.Damage = scaleDamageExact(r.Damage, MonsterDamageScale(tail.AttackerRarity))
	}
	if r.Damage == 0 && tail.Attack {
		r.Damage = 1
	}
	return r
}
