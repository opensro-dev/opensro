/*
===========================================================================

monster_worlds.go - population lifetime and world-scoped monster queries

World leases isolate actors; interest queries use current movement geometry.

===========================================================================
*/
package simulation

import (
	"sort"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

const (
	// RefGameWorld +0x20: zero marks a world the shard opens at boot.
	permanentWorldType = 0
	// 65C200: the first layer a world hands out; zero is its controller.
	firstResidentLayer = 1
	// INS_DEFAULT, the field every character starts in.
	defaultWorldDefinition instance.DefinitionID = 1
)

type populationKey struct {
	division string
	lease    instance.Lease
}

/*
================
CurrentTimeMillis
================
*/
func (s *MonsterState) CurrentTimeMillis() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nowMillis()
}

// AllocatePopulation binds a fresh timer/actor population to an allocated
// layer. No elapsed callback runs in this transition. All layers share the
// process GID allocator and immutable references, never live nest counters.
/*
================
AllocatePopulation
================
*/
func (s *MonsterState) AllocatePopulation(division string, id instance.ID) (instance.Lease, instance.Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.division(division)
	definition, found := instance.Lookup(id.Definition())
	if !found {
		return instance.Lease{}, instance.InvalidLayer
	}
	lease, status := s.worldAllocators[division].Allocate(id)
	if status != instance.Success {
		return lease, status
	}
	if s.worldPopulations == nil {
		s.worldPopulations = make(map[populationKey]*divisionMonsterState)
	}
	s.worldPopulations[populationKey{division, lease}] = s.createPopulation(lease, definition.CodeName)
	return lease, status
}

// ReleasePopulation invalidates the lease and all of its timers, live GIDs,
// contribution ledgers, summon reservations and burn sources in one owner
// transaction. A callback retaining the old lease cannot remove a replacement.
/*
================
ReleasePopulation
================
*/
func (s *MonsterState) ReleasePopulation(division string, lease instance.Lease) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := populationKey{division, lease}
	state := s.worldPopulations[key]
	if state == nil {
		return false
	}
	if !s.worldAllocators[division].Release(lease) {
		panic("population lost its allocation owner")
	}
	if state := s.worldPopulations[key]; state != nil {
		state.instances.release()
	}
	delete(s.worldPopulations, key)
	delete(s.retirementRequests, key)
	return true
}

/*
================
populationForLease
================
*/
func (s *MonsterState) populationForLease(division string, lease instance.Lease) *divisionMonsterState {
	if state := s.divs[division]; state != nil && state.lease == lease {
		return state
	}
	return s.worldPopulations[populationKey{division, lease}]
}

// populationForObject follows a native GID lookup back to its population.
// Unknown GIDs return an empty lookup state; queries never allocate a world.
/*
================
populationForObject
================
*/
func (s *MonsterState) populationForObject(division string, gid uint32) *divisionMonsterState {
	if state := s.divs[division]; state != nil {
		if state.instances.contains(gid) {
			return state
		}
	}
	for key, state := range s.worldPopulations {
		if key.division == division {
			if state.instances.contains(gid) {
				return state
			}
		}
	}
	return &divisionMonsterState{}
}

/*
================
PopulationLease
================
*/
func (s *MonsterState) PopulationLease(division string, id instance.ID) (instance.Lease, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	allocator := s.worldAllocators[division]
	if allocator == nil {
		return instance.Lease{}, false
	}
	return allocator.Lookup(id)
}

/*
================
CheckPopulationTransfer

CGameWorld_CheckTransfer (5EC5B0): the non-mutating door a teleport asks
before it moves a PC into another world. Nothing is reserved.
================
*/
/*
================
CheckPopulationTransfer
================
*/
func (s *MonsterState) CheckPopulationTransfer(division string, destination instance.ID, capacityBypass bool) instance.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.division(division)
	return s.worldAllocators[division].CheckTransfer(destination, capacityBypass)
}

/*
================
AdmitPopulationPC
================
*/
func (s *MonsterState) AdmitPopulationPC(division string, lease instance.Lease, gid uint32, capacityBypass bool) instance.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.populationForLease(division, lease) == nil {
		return instance.InvalidLayer
	}
	return s.worldAllocators[division].AdmitPC(lease, gid, capacityBypass)
}

/*
================
LeavePopulationPC
================
*/
func (s *MonsterState) LeavePopulationPC(division string, lease instance.Lease, gid uint32) (instance.Status, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.populationForLease(division, lease) == nil {
		return instance.MissingLayer, false
	}
	status, request := s.worldAllocators[division].LeavePC(lease, gid)
	if request {
		s.requestPopulationRetirement(division, lease)
	}
	return status, request
}

/*
================
BeginPopulationRetirement
================
*/
func (s *MonsterState) BeginPopulationRetirement(division string, lease instance.Lease) ([]uint32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.populationForLease(division, lease) == nil {
		return nil, false
	}
	residents, exists := s.worldAllocators[division].BeginRetirement(lease)
	if exists && len(residents) == 0 {
		s.requestPopulationRetirement(division, lease)
	}
	return residents, exists
}

/*
================
requestPopulationRetirement
================
*/
func (s *MonsterState) requestPopulationRetirement(division string, lease instance.Lease) {
	if s.retirementRequests == nil {
		s.retirementRequests = make(map[populationKey]struct{})
	}
	s.retirementRequests[populationKey{division, lease}] = struct{}{}
}

