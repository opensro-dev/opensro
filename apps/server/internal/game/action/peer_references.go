/*
===========================================================================

peer_references.go - item reference deltas ahead of peer equipment packets

The native client holds every item record (Media.pk2) and never guards the
lookup: GlobalDataManager_GetItemRecordById (7EFF90) returns 0 for an
unknown id, and both the 0x3314 equip visual (7777E0) and the 0x30D7
appearance equipment loop (86AFB0) dereference the result. The browser holds
only the references the server sent it, and the item's type word decides
how the rest of a 0x30D7 row parses, so an unknown id cannot be skipped.

A viewer's bootstrap seeds the division's persisted inventories, but a peer
can wear an item acquired after that bootstrap (mall, quest reward,
exchange, operator grant, a newly created character). Every packet that
shows another character's items is therefore preceded by the references
for those items, sent to the same audience. Rows equal what the bootstrap
and the commerce deltas carry, so a repeat is harmless (#340).

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
itemReferenceFrames

The reference deltas for items, one per maxReferencesPerFrame rows, with
repeats and empty ids left out.
================
*/
func (rt *Runtime) itemReferenceFrames(items []inventory.Item) []wire.Frame {
	rows := make([]inventory.Item, 0, len(items))
	seen := map[uint32]bool{}
	for _, item := range items {
		if item.RefObjID == 0 || seen[item.RefObjID] {
			continue
		}
		seen[item.RefObjID] = true
		rows = append(rows, item)
	}
	var frames []wire.Frame
	for len(rows) > 0 {
		n := min(len(rows), maxReferencesPerFrame)
		frames = append(frames, rt.commerceReferences(rows[:n], nil))
		rows = rows[n:]
	}
	return frames
}

/*
================
itemReferencesByID

itemReferenceFrames for bare ids (a transform skin or a spawn row carries
no codename). An id the item table cannot resolve is left out; the item
table is the same one every reference row is built from.
================
*/
func (rt *Runtime) itemReferencesByID(ids []uint32) []wire.Frame {
	source, ok := rt.deps.ItemReferences().(interface {
		ItemRefByID(uint32) (*enterworld.ItemRef, bool)
	})
	if !ok {
		return nil
	}
	items := make([]inventory.Item, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if ref, found := source.ItemRefByID(id); found && ref != nil {
			items = append(items, inventory.Item{RefObjID: id, Codename: ref.Codename, TypeFlags: ref.TypeFlags()})
		}
	}
	return rt.itemReferenceFrames(items)
}

/*
================
socketVisualReferences

The references for the items a set of socket changes puts on view; a
cleared socket names no item.
================
*/
func (rt *Runtime) socketVisualReferences(changes []inventory.SocketVisual) []wire.Frame {
	items := make([]inventory.Item, 0, len(changes))
	for _, change := range changes {
		if change.Worn {
			items = append(items, change.Item)
		}
	}
	return rt.itemReferenceFrames(items)
}

/*
================
PeerItemReferences

The simulation ticker's hook (Ticker.ItemReferences): the references a
viewer needs before a peer's 0x30D7 spawn row.
================
*/
func (rt *Runtime) PeerItemReferences(ids []uint32) []simulation.Frame {
	frames := rt.itemReferencesByID(ids)
	out := make([]simulation.Frame, 0, len(frames))
	for _, f := range frames {
		out = append(out, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload})
	}
	return out
}
