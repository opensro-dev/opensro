/*
===========================================================================

fortress_persist.go - fortress structures across a restart

The siege tick persists its fortress every thirty seconds (600F60 slot 0,
CSiegeFortress_PersistState), and a structure's row is rewritten once its
hit points have moved by 500 or its state changed
(CGObjSiegeStruct_PublishChangedVitals 4CFB60, 0x1F4). After a restart the
first tick that finds every fortress world's structures standing puts each
zone's stored hit points and state back on it; a zone whose stored
structure is another reference keeps the fresh one.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

const (
	fortressPersistPeriodMs = 30000
	structureHPSaveDelta    = 500
)

/*
================
savedStructureKey
================
*/
type savedStructureKey struct {
	division string
	fortress uint32
	zone     uint32
}

/*
================
fortressPersistence

What this runtime last saved and restored, owned by advanceFortressStructures.
================
*/
type fortressPersistence struct {
	saved    map[savedStructureKey]domain.FortressStructureRecord
	restored map[string]bool
	lastMs   map[string]int64
}

/*
================
advanceFortressStructures
================
*/
func (rt *Runtime) advanceFortressStructures(nowMs int64) {
	if rt.Fortresses == nil || rt.Monsters == nil || rt.FortressStore == nil {
		return
	}
	rt.fortressPersistMu.Lock()
	defer rt.fortressPersistMu.Unlock()
	p := &rt.fortressPersist
	if p.saved == nil {
		p.saved, p.restored, p.lastMs = map[savedStructureKey]domain.FortressStructureRecord{}, map[string]bool{}, map[string]int64{}
	}
	for _, division := range rt.Fortresses.Divisions() {
		if !p.restored[division] && !rt.restoreFortressStructures(division) {
			continue
		}
		p.restored[division] = true
		if nowMs-p.lastMs[division] < fortressPersistPeriodMs {
			continue
		}
		p.lastMs[division] = nowMs
		rt.saveFortressStructures(division)
	}
}

/*
================
fortressWorlds

Each fortress of the shipped catalog with its world.
================
*/
func (rt *Runtime) fortressWorlds() map[uint32]instance.ID {
	out := map[uint32]instance.ID{}
	for _, definition := range instance.Shipped() {
		if fortressID, ok := rt.Fortresses.ForWorld(definition); ok {
			out[fortressID] = instance.Pack(definition.ID, portalWorldLayer)
		}
	}
	return out
}

/*
================
restoreFortressStructures

Applies the stored rows once every fortress world's structures stand;
false until then (the populations spawn on their first hive tick).
================
*/
func (rt *Runtime) restoreFortressStructures(division string) bool {
	byZone := map[savedStructureKey]monster.Instance{}
	for fortressID, world := range rt.fortressWorlds() {
		structures := rt.Monsters.WorldStructures(division, world)
		if len(structures) == 0 {
			return false
		}
		for _, row := range structures {
			byZone[savedStructureKey{division, fortressID, row.Nest.EventStructID}] = row
		}
	}
	stored, err := rt.FortressStore.FortressStructures(division)
	if err != nil {
		return false
	}
	for _, record := range stored {
		key := savedStructureKey{division, record.FortressID, record.EventStructID}
		row, ok := byZone[key]
		if !ok || row.Ref.RefObjID != record.RefObjID {
			continue
		}
		rt.Monsters.RestoreStructure(division, row.Gid, record.HP, record.State)
		rt.fortressPersist.saved[key] = record
	}
	return true
}

/*
================
saveFortressStructures

Writes every structure whose row moved enough since it was last saved.
================
*/
func (rt *Runtime) saveFortressStructures(division string) {
	for fortressID, world := range rt.fortressWorlds() {
		record, _ := rt.Fortresses.Get(division, fortressID)
		for _, row := range rt.Monsters.WorldStructures(division, world) {
			key := savedStructureKey{division, fortressID, row.Nest.EventStructID}
			next := domain.FortressStructureRecord{
				FortressID: fortressID, EventStructID: row.Nest.EventStructID, RefObjID: row.Ref.RefObjID,
				OwnerGuildID: record.Holder(), HP: row.CurrentHP, State: row.StructureState,
			}
			previous, saved := rt.fortressPersist.saved[key]
			if saved && previous.RefObjID == next.RefObjID && previous.State == next.State &&
				previous.OwnerGuildID == next.OwnerGuildID && hpDelta(previous.HP, next.HP) < structureHPSaveDelta {
				continue
			}
			if rt.FortressStore.SaveFortressStructure(division, next, true) == nil {
				rt.fortressPersist.saved[key] = next
			}
		}
	}
}

/*
================
hpDelta
================
*/
func hpDelta(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

/*
================
forceFortressSave

A reinstall rewrites every row on the next tick.
================
*/
func (rt *Runtime) forceFortressSave(division string) {
	rt.fortressPersistMu.Lock()
	defer rt.fortressPersistMu.Unlock()
	if rt.fortressPersist.lastMs != nil {
		rt.fortressPersist.lastMs[division] = -fortressPersistPeriodMs
	}
}
