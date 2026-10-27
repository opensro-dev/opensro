/*
===========================================================================

potionamount.go - potion HP / MP amounts

===========================================================================
*/

package action

import (
	"math"

	"opensro.online/server/internal/game/enterworld"
)

/*
==================
potionAmount

potionAmount is the native recovery amount formula: one
potion's HP and MP amounts before any gauge clamps them. The v1.150 client
never predicts these amounts, so the server rule is the v1.188 one.

A nonzero Param1 (HP) or Param3 (MP) selects the absolute arm, which scales
with the keeper's effective STR/INT and the level curve:

	amount = ftol((stat/416 + 1) * 1.02^(level-1) * param)

Otherwise Param2/Param4 are percentages of the keeper maxima, stored to
float32 before the multiply. absolute reports the arm; it selects the
reuse-lock duration.
==================
*/
type potionAmount struct {
	hp, mp   int64
	absolute bool
}

func computePotionAmount(ref *enterworld.ItemRef, level uint8, strength, intellect float64, maxHP, maxMP int64) (potionAmount, bool) {
	for _, value := range []float64{ref.RecoveryHP, ref.RecoveryHPPercent, ref.RecoveryMP, ref.RecoveryMPPercent} {
		// Shipped params are non-negative ints; anything else is malformed
		// reference data and fails closed.
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > math.MaxInt32 {
			return potionAmount{}, false
		}
	}
	hpBase, mpBase := int32(ref.RecoveryHP), int32(ref.RecoveryMP)
	if hpBase != 0 || mpBase != 0 {
		growth := levelGrowth(level)
		return potionAmount{
			hp:       crtFtol((strength/416.0 + 1) * growth * float64(hpBase)),
			mp:       crtFtol((intellect/416.0 + 1) * growth * float64(mpBase)),
			absolute: true,
		}, true
	}
	out := potionAmount{}
	if pct := int32(ref.RecoveryHPPercent); pct != 0 {
		out.hp = crtFtol(float64(maxHP) * float64(float32(float64(pct)/100.0)))
	}
	if pct := int32(ref.RecoveryMPPercent); pct != 0 {
		out.mp = crtFtol(float64(maxMP) * float64(float32(float64(pct)/100.0)))
	}
	return out, true
}

// levelGrowth is the native inline 1.02^(level-1): binary exponentiation on
// the x87 stack, stored to a double before use.
func levelGrowth(level uint8) float64 {
	exponent := int(level) - 1
	if exponent < 0 {
		exponent = -exponent
	}
	base, result := 1.02, 1.0
	for exponent != 0 {
		if exponent&1 != 0 {
			result *= base
		}
		exponent >>= 1
		base *= base
	}
	if int(level)-1 < 0 {
		return 1 // native divides 1.0 by itself for a negative exponent
	}
	return result
}

// crtFtol is the C runtime's float-to-int conversion: truncation toward zero, with the native
// out-of-range result for NaN or overflow.
func crtFtol(v float64) int64 {
	if math.IsNaN(v) || v >= 2147483648 || v < -2147483648 {
		return math.MinInt32
	}
	return int64(int32(v))
}
