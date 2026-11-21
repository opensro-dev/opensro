/*
===========================================================================

item_holdings.go - what a character holds, counted by item (#3307)

Port-only, not native: the item ledger (store/item_ledger.go) keeps no
call in gameplay code. It reads a character's holdings at each door and
records the difference, as the gold ledger reads the balance. A holding
is a count per item codename across every container the character owns:
the bag and equipment rows, the avatar rows, and every companion's
container (pet bag, transport cargo).

Items carry no stable serial (InventoryRow.RecordID is optional), so the
count is a multiset by codename. A move inside one character (between bag
slots, into a pet's bag, onto the body) leaves the multiset unchanged and
records nothing; only an item entering or leaving the character does.

The ledger keeps, per stored character, the row counts in row order
(AppendItemRowCounts): a door that moved no item leaves that sequence
equal, and the check is one linear pass whose codename strings share
their bytes with the rows (measured 2026-11-22: a full bag and pet,
142 rows, 0.7 us; BenchmarkNoteItemsDoor). Only a changed sequence is folded
into holdings - sorted by item, one entry per item - and two holdings
diff in one merge pass.
The census (platform/economy/census.go) walks the same containers over
snapshot JSON, not live records, and keeps its own walk for that reason.

===========================================================================
*/
package domain

import (
	"slices"
	"strconv"
	"strings"
)

/*
================
ItemCount

How many of one item a character holds.
================
*/
type ItemCount struct {
	Item  string
	Count int64
}

/*
================
ItemHoldings

What c holds: its row counts folded into holdings.
================
*/
func ItemHoldings(c *Character) []ItemCount {
	return FoldItemCounts(AppendItemRowCounts(nil, c))
}

/*
================
AppendItemRowCounts

Appends one entry per row c holds to dst[:0], in row order, and returns
it: a caller that reuses dst allocates nothing once it is large enough.
A row counts its stack, or one for a row that carries no stack count. A
summoner row is an item too; the companion it summons is walked once
through VisitCompanions, which skips the active record's second seat in
its summoner row (#3129). Two equal sequences are equal holdings; unequal
ones may still be (a move), which FoldItemCounts settles.
================
*/
func AppendItemRowCounts(dst []ItemCount, c *Character) []ItemCount {
	dst = dst[:0]
	if c == nil {
		return dst
	}
	dst = appendItemRows(dst, c.MissionInventory)
	if c.AvatarInventory != nil {
		dst = appendItemRows(dst, c.AvatarInventory.Rows)
	}
	c.VisitCompanions(func(pet *CharacterCOS) bool {
		if pet.Container != nil {
			dst = appendItemRows(dst, pet.Container.Rows)
		}
		return true
	})
	return dst
}

/*
================
FoldItemCounts

Row counts as holdings: a sorted copy with one entry per item.
================
*/
func FoldItemCounts(rows []ItemCount) []ItemCount {
	held := slices.Clone(rows)
	slices.SortFunc(held, func(a, b ItemCount) int { return strings.Compare(a.Item, b.Item) })
	kept := 0
	for _, entry := range held {
		if kept > 0 && held[kept-1].Item == entry.Item {
			held[kept-1].Count += entry.Count
			continue
		}
		held[kept] = entry
		kept++
	}
	return held[:kept]
}

/*
================
appendItemRows
================
*/
func appendItemRows(dst []ItemCount, rows []InventoryRow) []ItemCount {
	for i := range rows {
		count := rows[i].StackCount
		if count < 1 {
			count = 1
		}
		dst = append(dst, ItemCount{Item: ItemHoldingKey(&rows[i]), Count: count})
	}
	return dst
}

/*
================
ItemHoldingKey

The item a row counts toward: its codename, or its reference id when a
row carries no codename, so such a row is never lost from the count.
================
*/
func ItemHoldingKey(row *InventoryRow) string {
	if row.Codename != "" {
		return row.Codename
	}
	return "ref:" + strconv.FormatUint(uint64(row.RefObjID), 10)
}

/*
================
SameItemHoldings

Whether two count sequences (holdings, or row counts) are equal entry
for entry.
================
*/
func SameItemHoldings(a, b []ItemCount) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

/*
================
DiffItemRowCounts

The signed change per item from one row-count sequence to another. When
both name the same items in the same positions - a stack used, an arrow
spent, a potion refilled: the bulk of the doors that move an item - the
change is read by position in one pass; any other change (a row added,
removed or moved) is folded and diffed whole.
================
*/
func DiffItemRowCounts(before, after []ItemCount) []ItemCount {
	if len(before) != len(after) {
		return DiffItemHoldings(FoldItemCounts(before), FoldItemCounts(after))
	}
	var out []ItemCount
	for i := range after {
		if before[i].Item != after[i].Item {
			return DiffItemHoldings(FoldItemCounts(before), FoldItemCounts(after))
		}
		if delta := after[i].Count - before[i].Count; delta != 0 {
			out = append(out, ItemCount{Item: after[i].Item, Count: delta})
		}
	}
	if len(out) < 2 {
		return out
	}
	// Two rows of one item may both have moved: one entry per item, in
	// item order, as DiffItemHoldings gives.
	folded := FoldItemCounts(out)
	kept := folded[:0]
	for _, change := range folded {
		if change.Count != 0 {
			kept = append(kept, change)
		}
	}
	return kept
}

/*
================
DiffItemHoldings

Every item whose count differs from before to after, as a signed change,
in item order: one merge pass over the two sorted holdings.
================
*/
func DiffItemHoldings(before, after []ItemCount) []ItemCount {
	var out []ItemCount
	i, j := 0, 0
	for i < len(before) || j < len(after) {
		switch {
		case j == len(after) || i < len(before) && before[i].Item < after[j].Item:
			out = append(out, ItemCount{Item: before[i].Item, Count: -before[i].Count})
			i++
		case i == len(before) || after[j].Item < before[i].Item:
			out = append(out, after[j])
			j++
		default:
			if delta := after[j].Count - before[i].Count; delta != 0 {
				out = append(out, ItemCount{Item: after[j].Item, Count: delta})
			}
			i++
			j++
		}
	}
	return out
}
