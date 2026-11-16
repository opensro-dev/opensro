/*
===========================================================================

partysummonscroll_test.go - the party Monster Summon Scroll's kind and
refusals

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
)

/*
================
TestPartySummonKindFollowsTheUsersLevel

The native table by the user's level, 30 to 110 (above 110 the top
row); nothing below 30; a kind the shard does not hold falls to the
nearest lower row it holds.
================
*/
func TestPartySummonKindFollowsTheUsersLevel(t *testing.T) {
	all := func(string) bool { return true }
	for level, want := range map[int64]string{
		30: "MOB_WC_HYEONGCHEON", 59: "MOB_KK_YETI", 60: "MOB_KK_YETI_CLON", 80: "MOB_RM_WINGTRIBE",
		100: "MOB_TQ_MANGNYANG", 110: "MOB_SD_SOPEDU", 130: "MOB_SD_SOPEDU",
	} {
		if got, ok := partySummonKind(level, all); !ok || got != want {
			t.Fatalf("level %d: %q, want %q", level, got, want)
		}
	}
	if _, ok := partySummonKind(29, all); ok {
		t.Fatal("level 29 summoned")
	}
	// A v1.150 shard without the Qin-Shi tomb floors or Egypt: the level-
	// 110 user falls to the highest row it holds (level 87's).
	native := func(codename string) bool {
		return codename[:7] != "MOB_TQ_" && codename[:7] != "MOB_SD_"
	}
	if got, ok := partySummonKind(110, native); !ok || got != "MOB_RM_WINGTRIBESPEAR" {
		t.Fatalf("fallback %q", got)
	}
	if _, ok := partySummonKind(90, func(string) bool { return false }); ok {
		t.Fatal("a shard without any kind summoned")
	}
	if len(partySummonKinds) != 81 {
		t.Fatalf("%d rows, want the native 81", len(partySummonKinds))
	}
}

/*
================
TestPartySummonScrollRefusesBeforeSpending

Out of So-Ok's Christmas Gift's mode, and below level 30, the scroll is
refused and stays in the bag; the scroll is one of So-Ok's gifts.
================
*/
func TestPartySummonScrollRefusesBeforeSpending(t *testing.T) {
	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 20, RefObjID: 10148, Codename: PartySummonScrollItem,
		TypeFlags: wire.PackTypeFlags(3, 3, 15, 2), StackCount: 2}}
	use := persistentSummonUse{division: testDivision, character: c, row: 0, request: wire.ItemUseRequest{Slot: 20}}

	rt := sookChristmasRuntime(t, c, true)
	t.Setenv(progression.EnvChristmasEventMode, string(progression.ChristmasModeWinter))
	rt.SoOkChristmas = progression.SoOkChristmasFromEnv()
	var result OpResult
	if rt.usePartySummonScroll(use, &result) || bagCount(c, PartySummonScrollItem) != 2 {
		t.Fatal("winter mode: the scroll was served")
	}

	rt = sookChristmasRuntime(t, c, false)
	if !rt.SoOkChristmas.Selected() {
		t.Fatal("the mode selects the event even with the switch off")
	}
	result = OpResult{}
	if rt.usePartySummonScroll(use, &result) || bagCount(c, PartySummonScrollItem) != 2 {
		t.Fatal("a level-1 user spent the scroll")
	}

	found := false
	for _, gift := range progression.SoOkChristmasDefaultGifts() {
		found = found || gift.Codename == PartySummonScrollItem
	}
	if !found {
		t.Fatal("the scroll is not one of So-Ok's gifts")
	}
	if family := admittedItemUseFamily(&enterworld.ItemRef{Codename: PartySummonScrollItem, TypeIDs: [4]int64{3, 3, 15, 2}}); family != itemUsePartySummonScroll {
		t.Fatalf("the scroll is admitted as family %d", family)
	}
}
