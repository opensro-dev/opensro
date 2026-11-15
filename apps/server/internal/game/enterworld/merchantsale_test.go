/*
===========================================================================

merchantsale_test.go - one row opened to the merchants at load

===========================================================================
*/
package enterworld

import (
	"testing"

	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestApplyMerchantSaleOpensTheSocksOnly

The shipped Red Santa Socks stack ten, never trade and never drop, and
merchants refuse them; opening them sets the sale gate and the price and
leaves the rest of the row, and every other row, native.
================
*/
func TestApplyMerchantSaleOpensTheSocksOnly(t *testing.T) {
	const socks = "ITEM_ETC_E071225_SANTASOCKS"
	items := NewTextdataItems(gamedatatest.TextdataDir(t))
	before, ok := items.ItemRefByCodename(socks)
	if !ok {
		t.Fatal("the shipped Red Santa Socks are missing")
	}
	if before.NativeFields.Get("maxStack") != 10 || before.NativeFields.Get("canTrade") != 0 ||
		before.CanDrop || before.NativeFields.Get("canSell") != 0 {
		t.Fatalf("native socks row changed: %v drop=%v", before.NativeFields, before.CanDrop)
	}
	potion, _ := items.ItemRefByCodename("ITEM_ETC_HP_POTION_01")
	potionPrice := potion.NativeFields.Get("sellPrice")
	if opened, err := items.ApplyMerchantSale(socks, 1000); !opened || err != nil {
		t.Fatalf("open: %v %v", opened, err)
	}
	after, _ := items.ItemRefByCodename(socks)
	if after.NativeFields.Get("canSell") != 1 || after.NativeFields.Get("sellPrice") != 1000 ||
		after.NativeFields.Get("maxStack") != 10 || after.NativeFields.Get("canTrade") != 0 || after.CanDrop {
		t.Fatalf("opened socks row: %v drop=%v", after.NativeFields, after.CanDrop)
	}
	if again, _ := items.ItemRefByCodename("ITEM_ETC_HP_POTION_01"); again.NativeFields.Get("sellPrice") != potionPrice {
		t.Fatal("another row changed")
	}
	if opened, err := items.ApplyMerchantSale("ITEM_NOT_A_ROW", 1); opened || err != nil {
		t.Fatalf("a missing row: %v %v", opened, err)
	}
}
