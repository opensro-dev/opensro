/*
===========================================================================

npctombbox_test.go - tests for npctombbox.go

===========================================================================
*/
package simulation

import (
	"testing"

	"opensro.online/server/internal/game/tombbox"
)

/*
================
TestTombBoxesJoinOnlyWithTheSwitch

On, the four boxes join on the tomb's second floor in their own band,
each greeting with its own textquest lines; off, nothing joins.
================
*/
func TestTombBoxesJoinOnlyWithTheSwitch(t *testing.T) {
	refs := map[uint32]npcWorldRef{}
	for index, box := range tombbox.Boxes {
		refs[23261+uint32(index)] = npcWorldRef{refObjID: 23261 + uint32(index), tidWord: 0x146, codename: box.Npc, scale: 100}
	}
	t.Setenv(tombbox.Env, "off")
	if got := appendTombBoxes(nil, refs, worldNpcLookups{}); len(got) != 0 {
		t.Fatalf("switch off appended %d boxes", len(got))
	}
	t.Setenv(tombbox.Env, "on")
	got := appendTombBoxes(nil, refs, worldNpcLookups{})
	if len(got) != len(tombbox.Boxes) {
		t.Fatalf("appended %d boxes, want %d", len(got), len(tombbox.Boxes))
	}
	for index, row := range got {
		box := tombbox.Boxes[index]
		if row.Codename != box.Npc || row.ObjectID != tombBoxGIDBase+uint32(index) || row.Spawn.RegionID != tombBoxRegion ||
			row.BaseSpeechSymbol != "SN_"+box.Npc+"_BS" || row.QuestSpeechSymbol != "SN_"+box.Npc+"_QS" {
			t.Fatalf("box %d: %+v", index, row)
		}
	}
}
