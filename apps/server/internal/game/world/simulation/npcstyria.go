/*
===========================================================================

npcstyria.go - the Styria Clash arena officers and treasure boxes

Port-only, not native (#3648). The shipped v1.150 tables carry no
NPC_BATTLE_ARENA_MANAGER row and place nothing for it: the later
client authors the officer (characterdata 26790) and six town stands
(its npcpos rows). While SRO_STYRIA_CLASH is on, the roster
synthesizes the manager from that row at those stands, the way the
Magic Pop machines join by placement (npcmagicpop.go); off, the
roster is exactly the shipped one. The exchanger (26791) stands
beside the manager in the later client but takes no applications;
it arrives with the reward slice, so only the manager joins here.

===========================================================================
*/
package simulation

import (
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/styria"
)

// arenaManagerGIDBase starts the manager band inside the static NPC
// range: above the Magic Pop operators (240016) and below the
// teleport gates at 250000 + RefObjID.
const arenaManagerGIDBase uint32 = 240100

/*
================
arenaManagerRef

The manager's reference row, transcribed from the later client's
characterdata 26790: TID 1/2/2 (the packed word the NPC class gate
admits), its name and speech symbols (npcchat's base line), its
model, and a town officer's still stance (walk/run 0, scale 100,
level and HP unrated in the source row).
================
*/
func arenaManagerRef() npcWorldRef {
	return npcWorldRef{
		refObjID: styria.ManagerRefObjID,
		tidWord:  0x146,
		codename: styria.ManagerCodename,
		nameID:   styria.ManagerNameSymbol,
		model:    styria.ManagerModel,
		scale:    100,
		base:     styria.ManagerSpeech,
	}
}

// styriaBoxGIDBase starts the treasure-box band inside the static
// NPC range, above the Muhan arena NPCs (241000..241999).
const styriaBoxGIDBase uint32 = 242000

/*
================
arenaBoxRef

The treasure box's reference row, transcribed from the later
client's characterdata 40460 the way the manager's is: TID 1/2/2,
its name symbol, its model, a still stance at scale 100.
================
*/
func arenaBoxRef() npcWorldRef {
	return npcWorldRef{
		refObjID: styria.BoxRefObjID,
		tidWord:  0x146,
		codename: styria.BoxCodename,
		nameID:   styria.BoxNameSymbol,
		model:    styria.BoxModel,
		scale:    100,
	}
}

/*
================
StyriaArenaBoxes

The treasure boxes standing in the arena world (107) while the
Styria switch is on, as world NPCs: a viewer sees them only inside
a layer of that world. Their stands are the port's (styria.BoxSpots).
Off, there are none. Which box is full and what an opening pays is
the match owner's (action/styria_match.go); these only stand.
================
*/
func StyriaArenaBoxes() []WorldNpc {
	if !styria.Enabled() {
		return nil
	}
	ref := arenaBoxRef()
	boxes := make([]WorldNpc, 0, len(styria.BoxSpots))
	for index, spot := range styria.BoxSpots {
		spawn := Spawn{RegionID: spot.RegionID, X: spot.X, Y: spot.Y, Z: spot.Z}
		npc := newWorldNpc(ref, styriaBoxGIDBase+uint32(index), spawn, worldNpcLookups{})
		boxes = append(boxes, WorldNpc{World: instance.DefinitionID(styria.WorldID), Npc: npc})
	}
	return boxes
}

/*
================
StyriaBoxIndex

The box index a world-NPC identity names (its stand in
styria.BoxSpots), or -1 when the identity is no Styria box.
================
*/
func StyriaBoxIndex(gid uint32) int {
	if gid < styriaBoxGIDBase || gid >= styriaBoxGIDBase+uint32(len(styria.BoxSpots)) {
		return -1
	}
	return int(gid - styriaBoxGIDBase)
}

/*
================
appendArenaManager

Appends the arena manager at each of its authored stands while the
Styria switch is on. The stands ride the shared town grid, so the
poses land as the later client authored them.
================
*/
func appendArenaManager(roster []NpcDef, lookups worldNpcLookups) []NpcDef {
	if !styria.Enabled() {
		return roster
	}
	ref := arenaManagerRef()
	for index, placement := range styria.ManagerPlacements {
		spawn := Spawn{
			RegionID: placement.RegionID,
			X:        placement.X,
			Y:        placement.Y,
			Z:        placement.Z,
		}
		roster = append(roster, newWorldNpc(ref, arenaManagerGIDBase+uint32(index), spawn, lookups))
	}
	return roster
}
