/*
===========================================================================

formula.go - hit damage (the v1.188 CFormulae call tree)

===========================================================================
*/

package combat

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math"
	"opensro.online/server/internal/game/abnormal"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const (
	physicalAttackFlag uint32 = 0x04
	magicalAttackFlag  uint32 = 0x08
	normalResultFlag   uint8  = 0x01
)

// Roll32767 returns one value in the same inclusive 0..32767 domain as the
// CRT rand() calls in the v1.188 helpers.
/*
================
Roll32767
================
*/
type Roll32767 func() (uint32, error)

// SecureRoll32767 is the production RNG. Formula tests inject a deterministic
// sequence; production does not share a mutable pseudo-random generator.
/*
================
SecureRoll32767
================
*/
func SecureRoll32767() (uint32, error) {
	var bytes [2]byte
	for {
		if _, err := rand.Read(bytes[:]); err != nil {
			return 0, err
		}
		value := uint32(binary.LittleEndian.Uint16(bytes[:]))
		if value < 65536-(65536%32768) {
			return value % 32768, nil
		}
	}
}

// Result is one formula evaluation before the monster HP mutation door.
/*
================
Result
================
*/
type Result struct {
	// Imbue is the active weapon imbue's abnormal authority (its bu block),
	// rolled per impact after the skill's own statuses (590B24).
	Imbue  abnormal.SkillParams
	Damage uint32
	// MagicalDamage retains the lane accumulator before total-damage
	// scaling. 58F491 releases Root only when this accumulator is nonzero.
	MagicalDamage uint32
	ResultFlags   uint8
	// Blocked is a type-2 record (0x58F0EF): no damage, no imbue share, no
	// status roll, no knockdown (the defender's +0xD2C is never set).
	Blocked bool
}

/*
==================
Resolve

Resolve ports the normal-result physical and magical CFormulae lanes.
Attack.Flags selects either or both helpers, exactly like the retail
attack-block gate. The learned E2SA channel is consumed only by its physical
getv branch. Other unimplemented modifier planes retain neutral inputs.
==================
*/
func Resolve(
	attacker Stats,
	defender Stats,
	attack enterworld.SkillAttack,
	roll Roll32767,
) (Result, error) {
	return resolve(attacker, defender, attack, roll, true, false)
}

/*
==================
ResolveMonster

ResolveMonster evaluates the non-player attacker branch of the same
v1.188 CFormulae call tree. Monster default-skill `att` rows already carry
their authored attack interval; unlike a CICPlayer they do not multiply
that interval by the STR/INT balance helpers. Keeping this entry point
separate prevents a synthetic zero-STR monster from collapsing to minimum
damage and prevents callers from fabricating player stats to compensate.
==================
*/
func ResolveMonster(
	attacker Stats,
	defender Stats,
	attack enterworld.SkillAttack,
	roll Roll32767,
) (Result, error) {
	return resolve(attacker, defender, attack, roll, false, false)
}

/*
================
resolve
================
*/
func resolve(
	attacker Stats,
	defender Stats,
	attack enterworld.SkillAttack,
	roll Roll32767,
	applyPlayerBalance bool,
	critical bool,
) (Result, error) {
	if !attack.Present {
		return Result{}, fmt.Errorf("combat: skill has no pinned primary attack block")
	}
	if roll == nil {
		return Result{}, fmt.Errorf("combat: random source is unavailable")
	}
	if attack.Percent < 0 || attack.Min < 0 || attack.Max < 0 {
		return Result{}, fmt.Errorf("combat: attack block contains a negative damage input")
	}
	if attack.Flags&(physicalAttackFlag|magicalAttackFlag) == 0 {
		return Result{}, fmt.Errorf(
			"combat: attack flags %#x select neither CFormulae damage lane",
			attack.Flags,
		)
	}

	var total uint64
	var magicalDamage uint32
	if attack.Flags&physicalAttackFlag != 0 {
		damage, err := resolveLane(attacker, defender, attack, roll, false, applyPlayerBalance, critical)
		if err != nil {
			return Result{}, err
		}
		total += uint64(downAttackDamage(damage, defender.MotionState, attack.DownAttack))
	}
	if attack.Flags&magicalAttackFlag != 0 {
		damage, err := resolveLane(attacker, defender, attack, roll, true, applyPlayerBalance, false)
		if err != nil {
			return Result{}, err
		}
		magicalDamage = downAttackDamage(damage, defender.MotionState, attack.DownAttack)
		total += uint64(magicalDamage)
	}
	// 58F52F: atca scales the summed lane dword by (1 + percent/100) when
	// its mask shares a bit with the target's abnormal mask (+0xD34).
	if attack.Atca && defender.AbnormalMask&attack.AtcaMask != 0 {
		total = uint64(fistpLow(float64(uint32(total)) * (1 + float64(attack.AtcaPercent)/100)))
	}
	if total > uint64(wire.MaxSkillActionDamage) {
		total = uint64(wire.MaxSkillActionDamage)
	}
	flags := normalResultFlag
	if critical && attack.Flags&physicalAttackFlag != 0 {
		flags = 2
	}
	if attacker.Berserk {
		flags |= 4
	}
	return Result{Damage: uint32(total), MagicalDamage: magicalDamage, ResultFlags: flags}, nil
}

