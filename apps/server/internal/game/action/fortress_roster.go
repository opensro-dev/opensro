/*
===========================================================================

fortress_roster.go - the fortress world's structure roster (0x3887 0x0F)

Every player in a fortress world hears which structure stands on each event
zone, with its hit points and state; the v1.150 client keeps one record per
zone from it for the battle aide's fortress map (76C870 case 0xF). The
v1.188 server sends it three ways: to a player entering a siege world (the
world-entry write 4DF9E0 -> CGameWorldMgr_SendSiegeStructAllInfoToPC
5F88C0), to the whole world every thirty seconds (CGameWorld_Siege_Tick
timer slot 4 -> CGameWorld_Siege_SendStructAllInfo 601860), and to the whole
world after a structure is built, demolished or upgraded and after a
conquest respawns them (CSiegeFortress_HandleDatabaseResult 6232C0 cases
0x13, 0x15, 0x16, 0x1D/0x1E -> 5F8890).

===========================================================================
*/
package action

import (
	"sort"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/world/instance"
)

// fortressRosterPeriodMs is 600F60's slot 4 period (0x7530).
const fortressRosterPeriodMs = 30000

/*
================
fortressRosterClock

When each division last sent its rosters, owned by advanceFortressRosters.
INFERENCE: native keeps the timer per siege world; one clock per division
sends every fortress world on the same beat, which no player can tell
apart.
================
*/
type fortressRosterClock struct {
	lastMs map[string]int64
}

/*
================
structureRoster

The 0x0F payload for one fortress world, rows in event-zone order (the
structure list's order). The port places no headquarters, so no row names
a guild.
================
*/
func (rt *Runtime) structureRoster(division string, fortressID uint32, world instance.ID) []byte {
	structures := rt.Monsters.WorldStructures(division, world)
	sort.Slice(structures, func(i, j int) bool {
		return structures[i].Nest.EventStructID < structures[j].Nest.EventStructID
	})
	rows := make([]siege.StructureRosterRow, 0, len(structures))
	for _, row := range structures {
		rows = append(rows, siege.StructureRosterRow{
			EventStructID: row.Nest.EventStructID,
			RefObjID:      row.Ref.RefObjID,
			HP:            row.CurrentHP,
			State:         row.StructureState,
		})
	}
	return siege.EncodeStructureRoster3887(fortressID, rows)
}

/*
================
pushStructureRoster

The roster to every player in the fortress world (601860).
================
*/
func (rt *Runtime) pushStructureRoster(division string, fortressID uint32, world instance.ID) {
	if rt.Monsters == nil {
		return
	}
	rt.pushFortressWorld(division, world, rt.structureRoster(division, fortressID, world))
}

/*
================
advanceFortressRosters

The siege tick's thirty-second roster to each fortress world.
================
*/
func (rt *Runtime) advanceFortressRosters(nowMs int64) {
	if rt.Fortresses == nil || rt.Monsters == nil {
		return
	}
	rt.fortressRosterMu.Lock()
	defer rt.fortressRosterMu.Unlock()
	if rt.fortressRoster.lastMs == nil {
		rt.fortressRoster.lastMs = map[string]int64{}
	}
	for _, division := range rt.Fortresses.Divisions() {
		if last, sent := rt.fortressRoster.lastMs[division]; sent && nowMs-last < fortressRosterPeriodMs {
			continue
		}
		rt.fortressRoster.lastMs[division] = nowMs
		unlock := rt.lockDivision(division)
		for fortressID, world := range rt.fortressWorlds() {
			rt.pushStructureRoster(division, fortressID, world)
		}
		unlock()
	}
}

/*
================
FortressRosterFrames

The roster a player entering a fortress world starts from (5F88C0 from
the world-entry write); nothing outside one.
================
*/
func (rt *Runtime) FortressRosterFrames(division string, c *enterworld.Character) []wire.Frame {
	if rt.Fortresses == nil || rt.Monsters == nil {
		return nil
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	snapshot := rt.characterSnapshot(division, c)
	if snapshot == nil {
		return nil
	}
	definition, ok := instance.Lookup(instance.ID(domain.CharacterWorldInstance(snapshot)).Definition())
	if !ok {
		return nil
	}
	fortressID, ok := rt.Fortresses.ForWorld(definition)
	if !ok {
		return nil
	}
	world := instance.Pack(definition.ID, portalWorldLayer)
	return []wire.Frame{{Opcode: opFortressWarState, Payload: rt.structureRoster(division, fortressID, world)}}
}
