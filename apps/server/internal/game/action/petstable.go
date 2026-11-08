/*
===========================================================================
petstable.go - port-only basic pet equipment in each town's stable Pet tab.

The expansion flag owns this stock. Rolled blues and sealed gear remain loot.
===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/item/commerce"
)

/*
================
petStableBand
================
*/
type petStableBand struct {
	tab         string
	id          int32
	first, last int
}

/*
================
petStableBands

Native refshoptab IDs and Pet labels; town bands approved by the owner.
================
*/
func petStableBands() [5]petStableBand {
	return [5]petStableBand{
		{"STORE_CH_STABLE_TAB3", 1920, 1, 3},
		{"STORE_EU_STABLE_TAB3", 2030, 1, 3},
		{"STORE_WC_STABLE_TAB3", 1942, 3, 5},
		{"STORE_CA_STABLE_TAB3", 2052, 3, 5},
		{"STORE_KT_STABLE_TAB3", 1970, 5, 9},
	}
}

/*
================
petStableGoods

Reuse the pet reference price (5000 * degree squared) and ordinary gold offers.
================
*/
func petStableGoods() []commerce.ExtraGood {
	var goods []commerce.ExtraGood
	for _, shop := range petStableBands() {
		for _, degree := range companion.D.Gear {
			if degree.Degree < shop.first || degree.Degree > shop.last {
				continue
			}
			for _, item := range degree.Items {
				goods = append(goods, commerce.ExtraGood{Tab: shop.tab, Codename: item.Codename,
					Price: uint64(5000 * degree.Degree * degree.Degree)})
			}
		}
	}
	return goods
}

/*
================
petStableOffer

Only a plain, unsealed single item in its town's degree band is shop stock.
================
*/
func petStableOffer(tab int32, offer commerce.Offer) bool {
	if offer.Ref == nil || companion.SealTier(offer.Ref.RefObjID) != 0 || offer.Currency != commerce.PaymentGold || len(offer.Contents) != 1 {
		return false
	}
	content := offer.Contents[0]
	if content.Ref != offer.Ref || content.Plus != 0 || content.Variance != 0 || len(content.Magic) != 0 {
		return false
	}
	degree, slot, ok := companion.GearByID(offer.Ref.RefObjID)
	if !ok || degree.Items[slot].ID != offer.Ref.RefObjID {
		return false
	}
	for _, shop := range petStableBands() {
		if shop.id == tab {
			return degree.Degree >= shop.first && degree.Degree <= shop.last
		}
	}
	return false
}
