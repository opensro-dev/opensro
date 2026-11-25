/*
===========================================================================

npcstyria.go - the Styria Clash arena manager's roster stands

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
