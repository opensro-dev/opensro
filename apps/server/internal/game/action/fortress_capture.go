/*
===========================================================================

fortress_capture.go - structures falling, and the fortress changing hands

A structure's death is CGObjSiegeStruct_OnKilled (52D2B0): it stays where
it stood with its destroyed state, and the fortress world hears it
(CGObjSiegeStruct_BroadcastState385F_0B 4CF9A0, 0x3887 subtype 0x0B). The
last guard tower's fall starts the stone's countdown (0x0A); the fort
stone's fall hands the fortress to the guild that dealt it the most
damage, reinstalls every structure for that guild, sends everyone else in
the fortress to the town gate and tells the shard (subtype 8). A war's end
settles the holder as the occupying guild and reinstalls the structures
again. The capture rules live in the fortress authority (capture.go).

INFERENCE: the stone's damage per guild is the native _SiegeFortressStoneState
row (AccumulateDamage); this server reads it from the stone's damage
contributions, summed over each guild's members.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// TypeID4 of the CICATStruct band: fort stone 1, guard tower 2 (the
	// kinds CSiegeFortress_CountStandingGuardTowers 62A6B0 counts).
	structureKindFortStone  uint8 = 1
	structureKindGuardTower uint8 = 2
)

/*
================
structureDeath
================
*/
type structureDeath struct {
	division string
	gid      uint32
}

/*
================
structureFortress

The fortress a structure belongs to and its world, joined through the
structure's world code.
================
*/
func (rt *Runtime) structureFortress(division string, target monster.Instance) (fortress.Record, instance.ID, bool) {
	if rt.Fortresses == nil {
		return fortress.Record{}, 0, false
	}
	for _, definition := range instance.Shipped() {
		if definition.CodeName != target.Nest.WorldCode {
			continue
		}
		fortressID, ok := rt.Fortresses.ForWorld(definition)
		if !ok {
			return fortress.Record{}, 0, false
		}
		record, ok := rt.Fortresses.Get(division, fortressID)
		return record, instance.Pack(definition.ID, portalWorldLayer), ok
	}
	return fortress.Record{}, 0, false
}

/*
================
queueStructureDeath

A dead structure is not defeated: its state changes on the next tick.
================
*/
func (rt *Runtime) queueStructureDeath(division string, gid uint32) {
	rt.structureDeathsMu.Lock()
	rt.structureDeaths = append(rt.structureDeaths, structureDeath{division: division, gid: gid})
	rt.structureDeathsMu.Unlock()
}

/*
================
drainStructureDeaths
================
*/
func (rt *Runtime) drainStructureDeaths(nowMs int64) []simulation.DivisionFrames {
	rt.structureDeathsMu.Lock()
	due := rt.structureDeaths
	rt.structureDeaths = nil
	rt.structureDeathsMu.Unlock()
	var out []simulation.DivisionFrames
	for _, death := range due {
		out = append(out, rt.settleStructureDeath(death, nowMs)...)
	}
	return out
}

/*
================
settleStructureDeath

52D2B0, then the fortress's answer to the structure that fell.
================
*/
func (rt *Runtime) settleStructureDeath(death structureDeath, nowMs int64) []simulation.DivisionFrames {
	if rt.Monsters == nil {
		return nil
	}
	contributions := rt.Monsters.Contributions(death.division, death.gid)
	row, ok := rt.Monsters.MarkStructureDestroyed(death.division, death.gid)
	if !ok {
		return nil
	}
	record, world, ok := rt.structureFortress(death.division, row)
	if !ok {
		return nil
	}
	unlock := rt.lockDivision(death.division)
	defer unlock()
	rt.pushFortressWorld(death.division, world, siege.EncodeStructureState3887(siege.StructureState{
		FortressID: record.ID, GID: row.Gid, EventStructID: row.Nest.EventStructID, State: row.StructureState,
		Headquarters: row.Ref.TypeID4 == structureKindHeadquarters,
	}))
	switch row.Ref.TypeID4 {
	case structureKindGuardTower:
		if rt.Monsters.StandingStructures(death.division, world, structureKindGuardTower) == 0 &&
			rt.Fortresses.TowersFallen(death.division, record.ID, nowMs) {
			rt.pushFortressWorld(death.division, world, siege.EncodeTowersFallen3887(record.ID))
		}
	case structureKindFortStone:
		return rt.captureFortress(death.division, record, world, rt.topStoneGuild(death.division, contributions), nowMs)
	}
	return nil
}

