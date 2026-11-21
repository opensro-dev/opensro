/*
===========================================================================

item_holdings_test.go - a character's holdings count every container once

ItemHoldings (#3307) is the count the item ledger diffs at each door: the
bag, the avatar rows and every companion's container, with a companion
seated twice (active and in its summoner row, #3129) counted once, and a
move between containers changing nothing.

===========================================================================
*/
package domain

import (
	"reflect"
	"testing"
)

/*
================
holdingsFixture

A character with a bag (a sword and two potion stacks of one item), an
avatar hat, and an active pet that also sits in its summoner row, its
bag holding one more potion stack.
================
*/
func holdingsFixture() *Character {
	pet := &CharacterCOS{Codename: "COS_P_RABBIT", Container: &COSContainer{Capacity: 4, Rows: []InventoryRow{
		{Slot: 0, Codename: "ITEM_ETC_HP_POTION_01", StackCount: 7},
	}}}
	return &Character{
		MissionInventory: []InventoryRow{
			{Slot: 6, Codename: "ITEM_CH_SWORD_01_A", StackCount: 1},
			{Slot: 13, Codename: "ITEM_ETC_HP_POTION_01", StackCount: 50},
			{Slot: 14, Codename: "ITEM_ETC_HP_POTION_01", StackCount: 20},
			{Slot: 15, Codename: "ITEM_COS_P_RABBIT_SCROLL", Summon: pet},
		},
		AvatarInventory: &AvatarInventory{Rows: []InventoryRow{{Slot: 0, Codename: "ITEM_AVATAR_HAT", StackCount: 1}}},
		ActiveCOS:       pet,
	}
}

/*
================
TestItemHoldingsCountsEveryContainerOnce
================
*/
func TestItemHoldingsCountsEveryContainerOnce(t *testing.T) {
	t.Parallel()

	got := ItemHoldings(holdingsFixture())
	want := []ItemCount{
		{Item: "ITEM_AVATAR_HAT", Count: 1},
		{Item: "ITEM_CH_SWORD_01_A", Count: 1},
		// 50 + 20 in the bag, 7 in the pet's bag: the pet seated twice
		// is still one container.
		{Item: "ITEM_COS_P_RABBIT_SCROLL", Count: 1},
		{Item: "ITEM_ETC_HP_POTION_01", Count: 77},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("holdings %+v, want %+v", got, want)
	}
}

/*
================
TestItemHoldingsIgnoreMovesInsideTheCharacter

A stack moved from the bag into the pet's bag, and a sword moved between
slots, leave the holdings as they were.
================
*/
func TestItemHoldingsIgnoreMovesInsideTheCharacter(t *testing.T) {
	t.Parallel()

	c := holdingsFixture()
	before := ItemHoldings(c)
	c.MissionInventory[0].Slot = 20
	moved := c.MissionInventory[2]
	c.MissionInventory = append(c.MissionInventory[:2], c.MissionInventory[3:]...)
	c.ActiveCOS.Container.Rows = append(c.ActiveCOS.Container.Rows, InventoryRow{Slot: 1, Codename: moved.Codename, StackCount: moved.StackCount})
	if after := ItemHoldings(c); !SameItemHoldings(before, after) {
		t.Fatalf("a move changed the holdings: %+v -> %+v", before, after)
	}
	if diff := DiffItemHoldings(before, ItemHoldings(c)); len(diff) != 0 {
		t.Fatalf("a move diffed %+v", diff)
	}
}

/*
================
TestDiffItemHoldingsSignsEveryChange

A gained item, a lost item and a changed count each come out once, in
item order; a row with no codename counts under its reference id.
================
*/
func TestDiffItemHoldingsSignsEveryChange(t *testing.T) {
	t.Parallel()

	before := []ItemCount{{Item: "A", Count: 2}, {Item: "B", Count: 1}, {Item: "C", Count: 5}}
	after := []ItemCount{{Item: "A", Count: 2}, {Item: "C", Count: 3}, {Item: "D", Count: 4}}
	want := []ItemCount{{Item: "B", Count: -1}, {Item: "C", Count: -2}, {Item: "D", Count: 4}}
	if got := DiffItemHoldings(before, after); !reflect.DeepEqual(got, want) {
		t.Fatalf("diff %+v, want %+v", got, want)
	}
	if got := DiffItemHoldings(nil, after); !reflect.DeepEqual(got, after) {
		t.Fatalf("from nothing %+v", got)
	}
	unnamed := &Character{MissionInventory: []InventoryRow{{RefObjID: 62, StackCount: 0}}}
	if got := ItemHoldings(unnamed); !reflect.DeepEqual(got, []ItemCount{{Item: "ref:62", Count: 1}}) {
		t.Fatalf("an unnamed row counts %+v", got)
	}
}

/*
================
TestItemDoorBulkNamesTheConsumptionDoors

The bulk set holds the consumption and loot doors and none of the
evidence doors a duplication case reads.
================
*/
func TestItemDoorBulkNamesTheConsumptionDoors(t *testing.T) {
	t.Parallel()

	for _, door := range []string{"item-use", "potion-recovery", "player-basic-attack", "pickup-item", "shop-transaction"} {
		if !ItemDoorBulk(door) {
			t.Fatalf("%s is not bulk", door)
		}
	}
	for _, door := range []string{"exchange", "stall-sale", "storage-transfer", "operator-grant-items", "mall-purchase", "create-character", "quest-reward"} {
		if ItemDoorBulk(door) {
			t.Fatalf("%s is bulk; its rows are evidence", door)
		}
	}
}

/*
================
TestDiffItemRowCountsMatchesTheFoldedDiff

The positional pass and the folded diff agree: a stack used, two stacks
of one item changed in opposite directions (a split, which nets to
nothing), a row added and a row moved.
================
*/
func TestDiffItemRowCountsMatchesTheFoldedDiff(t *testing.T) {
	t.Parallel()

	base := []ItemCount{{Item: "B", Count: 5}, {Item: "A", Count: 1}, {Item: "B", Count: 3}}
	cases := [][]ItemCount{
		{{Item: "B", Count: 4}, {Item: "A", Count: 1}, {Item: "B", Count: 3}},
		{{Item: "B", Count: 2}, {Item: "A", Count: 1}, {Item: "B", Count: 6}},
		{{Item: "B", Count: 5}, {Item: "A", Count: 1}, {Item: "B", Count: 3}, {Item: "C", Count: 2}},
		{{Item: "A", Count: 1}, {Item: "B", Count: 5}, {Item: "B", Count: 2}},
	}
	for i, after := range cases {
		want := DiffItemHoldings(FoldItemCounts(base), FoldItemCounts(after))
		got := DiffItemRowCounts(base, after)
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("case %d: positional %+v, folded %+v", i, got, want)
		}
	}
}

/*
================
TestDiffItemHoldingsSkipsZeroEntries

An item held at zero on one side (the commit check's expectation after a
noted loss) is no change against its absence on the other.
================
*/
func TestDiffItemHoldingsSkipsZeroEntries(t *testing.T) {
	t.Parallel()

	before := []ItemCount{{Item: "A", Count: 0}, {Item: "B", Count: 2}}
	after := []ItemCount{{Item: "B", Count: 2}, {Item: "C", Count: 0}}
	if got := DiffItemHoldings(before, after); len(got) != 0 {
		t.Fatalf("zero entries diffed %+v", got)
	}
}
