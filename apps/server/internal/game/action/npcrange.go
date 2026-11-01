/*
===========================================================================

npcrange.go - the native hit-range gate for NPC interaction

The reference server's hit-range check admits an interaction only while the
target is inside the range of its object class, measured in 3D through
a relative 3D position. The object select (v1.188 0x7045, here 0x745A) and every
NPC function request (v1.188 0x7046, here 0x7338) run it and answer
code 4 when the target is too far. Trades do not measure distance: they
need the NPC function an in-range request opened (player +0xC+6 state 5),
which the selection store keeps (SelectionStore.OpenFunction).

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// npcHitRange is the reference server's range for the NPC class (vtable +0x24, the
	// class the NPC function request requires before any NPC function).
	npcHitRange = 300
	// teleportHitRange is the gate class range. The reference server selects 800 for one
	// class; the v1.150 client selects a gate only inside 800 units (698740)
	// and approaches to 640, so the gate is that class. Inferred, not traced.
	teleportHitRange = 800
	// hitRangeTooFar is the reference server's out-of-range result. The client shows it as
	// category 13 code 4, UIIT_MSG_INTERACTION_FAIL_TOO_FAR (client 75AE50 -> 689420).
	hitRangeTooFar uint8 = 4
	// selectObjectNotFound is the reference server's code when the object lookup finds no
	// object; v1.150's 7651F1 reads it as one byte and shows text only for 7.
	selectObjectNotFound uint8 = 3
)

/*
================
npcInteractionRange

The reference server's range for an NPC's object class.
================
*/
func npcInteractionRange(npc simulation.NpcDef) float32 {
	if npc.Teleport != nil {
		return teleportHitRange
	}
	return npcHitRange
}

/*
================
npcWithinHitRange

True while the character stands inside the NPC's class range, measured to
the station the NPC is published at (simulation.NpcStation). Authored NPCs
stand there; the synthetic fixture's patrol leg is short and bounded, so
its station is the measure for it too. Incompatible planes (dungeon
against field) measure as overflow and refuse, as natively.
================
*/
func (rt *Runtime) npcWithinHitRange(divisionID string, character *enterworld.Character, npc simulation.NpcDef) bool {
	viewer := rt.liveSpawn(simulation.WorldKey(divisionID, character.Name), character, rt.Now().UnixMilli())
	station := simulation.NpcStation(npc, rt.NpcSpawn.Anchor(simulation.SeedWorldState(character).Spawn))
	distance := monster.NativeActorDistance(
		monster.Pose{RegionID: viewer.RegionID, X: viewer.X, Y: viewer.Y, Z: viewer.Z},
		monster.Pose{RegionID: station.RegionID, X: station.X, Y: station.Y, Z: station.Z},
	)
	// The reference server refuses when the range is below the distance.
	return !(npcInteractionRange(npc) < distance)
}

/*
================
npcFunctionTooFar

The native NPC function refusal for an out-of-range request: 0xB338 kind 2
with code 4, which the client turns into the "too far" notice.
================
*/
func npcFunctionTooFar() []wire.Frame {
	return []wire.Frame{{
		Opcode:  wire.OpNpcInteractionAck,
		Payload: wire.EncodeNpcInteractionRefusal(hitRangeTooFar),
	}}
}
