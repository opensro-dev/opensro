/*
===========================================================================
petstable_test.go - town stock bands and unsealed, blue-free pet purchases.
===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestPetStableStockAndPurchase
================
*/
func TestPetStableStockAndPurchase(t *testing.T) {
	if len(petStableGoods()) != 51 {
		t.Fatal("missing town stock")
	}
	for _, shop := range petStableBands() {
		for _, d := range companion.D.Gear {
			for _, g := range d.Items {
				rt, c := merchantFixture(t)
				rt.AttackPetExpansion = true
				ref := &enterworld.ItemRef{RefObjID: g.ID, Codename: g.Codename, Name: g.Name, TypeIDs: [4]int64{3, 3, 9, 0}, NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 1})}
				rt.deps.ItemReferences().(staticItemSource)[g.Codename] = ref
				offer := commerce.Offer{Slot: 3, Ref: ref, Price: 1, Currency: commerce.PaymentGold, Stack: 1, Contents: []commerce.Content{{Ref: ref, Stack: 1}}}
				rt.NpcRoster[0].NpcTalkStoreGroups[0].Tabs[0].TabID = shop.id
				rt.Commerce.Tabs[shop.id] = []commerce.Offer{offer}
				want := d.Degree >= shop.first && d.Degree <= shop.last
				if petStableOffer(shop.id, offer) != want {
					t.Fatalf("wrong degree band %s D%d", shop.tab, d.Degree)
				}
				trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopBuy, NpcGID: 17, ShopSlot: 3, Quantity: 1})
				item, bought := bagItem(c, g.Codename)
				if bought != want || bought && len(item.MagicOptions) != 0 {
					t.Fatalf("purchase %s D%d bought=%v", shop.tab, d.Degree, bought)
				}
				offer.Contents[0].Magic = []uint64{companion.EncodeAffix(84, 5)}
				if petStableOffer(shop.id, offer) {
					t.Fatal("stable admitted rolled blues")
				}
				offer.Contents[0].Magic = nil
				ref.RefObjID += 10
				if petStableOffer(shop.id, offer) {
					t.Fatal("stable admitted sealed gear")
				}
			}
		}
	}
}
