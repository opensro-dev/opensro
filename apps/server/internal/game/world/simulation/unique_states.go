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

One unique reference's living copies in a division, keyed by gid, and its
last spawn. A reference can stand more than once at a time: the Ch'in Tomb's
Sarin generals have their own Floor 3 nests, and the Floor 6 South stage
raises the same four references again (the v1.188 server's Floor 6 spawner
setup). A per-reference counter with one spawn time showed a killed
copy's neighbour as "up since" the dead copy's spawn; each copy is its own
row here, and a death or despawn removes exactly the copy it names.
================
*/
type uniqueTimeline struct {
	copies      map[uint32]UniqueCopy
	spawnedAtMs int64
	regionID    uint16
}

/*
================
UniqueCopy

One living copy of a unique: where it stands and when it spawned.
================
*/
type UniqueCopy struct {
	RegionID    uint16
	SpawnedAtMs int64
}

/*
================
UniqueState

What the public tracker knows about one unique reference. Copies lists the
living copies, oldest first; RegionID and SpawnedAtMs are the oldest living
copy's, or the last spawn's while none lives. SpawnedAtMs is zero until the
first spawn since the server started.
================
*/
type UniqueState struct {
	RefObjID    uint32
	Codename    string
	Level       uint8
	RegionID    uint16
	Alive       bool
	SpawnedAtMs int64
	Copies      []UniqueCopy
}

/*
================
markUniqueSpawn
================
*/
func (state *divisionMonsterState) markUniqueSpawn(ref, gid uint32, regionID uint16, nowMs int64) {
	if state.uniqueTimes == nil {
		state.uniqueTimes = map[uint32]*uniqueTimeline{}
	}
	line := state.uniqueTimes[ref]
	if line == nil {
		line = &uniqueTimeline{copies: map[uint32]UniqueCopy{}}
		state.uniqueTimes[ref] = line
	}
	line.copies[gid] = UniqueCopy{RegionID: regionID, SpawnedAtMs: nowMs}
	line.spawnedAtMs = nowMs
	line.regionID = regionID
}

/*
================
markUniqueDeath

Forgets the copy gid names. A gid the timeline never marked (a quest
unique spawned without its announcement) leaves every other copy alone.
================
*/
func (state *divisionMonsterState) markUniqueDeath(ref, gid uint32) {
	if line := state.uniqueTimes[ref]; line != nil {
		delete(line.copies, gid)
	}
}

/*
================
livingCopies

The timeline's living copies, oldest first (gid breaks ties so the order
is stable).
================
*/
func (line *uniqueTimeline) livingCopies() []UniqueCopy {
	gids := make([]uint32, 0, len(line.copies))
	for gid := range line.copies {
		gids = append(gids, gid)
	}
	sort.Slice(gids, func(i, j int) bool {
		a, b := line.copies[gids[i]], line.copies[gids[j]]
		if a.SpawnedAtMs != b.SpawnedAtMs {
			return a.SpawnedAtMs < b.SpawnedAtMs
		}
		return gids[i] < gids[j]
	})
	out := make([]UniqueCopy, len(gids))
	for i, gid := range gids {
		out[i] = line.copies[gid]
	}
	return out
}

/*
================
UniqueStates

Every unique the division's nests can spawn, ordered by reference id, with
its live state. One reference may own several nests (a hive of alternative
locations), and a trigger stage can raise it beside its nest: it is alive
while any copy lives.
================
*/
func (s *MonsterState) UniqueStates(division string) []UniqueState {
	s.mu.Lock()
	defer s.mu.Unlock()
	byRef := map[uint32]*UniqueState{}
	for _, nest := range s.template.Nests {
		ref, ok := s.template.Refs[nest.RefObjID]
		if !ok || !monster.NestIsUnique(nest, ref) {
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
			entry.Copies = line.livingCopies()
			entry.Alive = len(entry.Copies) > 0
			entry.SpawnedAtMs = line.spawnedAtMs
			if line.regionID != 0 {
				entry.RegionID = line.regionID
			}
			if entry.Alive {
				entry.SpawnedAtMs = entry.Copies[0].SpawnedAtMs
				entry.RegionID = entry.Copies[0].RegionID
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
