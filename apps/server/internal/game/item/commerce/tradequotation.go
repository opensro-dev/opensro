/*
===========================================================================

tradequotation.go - the native trade goods quotation arithmetic

The server's shop-entry refresh determines a foreign trader's unit
price. Its thief path values a quantity with signed 32-bit multiplication
first.
These functions own arithmetic only: admission, owner identity, stock
commit, tax and rewards belong to the transaction owner.

===========================================================================
*/
package commerce

import (
	"fmt"
	"math"
)

const (
	quotationStockRate    = 0.01
	quotationRateScale    = 100.0
	thiefValueNumerator   = 3
	thiefValueDenominator = 4
)

/*
================
TradeQuotation

_ItemQuotation's float32 rates and signed stock fields. Keep the authored
float32 boundaries: rounding only the final price changes whole gold units.
================
*/
type TradeQuotation struct {
	Base      float32 `json:"base"`
	Lower     float32 `json:"lower"`
	Upper     float32 `json:"upper"`
	BaseStock int32   `json:"baseStock"`
	Step      int32   `json:"step"`
	Stock     int32   `json:"stock"`
}

/*
================
TradeQuotationPrice

The reference server's quotation price, with x87 precision 53. Explicit
float32 stores match the native FSTP stores through the computation. FISTP
truncates rate*100 before dividing by 100;
the decompiler's apparent x/x expression loses that live FPU-stack value.
The returned word preserves native ADD overflow. Invalid authority is an
error rather than entering the native minidump paths.
================
*/
func TradeQuotationPrice(basePrice uint32, q TradeQuotation) (uint32, error) {
	if basePrice == 0 || q.BaseStock <= 0 || q.Step <= 0 || q.Stock < 0 {
		return 0, fmt.Errorf("trade quotation: invalid price or stock")
	}
	for _, value := range []float32{q.Base, q.Lower, q.Upper} {
		if value <= 0 || math.IsInf(float64(value), 0) || math.IsNaN(float64(value)) {
			return 0, fmt.Errorf("trade quotation: invalid rate")
		}
	}
	if q.Lower > q.Upper {
		return 0, fmt.Errorf("trade quotation: reversed bounds")
	}
	delta := float32(float64(q.Stock-q.BaseStock) / float64(q.Step) * quotationStockRate)
	rate := min(q.Upper, max(q.Lower, float32(float64(q.Base)-float64(delta))))
	scaled := math.Trunc(float64(rate) * quotationRateScale)
	if scaled > math.MaxInt32 {
		return 0, fmt.Errorf("trade quotation: rate exceeds native signed conversion")
	}
	rounded := float32(scaled / quotationRateScale)
	base := float64(float32(basePrice))
	product := float64(float64(rounded) * base)
	change := math.Trunc(product - base)
	price := basePrice + uint32(int64(change))
	if price == 0 {
		return 0, fmt.Errorf("trade quotation: zero native price")
	}
	return price, nil
}

/*
================
ThiefGoodsValue

The reference server's thief-goods value performs IMUL followed by CDQ
before multiplying by 0.75. Do not widen before multiplication or round
each unit before multiplying.
The sale owner clamps nonpositive credit and profit after this calculation.
================
*/
func ThiefGoodsValue(basePrice uint32, quantity uint16) (credit, profit int64) {
	profit = int64(int32(basePrice * uint32(quantity)))
	return profit * thiefValueNumerator / thiefValueDenominator, profit
}