/*
================
topStoneGuild

CSiegeFortress_TopStoneDamageGuild (62A5B0): the guild with the greatest
damage on the stone; ties keep the first in object order, as 62A5B0 keeps
the first greater value.
================
*/
func (rt *Runtime) topStoneGuild(division string, contributions []simulation.MonsterContribution) int64 {
	byGuild := map[int64]uint64{}
	var order []int64
	for _, c := range rt.deps.CharactersForDivision(division) {
		if c == nil || c.GuildID == nil || *c.GuildID == 0 {
			continue
		}
		for _, credit := range contributions {
			if credit.CreditGID == enterworld.ObjectIDForCharacter(c) {
				if _, seen := byGuild[*c.GuildID]; !seen {
					order = append(order, *c.GuildID)
				}
				byGuild[*c.GuildID] += uint64(credit.Damage)
			}
		}
	}
	var best int64
	var most uint64
	for _, guild := range order {
		if byGuild[guild] > most {
			best, most = guild, byGuild[guild]
		}
	}
	return best
}

/*
================
captureFortress

CSiegeFortress_OnFortStoneBroken (61F260) through the temporary owner's
result: the guild holds the fortress, its structures stand again for it,
the shard hears the conquest, and the fortress world keeps only the new
holder (CGameWorld_Siege_ApplyCapture 601400).
================
*/
func (rt *Runtime) captureFortress(division string, record fortress.Record, world instance.ID, guild int64, nowMs int64) []simulation.DivisionFrames {
	if guild == 0 || !rt.Fortresses.Capture(division, record.ID, guild, nowMs) {
		return nil
	}
	rt.Monsters.ReinstallStructures(division, world, nowMs)
	rt.forceFortressSave(division)
	gate, haveGate := rt.fortressTownGate(record.TownGate)
	for _, c := range rt.fortressResidents(division, world) {
		if c.GuildID != nil && *c.GuildID == guild {
			if rt.FortressList != nil && rt.PushCharacterFrames != nil {
				rt.PushCharacterFrames(division, c.Name, []wire.Frame{{Opcode: opFortressWarState, Payload: rt.FortressList(guild)}})
			}
			continue
		}
		if haveGate {
			rt.relocateCharacter(division, c, "fortress-capture-expel", func() (travelPoint, bool) { return gate, true })
		}
	}
	return rt.conquestFrames(division, record.ID, guild)
}

/*
================
conquestFrames

0x3887 subtype 8 to the whole division, naming the holding guild.
================
*/
func (rt *Runtime) conquestFrames(division string, fortressID uint32, guild int64) []simulation.DivisionFrames {
	row := siege.FortressRow{FortressID: fortressID}
	row.Discarded[0] = uint32(guild)
	if rt.Guilds != nil {
		if record, _, ok := rt.Guilds.Guild(division, guild); ok {
			row.OwnerName = record.Name
		}
	}
	return []simulation.DivisionFrames{{DivisionID: division, Frames: []simulation.Frame{{Opcode: opFortressWarState, Payload: siege.EncodeConquest3887(row)}}}}
}

/*
================
beginFortressWar

CSiegeFortress_BeginWar (61F130) for every fortress: the stone is guarded
while its guard towers stand.
================
*/
func (rt *Runtime) beginFortressWar(division string) {
	if rt.Fortresses == nil || rt.Monsters == nil {
		return
	}
	for _, definition := range instance.Shipped() {
		fortressID, ok := rt.Fortresses.ForWorld(definition)
		if !ok {
			continue
		}
		world := instance.Pack(definition.ID, portalWorldLayer)
		rt.Fortresses.BeginWar(division, fortressID, rt.Monsters.StandingStructures(division, world, structureKindGuardTower) > 0)
	}
}

/*
================
finishFortressWar

CSiegeFortress_RestoreOccupation (61F1E0) for every fortress: the holder
occupies it, its structures are reinstalled, and a change of hands is
announced. An unoccupied fortress after its war is not announced: the
v1.150 notice names a guild.
================
*/
func (rt *Runtime) finishFortressWar(division string, nowMs int64) []simulation.DivisionFrames {
	if rt.Fortresses == nil || rt.Monsters == nil {
		return nil
	}
	var out []simulation.DivisionFrames
	for _, definition := range instance.Shipped() {
		fortressID, ok := rt.Fortresses.ForWorld(definition)
		if !ok {
			continue
		}
		owner, changed := rt.Fortresses.FinishWar(division, fortressID)
		rt.Monsters.ReinstallStructures(division, instance.Pack(definition.ID, portalWorldLayer), nowMs)
		rt.forceFortressSave(division)
		if changed && owner != 0 {
			out = append(out, rt.conquestFrames(division, fortressID, owner)...)
		}
	}
	return out
}

/*
================
pushFortressWorld

One 0x3887 frame to every PC in a fortress world (CGameWorld_SendMsgToUser,
siege vtable +0x64).
================
*/
func (rt *Runtime) pushFortressWorld(division string, world instance.ID, payload []byte) {
	if rt.PushCharacterFrames == nil {
		return
	}
	for _, c := range rt.fortressResidents(division, world) {
		rt.PushCharacterFrames(division, c.Name, []wire.Frame{{Opcode: opFortressWarState, Payload: payload}})
	}
}
