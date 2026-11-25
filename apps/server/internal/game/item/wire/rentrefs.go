/*
===========================================================================

rentrefs.go - the rental references every equipment body is measured by

A reference the built refrentitem names (enterworld.ItemRef.Rent, #4240)
carries the 8-byte rent deadline leg on EVERY row, rented or permanent:
the client measures the body by the reference alone (inventory-item.ts
decodeInventoryItem, rentRefs), so a permanent row of a rental kind
writes the leg as 0. r204l wrote the leg only for rows with a deadline,
and every character holding a permanent Halloween or other rental-kind
avatar was refused entry with "Truncated inventory item" (S2).

The catalog load installs the set (enterworld markRentItems); this
package owns it so every encoder reads one answer.

===========================================================================
*/
package wire

import "sync/atomic"

var rentRefs atomic.Pointer[map[uint32]struct{}]

/*
================
SetRentRefs

Replaces the rental reference set. The catalog calls it once its
refrentitem is read; an empty list (the native tree) names none.
================
*/
func SetRentRefs(ids []uint32) {
	set := make(map[uint32]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	rentRefs.Store(&set)
}

/*
================
IsRentRef

Whether a row of refObjID carries the rent deadline leg.
================
*/
func IsRentRef(refObjID uint32) bool {
	set := rentRefs.Load()
	if set == nil {
		return false
	}
	_, ok := (*set)[refObjID]
	return ok
}
