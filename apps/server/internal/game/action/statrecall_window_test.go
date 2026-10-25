/*
===========================================================================

statrecall_window_test.go - the recall scrolls' level windows under the
mall's statrecall preset

Port-only, not native (#1609). With StatRecallWindows off a scroll is
gated by its first level alone, as today; on, each tier also stops at its
last level, and the top scroll (9258) serves 81-110.

===========================================================================
*/
package action

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
recallScrollRef

A recall scroll row as itemdata ships it: TID 3/3/13/13, Param1 2, and its
tier as two character-level requirements.
================
*/
func recallScrollRef(id uint32, codename string, first, last int64) *enterworld.ItemRef {
	return &enterworld.ItemRef{RefObjID: id, Codename: codename, TypeIDs: [4]int64{3, 3, 13, 13},
		ReqQuadTypes:  [4]int64{1, 1, -1, -1},
		ReqQuadValues: [4]int64{first, last, 0, 0},
		NativeFields:  enterworld.NewNativeFields(map[string]float64{"canUse": 1, "itemParam1_29c": 2})}
}

/*
================
TestStatRecallWindowsHoldEachTier
================
*/
func TestStatRecallWindowsHoldEachTier(t *testing.T) {
	low := recallScrollRef(9253, "ITEM_MALL_STATS_POINT_RECALL", 1, 40)
	top := recallScrollRef(9258, statRecallTopItem, 81, 90)
	for _, test := range []struct {
		name    string
		windows bool
		ref     *enterworld.ItemRef
		level   int64
		allowed bool
	}{
		{"off: a level-85 spends the 1-40 scroll, as today", false, low, 85, true},
		{"off: a level-110 spends the 81-90 scroll, as today", false, top, 110, true},
		{"on: the 1-40 scroll stops at 40", true, low, 85, false},
		{"on: the 1-40 scroll serves level 40", true, low, 40, true},
		{"on: the top scroll serves its first level", true, top, 81, true},
		{"on: the top scroll serves level 110", true, top, 110, true},
		{"on: the top scroll still refuses below 81", true, top, 80, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := testCharacter()
			level := test.level
			c.Level = &level
			items := testItems()
			items[test.ref.Codename] = test.ref
			c.MissionInventory = []enterworld.InventoryRow{{Slot: 21, RefObjID: test.ref.RefObjID,
				Codename: test.ref.Codename, TypeFlags: test.ref.TypeFlags(), StackCount: 1}}
			rt, _ := newTestRuntime(c, items)
			rt.StatRecallWindows = test.windows
			recalled := false
			rt.RecallStatPoints = func(*enterworld.Character) ([]wire.Frame, bool) { recalled = true; return nil, true }
			p := []byte{21, 0, 0}
			binary.LittleEndian.PutUint16(p[1:], test.ref.TypeFlags())
			r := rt.HandleItemUse(testDivision, c, p)
			if recalled != test.allowed {
				t.Fatalf("recalled = %t at level %d, want %t", recalled, test.level, test.allowed)
			}
			if test.allowed {
				return
			}
			refusal := wire.EncodeItemUseError(wire.ErrCodeItemUseLevelRequired)
			if len(r.Frames) == 0 || !bytes.Equal(r.Frames[0].Payload, refusal) || len(c.MissionInventory) != 1 {
				t.Fatalf("refusal %+v, bag %+v; want the level refusal and the scroll kept", r.Frames, c.MissionInventory)
			}
		})
	}
}
