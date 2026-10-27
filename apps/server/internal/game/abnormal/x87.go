/*
===========================================================================

x87.go - x87-exact scaling helpers for the abnormal engine

===========================================================================
*/

package abnormal

import "math/big"

func x87(v float64) *big.Float {
	return new(big.Float).SetPrec(64).SetMode(big.ToNearestEven).SetFloat64(v)
}

/*
==================
x87Scale

x87Scale is CRT_ftol((1 - percent/100) * value) evaluated at the x87's
64 significand bits, nearest-even, as the server's grade roll does. Float64 alone can move
an integral result just below a boundary.
==================
*/
func x87Scale(value, percent float64) int32 {
	ratio := x87(0).Quo(x87(percent), x87(100))
	factor := x87(0).Sub(x87(1), ratio)
	result := x87(0).Mul(factor, x87(value))
	integer, _ := result.Int64()
	return int32(integer)
}

// x87Fraction is CRT_ftol(numerator / 100 * value) at x87 precision, the
// element-status level the roll computes.
func x87Fraction(numerator, value float64) int32 {
	ratio := x87(0).Quo(x87(numerator), x87(100))
	result := x87(0).Mul(ratio, x87(value))
	integer, _ := result.Int64()
	return int32(integer)
}
