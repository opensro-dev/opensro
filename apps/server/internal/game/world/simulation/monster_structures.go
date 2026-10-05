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
	delete(state.movers, gid)
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
