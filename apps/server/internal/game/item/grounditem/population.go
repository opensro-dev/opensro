/*
===========================================================================

population.go - ground entries by world lifetime

A division's drops belong to a population: the persistent main world
(the zero value) or one live instance world's packed id and
generation. The reads and the retirement here never cross from one
lifetime into another, so a recycled world id cannot see or remove
its successor's drops.

===========================================================================
*/
package grounditem

import (
	"slices"
	"sort"

	"opensro.online/server/internal/domain"
)

// Population is a ground object's owning world lifetime. The zero value is
// the persistent main-world population used by pre-instance ground records.
// Other populations require both the packed world ID and its live generation.
type Population struct {
	World      uint32
	Generation uint64
}

func (p Population) Valid() bool {
	return p == (Population{}) || p.World&0xffff != 0 && p.World>>16 != 0 && p.Generation != 0
}

func (r *Registry) GetInPopulation(division string, population Population, gid uint32) (Item, bool) {
	item, ok := r.Get(division, gid)
	if !ok || !population.Valid() || item.Population != population {
		return Item{}, false
	}
	return item, true
}

// AllInPopulation returns the population's entries in the division,
// in All's entity-id order and detached the same way, but filtered
// under the lock: only the matching entries pay the deep copy. The
// old read copied every drop of the division and kept a fraction
// (#2030) - one dungeon's bootstrap paid for the whole main world.
/*
================
AllInPopulation
================
*/
func (r *Registry) AllInPopulation(division string, population Population) []Item {
	if !population.Valid() {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	items := r.byDivision[division]
	var gids []uint32
	for gid, item := range items {
		if item.Population == population {
			gids = append(gids, gid)
		}
	}
	if len(gids) == 0 {
		return nil
	}
	slices.Sort(gids)
	out := make([]Item, 0, len(gids))
	for _, gid := range gids {
		item := items[gid]
		item.MagicOptions = append([]uint64(nil), item.MagicOptions...)
		item.Summon = domain.CloneCOS(item.Summon)
		out = append(out, item)
	}
	return out
}

// RemovePopulation retires only this allocation's objects. A recycled packed
// world ID cannot remove its successor's ground items.
func (r *Registry) RemovePopulation(division string, population Population) []Item {
	r.mu.Lock()
	defer r.mu.Unlock()
	var removed []Item
	if !population.Valid() {
		return removed
	}
	for gid, item := range r.byDivision[division] {
		if item.Population == population {
			removed = append(removed, item)
			delete(r.byDivision[division], gid)
		}
	}
	if len(removed) != 0 {
		r.bumpLocked(division)
		for _, item := range removed {
			r.stampLocked(division, item.Gid)
		}
	}
	sort.Slice(removed, func(i, j int) bool { return removed[i].Gid < removed[j].Gid })
	return removed
}
