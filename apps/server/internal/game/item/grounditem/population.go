package grounditem

import "sort"

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

func (r *Registry) AllInPopulation(division string, population Population) []Item {
	var out []Item
	if !population.Valid() {
		return out
	}
	for _, item := range r.All(division) {
		if item.Population == population {
			out = append(out, item)
		}
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
		r.revision++
		for _, item := range removed {
			r.stampLocked(division, item.Gid)
		}
	}
	sort.Slice(removed, func(i, j int) bool { return removed[i].Gid < removed[j].Gid })
	return removed
}
