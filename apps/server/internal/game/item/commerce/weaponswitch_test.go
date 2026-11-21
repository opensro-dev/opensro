/*
===========================================================================

weaponswitch_test.go - the weapon switch scroll's listing (port-only)

On, the alchemy shop's elixirs tab sells every scroll row at its price
after the elixirs and the powder, and the mall's scroll tab at its silk;
off, the shops are today's.

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
TestWeaponSwitchScrollListsOnTheElixirsTab
================
*/
func TestWeaponSwitchScrollListsOnTheElixirsTab(t *testing.T) {
	t.Setenv(EnvShopExtraGoods, "on")
	t.Setenv(EnvJanganAlchemyShop, "on")
	t.Setenv(EnvWeaponSwitchScroll, "on")
	c := janganCatalog(t)
	elixirs := c.Tabs[c.tabIDs[janganElixirsTab]]
	last := elixirs[len(elixirs)-1]
	scroll := WeaponSwitchScrolls[0]
	if last.Ref.Codename != scroll.Codename || last.Ref.RefObjID != 3790 {
		t.Fatalf("elixirs tab ends with %s, want the scroll", last.Ref.Codename)
	}
	if last.Price != scroll.Price || last.Currency != PaymentGold {
		t.Fatalf("scroll price = %d in currency %d, want %d gold", last.Price, last.Currency, scroll.Price)
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
		if _, ok := WeaponSwitchScrollByCodename(good.Codename); ok {
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
	scroll := WeaponSwitchScrolls[0]
	dir := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(dir)
	catalog, err := LoadMall(dir, WeaponSwitchMallGoods(), false, items)
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
