/*
===========================================================================

fortress_structures.go - the fortress manager's structure services

0x71E1 actions 0x18 (structure list), 0x0A to 0x0C (construction, upgrade,
repair), 0x15 (gate pulley), 0x16 and 0x17 (dismiss, demolish). The
reference server's siege action dispatcher serves them; the v1.150 replies are
read by CPSMission_OnFortressManagerResponse0xB1E1 (754A40). The structures
themselves are population nests of the fortress world
(enterworld/fortress_structures.go), which the monster state owns.

===========================================================================
*/
package action

import (
	"math"
	"sort"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// fortressMaxStructureRows is the one-byte row count the native list writes (+0x2C).
	fortressMaxStructureRows = 255

	// Low bytes of the structure-repair refusals.
	fortressErrWarActive       uint8 = 0x18 // 0x2818
	fortressErrOtherFortress   uint8 = 0x26 // 0x2826
	fortressErrRepairGold      uint8 = 0x30 // 0x2830
	fortressErrRepairFull      uint8 = 0x31 // 0x2831
	fortressErrRepairDestroyed uint8 = 0x32 // 0x2832

	// The native repair cost's bounds: the hit points a purse may buy and the price cap.
	repairMaxAffordableHP = 0x3d0900
	repairMaxPrice        = 0x5f5e100
)

/*
================
fortressStructureList

The reference server's structure list query: an unknown fortress
answers 2 with code 3; otherwise it writes the structure count and, per
structure, its event zone
(vfunc +0x648) and the minutes left of its construction (+0x65C less the
minutes since +0x658, floored at zero). 754A40 stores each value on the
zone's record (+0x1C) for the manager's structure menu.

The port builds no structure over time yet, so every standing structure
reports zero minutes. INFERENCE: the native list walks a map keyed by structure;
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
		w.U32(row.Nest.EventStructID).U32(rt.structureMinutesLeft(division, request.Fortress, row))
	}
	return OpResult{Frames: []wire.Frame{{Opcode: opFortressInteractionResult, Payload: w.Payload()}}}
}

/*
================
structureMinutesLeft

The structure list's timer: the structure's build minutes less the
minutes since it was made, floored at zero. A structure never raised was
made long ago.
================
*/
func (rt *Runtime) structureMinutesLeft(division string, fortressID uint32, row monster.Instance) uint32 {
	made := rt.structureMadeAtMs(division, fortressID, row.Nest.EventStructID)
	elapsed := (rt.Now().UnixMilli() - made) / msPerMinute
	if made == 0 || elapsed >= int64(row.Ref.BuildMinutes) {
		return 0
	}
	return row.Ref.BuildMinutes - uint32(elapsed)
}

/*
================
structureByZone

The structure standing on an event zone, with the fortress whose world
holds it (the zone record's fortress codename in the native repair).
================
*/
func (rt *Runtime) structureByZone(division string, zone uint32) (uint32, instance.ID, monster.Instance, bool) {
	for fortressID, world := range rt.fortressWorlds() {
		for _, row := range rt.Monsters.WorldStructures(division, world) {
			if row.Nest.EventStructID == zone {
				return fortressID, world, row, true
			}
		}
	}
	return 0, 0, monster.Instance{}, false
}

/*
================
structureRepairCost

The native repair cost: a full repair or nothing.
The price per hit point is the record's CostRepair over the maximum, never
below one; a purse that cannot buy every missing point refuses. A
destroyed structure counts as one hit point and gets that point back too.
Returns the hit points the structure ends with and the price, or a
refusal.
================
*/
func structureRepairCost(row monster.Instance, gold int64) (uint32, int64, uint8) {
	if gold <= 0 {
		return 0, 0, fortressErrRepairGold
	}
	maximum := row.EffectiveMaxHP()
	hp, revived := row.CurrentHP, row.CurrentHP == 0
	if revived {
		hp = 1
	}
	if hp >= maximum {
		return 0, 0, fortressErrRepairFull
	}
	missing := maximum - hp
	perPoint := max(float32(row.Ref.CostRepair)/float32(maximum), 1)
	affordable := min(max(math.Trunc(float64(gold)/float64(perPoint)), 0), repairMaxAffordableHP)
	if float64(missing) > affordable {
		return 0, 0, fortressErrRepairGold
	}
	price := min(int64(math.Trunc(float64(perPoint)*float64(missing))), repairMaxPrice)
	return maximum, price, 0
}

