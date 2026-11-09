/*
===========================================================================

duplicate_rows.go - healing a bag that holds two rows on one slot

New arms every player bag through here (#3005): the first row keeps a
contested slot, each shadowed row moves to the lowest free bag slot so
the item surfaces, and only a shadow with nowhere to go is dropped.

===========================================================================
*/
package inventory

/*
================
healDuplicateSlots

Two rows can arrive on one wire slot (a persisted bag damaged by an
older build, #3005): the slot reads through its first row, so the
shadowed row is invisible to the client yet still blocks every grant,
which then refuses "inventory full" against visibly free slots. Keep
the first row on the contested slot; move each shadowed row to the
lowest free bag slot so the item surfaces intact; only when the bag
holds no free slot is the shadow dropped - there the refusal that
follows is honest, because both sides see the same full bag.
================
*/
func healDuplicateSlots(rows []Item, bagEnd uint8) []Item {
	occupied := make(map[uint8]bool, len(rows))
	for _, row := range rows {
		occupied[row.Slot] = true
	}
	seen := make(map[uint8]bool, len(rows))
	healed := make([]Item, 0, len(rows))
	for _, row := range rows {
		if !seen[row.Slot] {
			seen[row.Slot] = true
			healed = append(healed, row)
			continue
		}
		relocated := false
		for slot := int(EquipmentSlotEnd); slot < int(bagEnd); slot++ {
			if occupied[uint8(slot)] {
				continue
			}
			row.Slot = uint8(slot)
			occupied[row.Slot] = true
			seen[row.Slot] = true
			healed = append(healed, row)
			relocated = true
			break
		}
		if !relocated {
			// The bag is genuinely full: the shadow has nowhere to
			// surface, and both sides agree every slot is taken.
			continue
		}
	}
	return healed
}
