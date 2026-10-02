/*
===========================================================================

critical.go - native critical probability and damage outcome

===========================================================================
*/

package combat

import (
	"fmt"
	"math"
	"opensro.online/server/internal/game/enterworld"
)

// Probability is the per-actor, per-skill accumulator in v1.188 599CC0.
// Copy it when planning; publish the next value only after a successful roll.
// It belongs to the actor lifetime, not a cast, target, or persisted character.
/*
================
Probability
================
*/
type Probability struct {
	Initialized bool
	Threshold   int32
}

// CriticalOutcome follows 58E800..58E848, 5A1A20 and 599CC0. Native converts
// Param12 to a byte. The inclusive rand()%101 comparison and accumulating
// threshold are intentional: independent percentage rolls are not equivalent.
/*
================
CriticalOutcome
================
*/
func CriticalOutcome(rate float64, previous Probability, roll Roll32767) (bool, Probability, error) {
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 || rate > math.MaxInt32 {
		return false, previous, fmt.Errorf("combat: invalid critical rate %v", rate)
	}
	chance := int32(uint8(int32(rate)))
	if chance == 0 {
		return false, previous, nil
	}
	if chance >= 100 {
		return true, previous, nil
	}
	if roll == nil {
		return false, previous, fmt.Errorf("combat: random source is unavailable")
	}
	value, err := roll()
	if err != nil {
		return false, previous, fmt.Errorf("combat: critical random source: %w", err)
	}
	if value > 32767 {
		return false, previous, fmt.Errorf("combat: random source returned %d outside 0..32767", value)
	}
	threshold := previous.Threshold
	if !previous.Initialized {
		threshold = chance
	}
	critical := int32(value%101) <= threshold
	threshold += chance
	if critical {
		threshold -= 100
	}
	return critical, Probability{Initialized: true, Threshold: threshold}, nil
}

// ResolveOutcome keeps outcome selection upstream of both damage lanes. A
// critical doubles physical damage before level/balance/minimum-floor logic
// (40E67C..40E696); 40E830 has no corresponding magical critical multiplier.
/*
================
ResolveOutcome
================
*/
func ResolveOutcome(attacker, defender Stats, attack enterworld.SkillAttack, roll Roll32767, player, critical bool) (Result, error) {
	return ResolveCalculation(attacker, defender, AttackCalculation{
		Attack: attack, OriginalFlags: attack.Flags, Lanes: attack.Flags & (physicalAttackFlag | magicalAttackFlag), Player: player, Critical: critical,
	}, roll)
}
