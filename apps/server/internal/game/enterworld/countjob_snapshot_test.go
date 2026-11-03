/*
===========================================================================

countjob_snapshot_test.go - a premium package's board references ride the login

A used premium package leaves the bag, and its limited rows never sit in one:
after the world-entry reset their item references exist only if the login
snapshot carries the package and one row per limited item, the way a param
job's internal item already rides it. A quest-limit work has no board row
and seeds nothing.
===========================================================================
*/
package enterworld

import (
	"testing"

	"opensro.online/server/internal/domain"
)

const (
	countJobPackageID   = 24306
	countJobReturnID    = 24311
	countJobReverseID   = 24312
	countJobResurrectID = 24313
)

func countJobItems() fakeItems {
	return fakeItems{
		"ITEM_PRE_GOLTAM_4W_PLUS": {
			RefObjID: countJobPackageID, Codename: "ITEM_PRE_GOLTAM_4W_PLUS", Name: "Premium PLUS",
			TypeIDs:      [4]int64{3, 3, 3, 1},
			NativeFields: NewNativeFields(map[string]float64{"itemParam1_29c": 2419200}),
		},
		"ITEM_MALL_RETURN_SCROLL_HIGH_SPEED": {
			RefObjID: countJobReturnID, Codename: "ITEM_MALL_RETURN_SCROLL_HIGH_SPEED", Name: "Instant Return Scroll",
			TypeIDs: [4]int64{3, 3, 3, 1},
		},
		"ITEM_MALL_REVERSE_RETURN_SCROLL": {
			RefObjID: countJobReverseID, Codename: "ITEM_MALL_REVERSE_RETURN_SCROLL", Name: "Reverse Return Scroll",
			TypeIDs: [4]int64{3, 3, 3, 3},
		},
		"ITEM_MALL_RESURRECTION_100P_SCROLL": {
			RefObjID: countJobResurrectID, Codename: "ITEM_MALL_RESURRECTION_100P_SCROLL", Name: "100% resurrection scroll",
			TypeIDs: [4]int64{3, 3, 13, 6},
		},
	}
}

func countJobRow(id uint32, codename string) domain.CompositeJob {
	return domain.CompositeJob{
		Kind: domain.CompositeUsedItemLimit, PackageRefObjID: countJobPackageID,
		Target: id, TargetCodename: codename, Uses: 3, MaxUses: 3, EndUnixMs: 1 << 62,
	}
}

/*
================
countJobSnapshotFixture

A character whose premium package is used up out of the bag: three live
limited rows under it, plus a quest-limit work that owns no board row.
================
*/
func countJobSnapshotFixture() (*Deps, *Character) {
	character := chinaSpearman()
	character.CompositeJobs = []domain.CompositeJob{
		countJobRow(countJobReturnID, "ITEM_MALL_RETURN_SCROLL_HIGH_SPEED"),
		countJobRow(countJobReverseID, "ITEM_MALL_REVERSE_RETURN_SCROLL"),
		countJobRow(countJobResurrectID, "ITEM_MALL_RESURRECTION_100P_SCROLL"),
		{Kind: domain.CompositeUsedQuestLimit, PackageRefObjID: countJobPackageID, QuestCodename: "QNO_QT_GT_QUEST", EndUnixMs: 1 << 62},
	}
	deps := testDeps(character)
	deps.Items = countJobItems()
	return deps, character
}

func rowsByID(rows []RefItemRow) map[uint32]RefItemRow {
	byID := map[uint32]RefItemRow{}
	for _, row := range rows {
		byID[row.RefObjID] = row
	}
	return byID
}

/*
================
TestCountJobSnapshotCarriesPackageAndLimitedItems
================
*/
func TestCountJobSnapshotCarriesPackageAndLimitedItems(t *testing.T) {
	deps, character := countJobSnapshotFixture()
	byID := rowsByID(buildRefItemSnapshot(deps, DefaultDivisionID, character))

	pkg, ok := byID[countJobPackageID]
	if !ok {
		t.Fatalf("snapshot %v lacks the premium package row", refItemIDs(buildRefItemSnapshot(deps, DefaultDivisionID, character)))
	}
	if pkg.Name != "Premium PLUS" {
		t.Fatalf("package row name = %q, want Premium PLUS", pkg.Name)
	}
	if got := pkg.NativeFields.Get("itemParam1_29c"); got != 2419200 {
		t.Fatalf("package row itemParam1_29c = %v, want 2419200 (the board bar's period)", got)
	}
	for id, name := range map[uint32]string{
		countJobReturnID:    "Instant Return Scroll",
		countJobReverseID:   "Reverse Return Scroll",
		countJobResurrectID: "100% resurrection scroll",
	} {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("snapshot lacks limited item %d (%s)", id, name)
		}
		if row.Name != name {
			t.Fatalf("limited item %d name = %q, want %q", id, row.Name, name)
		}
	}
}

/*
================
TestCountJobSnapshotDedupesAgainstTheBag

A limited item the character also holds as a real bag row is still one
reference row: the collector keys by RefObjID whichever leg asks first.
================
*/
func TestCountJobSnapshotDedupesAgainstTheBag(t *testing.T) {
	deps, character := countJobSnapshotFixture()
	items := countJobItems()
	scroll := items["ITEM_MALL_RESURRECTION_100P_SCROLL"]
	character.MissionInventory = append(character.MissionInventory, InventoryRow{
		Slot: 13, RefObjID: scroll.RefObjID, Codename: scroll.Codename, TypeFlags: scroll.TypeFlags(), StackCount: 1,
	})
	counts := map[uint32]int{}
	for _, row := range buildRefItemSnapshot(deps, DefaultDivisionID, character) {
		counts[row.RefObjID]++
	}
	for _, id := range []uint32{countJobPackageID, countJobReturnID, countJobReverseID, countJobResurrectID} {
		if counts[id] != 1 {
			t.Fatalf("reference %d appears %d times, want once", id, counts[id])
		}
	}
}

/*
================
TestCountJobSnapshotStaysOutWithoutALimitedRow

Quest-limit and booth works own no board row, so a character holding only
those seeds neither the package nor any target.
================
*/
func TestCountJobSnapshotStaysOutWithoutALimitedRow(t *testing.T) {
	character := chinaSpearman()
	character.CompositeJobs = []domain.CompositeJob{
		{Kind: domain.CompositeUsedQuestLimit, PackageRefObjID: countJobPackageID, QuestCodename: "QNO_QT_GT_QUEST", EndUnixMs: 1 << 62},
		{Kind: domain.CompositeBuffItem, PackageRefObjID: countJobPackageID, Target: countJobReturnID, TargetCodename: "ITEM_MALL_RETURN_SCROLL_HIGH_SPEED", EndUnixMs: 1 << 62},
	}
	deps := testDeps(character)
	deps.Items = countJobItems()
	for _, row := range buildRefItemSnapshot(deps, DefaultDivisionID, character) {
		switch row.RefObjID {
		case countJobPackageID, countJobReturnID, countJobReverseID, countJobResurrectID:
			t.Fatalf("snapshot seeded board reference %d for a character with no limited row", row.RefObjID)
		}
	}
}
