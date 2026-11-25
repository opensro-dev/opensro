/*
===========================================================================

npcstyria_box_test.go - the Styria Clash treasure boxes stand in world 107

===========================================================================
*/
package simulation

import (
	"testing"

	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/styria"
)

/*
================
TestStyriaArenaBoxes

Off, no boxes stand. On, one box per authored spot stands in the
arena world under its own identity band, and StyriaBoxIndex reads
the band back.
================
*/
func TestStyriaArenaBoxes(t *testing.T) {
	t.Setenv(styria.Env, "off")
	if boxes := StyriaArenaBoxes(); boxes != nil {
		t.Fatalf("boxes while off: %+v", boxes)
	}

	t.Setenv(styria.Env, "on")
	boxes := StyriaArenaBoxes()
	if len(boxes) != len(styria.BoxSpots) {
		t.Fatalf("boxes = %d, want one per spot (%d)", len(boxes), len(styria.BoxSpots))
	}
	for index, box := range boxes {
		if box.World != instance.DefinitionID(styria.WorldID) {
			t.Fatalf("box %d stands in world %d", index, box.World)
		}
		if box.Npc.RefObjID != styria.BoxRefObjID || box.Npc.Codename != styria.BoxCodename ||
			box.Npc.ModelPath != styria.BoxModel {
			t.Fatalf("box %d identity: %+v", index, box.Npc)
		}
		if got := StyriaBoxIndex(box.Npc.ObjectID); got != index {
			t.Fatalf("box %d identity reads back as %d", index, got)
		}
		spot := styria.BoxSpots[index]
		if box.Npc.Spawn.RegionID != spot.RegionID || box.Npc.Spawn.X != spot.X || box.Npc.Spawn.Z != spot.Z {
			t.Fatalf("box %d stands at %+v, want spot %+v", index, box.Npc.Spawn, spot)
		}
	}
	if StyriaBoxIndex(242000+uint32(len(styria.BoxSpots))) != -1 || StyriaBoxIndex(240100) != -1 {
		t.Fatal("a foreign identity read as a Styria box")
	}
}
