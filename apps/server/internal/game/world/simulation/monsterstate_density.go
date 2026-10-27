/*
===========================================================================

monsterstate_density.go - hive player-density sampling and spawn-speed scaling

===========================================================================
*/
package simulation

import (
	"sort"

	worldgeom "opensro.online/server/internal/game/world"
)

/*
================
PopulationPlayer

A present PC in one native world instance. Dead PCs remain message-cell
members; reward eligibility's alive test is separate.
================
*/
type PopulationPlayer struct {
	GID   uint32
	World uint32
	Spawn Spawn
}

/*
================
MonsterState.SetPopulationPlayers
================
*/
func (s *MonsterState) SetPopulationPlayers(source func(string, int64) []PopulationPlayer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.players = source
}

/*
================
densityCell
================
*/
type densityCell struct {
	region uint16
	x, z   int
}

/*
================
populationCell
================
*/
func populationCell(p Spawn) (densityCell, bool) {
	// The retail server's message grid: six 320-unit message cells per outdoor region axis.
	// A dungeon's region lookup uses a different owner, never this grid.
	x, z, ok := worldgeom.OutdoorLocalCell(worldgeom.RegionXZ{RegionID: p.RegionID, X: p.X, Z: p.Z})
	if !ok {
		return densityCell{}, false
	}
	return densityCell{p.RegionID, x, z}, true
}

/*
================
MonsterState.sampleHiveDensity
================
*/
func (s *MonsterState) sampleHiveDensity(state *divisionMonsterState, players []PopulationPlayer, now int64) {
	state.lastDensityMs = now
	cells := make(map[densityCell]uint16)
	seen := make(map[uint32]bool)
	for _, player := range players {
		if player.World != uint32(state.lease.ID) || player.GID == 0 || seen[player.GID] {
			continue
		}
		seen[player.GID] = true
		if cell, ok := populationCell(player.Spawn); ok {
			cells[cell]++
		}
	}
	keys := make([]string, 0, len(state.hives))
	for key := range state.hives {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		members := s.template.HiveNestIndexes(key)
		policy := s.template.Nests[members[0]].HiveDensity
		if policy.Kind != 1 {
			continue
		}
		var count, total uint32
		for _, index := range members {
			nest := s.template.Nests[index]
			total += uint32(nest.InstanceLimit())
			if cell, ok := populationCell(spawnPointFrame(nest.SpawnPoint)); ok {
				count += uint32(cells[cell])
			}
		}
		h := state.hives[key]
		rate := h.density.Sample(count, policy.Denominator(total), policy)
		if rate == h.ratePct {
			continue
		}
		h.ratePct = rate
		// The retail server writes +0C then calls its spawn-state setter immediately. Last spawn
		// time is unchanged; only the interval and reduction are re-rolled.
		for _, index := range members {
			n, nest := state.nests[index], s.template.Nests[index]
			n.ratePct = rate
			n.intervalMs, n.reduceMs = s.nestDelay(nest, rate)
		}
		s.scheduleHive(state, key)
	}
}
