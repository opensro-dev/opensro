/*
===========================================================================

rankcache.go - each character's learned rank rows, computed once per skill list

learnedRankRows picks, for every learned skill group, the row of its highest
rank. Every stats projection asked for it (stats, passives, status
resistance, damage return), from the tick and from the request handlers, and
rebuilt it each time: a group map plus a ~1.5 KB SkillRow copy per selected
rank (7.5% of the GameWorld's CPU in the 2026-10-14 profile).

The selection reads only the character's skill-ID list and the skill source
(passives.go), and the source is immutable once loaded (TextdataSkills.Load).
So the cache is keyed on the CONTENT of that list plus the source's
identity: any change to c.Skills, by any writer, misses. No writer has to
remember to invalidate anything.

Rows handed out are shared and read-only: every caller only reads them. The
cache is safe for concurrent use; a miss computes outside the lock.

===========================================================================
*/
package combat

import (
	"container/list"
	"reflect"
	"slices"
	"sync"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

// rankRowCacheMax bounds the cache; the least recently used entry leaves
// first. Production peaks at a few dozen characters.
const rankRowCacheMax = 2048

/*
================
rankRowEntry

One character's selection: the skill-ID list and source it was computed
from, and the rows.
================
*/
type rankRowEntry struct {
	id     int64
	skills []uint32
	source uintptr
	rows   []enterworld.SkillRow
	at     *list.Element
}

/*
================
rankRowCache
================
*/
type rankRowCache struct {
	mu      sync.Mutex
	entries map[int64]*rankRowEntry
	order   *list.List
}

var rankRows = rankRowCache{entries: make(map[int64]*rankRowEntry), order: list.New()}

/*
================
sourceIdentity

A comparable identity for a data source: its pointer, for the pointer and
map sources the server and tests use. 0 means "not cacheable": the rows are
computed fresh every time.
================
*/
func sourceIdentity(source any) uintptr {
	value := reflect.ValueOf(source)
	switch value.Kind() {
	case reflect.Pointer, reflect.Map:
		return value.Pointer()
	}
	return 0
}

/*
================
learnedRankRowsCached

The learned rank rows of c, shared and read-only. A character without an
ID or a source without an identity is computed fresh.
================
*/
func learnedRankRowsCached(c *domain.Character, skills enterworld.SkillDataSource) []enterworld.SkillRow {
	source := sourceIdentity(skills)
	if c.ID == 0 || source == 0 {
		return learnedRankRows(c, skills, nil)
	}
	if rows, ok := rankRows.lookup(c.ID, c.Skills, source); ok {
		return rows
	}
	// Computed outside the lock: two concurrent misses compute the same
	// content-keyed result, and the later insert wins harmlessly.
	rows := learnedRankRows(c, skills, nil)
	rankRows.insert(c.ID, slices.Clone(c.Skills), source, rows)
	return rows
}

/*
================
rankRowCache.lookup
================
*/
func (r *rankRowCache) lookup(id int64, skills []uint32, source uintptr) ([]enterworld.SkillRow, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[id]
	if !ok || entry.source != source || !slices.Equal(entry.skills, skills) {
		return nil, false
	}
	r.order.MoveToFront(entry.at)
	return entry.rows, true
}

/*
================
rankRowCache.insert
================
*/
func (r *rankRowCache) insert(id int64, skills []uint32, source uintptr, rows []enterworld.SkillRow) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry, ok := r.entries[id]; ok {
		entry.skills, entry.source, entry.rows = skills, source, rows
		r.order.MoveToFront(entry.at)
		return
	}
	entry := &rankRowEntry{id: id, skills: skills, source: source, rows: rows}
	entry.at = r.order.PushFront(entry)
	r.entries[id] = entry
	for r.order.Len() > rankRowCacheMax {
		oldest := r.order.Back()
		r.order.Remove(oldest)
		delete(r.entries, oldest.Value.(*rankRowEntry).id)
	}
}

/*
================
ForgetRankRows

Drops a character's cached rows when its session ends. Correctness never
depends on it (the cache is content-keyed); it returns the memory early.
================
*/
func ForgetRankRows(id int64) {
	rankRows.mu.Lock()
	defer rankRows.mu.Unlock()
	if entry, ok := rankRows.entries[id]; ok {
		rankRows.order.Remove(entry.at)
		delete(rankRows.entries, id)
	}
}
