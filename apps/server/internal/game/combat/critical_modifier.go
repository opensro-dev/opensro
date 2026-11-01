package combat

import (
	"fmt"
	"math"

	"opensro.online/server/internal/game/enterworld"
)

// EffectiveCriticalRate ports the server's critical-rate modifier. Truncate the base to a byte
// BEFORE multiplying the percentage. Flat is added AFTER that multiplication.
// Both additions wrap at eight bits; clamping to 100 changes native outcomes.
// Keep division and multiplication separate, with 53-bit round-to-nearest
// arithmetic. All shipped v1.150 cr percentage words are zero, independent of
// x87 precision. Nonzero percentage parity would additionally require
// qualifying the native x87 environment.
func EffectiveCriticalRate(base float64, modifier enterworld.SkillCriticalModifier) (uint8, error) {
	if math.IsNaN(base) || math.IsInf(base, 0) || base < 0 || base > math.MaxInt32 {
		return 0, fmt.Errorf("combat: invalid critical rate %v", base)
	}
	rate := uint8(int32(base))
	if !modifier.Present {
		return rate, nil
	}
	percentage := float64(modifier.Percent) / 100.0
	bonus := float64(percentage * float64(rate))
	// Native FISTP int32 returns integer-indefinite on masked overflow. Its
	// low byte is zero. Do not rely on host float-to-int overflow behavior.
	var bonusByte uint8
	if bonus < 2147483648.0 {
		bonusByte = uint8(int32(bonus))
	}
	return rate + uint8(modifier.Flat) + bonusByte, nil
}
