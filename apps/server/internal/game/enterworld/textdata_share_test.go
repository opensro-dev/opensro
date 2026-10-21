/*
===========================================================================

textdata_share_test.go - one shared item parse per process, private edits

===========================================================================
*/
package enterworld

import (
	"testing"

	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestItemLoadersShareOneParseAndCopyBeforeChanging

Two loaders over one directory adopt the same parse, and a shard setting
applied to one (stack sizes, prices, ride speeds) never reaches the other.
================
*/
func TestItemLoadersShareOneParseAndCopyBeforeChanging(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	first, second := NewTextdataItems(dir), NewTextdataItems(dir)
	a, okA := first.ItemRefByCodename("ITEM_ETC_HP_POTION_01")
	b, okB := second.ItemRefByCodename("ITEM_ETC_HP_POTION_01")
	if !okA || !okB {
		t.Fatal("the shipped HP potion is missing")
	}
	if a != b {
		t.Fatal("two loaders over one directory must adopt one parse")
	}
	native := b.NativeStackCap()
	sizes, err := ParseStackSizes("potion=2000")
	if err != nil {
		t.Fatal(err)
	}
	if raised, err := first.ApplyStackSizes(sizes); err != nil || raised == 0 {
		t.Fatalf("raise: %d %v", raised, err)
	}
	raised, _ := first.ItemRefByCodename("ITEM_ETC_HP_POTION_01")
	if raised == b || raised.NativeFields.Get("maxStack") != 2000 {
		t.Fatalf("the raising loader must read its own raised copy: %v", raised.NativeFields.Get("maxStack"))
	}
	if b.NativeFields.Get("maxStack") != float64(native) {
		t.Fatalf("another loader's row changed: %v, want %d", b.NativeFields.Get("maxStack"), native)
	}
	again, _ := NewTextdataItems(dir).ItemRefByCodename("ITEM_ETC_HP_POTION_01")
	if again.NativeFields.Get("maxStack") != float64(native) {
		t.Fatal("the shared parse changed")
	}
}
