/*
===========================================================================

monster_population_keys.go - the sorted population membership list

populationKey and populationKeys: the (division, lease) list every
population door walks, sorted once per membership change and cached
on MonsterState (popKeysMemo) between them. The membership writers
that drop the cache live with the population lifetime code in
monster_worlds.go and monsterstate.go; this file owns only the
list and its order.

===========================================================================
*/
package simulation

import (
	"sort"

	"opensro.online/server/internal/game/world/instance"
)

type populationKey struct {
	division string
	lease    instance.Lease
}

/*
================
populationKeys

The sorted (division, lease) list every population door walks.
The membership turns only when a population registers or
releases, so the sorted list is cached (popKeysMemo) and rebuilt
only after a membership writer - division, AllocatePopulation,
OpenInstanceLayer, ReleasePopulation - drops the cache; between
writes the per-tick doors re-sorted the same list and paid the
sort's swaps for it (#2033). A rebuild always allocates a fresh
slice: AdvancePopulation ranges the list after unlocking, so the
cached backing is never mutated in place. Callers hold s.mu, as
they already did.
================
*/
func (s *MonsterState) populationKeys() []populationKey {
	if s.popKeysMemo != nil {
		return s.popKeysMemo
	}
	keys := make([]populationKey, 0, len(s.divs)+len(s.worldPopulations))
	for division, state := range s.divs {
		keys = append(keys, populationKey{division, state.lease})
	}
	for key := range s.worldPopulations {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].division != keys[j].division {
			return keys[i].division < keys[j].division
		}
		return keys[i].lease.Generation < keys[j].lease.Generation
	})
	s.popKeysMemo = keys
	return keys
}