/*
================
fortressStructureRepair

The reference server's structure repair, in its refusal order:
a war in progress, an unknown zone, a zone of another fortress, no
structure on it, then the purse. A standing structure is restored to full
for the native repair price. A destroyed one must have CanRevive (vfunc +0xDC)
or refuses 0x32; it pays CostRevive first, out of a purse that
must hold it (0x30), then the repair price from what remains, and stands
again. The charge is clamped into [1, purse] and taken
with the reply (the pending repair charge after the
0xB05E 0x0C write in the database result), which carries the fortress, the zone and the
new hit points.
================
*/
func (rt *Runtime) fortressStructureRepair(division string, c *enterworld.Character, request siege.Interaction) OpResult {
	if rt.Fortresses == nil || rt.Monsters == nil {
		return fortressRefusal(request.Action, fortressErrUnknown)
	}
	if rt.Fortresses.WarActive(division) {
		return fortressRefusal(request.Action, fortressErrWarActive)
	}
	fortressID, world, row, ok := rt.structureByZone(division, request.Reference)
	if !ok {
		return fortressRefusal(request.Action, fortressErrInvalid)
	}
	if fortressID != request.Fortress {
		return fortressRefusal(request.Action, fortressErrOtherFortress)
	}
	destroyed := row.CurrentHP == 0 || row.StructureState&structureDestroyedMask != 0
	if destroyed {
		row.CurrentHP = 0
	}
	var restored uint32
	var refusal uint8
	if !rt.deps.Update(c, "fortress-structure-repair", func() bool {
		gold := int64(0)
		if c.Gold != nil {
			gold = *c.Gold
		}
		if gold <= 0 {
			refusal = fortressErrRepairGold
			return false
		}
		base := int64(0)
		if destroyed {
			if !row.Ref.CanRevive {
				refusal = fortressErrRepairDestroyed
				return false
			}
			base = int64(row.Ref.CostRevive)
			if base > gold {
				refusal = fortressErrRepairGold
				return false
			}
		}
		var price int64
		restored, price, refusal = structureRepairCost(row, gold-base)
		if refusal != 0 {
			return false
		}
		left := gold - min(max(price+base, 1), gold)
		c.Gold = &left
		return true
	}) {
		if refusal == 0 {
			refusal = fortressErrUnknown
		}
		return fortressRefusal(request.Action, refusal)
	}
	// A repair keeps a standing gate open or shut; a revive stands the
	// structure up with state 0.
	state := row.StructureState
	if destroyed {
		state = 0
	}
	rt.Monsters.RestoreStructure(division, row.Gid, restored, state)
	if destroyed {
		// SetState(0, 1): the structure stands again for its world.
		rt.pushFortressWorld(division, world, siege.EncodeStructureState3887(siege.StructureState{
			FortressID: fortressID, GID: row.Gid, EventStructID: row.Nest.EventStructID, State: 0,
			Headquarters: row.Ref.TypeID4 == structureKindHeadquarters,
		}))
	}
	hp := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.HPRefreshPayload(row.Gid, 0x40, restored)}
	if rt.PushCharacterFrames != nil {
		for _, resident := range rt.fortressResidents(division, world) {
			rt.PushCharacterFrames(division, resident.Name, []wire.Frame{hp})
		}
	}
	reply := wire.NewWriter(14).U8(request.Action).U8(1).U32(fortressID).U32(row.Nest.EventStructID).U32(restored)
	return OpResult{Frames: []wire.Frame{goldFrame(rt.characterSnapshot(division, c)),
		{Opcode: opFortressInteractionResult, Payload: reply.Payload()}}}
}
