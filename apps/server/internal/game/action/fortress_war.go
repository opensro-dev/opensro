/*
===========================================================================

fortress_war.go - what a fortress world does when its war begins and ends

CGameWorld_Siege_Tick (600F60) runs the war's phases on the fortress
world: five seconds after the war begins (timer slot 1, 0x1388 ms) and at
once when it ends (slot 2), CGameWorld_Siege_BroadcastWarPhase (601170)
walks the PCs of the world's layer. At the beginning (mode 0) an occupied
fortress keeps its occupying guild, which is sent its fortress row
(CGObjPC_SendFortressRow 4E0680); everyone else, and everyone when the
fortress is unoccupied, is teleported to the fortress's town gate
(CGameWorld_Siege_GetFortressTeleportPos 601690). At the end (mode 2)
only the guilds the fortress keeps after the war stay: its occupying
guild, as this server has no guild unions.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	fortressPhaseBegin uint8 = 0
	fortressPhaseEnd   uint8 = 2
	// 600F60 slot 1: the beginning's expulsion waits five seconds.
	fortressBeginDelayMs = 5000
)

/*
================
fortressPhase
================
*/
type fortressPhase struct {
	division string
	mode     uint8
	dueMs    int64
}

/*
================
FortressWarChanged

The fortress-war lane's war edge for one division: schedules the fortress
worlds' phase.
================
*/
func (rt *Runtime) FortressWarChanged(division string, nowMs int64, active bool) []simulation.DivisionFrames {
	phase := fortressPhase{division: division, mode: fortressPhaseEnd, dueMs: nowMs}
	if active {
		phase.mode, phase.dueMs = fortressPhaseBegin, nowMs+fortressBeginDelayMs
	}
	rt.fortressPhasesMu.Lock()
	rt.fortressPhases = append(rt.fortressPhases, phase)
	rt.fortressPhasesMu.Unlock()
	return nil
}

/*
================
advanceFortressPhases
================
*/
func (rt *Runtime) advanceFortressPhases(nowMs int64) {
	rt.fortressPhasesMu.Lock()
	var due []fortressPhase
	kept := rt.fortressPhases[:0]
	for _, phase := range rt.fortressPhases {
		if phase.dueMs <= nowMs {
			due = append(due, phase)
		} else {
			kept = append(kept, phase)
		}
	}
	rt.fortressPhases = kept
	rt.fortressPhasesMu.Unlock()
	for _, phase := range due {
		rt.runFortressPhase(phase)
	}
}

/*
================
runFortressPhase

601170 for every fortress world of the division.
================
*/
func (rt *Runtime) runFortressPhase(phase fortressPhase) {
	if rt.Fortresses == nil {
		return
	}
	unlock := rt.lockDivision(phase.division)
	defer unlock()
	for _, definition := range instance.Shipped() {
		fortressID, ok := rt.Fortresses.ForWorld(definition)
		if !ok {
			continue
		}
		record, ok := rt.Fortresses.Get(phase.division, fortressID)
		if !ok {
			continue
		}
		gate, ok := rt.fortressTownGate(record.TownGate)
		if !ok {
			continue
		}
		world := instance.Pack(definition.ID, portalWorldLayer)
		for _, c := range rt.fortressResidents(phase.division, world) {
			if record.GuildID != 0 && c.GuildID != nil && *c.GuildID == record.GuildID {
				if phase.mode == fortressPhaseBegin && rt.FortressList != nil && rt.PushCharacterFrames != nil {
					rt.PushCharacterFrames(phase.division, c.Name, []wire.Frame{{Opcode: opFortressWarState, Payload: rt.FortressList(record.GuildID)}})
				}
				continue
			}
			rt.relocateCharacter(phase.division, c, "fortress-war-expel", func() (travelPoint, bool) { return gate, true })
		}
	}
}

/*
================
fortressResidents

The characters admitted to a world's layer, in name order.
================
*/
func (rt *Runtime) fortressResidents(division string, world instance.ID) []*enterworld.Character {
	var out []*enterworld.Character
	rt.characterAdmissions.Range(func(_, value any) bool {
		admission := value.(populationAdmission)
		if admission.division == division && admission.lease.ID == world {
			if c := rt.findCharacter(division, admission.name); c != nil {
				out = append(out, c)
			}
		}
		return true
	})
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Name < out[j-1].Name; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

/*
================
fortressTownGate

The spawn and world of the teleport a fortress's row names as its town
gate (siegefortress.txt column 6, e.g. GATE_CH).
================
*/
func (rt *Runtime) fortressTownGate(code string) (travelPoint, bool) {
	if rt.portals == nil {
		return travelPoint{}, false
	}
	for _, destination := range rt.portals.destinations {
		if destination.code == code {
			return travelPoint{spawn: destination.spawn, world: portalWorld(destination)}, true
		}
	}
	return travelPoint{}, false
}
