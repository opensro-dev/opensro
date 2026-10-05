/*
===========================================================================

select_cos_test.go - selecting a summoned companion

CGObjPC_HandleSelectRequest0x7045 (52B040) grants any character in hit
range, and CGObjCOS inherits the NPC/monster select writer, so a companion
answers with the non-user arm carrying its current HP.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestSelectingAnOwnCompanionGrantsItsHealth

A summoned pet is selectable by its owner; the grant is the 14-byte arm
with the pet's current HP, and the selection is recorded.
================
*/
func TestSelectingAnOwnCompanionGrantsItsHealth(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, _ := newTestRuntime(c, refs)
	rt.CompanionRoll = func() (uint32, error) { return 0, nil }
	rt.BindPetSession(testDivision, c, 101)
	useSummonerFixture(t, rt, c, 23, refs.staticItemSource["SUMMON_ATTACK"])
	pets := rt.CompanionPresentations(testDivision, c.Name)
	if len(pets) == 0 {
		t.Fatal("no summoned companion")
	}
	gid := pets[0].Row.Gid
	record := c.CompanionByGID(gid)
	if record == nil {
		t.Fatal("companion record missing")
	}
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, gid)
	out := rt.HandleObjectSelect(testDivision, c, payload)
	if out.Refusal != "" || out.Selected != gid {
		t.Fatalf("companion select refused: %q", out.Refusal)
	}
	frame, ok := findFrame(out.Frames, wire.OpObjectSelectResult)
	if !ok || len(frame.Payload) != 14 || frame.Payload[0] != 1 || frame.Payload[5] != 1 {
		t.Fatalf("companion grant %x", frame.Payload)
	}
	if binary.LittleEndian.Uint32(frame.Payload[1:]) != gid || binary.LittleEndian.Uint32(frame.Payload[6:]) != record.CurrentHP {
		t.Fatalf("grant names %x, want gid %d and HP %d", frame.Payload, gid, record.CurrentHP)
	}
}
