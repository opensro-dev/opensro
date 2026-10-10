/*
===========================================================================

fortress_structures_test.go - the fortress manager's structure services

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
)

/*
================
TestFortressStructureListNamesEveryZone

634850 / 61E000: Jangan's two standing structures (zones 84 and 85) list
with zero construction minutes, in zone order; an unknown fortress answers
code 3; and the dispatcher hands action 0x18 to the NPC service admission
(service 0x19) instead of refusing it as unknown.
================
*/
func TestFortressStructureListNamesEveryZone(t *testing.T) {
	rt, c, _, _ := captureFixture(t)
	jangan := uint32(0)
	for _, record := range rt.Fortresses.Records(testDivision) {
		if record.CodeName == "FORTRESS_JANGAN" {
			jangan = record.ID
		}
	}
	out := rt.fortressStructureList(testDivision, c, siege.Interaction{Action: siege.ActionStructureQuery, Fortress: jangan})
	want := wire.NewWriter(32).U8(siege.ActionStructureQuery).U8(1).U32(jangan).U8(2).U32(84).U32(0).U32(85).U32(0).Payload()
	if len(out.Frames) != 1 || out.Frames[0].Opcode != opFortressInteractionResult || !bytes.Equal(out.Frames[0].Payload, want) {
		t.Fatalf("structure list %+v, want % x", out.Frames, want)
	}
	missing := rt.fortressStructureList(testDivision, c, siege.Interaction{Action: siege.ActionStructureQuery, Fortress: 999})
	if len(missing.Frames) != 1 || !bytes.Equal(missing.Frames[0].Payload, []byte{siege.ActionStructureQuery, 2, fortressErrInvalid}) {
		t.Fatalf("unknown fortress answered %+v", missing.Frames)
	}
	// No manager is selected: the request reaches the service admission,
	// which refuses with code 3 rather than the unknown-action code 2.
	request := wire.NewWriter(9).U32(17).U8(siege.ActionStructureQuery).U32(jangan).Payload()
	routed := rt.HandleFortressInteraction(testDivision, c, request)
	if len(routed.Frames) != 1 || !bytes.Equal(routed.Frames[0].Payload, []byte{siege.ActionStructureQuery, 2, fortressErrInvalid}) {
		t.Fatalf("dispatcher answered %+v", routed.Frames)
	}
}
