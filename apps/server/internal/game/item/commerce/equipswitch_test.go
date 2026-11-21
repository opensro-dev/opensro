/*
===========================================================================

equipswitch_test.go - the equipment switch scrolls' listing (port-only)

On, the mall's scroll tab sells each scroll at its silk, and never the
alchemy shop; off, the shops are today's.

===========================================================================
*/
package commerce

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
TestWeaponSwitchListsOnlyInMall

The scroll sells in the Item Mall alone, at 1490 silk (owner,
2026-11-21): the alchemy shop's elixirs tab never lists it.
================
*/
func TestWeaponSwitchListsOnlyInMall(t *testing.T) {
	t.Setenv(EnvShopExtraGoods, "on")
	t.Setenv(EnvJanganAlchemyShop, "on")
	t.Setenv(EnvWeaponSwitchScroll, "on")
	goods, err := ExtraGoodsFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	for _, good := range goods {
		if good.Codename == "ITEM_MALL_WEAPON_SWITCH_SCROLL" {
			t.Fatalf("the alchemy shop lists the scroll: %+v", good)
		}
	}
	if mall := EquipSwitchMallGoods(map[EquipSwitchKind]bool{SwitchWeapon: true}); len(mall) != 1 || mall[0].Silk != 1490 {
		t.Fatalf("mall goods %+v, want one at 1490 silk", mall)
	}
}

/*
================
TestWeaponSwitchScrollOffListsNothing
================
*/
func TestWeaponSwitchScrollOffListsNothing(t *testing.T) {
	t.Setenv(EnvShopExtraGoods, "on")
	t.Setenv(EnvJanganAlchemyShop, "on")
	t.Setenv(EnvWeaponSwitchScroll, "")
	goods, err := ExtraGoodsFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	for _, good := range goods {
		if _, ok := EquipSwitchScrollByCodename(good.Codename); ok {
			t.Fatalf("flag off still lists %s", good.Codename)
		}
	}
}

/*
================
TestWeaponSwitchScrollListsInTheMall

The authored package reaches the mall catalogue at the row's silk.
================
*/
func TestWeaponSwitchScrollListsInTheMall(t *testing.T) {
	tier.Long(t, "the shipped mall data plus the weapon switch package")
	scroll := EquipSwitchScrolls[0]
	dir := gamedatatest.TextdataDir(t)
	shipped := enterworld.NewTextdataItems(dir)
	// The native test data carries no variant rows: the scroll's item is
	// its donor's row under its own codename, as the variant import
	// stages it (scripts/switch_scroll_import.py).
	donor, ok := shipped.ItemRefByCodename("ITEM_MALL_REINFORCE_WEAPON_INC_2_SCROLL")
	if !ok {
		t.Fatal("shipped donor missing")
	}
	item := *donor
	item.Codename, item.RefObjID = scroll.Codename, 290001
	items := oneMoreItem{ItemRefSource: shipped, item: &item}
	catalog, err := LoadMall(dir, EquipSwitchMallGoods(map[EquipSwitchKind]bool{SwitchWeapon: true}), false, items)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Hydrate(items); err != nil {
		t.Fatal(err)
	}
	for _, offer := range catalog.Offers {
		if offer.Code != scroll.MallPackage {
			continue
		}
		if offer.Silk != scroll.Silk || len(offer.Scraps) != 1 {
			t.Fatalf("mall offer %+v, want one scroll at %d silk", offer, scroll.Silk)
		}
		return
	}
	t.Fatalf("mall does not list %s", scroll.MallPackage)
}

/*
================
oneMoreItem

An item table plus one item.
================
*/
type oneMoreItem struct {
	enterworld.ItemRefSource
	item *enterworld.ItemRef
}

/*
================
oneMoreItem.ItemRefByCodename
================
*/
func (s oneMoreItem) ItemRefByCodename(codename string) (*enterworld.ItemRef, bool) {
	if codename == s.item.Codename {
		return s.item, true
	}
	return s.ItemRefSource.ItemRefByCodename(codename)
}

/*
================
oneMoreItem.ItemRefByID
================
*/
func (s oneMoreItem) ItemRefByID(id uint32) (*enterworld.ItemRef, bool) {
	if id == s.item.RefObjID {
		return s.item, true
	}
	return s.ItemRefSource.ItemRefByID(id)
}

/*
================
TestEquipSwitchMallGoodsFollowFlags

Each scroll lists only while its own flag is on.
================
*/
func TestEquipSwitchMallGoodsFollowFlags(t *testing.T) {
	goods := EquipSwitchMallGoods(map[EquipSwitchKind]bool{SwitchArmor: true})
	want := MallExtraGood{Tab: "MALL_CONSUME_SCROLL", Package: "PACKAGE_ITEM_MALL_ARMOR_SWITCH",
		Item: "ITEM_MALL_ARMOR_SWITCH_SCROLL", Silk: 990}
	if len(goods) != 1 || goods[0] != want {
		t.Fatalf("armor-only goods %+v, want %+v", goods, want)
	}
	all := EquipSwitchMallGoods(map[EquipSwitchKind]bool{SwitchWeapon: true, SwitchArmor: true, SwitchAccessory: true})
	if len(all) != 3 || all[2].Item != "ITEM_MALL_ACCESSORY_SWITCH_SCROLL" || all[2].Silk != 990 ||
		all[0].Silk != 1490 {
		t.Fatalf("all goods %+v", all)
	}
}
