/*
===========================================================================

preparedcost.go - the MP a cast is charged

The execution context snapshots the cost when the cast is prepared
(58312C persistent, 5867DC instant). The server runs its FPU with
FPCW 0x27F (53-bit precision), so the x87 stack is IEEE double here.

===========================================================================
*/

package combat

import (
	"math"

	"opensro.online/server/internal/game/enterworld"
)

/*
==================
PreparedCost

flat + percent of current, then a player's parameter 8D (native default
100) as a final percentage. Persistent casts divide on the x87 stack before
multiplying; instant casts wrap the percent product with 32-bit IMUL.
This is the charged snapshot, not admission's maximum-vital check.
==================
*/
func PreparedCost(current, flat uint32, percent uint16, persistent, player bool, remainingPercent float32) int32 {
	var ratio uint32
	if persistent {
		ratio = fistpLow(float64(percent) / 100 * float64(int32(current)))
	} else {
		ratio = uint32(int32(current*uint32(percent)) / 100)
	}

	value := flat + ratio
	if player {
		value = fistpLow(float64(remainingPercent) / 100 * float64(value))
	}
	return int32(value)
}

/*
==================
ApplyMPDecrease

The WIMD / BDMD / HLMD cuts on a prepared MP cost:
instant 5868F1 / 58695F / 5869CD, persistent 58327A / 5832E6 / 583352.

For each modifier the skill asks for (getv), the caster's value v
(CSkillManager_GetSkillModifier 5A0330) gives

	cost = trunc(unsigned(cost) * (1 - unsigned(v) / 100))

FISTP keeps the low dword, so v > 100 wraps; there is no clamp. A modifier
the caster lacks reads 0 and leaves the cost alone. Projectile casts read
none of these.

58436B and 58535D are actor vfunc +0x55C calls, not HLMD. 58528A scales
only BDMD onto the vfunc +0x10C amount before vfunc +0x308: the same cut,
taken when the persistent context is rebuilt.
==================
*/
func ApplyMPDecrease(cost int32, mask enterworld.SkillParameterMask, values enterworld.SkillParameterValues) int32 {
	cuts := [...]enterworld.SkillParameter{
		enterworld.ParameterWizardMPDecrease,
		enterworld.ParameterBardMPDecrease,
		enterworld.ParameterHealerMPDecrease,
	}
	for _, slot := range cuts {
		if !mask.Has(slot) {
			continue
		}
		cost = CutMPCost(cost, values[slot])
	}
	return cost
}

/*
==================
CutMPCost

One percent cut of a prepared MP cost, the 5868F1 arithmetic every MP
Decrease key uses: cost x (1 - percent / 100), stored by FISTP.
==================
*/
func CutMPCost(cost int32, percent uint32) int32 {
	factor := 1 - float64(percent)/100
	return int32(fistpLow(factor * float64(uint32(cost))))
}

// fistpLow is FISTP qword under truncation, keeping the low dword. An
// out-of-range value stores the integer indefinite, whose low dword is 0.
func fistpLow(v float64) uint32 {
	if math.IsNaN(v) || v >= 1<<63 || v < -(1<<63) {
		return 0
	}
	return uint32(int64(v))
}
