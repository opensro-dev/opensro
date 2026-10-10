/*
===========================================================================

fortress_structures.go - the fortress manager's structure services

0x71E1 actions 0x18 (structure list), 0x0A to 0x0C (construction, upgrade,
repair), 0x15 (gate pulley), 0x16 and 0x17 (dismiss, demolish). The v1.188
dispatcher is CGObjPC_HandleSiegeAction705E (519E60); the v1.150 replies are
read by CPSMission_OnFortressManagerResponse0xB1E1 (754A40). The structures
themselves are population nests of the fortress world
(enterworld/fortress_structures.go), which the monster state owns.

===========================================================================
*/
package action

import (
	"sort"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/world/monster"
)

// fortressMaxStructureRows is the one-byte row count 61E000 writes (+0x2C).
const fortressMaxStructureRows = 255

/*
================
fortressStructureList

CSiegeFortressMgr_HandleStructureListQuery (634850): an unknown fortress
answers 2 with code 3; otherwise CSiegeFortress_WriteConstructionTimers
(61E000) writes the structure count and, per structure, its event zone
(vfunc +0x648) and the minutes left of its construction (+0x65C less the
minutes since +0x658, floored at zero). 754A40 stores each value on the
zone's record (+0x1C) for the manager's structure menu.

The port builds no structure over time yet, so every standing structure
reports zero minutes. INFERENCE: 61E000 walks a map keyed by structure;
the rows go out in event-zone order.
================
*/
func (rt *Runtime) fortressStructureList(division string, c *enterworld.Character, request siege.Interaction) OpResult {
	if rt.Fortresses == nil || rt.Monsters == nil {
		return fortressRefusal(request.Action, fortressErrUnknown)
	}
	world, known := rt.fortressWorlds()[request.Fortress]
	if _, exists := rt.Fortresses.Get(division, request.Fortress); !exists || !known {
		return fortressRefusal(request.Action, fortressErrInvalid)
	}
	structures := rt.Monsters.WorldStructures(division, world)
	if len(structures) > fortressMaxStructureRows {
		return fortressRefusal(request.Action, fortressErrUnknown)
	}
	sort.Slice(structures, func(i, j int) bool {
		return structures[i].Nest.EventStructID < structures[j].Nest.EventStructID
	})
	w := wire.NewWriter(8 + 8*len(structures)).U8(request.Action).U8(1).U32(request.Fortress).U8(uint8(len(structures)))
	for _, row := range structures {
		w.U32(row.Nest.EventStructID).U32(structureConstructionMinutesLeft(row))
	}
	return OpResult{Frames: []wire.Frame{{Opcode: opFortressInteractionResult, Payload: w.Payload()}}}
}

/*
================
structureConstructionMinutesLeft

61E000's per-structure timer. A structure the port spawned is built.
================
*/
func structureConstructionMinutesLeft(monster.Instance) uint32 {
	return 0
}
