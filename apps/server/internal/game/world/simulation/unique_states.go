/*
===========================================================================

unique_states.go - every unique monster's spawn and death times

Port-only, not native: the community site's unique tracker shows whether
each unique is alive and when it spawned. The original keeps the same
times on its nests but never publishes them. When a dead unique
returns stays secret (owner, 2026-10-11), so no respawn delay or death
time leaves this file.

The timeline is written on the spawn and defeat paths the population
already runs under its lock; UniqueStates copies it for a reader that runs
off the tick.

===========================================================================
*/
package simulation

import (
	"sort"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
uniqueTimeline

One unique reference's last spawn and death in a division.
================
*/
type uniqueTimeline struct {
	live        int
	spawnedAtMs int64
	regionID    uint16
}

/*
================
UniqueState

What the public tracker knows about one unique reference. SpawnedAtMs is
zero until the first spawn since the server started.
================
*/
type UniqueState struct {
	RefObjID    uint32
	Codename    string
	Level       uint8
	RegionID    uint16
	Alive       bool
	SpawnedAtMs int64
}

/*
================
nestIsUnique

A nest spawns a unique when its rarity (the nest's override, else the
reference's monster type) has low nibble 3, as Instance.Rarity reads it.
================
*/
func nestIsUnique(nest monster.NestRow, ref monster.MonsterRef) bool {
	if nest.HasRarityOverride {
		return nest.RarityOverride&15 == 3
	}
	return ref.MonsterType&15 == 3
}

/*
================
markUniqueSpawn
================
*/
func (state *divisionMonsterState) markUniqueSpawn(ref uint32, regionID uint16, nowMs int64) {
	if state.uniqueTimes == nil {
		state.uniqueTimes = map[uint32]*uniqueTimeline{}
	}
	line := state.uniqueTimes[ref]
	if line == nil {
		line = &uniqueTimeline{}
		state.uniqueTimes[ref] = line
	}
	line.live++
	line.spawnedAtMs = nowMs
	line.regionID = regionID
}

/*
================
markUniqueDeath
================
*/
func (state *divisionMonsterState) markUniqueDeath(ref uint32) {
	line := state.uniqueTimes[ref]
	if line == nil {
		return
	}
	if line.live > 0 {
		line.live--
	}
}

/*
================
UniqueStates

Every unique the division's nests can spawn, ordered by reference id, with
its live state. One reference may own several nests (a hive of alternative
locations): it is alive while any of them holds it.
================
*/
func (s *MonsterState) UniqueStates(division string) []UniqueState {
	s.mu.Lock()
	defer s.mu.Unlock()
	byRef := map[uint32]*UniqueState{}
	for _, nest := range s.template.Nests {
		ref, ok := s.template.Refs[nest.RefObjID]
		if !ok || !nestIsUnique(nest, ref) {
			continue
		}
		entry := byRef[ref.RefObjID]
		if entry == nil {
			entry = &UniqueState{RefObjID: ref.RefObjID, Codename: ref.Codename, Level: ref.Level,
				RegionID: nest.RegionID}
			byRef[ref.RefObjID] = entry
		}
	}
	if state := s.divs[division]; state != nil {
		for ref, line := range state.uniqueTimes {
			entry := byRef[ref]
			if entry == nil {
				continue
			}
			entry.Alive = line.live > 0
			entry.SpawnedAtMs = line.spawnedAtMs
			if line.regionID != 0 {
				entry.RegionID = line.regionID
			}
		}
	}
	out := make([]UniqueState, 0, len(byRef))
	for _, entry := range byRef {
		out = append(out, *entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RefObjID < out[j].RefObjID })
	return out
}

/*
================
MonsterName

The retail display name of any monster reference, unique or not, or "" for
an id the template does not hold. The template never changes after boot,
so no lock is taken.
================
*/
func (s *MonsterState) MonsterName(refObjID uint32) string {
	ref, ok := s.template.Refs[refObjID]
	if !ok {
		return ""
	}
	return ref.DisplayName()
}
