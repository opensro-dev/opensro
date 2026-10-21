/*
===========================================================================

bounded_references.go - bounded residency for the reference caches

===========================================================================
*/

package enterworld

import (
	"iter"
	"opensro.online/server/internal/data/recordcache"
)

// UseBoundedCache runs once after startup compilation, before listeners. Cache
// eviction changes residency only; source bytes were verified before publication.
func (t *TextdataSkills) UseBoundedCache(capacity int) error {
	if err := t.Load(); err != nil {
		return err
	}
	cache, err := recordcache.New[SkillRow](capacity)
	if err != nil {
		return err
	}
	for id, row := range t.rows.values() {
		if err := cache.Put(id, row); err != nil {
			cache.Close()
			return err
		}
	}
	plans := make(map[uint32]SkillExecutionPlan)
	for id, p := range t.plans {
		if p.Len() == 0 {
			continue
		}
		ids := make([]uint32, p.Len())
		for i := range ids {
			ids[i] = p.Stage(i).ID
		}
		plans[id] = SkillExecutionPlan{kind: p.kind, source: &t.rows, ids: ids}
	}
	t.plans = plans
	t.rows.archive = cache
	t.rows.hot = nil
	t.shared = nil
	sharedSkillParses.Release(t.sharedKey)
	return nil
}
func (t *TextdataSkills) Close() error {
	if t.rows.archive != nil {
		return t.rows.archive.Close()
	}
	return nil
}

func (t *TextdataItems) UseBoundedCache(capacity int) error {
	t.once.Do(t.load)
	cache, err := recordcache.New[*ItemRef](capacity)
	if err != nil {
		return err
	}
	names := make(map[string]uint32, len(t.byCodename))
	ids := make(map[uint32]uint32, len(t.byID))
	identities := make(map[*ItemRef]uint32, len(t.byCodename))
	var key uint32
	for name, row := range t.byCodename {
		key++
		if err := cache.Put(key, row); err != nil {
			cache.Close()
			return err
		}
		names[name] = key
		identities[row] = key
	}
	for id, row := range t.byID {
		ids[id] = identities[row]
	}
	t.archive = cache
	t.nameKeys = names
	t.idKeys = ids
	t.byCodename = nil
	t.byID = nil
	// The record cache now holds the rows; stop pinning the shared parse.
	sharedItemParses.Release(t.sharedKey)
	return nil
}
func (t *TextdataItems) Close() error {
	if t.archive != nil {
		return t.archive.Close()
	}
	return nil
}
func (t *TextdataItems) itemRows() iter.Seq[*ItemRef] {
	return func(yield func(*ItemRef) bool) {
		if t.archive != nil {
			for _, id := range t.archive.IDs() {
				row, _ := t.archive.Get(id)
				if !yield(row) {
					return
				}
			}
			return
		}
		for _, row := range t.byCodename {
			if !yield(row) {
				return
			}
		}
	}
}
