package commerce

import (
	"math"
	"opensro.online/server/internal/game/enterworld"
	"strings"
)

// SaleUnitPrice implements the zero-tax v1.150 sale/restore price shared by
// the client's CIFInventory_OpenSellConfirmModal and
// CIFInventory_OpenRepurchaseConfirmModal. +C4 is the signed sellPrice
// reference field. CSOItem_CheckEquipBindings78B4A0 substitutes one gold
// for equipment with exhausted repair permission.
// This does not claim fortress tax exemption or x87 taxed-price parity.
func SaleUnitPrice(ref *enterworld.ItemRef, magic []uint64, source enterworld.MagicOptionSource) (uint64, bool) {
	price, _, ok := salePriceBasis(ref, magic, source)
	return price, ok
}

// Tax and repair policy apply to a unit before stack multiplication in v1.150.
// Retention is at least the untaxed base (client
// CIFInventory_OpenRepurchaseConfirmModal; the server's NPC sale range),
// not necessarily the credited sale amount. Repair exhaustion overrides tax.
func SalePrices(ref *enterworld.ItemRef, magic []uint64, source enterworld.MagicOptionSource, tax Tax) (credit, restore uint64, ok bool) {
	base, exhausted, valid := salePriceBasis(ref, magic, source)
	if !valid {
		return 0, 0, false
	}
	if exhausted {
		return 1, 1, true
	}
	credit, ok = AdjustPrice(base, tax, false)
	if !ok {
		return 0, 0, false
	}
	return credit, max(base, credit), true
}

func salePriceBasis(ref *enterworld.ItemRef, magic []uint64, source enterworld.MagicOptionSource) (uint64, bool, bool) {
	if ref == nil {
		return 0, false, false
	}
	flags := ref.TypeFlags()
	if flags&2 == 0 && flags&0x1c == 0xc && flags&0x60 == 0x20 {
		for _, encoded := range magic {
			if source == nil {
				return 0, false, false
			}
			m, found := source.MagicOptionByParamID(uint32(uint16(encoded)))
			if !found || m == nil {
				continue
			} // client CSOItem_CheckEquipBindings78B4A0 skips an absent definition.
			if m.OptionName == "MATTR_NOT_REPARABLE" && strings.Contains(m.ParamName, "-") ||
				m.OptionName == "MATTR_REPAIR" && uint32(encoded>>32) <= 1 {
				return 1, true, true
			}
		}
	}
	price, present := ref.NativeFields.Lookup("sellPrice")
	if !present || price < 0 || price > math.MaxInt32 || price != math.Trunc(price) {
		return 0, false, false
	}
	return uint64(price), false, true
}
