/*
===========================================================================

ground_references.go - item reference deltas for newly dropped ground items

A viewer can only parse a ground item whose reference it holds. The static
set (StaticRefItemCodenames) is already in every browser from the published
reference file; anything else is sent as a reference delta ahead of the
spawn, to the actor and observers alike.

===========================================================================
*/
package action

import (
	"slices"

	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

// maxReferencesPerFrame bounds one reference delta frame.
const maxReferencesPerFrame = 64

/*
================
groundReferences

Reference deltas precede every newly introduced ground type. Gold heaps and
the published static set are never repeated; re-entry separately seeds the
actual Ground contents through GroundRefItemCodenames.
================
*/
func (rt *Runtime) groundReferences(drops []grounditem.Item) []wire.Frame {
	published := rt.StaticRefItemCodenames()
	rows := []inventory.Item{}
	seen := map[uint32]bool{}
	for _, drop := range drops {
		if drop.IsGold() || seen[drop.RefObjID] || slices.Contains(published, drop.Codename) {
			continue
		}
		seen[drop.RefObjID] = true
		rows = append(rows, inventory.Item{RefObjID: drop.RefObjID, Codename: drop.Codename, TypeFlags: drop.TypeFlags})
	}
	return rt.itemReferenceFrames(rows)
}