/*
================
resolveLane
================
*/
func resolveLane(
	attacker Stats,
	defender Stats,
	attack enterworld.SkillAttack,
	roll Roll32767,
	magical bool,
	applyPlayerBalance bool,
	critical bool,
) (uint32, error) {
	attackMin := attacker.PhysicalAttackMin
	attackMax := attacker.PhysicalAttackMax
	defense := defender.PhysicalDefense
	parry := defender.ParryRate
	if magical {
		attackMin = attacker.MagicalAttackMin
		attackMax = attacker.MagicalAttackMax
		defense = defender.MagicalDefense
		parry = defender.MagicalParry
	}
	attackMin += float64(attack.Min)
	attackMax += float64(attack.Max)

	// 59E770: absent MAAT is zero; each absent mastery ID contributes one,
	// otherwise consume the learned level, and choose the larger slot.
	enhancement := float64(masteryEnhancement(attacker.masteries, attack))
	attackMin *= 1 + enhancement/100
	attackMax *= 1 + enhancement/100
	if attackMax < attackMin {
		return 0, fmt.Errorf(
			"combat: attack interval %.3f..%.3f is inverted",
			attackMin,
			attackMax,
		)
	}

	center := hitCenter(attacker, defender)
	spread, err := minimumOfThreePercentRolls(roll)
	if err != nil {
		return 0, err
	}
	direction, err := roll()
	if err != nil {
		return 0, err
	}
	percentile := center + spread
	if direction%2 == 0 {
		percentile = center - spread
	}
	percentile = parameterPercentile(percentile, attacker.SkillParameters, attack.Parameters, magical)
	percentile = clamp(percentile, 0, 100)
	attackPoint := attackMin + (attackMax-attackMin)*percentile/100
	attackPoint = parameterAttackPoint(attackPoint, attacker.SkillParameters, attack.Parameters, magical)
	attackPoint = stealthStrikePoint(attackPoint, attacker, attack.Parameters, magical)

	damage := attackPoint/(parry/100+1) - defense
	if damage < 0 {
		damage = 0
	}
	damage *= float64(attack.Percent) / 100
	// 40E58C/40E59A and 40E934/40E940 select 80..83 after defense
	// subtraction and attack-percent scaling. This is not attack-stat scaling.
	rate := float64(0)
	if attack.Flags&1 != 0 {
		if magical {
			rate = attacker.MagicalBasicRate
		} else {
			rate = attacker.PhysicalBasicRate
		}
	} else if attack.Flags&2 != 0 {
		if magical {
			rate = attacker.MagicalSkillRate
		} else {
			rate = attacker.PhysicalSkillRate
		}
	}
	if rate != 0 {
		damage = float64(float32(damage * (1 + rate/100)))
	}
	// AE..B1 are written with 80..83's lane layout (odar normalizes its
	// bits to {basic, skill} x {physical, magical}); the defender's factor
	// is selected by the same attack kind. The address of this read is not
	// in the port's corpus: it is inferred from the shared layout and the
	// "absorbs damage" text of Earth Barrier and the dara item option.
	taken := float32(0)
	if attack.Flags&1 != 0 {
		taken = defender.PhysicalBasicTaken
		if magical {
			taken = defender.MagicalBasicTaken
		}
	} else if attack.Flags&2 != 0 {
		taken = defender.PhysicalSkillTaken
		if magical {
			taken = defender.MagicalSkillTaken
		}
	}
	if taken != 0 {
		damage = float64(float32(damage * float64(taken)))
	}
	// 58E5F0 body1 sets result bit4; 40E450/40E830 multiply by2
	// before level, balance and minimum-damage fallback.
	if attacker.Berserk {
		damage = float64(float32(damage * 2))
	}
	if critical {
		damage *= 2
	}
	damage *= 1 + levelAdvantage(attacker.Level, defender.Level)
	if applyPlayerBalance {
		if magical {
			damage *= magicalBalance(attacker)
		} else {
			damage *= physicalBalance(attacker)
		}
	}

	// The retail guard compares against 5% of the rolled attack point. If
	// under it, the replacement is uniform from one through a 10% ceiling.
	if damage < attackPoint*0.05 {
		ceiling := attackPoint * 0.10
		if magical {
			ceiling = math.Ceil(ceiling)
		}
		if ceiling < 1 {
			ceiling = 1
		}
		floorRoll, rollErr := normalizedRoll(roll)
		if rollErr != nil {
			return 0, rollErr
		}
		damage = 1 + (ceiling-1)*floorRoll
	}
	// 40E758/40EB0A: modifiers follow the minimum-damage fallback.
	outgoing, incoming := attacker.PhysicalOutgoing, defender.PhysicalIncoming
	if magical {
		outgoing, incoming = attacker.MagicalOutgoing, defender.MagicalIncoming
	}
	for _, factor := range []float32{outgoing, incoming} {
		if factor != 0 {
			damage = float64(float32(damage * float64(factor)))
		}
	}
	damage = clamp(damage, 0, float64(wire.MaxSkillActionDamage))
	return uint32(math.Trunc(damage)), nil
}

