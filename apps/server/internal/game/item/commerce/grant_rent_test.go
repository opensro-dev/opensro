/*
===========================================================================

grant_rent_test.go - a delivered rental carries its deletion time (#4240)

===========================================================================
*/
package commerce

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
)

/*
================
TestGrantPackageStampsTheRentalClock

A rented content stamps now plus its period onto the delivered item; a
rented content granted without a clock refuses (it would ship
permanently), and a plain content stamps nothing.
================
*/
func TestGrantPackageStampsTheRentalClock(t *testing.T) {
	ref := &enterworld.ItemRef{RefObjID: 777, Codename: "ITEM_RENT_TEST", TypeIDs: [4]int64{3, 1, 13, 1}}
	rented := []Content{{Ref: ref, Stack: 1, RentMinutes: 1440}}
	inv := inventory.New(nil, 40)
	if _, err := GrantPackage(inv, rented, 1, 27, GrantMarks{NowUnix: 1_800_000_000}); err != nil {
		t.Fatal(err)
	}
	items := inv.Items()
	if len(items) != 1 || items[0].RentExpiresUnix != 1_800_000_000+1440*60 {
		t.Fatalf("delivered = %+v, want the one-day deadline stamped", items)
	}
	if _, err := GrantPackage(inventory.New(nil, 40), rented, 1, 27, GrantMarks{}); err == nil ||
		!strings.Contains(err.Error(), "without a clock") {
		t.Fatalf("clockless rental grant = %v, want the refusal", err)
	}
	plain := []Content{{Ref: ref, Stack: 1}}
	inv = inventory.New(nil, 40)
	if _, err := GrantPackage(inv, plain, 1, 27, GrantMarks{NowUnix: 1_800_000_000}); err != nil {
		t.Fatal(err)
	}
	if got := inv.Items(); len(got) != 1 || got[0].RentExpiresUnix != 0 {
		t.Fatalf("plain delivery = %+v, want no deadline", got)
	}
}
