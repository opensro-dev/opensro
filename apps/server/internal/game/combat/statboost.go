/*
===========================================================================

statboost.go - stri / inti parameter writes (skill-parameter installer)

===========================================================================
*/

package combat

import (
	"fmt"
	"math"

	"opensro.online/server/internal/game/paramkeeper"
)

// StatBoost is one stri (parameter 1) or inti (parameter 2) block as the
// recipient receives it: the authored value, the caster's context addend
// (HLFS +2C / HLMI +30) and the recipient's current parameter value.
type StatBoost struct {
	Present           bool
	Value, CapPercent uint32
	Addend            uint32
	Current           float32
}

/*
==================
StatBoostWrites

The installer's stri / inti branch: value = float(addend) + float(word 0). A
nonzero word 1 caps it at word 1 / 100 times the recipient's current value,
truncated to an integer first (the CRT ftol); the smaller of the two is
written to the flat channel.
==================
*/
func StatBoostWrites(strength, intellect StatBoost) ([]paramkeeper.Write, error) {
	var out []paramkeeper.Write
	for i, b := range [2]StatBoost{strength, intellect} {
		if !b.Present {
			continue
		}
		value := float32(float64(b.Addend) + float64(b.Value))
		if b.CapPercent != 0 {
			if math.IsNaN(float64(b.Current)) || math.IsInf(float64(b.Current), 0) || b.Current < 0 || b.Current > math.MaxInt32 {
				return nil, fmt.Errorf("combat: invalid stat boost snapshot")
			}
			cap := float32(float64(b.CapPercent) / 100 * float64(int32(b.Current)))
			if cap < value {
				value = cap
			}
		}
		out = append(out, paramkeeper.Write{Parameter: uint16(1 + i), Channel: paramkeeper.Flat, Value: value})
	}
	return out, nil
}
