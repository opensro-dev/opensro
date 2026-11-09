/*
===========================================================================

duplicate_rows.go - healing a bag that holds two rows on one slot

New arms every player bag through here (#3005): the first row keeps a
contested slot, each shadowed row moves to the lowest free bag slot so
the item surfaces, and a shadow with nowhere to go is kept as it lies
until a later arm finds a slot free. No row is ever deleted here.

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
lowest free bag slot so the item surfaces intact. When the bag holds
no free slot the shadow is kept unchanged - deleting it would burn a
real item on write-back - and it surfaces on a later arm once a slot
frees; meanwhile the refusal stays honest, because both sides see the
same full bag.
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
			// surface yet. Keep it unchanged rather than let the
			// write-back delete a real item; it surfaces on a later
			// arm once a slot frees.
			healed = append(healed, row)
		}
	}
	return healed
}
