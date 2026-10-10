/*
===========================================================================

monster_structures.go - fortress structures in their population

A fortress structure lives in its world's population like a monster, but
its death is a state, not a removal: CGObjSiegeStruct_OnKilled (52D2B0)
sets the destroyed bit and the structure stays where it stood until the
fortress's structures are reinstalled. The fortress-war owner in the
action package drives these functions; this file only keeps the
population consistent.

===========================================================================
*/
package simulation

import (
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

const (
	// 52D2B0 passes CGObjSiegeStruct_SetState (4CF860) 1, or 3 for a gate.
	structureStateDestroyed     uint16 = 1
	structureStateGateDestroyed uint16 = 3
	structureKindGate           uint8  = 3
)

/*
================
MarkStructureDestroyed

Sets a dead structure's destroyed state and returns it; false when gid is
not a dead structure or is already marked.
================
*/
func (s *MonsterState) MarkStructureDestroyed(divisionID string, gid uint32) (monster.Instance, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(divisionID, gid)
	row, ok := state.instances.lookup(gid)
	if !ok || !row.Ref.Structure || row.CurrentHP != 0 || row.StructureState&structureStateDestroyed != 0 {
		return monster.Instance{}, false
	}
	row.StructureState = structureStateDestroyed
	if row.Ref.TypeID4 == structureKindGate {
		row.StructureState = structureStateGateDestroyed
	}
	state.instances.set(gid, row)
	return row, true
}

/*
================
StandingStructures

How many structures of TypeID4 kind still stand in a world's population
(CSiegeFortress_CountStandingGuardTowers 62A6B0 counts kind 2).
================
*/
func (s *MonsterState) StandingStructures(divisionID string, world instance.ID, kind uint8) int {
	lease, ok := s.PopulationLease(divisionID, world)
	if !ok {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(divisionID, lease)
	if state == nil {
		return 0
	}
	standing := 0
	for _, row := range state.instances.values() {
		if row.Ref.Structure && row.Ref.TypeID4 == kind && row.StructureState&structureStateDestroyed == 0 {
			standing++
		}
	}
	return standing
}

/*
================
Contributions

The damage each credited actor dealt gid, in object order.
================
*/
func (s *MonsterState) Contributions(divisionID string, gid uint32) []MonsterContribution {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.populationForObject(divisionID, gid).contributionSnapshot(gid)
}

/*
================
ReinstallStructures

Removes every structure of a world's population and spawns each one's
nest afresh, at full hit points under a new object: the reinstall that
follows a capture and a war's end (the 0x1D/0x1E results of
CSiegeFortress_HandleDatabaseResult 6232C0). Returns how many stand again.
================
*/
func (s *MonsterState) ReinstallStructures(divisionID string, world instance.ID, nowMs int64) int {
	lease, ok := s.PopulationLease(divisionID, world)
	if !ok {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(divisionID, lease)
	if state == nil {
		return 0
	}
	var nests []int
	for gid, row := range state.instances.values() {
		if !row.Ref.Structure {
			continue
		}
		index, attached := state.gidNests[gid]
		s.removeInstanceLocked(state, gid, row)
		if attached {
			if n := state.nests[index]; n.live > 0 {
				n.live--
			}
			nests = append(nests, index)
		}
	}
	installed := 0
	for _, index := range nests {
		if s.attemptNestSpawn(state, index, nowMs) {
			installed++
		}
	}
	return installed
}

/*
================
SetStructureOccupant

The one owner of a fortress zone's occupant: the stored row's RefObjID.
0 vacates the zone, as a demolition does (CSiegeFortress_HandleDatabaseResult
6232C0 case 0x15 erases the structure and releases it): whatever stands
there goes, and neither the hive nor ReinstallStructures spawns it again.
Any other reference replaces what stands there with that structure at full
hit points, also when it is not the nest's authored default (construction,
upgrade). Reports false when the world has no structure nest on zone.
================
*/
func (s *MonsterState) SetStructureOccupant(divisionID string, world instance.ID, zone uint32, refObjID uint32, nowMs int64) bool {
	lease, ok := s.PopulationLease(divisionID, world)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(divisionID, lease)
	if state == nil {
		return false
	}
	index, found := -1, false
	for candidate := range state.nests {
		nest := s.template.Nests[candidate]
		if nest.EventStructID == zone && s.template.Refs[nest.RefObjID].Structure {
			index, found = candidate, true
			break
		}
	}
	if !found {
		return false
	}
	if _, known := s.template.Refs[refObjID]; refObjID != 0 && !known {
		return false
	}
	n := state.nests[index]
	for gid, attached := range state.gidNests {
		if attached != index {
			continue
		}
		if row, live := state.instances.lookup(gid); live {
			s.removeInstanceLocked(state, gid, row)
		}
		if n.live > 0 {
			n.live--
		}
	}
	n.vacant, n.occupant = refObjID == 0, 0
	if refObjID != 0 && refObjID != s.template.Nests[index].RefObjID {
		n.occupant = refObjID
	}
	if n.vacant {
		return true
	}
	return s.attemptNestSpawn(state, index, nowMs)
}

/*
================
StructureZone

A fortress world's structure site on zone: the reference the event zone
authors for it (the nest's default) and whether a structure stands there
now. A zero reference when the zone names a structure that has none;
false when the world has no structure nest on zone.
================
*/
func (s *MonsterState) StructureZone(divisionID string, world instance.ID, zone uint32) (monster.MonsterRef, bool, bool) {
	lease, ok := s.PopulationLease(divisionID, world)
	if !ok {
		return monster.MonsterRef{}, false, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(divisionID, lease)
	if state == nil {
		return monster.MonsterRef{}, false, false
	}
	for index := range state.nests {
		nest := s.template.Nests[index]
		if nest.EventStructID != zone {
			continue
		}
		ref, known := s.template.Refs[nest.RefObjID]
		if !known {
			// 6341B0's 0x281E: the zone names a structure with no reference.
			return monster.MonsterRef{}, false, true
		}
		if !ref.Structure {
			continue
		}
		occupied := false
		for _, attached := range state.gidNests {
			occupied = occupied || attached == index
		}
		return ref, occupied, true
	}
	return monster.MonsterRef{}, false, false
}

/*
================
removeInstanceLocked

Drops one instance and everything keyed by it, as Defeat does, without
the nest's death bookkeeping.
================
*/
func (s *MonsterState) removeInstanceLocked(state *divisionMonsterState, gid uint32, row monster.Instance) {
	state.instances.remove(gid)
	state.forgetDormant(gid)
	delete(state.contributions, gid)
	state.releaseApproachActor(gid)
	state.movers.remove(gid)
	state.behavior.remove(gid)
	delete(state.aiTimers, gid)
	delete(state.gidNests, gid)
	removeRegionGid(state.byRegion, row.Spawn.RegionID, gid)
}

/*
================
WorldStructures

Every structure of a world's population, standing or destroyed.
================
*/
func (s *MonsterState) WorldStructures(divisionID string, world instance.ID) []monster.Instance {
	lease, ok := s.PopulationLease(divisionID, world)
	if !ok {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(divisionID, lease)
	if state == nil {
		return nil
	}
	var out []monster.Instance
	for _, row := range state.instances.values() {
		if row.Ref.Structure {
			out = append(out, row)
		}
	}
	return out
}

/*
================
RestoreStructure

Puts a stored structure's hit points and state back on the instance that
stands on its event zone (never above its maximum). Reports whether a
structure was there.
================
*/
func (s *MonsterState) RestoreStructure(divisionID string, gid uint32, hp uint32, state uint16) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	population := s.populationForObject(divisionID, gid)
	row, ok := population.instances.lookup(gid)
	if !ok || !row.Ref.Structure {
		return false
	}
	row.CurrentHP = min(hp, row.EffectiveMaxHP())
	row.StructureState = state
	if row.StructureState&structureStateDestroyed != 0 {
		row.CurrentHP = 0
	}
	population.instances.set(gid, row)
	return true
}

/*
================
HealStructure

The population remains the only HP owner. A destroyed structure cannot be
revived by a delayed pulse after its target snapshot was taken.
================
*/
func (s *MonsterState) HealStructure(division string, gid uint32, amount uint32) (monster.Instance, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	population := s.populationForObject(division, gid)
	row, ok := population.instances.lookup(gid)
	if !ok || !row.Ref.Structure || row.CurrentHP == 0 || row.StructureState&structureStateDestroyed != 0 {
		return monster.Instance{}, false
	}
	row.CurrentHP += min(amount, row.EffectiveMaxHP()-row.CurrentHP)
	population.instances.set(gid, row)
	return row, true
}
