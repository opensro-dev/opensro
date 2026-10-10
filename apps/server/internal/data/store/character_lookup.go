package store

import (
	"opensro.online/server/internal/domain"
	"strings"
	"unicode"
)

type characterLookupIndex struct {
	names map[string]*domain.Character
	ids   map[int64]*domain.Character
}

// Match strings.EqualFold, including Unicode simple-fold cycles. First source
// row wins, exactly as the former linear lookup did for ambiguous fixtures.
func characterLookupName(name string) string {
	var key strings.Builder
	key.Grow(len(name))
	for _, r := range name {
		least := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < least {
				least = next
			}
		}
		key.WriteRune(least)
	}
	return key.String()
}

/*
================
characterLookup

The division's published index. Lock-free: lookups run inside store doors
and authority callbacks, which already hold s.mu, and a nested RLock there
deadlocks the moment a writer waits (#570's sibling, the 2026-10-10
freezes). Indexes are immutable; the roster writers rebuild them under the
write lock (rebuildCharacterLookupLocked). Character fields still require
the normal authority read or mutation door.
================
*/
func (s *Store) characterLookup(division string) *characterLookupIndex {
	if published := s.characterLookups.Load(); published != nil {
		if index := (*published)[division]; index != nil {
			return index
		}
	}
	return &characterLookupIndex{}
}

/*
================
rebuildCharacterLookupLocked

Rebuilds division's index from the live roster and publishes a new map.
Every roster change (create, reap, rename, load) calls it holding s.mu for
writing. First row wins, as the former linear lookup did.
================
*/
func (s *Store) rebuildCharacterLookupLocked(division string) {
	index := &characterLookupIndex{names: make(map[string]*domain.Character), ids: make(map[int64]*domain.Character)}
	for _, c := range s.characters[division] {
		if c == nil {
			continue
		}
		key := characterLookupName(c.Name)
		if index.names[key] == nil {
			index.names[key] = c
		}
		if index.ids[c.ID] == nil {
			index.ids[c.ID] = c
		}
	}
	next := map[string]*characterLookupIndex{}
	if published := s.characterLookups.Load(); published != nil {
		for d, i := range *published {
			next[d] = i
		}
	}
	next[division] = index
	s.characterLookups.Store(&next)
}

/*
================
rebuildAllCharacterLookupsLocked
================
*/
func (s *Store) rebuildAllCharacterLookupsLocked() {
	s.characterLookups.Store(&map[string]*characterLookupIndex{})
	for division := range s.characters {
		s.rebuildCharacterLookupLocked(division)
	}
}

func (src storeCharacterSource) CharacterByName(division, name string) *domain.Character {
	return src.s.characterLookup(division).names[characterLookupName(name)]
}
func (src storeCharacterSource) CharacterByID(division string, id int64) *domain.Character {
	return src.s.characterLookup(division).ids[id]
}