/*
================
hitCenter
================
*/
func hitCenter(attacker, defender Stats) float64 {
	ratio := 0.0
	if defender.EvasionRate > 0 {
		ratio = attacker.HitRate / defender.EvasionRate
	} else if attacker.HitRate > 0 {
		ratio = math.Inf(1)
	}
	center := (ratio*0.5 + levelAdvantage(attacker.Level, defender.Level)) * 100
	return clamp(center, 10, 90)
}

/*
================
levelAdvantage
================
*/
func levelAdvantage(attacker, defender uint8) float64 {
	if attacker <= defender {
		return 0
	}
	return math.Min(float64(attacker-defender)*0.03, 0.30)
}

/*
================
physicalBalance
================
*/
func physicalBalance(stats Stats) float64 {
	levelOffset := float64(stats.MaxLevel) - 1
	numerator := stats.Strength + 16 + 2*levelOffset
	denominator := (7*levelOffset + 56) * (6.0 / 7.0)
	if denominator <= 0 {
		return 0
	}
	return clamp(numerator/denominator, 0, 1.2)
}

/*
================
magicalBalance
================
*/
func magicalBalance(stats Stats) float64 {
	levelOffset := float64(stats.MaxLevel) - 1
	denominator := (5*levelOffset + 40) * 0.8
	if denominator <= 0 {
		return 0
	}
	return clamp(stats.Intellect/denominator, 0, 1.2)
}

/*
================
minimumOfThreePercentRolls
================
*/
func minimumOfThreePercentRolls(roll Roll32767) (float64, error) {
	minimum := 100.0
	for range 3 {
		value, err := normalizedRoll(roll)
		if err != nil {
			return 0, err
		}
		value *= 100
		if value < minimum {
			minimum = value
		}
	}
	return minimum, nil
}

/*
================
normalizedRoll
================
*/
func normalizedRoll(roll Roll32767) (float64, error) {
	value, err := roll()
	if err != nil {
		return 0, fmt.Errorf("combat: random source failed: %w", err)
	}
	if value > 32767 {
		return 0, fmt.Errorf("combat: random source returned %d outside 0..32767", value)
	}
	return float64(value) / 32767, nil
}

/*
================
clamp
================
*/
func clamp(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
