/*
===========================================================================

fieldpopulation.go - what a world event asks of the open world

An event has no character to resolve a population through, so it asks for
each division's open-world population directly, and for a field's ground
by the codename of a monster that lives there.

===========================================================================
*/
package simulation

import (
	"sort"

	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

// fieldDefaultWorld is the world code of the open world's nests; an empty
// code means the same (monster.NestRow.WorldCode).
const fieldDefaultWorld = "INS_DEFAULT"

/*
================
WorldPopulation

One division's open-world population.
================
*/
type WorldPopulation struct {
	Division string
	Lease    instance.Lease
}

/*
================
MonsterState.OpenWorldPopulations

Every started division's open-world population, in a stable order.
================
*/
func (s *MonsterState) OpenWorldPopulations() []WorldPopulation {
	s.mu.Lock()
	defer s.mu.Unlock()
	populations := make([]WorldPopulation, 0, len(s.divs))
	for division, state := range s.divs {
		populations = append(populations, WorldPopulation{Division: division, Lease: state.lease})
	}
	sort.Slice(populations, func(i, j int) bool { return populations[i].Division < populations[j].Division })
	return populations
}

/*
================
MonsterState.FieldNest

A field's anchor: the middle of the open-world nests of the monster that
lives there, so an event lands on real ground inside the field rather than
at its edge. The template never changes, so this reads it without a lock.
================
*/
func (s *MonsterState) FieldNest(codename string) (monster.NestRow, bool) {
	var nests []monster.NestRow
	for _, nest := range s.template.Nests {
		if !FeaturesAt(nest.RegionID).QuestMonsterSpawns || nest.WorldCode != "" && nest.WorldCode != fieldDefaultWorld {
			continue
		}
		if ref, found := s.template.Refs[nest.RefObjID]; found && ref.Codename == codename {
			nests = append(nests, nest)
		}
	}
	if len(nests) == 0 {
		return monster.NestRow{}, false
	}
	return nests[len(nests)/2], true
}
