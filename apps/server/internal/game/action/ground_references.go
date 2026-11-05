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
	"sync"

	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/gacha"
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
	published := rt.staticRefSet()
	rows := []inventory.Item{}
	seen := map[uint32]bool{}
	for _, drop := range drops {
		if _, static := published[drop.Codename]; drop.IsGold() || seen[drop.RefObjID] || static {
			continue
		}
		seen[drop.RefObjID] = true
		rows = append(rows, inventory.Item{RefObjID: drop.RefObjID, Codename: drop.Codename, TypeFlags: drop.TypeFlags})
	}
	return rt.itemReferenceFrames(rows)
}

/*
================
staticRefCache

StaticRefItemCodenames as a set, built once per catalog pair. Every new
drop asked for the list, which rebuilt and deduplicated it each time
(about 0.16 GB a minute live, #2031). The loot table is fixed at init;
the alchemy and Gacha catalogs are the inputs that can be replaced, so a
replacement rebuilds the set.
================
*/
type staticRefCache struct {
	mu      sync.Mutex
	alchemy *alchemy.Catalog
	gacha   *gacha.Catalog
	names   map[string]struct{}
}

/*
================
staticRefSet

The shared set; callers read it and never mutate it.
================
*/
func (rt *Runtime) staticRefSet() map[string]struct{} {
	rt.staticRefs.mu.Lock()
	defer rt.staticRefs.mu.Unlock()
	cache := &rt.staticRefs
	if cache.names == nil || cache.alchemy != rt.Alchemy || cache.gacha != rt.GachaCatalog {
		names := rt.StaticRefItemCodenames()
		cache.names = make(map[string]struct{}, len(names))
		for _, name := range names {
			cache.names[name] = struct{}{}
		}
		cache.alchemy, cache.gacha = rt.Alchemy, rt.GachaCatalog
	}
	return cache.names
}
