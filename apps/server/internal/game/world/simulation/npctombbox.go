/*
===========================================================================

npctombbox.go - the Ch'in Tomb treasure boxes on the tomb's second floor

Port-only switch, native content (SRO_TOMB_TREASURE_BOXES, #4810). The
v1.150 characterdata defines NPC_TQ_TREASUREBOX1-4 (23261-23264, TID
1/2/2) but npcpos.txt places none of them; the later server places them
through its nest table. While the switch is on, the roster appends the
four boxes at those nest positions, the way the Magic Pop machines join
(npcmagicpop.go). Off, the roster is exactly the shipped one.

===========================================================================
*/
package simulation

import (
	"os"

	"opensro.online/server/internal/game/tombbox"
)

// tombBoxGIDBase starts the treasure-box band inside the static NPC
// range, above the Styria arena boxes (242000) and below the teleport
// gates at 250000 + RefObjID.
const tombBoxGIDBase uint32 = 243000

// tombBoxRegion is the tomb's second floor (region 0x8006).
const tombBoxRegion = 0x8006

/*
================
tombBoxPlacements

The later server's nest rows for the four boxes (ISRO-R Tab_RefNest
24068-24071, all in region 0x8006), in Boxes order.
================
*/
var tombBoxPlacements = [len(tombbox.Boxes)]Spawn{
	{RegionID: tombBoxRegion, X: 18103, Y: -47, Z: 5589},
	{RegionID: tombBoxRegion, X: -18147, Y: -47, Z: -30},
	{RegionID: tombBoxRegion, X: 18101, Y: -47, Z: -33},
	{RegionID: tombBoxRegion, X: -18151, Y: -47, Z: -5825},
}

/*
================
appendTombBoxes

Appends the four boxes while the switch is on. Each takes its greeting
from its own textquest lines (npcchat has no box rows): the base line
SN_<box>_BS and the quest line SN_<box>_QS. A reference table without a
box row (a partial data set) appends nothing for it.
================
*/
func appendTombBoxes(roster []NpcDef, refs map[uint32]npcWorldRef, lookups worldNpcLookups) []NpcDef {
	if !tombbox.Enabled(os.Getenv(tombbox.Env)) {
		return roster
	}
	byCodename := map[string]npcWorldRef{}
	for _, ref := range refs {
		byCodename[ref.codename] = ref
	}
	for index, box := range tombbox.Boxes {
		ref, found := byCodename[box.Npc]
		if !found {
			continue
		}
		ref.base, ref.quest = "SN_"+box.Npc+"_BS", "SN_"+box.Npc+"_QS"
		roster = append(roster, newWorldNpc(ref, tombBoxGIDBase+uint32(index), tombBoxPlacements[index], lookups))
	}
	return roster
}
