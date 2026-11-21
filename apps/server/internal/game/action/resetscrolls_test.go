/*
===========================================================================

resetscrolls_test.go - the all-levels stat recall scroll's use

Port-only, not native. The scroll rides the native stat recall family;
its own switch off refuses it and keeps it, and it has no level window
to hold even while the tier scrolls' windows are on.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
statResetAllCase

A character above the top tier's last level holding one all-levels
scroll (the v1.696 row: 3/3/13/13, Param1 2, no level requirement).
================
*/
func statResetAllCase(t *testing.T, on bool) (*Runtime, *enterworld.Character, *bool) {
	t.Helper()
	ref := &enterworld.ItemRef{RefObjID: 51600, Codename: commerce.StatResetScrollItem, TypeIDs: [4]int64{3, 3, 13, 13},
		ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "itemParam1_29c": 2})}
	c := testCharacter()
	level := int64(115)
	c.Level = &level
	items := testItems()
	items[ref.Codename] = ref
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 21, RefObjID: ref.RefObjID,
		Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 1}}
	rt, _ := newTestRuntime(c, items)
	rt.StatRecallWindows = true
	rt.ResetScrolls = map[commerce.ResetScrollKind]bool{commerce.StatReset: on}
	recalled := false
	rt.RecallStatPoints = func(*enterworld.Character) ([]wire.Frame, bool) { recalled = true; return nil, true }
	return rt, c, &recalled
}

/*
================
useSlot21
================
*/
func useSlot21(rt *Runtime, c *enterworld.Character) OpResult {
	p := []byte{21, 0, 0}
	binary.LittleEndian.PutUint16(p[1:], c.MissionInventory[0].TypeFlags)
	return rt.HandleItemUse(testDivision, c, p)
}

/*
================
TestStatResetAllOffIsRefused
================
*/
func TestStatResetAllOffIsRefused(t *testing.T) {
	rt, c, recalled := statResetAllCase(t, false)
	r := useSlot21(rt, c)
	if *recalled || r.DiagnosticRefusal == "" || len(c.MissionInventory) != 1 {
		t.Fatalf("switch off: recalled %t, refusal %q, bag %+v; want refused and kept", *recalled, r.DiagnosticRefusal, c.MissionInventory)
	}
}

/*
================
TestStatResetAllRecallsAtAnyLevel
================
*/
func TestStatResetAllRecallsAtAnyLevel(t *testing.T) {
	rt, c, recalled := statResetAllCase(t, true)
	useSlot21(rt, c)
	if !*recalled || len(c.MissionInventory) != 0 {
		t.Fatalf("switch on at level 115: recalled %t, bag %+v; want recalled and spent", *recalled, c.MissionInventory)
	}
}
