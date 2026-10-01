/*
===========================================================================

mallpurchase.go - authoritative merchandise identity and currency arithmetic

Only catalogue data can choose prices, contents and quantity limits. Requests
carry a native address and optional point contribution, never an account ID.

===========================================================================
*/
package commerce

import (
	"fmt"
	"math"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
MallQuote

The selected offer remains immutable after catalogue construction. Currency
values have already been widened, multiplied and checked before narrowing.
================
*/
type MallQuote struct {
	Offer    *MallPackage
	Cost     domain.MallBalance
	Quantity uint16
}

/*
================
Quote

6BFD00 limits point contribution to the multiplied Silk cost. Reject invalid
wire values here instead of applying the UI's convenience clamp to a request.
================
*/
func (catalog *MallCatalog) Quote(q wire.MallPurchase) (MallQuote, error) {
	if catalog == nil || q.Quantity == 0 {
		return MallQuote{}, fmt.Errorf("mall: unavailable purchase")
	}
	address := MallAddress{Group: q.Group, Shop: q.Shop, Tab: q.Tab, Slot: q.Slot}
	for index := range catalog.Offers {
		offer := &catalog.Offers[index]
		if offer.MallAddress != address {
			continue
		}
		if offer.PackageID != q.Package || q.Package == 0 || offer.PurchaseLimit == 0 || q.Quantity > offer.PurchaseLimit {
			return MallQuote{}, fmt.Errorf("mall: stale package or invalid quantity")
		}
		silk := uint64(offer.Silk) * uint64(q.Quantity)
		gift := uint64(offer.GiftSilk) * uint64(q.Quantity)
		if silk > math.MaxUint32 || gift > math.MaxUint32 || uint64(q.Points) > silk || q.Points != 0 && !offer.AllowsPoints {
			return MallQuote{}, fmt.Errorf("mall: invalid currency contribution")
		}
		return MallQuote{Offer: offer, Quantity: q.Quantity, Cost: domain.MallBalance{Silk: uint32(silk) - q.Points, GiftSilk: uint32(gift), Points: q.Points}}, nil
	}
	return MallQuote{}, fmt.Errorf("mall: unknown merchandise address")
}
