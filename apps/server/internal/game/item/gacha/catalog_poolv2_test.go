package gacha

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

// The shipped v1.150 pool shape: set 1 entries 1-6 pay the plain Lv76
// weapons at 9/8, set 2 carries the two 1800-weight grain rows 15/16 and
// one 6400-weight filler so the waste weights total 10000.
const poolV2FixturePrizes = "1 1 4140 9 1 1\n" +
	"1 1 4035 8 1 2\n" +
	"1 1 4104 9 1 3\n" +
	"1 1 4071 8 1 4\n" +
	"1 1 4176 9 1 5\n" +
	"1 1 4215 8 1 6\n" +
	"1 2 9 1800 50 15\n" +
	"1 2 16 1800 50 16\n" +
	"1 2 3823 6400 1 26\n"

func poolV2Items() testItems {
	items := fixtureItems()
	for name, id := range map[string]uint32{
		"WEAPON_A": 4140, "WEAPON_B": 4035, "WEAPON_C": 4104,
		"WEAPON_D": 4071, "WEAPON_E": 4176, "WEAPON_F": 4215,
		"GRAIN_A": 9, "GRAIN_B": 16, "FILLER": 3823,
		"ITEM_CH_TBLADE_09_A_RARE": 4146, "ITEM_CH_SWORD_09_A_RARE": 4038,
		"ITEM_CH_SPEAR_09_A_RARE": 4110, "ITEM_CH_BLADE_09_A_RARE": 4074,
		"ITEM_CH_BOW_09_A_RARE": 4182, "ITEM_CH_SHIELD_09_A_RARE": 4218,
		"ITEM_MALL_HWAN": 9259,
	} {
		items[name] = &enterworld.ItemRef{RefObjID: id, Codename: name, TypeIDs: [4]int64{3, 1, 6, 2}}
	}
	// The shipped Hwan is TID 3/3/13/13 with Param1 1 (itemdata row 9259).
	items["ITEM_MALL_HWAN"].TypeIDs = [4]int64{3, 3, 13, 13}
	items["ITEM_MALL_HWAN"].NativeFields = enterworld.NewNativeFields(map[string]float64{"itemParam1_29c": 1})
	return items
}

func loadPoolV2Fixture(t *testing.T, items testItems) *Catalog {
	t.Helper()
	dir := t.TempDir()
	writeGachaFixture(t, dir, poolV2FixturePrizes, "1 9251 1 2\n")
	catalog, err := LoadCatalog(dir, items)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	return catalog
}

func TestPoolV2SwapsWinRewardsAndPaysFromGrain(t *testing.T) {
	catalog := loadPoolV2Fixture(t, poolV2Items())
	if err := catalog.ApplyPoolV2(poolV2Items()); err != nil {
		t.Fatalf("ApplyPoolV2: %v", err)
	}
	wantRewards := map[uint32]uint32{1: 4146, 2: 4038, 3: 4110, 4: 4074, 5: 4182, 6: 4218}
	for entry, reward := range wantRewards {
		prize, ok := catalog.PrizeForNpc(9251, entry)
		if !ok || prize.RewardRefObjID != reward || prize.ChancePer10000 != 30 {
			t.Fatalf("entry %d = %#v ok=%v, want reward %d at 30/10000", entry, prize, ok, reward)
		}
		if !catalog.IsPrizePair(reward, 1) {
			t.Fatalf("pair %d x1 not a payable pair", reward)
		}
	}
	for _, entry := range []uint32{15, 16} {
		if got := catalog.prizesByEntry[entry].ChancePer10000; got != 1650 {
			t.Fatalf("grain entry %d chance %d, want 1650", entry, got)
		}
	}
	if !catalog.IsPrizePair(9259, 1) {
		t.Fatal("Hwan x1 is not a payable waste pair")
	}
	// The waste lane must resolve a Hwan somewhere in [0, 10000).
	found := false
	for sample := uint32(0); sample < 10000; sample++ {
		if prize, ok := catalog.WastePrizeForNpc(9251, sample); ok && prize.RewardRefObjID == 9259 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no waste sample draws the Hwan")
	}
	names := strings.Join(catalog.RewardCodenames(), ",")
	for _, want := range []string{"ITEM_MALL_HWAN", "ITEM_CH_SWORD_09_A_RARE", "ITEM_CH_SHIELD_09_A_RARE"} {
		if !strings.Contains(names, want) {
			t.Fatalf("reward closure %q lacks %s", names, want)
		}
	}
	for _, gone := range []string{"WEAPON_A", "WEAPON_F"} {
		if strings.Contains(names, gone) {
			t.Fatalf("reward closure %q still admits swapped-out %s", names, gone)
		}
	}
}

func TestPoolV2RefusesAnUnshippedShape(t *testing.T) {
	items := poolV2Items()
	delete(items, "ITEM_MALL_HWAN")
	catalog := loadPoolV2Fixture(t, items)
	// LoadCatalog resolves every shipped reward; deleting the Hwan (not a
	// shipped reward) leaves loading intact, so the override must refuse.
	if err := catalog.ApplyPoolV2(items); err == nil || !strings.Contains(err.Error(), "ITEM_MALL_HWAN") {
		t.Fatalf("ApplyPoolV2 error = %v, want the missing Hwan named", err)
	}
}