// PendingPopulationRetirements is the manager's native 7C14 release-request
// inbox. Reading does not acknowledge it: only releasing that exact lifetime
// removes the request, so a disconnected caller cannot lose the last leave.
/*
================
PendingPopulationRetirements
================
*/
func (s *MonsterState) PendingPopulationRetirements(division string) []instance.Lease {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []instance.Lease
	for key := range s.retirementRequests {
		if key.division == division {
			out = append(out, key.lease)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Generation < out[j].Generation })
	return out
}

// PopulationInstances is a read: allocation, callbacks and random draws are
// separate operations. The exact lease is required, including its generation.
/*
================
PopulationInstances
================
*/
func (s *MonsterState) PopulationInstances(division string, lease instance.Lease, regions []uint16) []monster.Instance {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(division, lease)
	if state == nil {
		return nil
	}
	var out []monster.Instance
	seen := make(map[uint16]bool, len(regions))
	for _, region := range regions {
		if seen[region] {
			continue
		}
		seen[region] = true
		for _, gid := range state.byRegion[region] {
			out = append(out, state.instances.get(gid))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Gid < out[j].Gid })
	return out
}

// PopulationInterestInstances projects one allocated lifetime under the owner
// lock. Release, pose changes and replacement allocation cannot interleave
// between candidate collection and visibility filtering.
/*
================
PopulationInterestInstances
================
*/
func (s *MonsterState) PopulationInterestInstances(division string, lease instance.Lease, viewer worldgeom.RegionXZ, nowMs int64) []monster.Instance {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(division, lease)
	if state == nil {
		return nil
	}
	gids := populationInterestGIDs(state, viewer, nowMs)
	out := make([]monster.Instance, 0, len(gids))
	for _, gid := range gids {
		out = append(out, state.instances.get(gid))
	}
	return out
}

/*
================
populationInterestGIDs

The spawn index owns nest/lifecycle queries. Visibility instead queries the
mover's live region bounds: a pursuing actor can leave its spawn region ring.
Filter exact live blocks before any archived actor read (native 53AC20).
Caller holds MonsterState.mu.
================
*/
func populationInterestGIDs(state *divisionMonsterState, viewer worldgeom.RegionXZ, nowMs int64) []uint32 {
	var out []uint32
	for gid := range state.movers.candidates(viewer) {
		if !state.instances.contains(gid) {
			continue
		}
		pose, _ := state.movers.livePoseAt(gid, nowMs, nil)
		if worldgeom.InterestVisible(viewer, worldgeom.RegionXZ{RegionID: pose.RegionID, X: pose.X, Z: pose.Z}) {
			out = append(out, gid)
		}
	}
	// Static entries without a mover still use their immutable spawn position.
	for _, region := range RegionScopeRing(viewer.RegionID) {
		for _, gid := range state.byRegion[region] {
			if state.movers.has(gid) {
				continue
			}
			actor := state.instances.get(gid)
			position := worldgeom.RegionXZ{RegionID: actor.Spawn.RegionID, X: actor.Spawn.X, Z: actor.Spawn.Z}
			if worldgeom.InterestVisible(viewer, position) {
				out = append(out, gid)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Visibility is a delta stream. Copy full actors only for new/unseeded viewers,
// not every already-published monster on every viewer tick. Both the identity
// set and required snapshots are captured under the same population lock.
/*
================
populationInterestDelta
================
*/
func (s *MonsterState) populationInterestDelta(division string, lease instance.Lease, viewer worldgeom.RegionXZ, nowMs int64, known map[uint32]bool) ([]uint32, map[uint32]monster.Instance) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(division, lease)
	if state == nil {
		return nil, nil
	}
	gids := populationInterestGIDs(state, viewer, nowMs)
	var snapshots map[uint32]monster.Instance
	for _, gid := range gids {
		if known[gid] {
			continue
		}
		if snapshots == nil {
			snapshots = make(map[uint32]monster.Instance)
		}
		snapshots[gid] = state.instances.get(gid)
	}
	return gids, snapshots
}

/*
================
ObjectPopulation
================
*/
func (s *MonsterState) ObjectPopulation(division string, gid uint32) (instance.Lease, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	return state.lease, state.instances.contains(gid)
}

// GetInWorld is the actor-facing lookup. A GID remains globally resolvable
// for internal callbacks, but a resident cannot use it across a world boundary.
/*
================
GetInWorld
================
*/
func (s *MonsterState) GetInWorld(division string, world instance.ID, gid uint32) (monster.Instance, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	if state.lease.ID != world {
		return monster.Instance{}, false
	}
	actor, exists := state.instances.lookup(gid)
	if exists {
		if updated := finishSummonAction(actor, s.nowMillis()); updated != actor {
			actor = updated
			state.instances.set(gid, actor)
		}
	}
	return actor, exists
}

/*
================
GetInPopulation
================
*/
func (s *MonsterState) GetInPopulation(division string, lease instance.Lease, gid uint32) (monster.Instance, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(division, lease)
	if state == nil {
		return monster.Instance{}, false
	}
	actor, exists := state.instances.lookup(gid)
	if exists {
		if updated := finishSummonAction(actor, s.nowMillis()); updated != actor {
			actor = updated
			state.instances.set(gid, actor)
		}
	}
	return actor, exists
}

/*
================
populationKeys
================
*/
func (s *MonsterState) populationKeys() []populationKey {
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
	return keys
}
