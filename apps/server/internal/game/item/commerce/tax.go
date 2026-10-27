package commerce

import (
	"math"
	"math/big"
)

// Tax is an authority-supplied merchant adjustment, never request data.
// Precision identifies the evidenced x87 control-word precision. Keeping it
// explicit prevents silently substituting exact rational division for FDIV.
type Tax struct {
	Percent   int16
	Exempt    bool
	Precision uint
}

// AdjustPrice follows 5D67F0: signed percent / 100, multiply, truncate toward
// zero, then add for purchases or subtract for sales. Round each arithmetic
// operation, just as x87 does. FILD itself loads the signed integer exactly.
func AdjustPrice(base uint64, tax Tax, purchase bool) (uint64, bool) {
	if base > math.MaxInt64 {
		return 0, false
	}
	if tax.Percent == 0 || tax.Percent > 0 && tax.Exempt {
		return base, true
	}
	if tax.Precision != 24 && tax.Precision != 53 && tax.Precision != 64 {
		return 0, false
	}
	makeFloat := func() *big.Float { return new(big.Float).SetPrec(tax.Precision).SetMode(big.ToNearestEven) }
	rate := makeFloat().Quo(new(big.Float).SetInt64(int64(tax.Percent)), new(big.Float).SetInt64(100))
	amount := makeFloat().Mul(rate, new(big.Float).SetPrec(64).SetUint64(base))
	delta, _ := amount.Int(nil)
	value := new(big.Int).SetUint64(base)
	if purchase {
		value.Add(value, delta)
	} else {
		value.Sub(value, delta)
	}
	if value.Sign() < 0 || !value.IsInt64() {
		return 0, false
	}
	return value.Uint64(), true
}
