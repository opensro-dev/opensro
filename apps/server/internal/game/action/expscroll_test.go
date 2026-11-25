/*
===========================================================================

expscroll_test.go - the later EXP and skill-EXP increase scrolls

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
expScrollCharacter

A character holding two of the scroll its codename names.
================
*/
func expScrollCharacter(t *testing.T, codename string, refObjID uint32, params map[string]float64) (*enterworld.Character, *Runtime, *enterworld.ItemRef) {
	t.Helper()
	c := testCharacter()
	fields := map[string]float64{"canUse": 1, "maxStack": 10}
	for key, value := range params {
		fields[key] = value
	}
	scroll := &enterworld.ItemRef{RefObjID: refObjID, Codename: codename, Country: 3,
		TypeIDs: [4]int64{3, 3, 13, 21}, ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(fields)}
	items := testItems()
	items[scroll.Codename] = scroll
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 25, RefObjID: scroll.RefObjID,
		Codename: scroll.Codename, TypeFlags: scroll.TypeFlags(), StackCount: 2})
	rt, _ := newTestRuntime(c, items)
	return c, rt, scroll
}

/*
================
TestExpScrollRaisesTheExpKeeper

A 100% EXP scroll doubles kill EXP for its hour and leaves skill EXP
alone; one scroll leaves the stack.
================
*/
func TestExpScrollRaisesTheExpKeeper(t *testing.T) {
	c, rt, scroll := expScrollCharacter(t, "ITEM_MALL_100_EXP_SCROLL_1TIME", 33967, map[string]float64{
		"itemParam1_29c": 3600, "itemParam2_2a0": 1, "itemParam3_2a4": 100})
	use := wire.NewWriter(3).U8(25).U16(scroll.TypeFlags()).Payload()
	if out := rt.HandleItemUse(testDivision, c, use); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("scroll = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	exp, skill := paramJobRewardBonus(c, 1000, 500, 0)
	if exp != 2000 || skill != 500 {
		t.Fatalf("award with the scroll: %d EXP %d skill EXP", exp, skill)
	}
	for _, row := range c.MissionInventory {
		if row.Codename == scroll.Codename && row.StackCount != 1 {
			t.Fatalf("stack after one use = %d", row.StackCount)
		}
	}
}

/*
================
TestSkillExpScrollRaisesOnlyTheSkillKeeper

A 200% skill-EXP scroll triples skill EXP and restarts on a second
use of the same scroll.
================
*/
func TestSkillExpScrollRaisesOnlyTheSkillKeeper(t *testing.T) {
	c, rt, scroll := expScrollCharacter(t, "ITEM_MALL_200_SKILL_EXP_SCROLL_1TIME", 33970, map[string]float64{
		"itemParam1_29c": 3600, "itemParam2_2a0": 2, "itemParam3_2a4": 200})
	use := wire.NewWriter(3).U8(25).U16(scroll.TypeFlags()).Payload()
	for range 2 {
		if out := rt.HandleItemUse(testDivision, c, use); out.Frames[0].Payload[0] != wire.ResultSuccess {
			t.Fatalf("scroll = %+v / %q", out.Frames, out.DiagnosticRefusal)
		}
	}
	if len(c.ParamJobs) != 1 || c.ParamJobs[0].Param != paramSkillExpRate || c.ParamJobs[0].Value != 200 {
		t.Fatalf("jobs after two uses = %+v", c.ParamJobs)
	}
	exp, skill := paramJobRewardBonus(c, 1000, 500, 0)
	if exp != 1000 || skill != 1500 {
		t.Fatalf("award with the scroll: %d EXP %d skill EXP", exp, skill)
	}
}

/*
================
TestExpScrollUnstagedKindRefused

Kind 3 (both keepers) stands in no staged row: the use spends nothing
and starts no job.
================
*/
func TestExpScrollUnstagedKindRefused(t *testing.T) {
	c, rt, scroll := expScrollCharacter(t, "ITEM_MALL_100_EXP_SCROLL_1TIME", 33967, map[string]float64{
		"itemParam1_29c": 3600, "itemParam2_2a0": 3, "itemParam3_2a4": 100})
	use := wire.NewWriter(3).U8(25).U16(scroll.TypeFlags()).Payload()
	out := rt.HandleItemUse(testDivision, c, use)
	if len(out.Frames) != 0 && out.Frames[0].Payload[0] == wire.ResultSuccess {
		t.Fatalf("kind 3 answered success: %+v", out.Frames)
	}
	if len(c.ParamJobs) != 0 {
		t.Fatalf("kind 3 started jobs: %+v", c.ParamJobs)
	}
	for _, row := range c.MissionInventory {
		if row.Codename == scroll.Codename && row.StackCount != 2 {
			t.Fatalf("kind 3 spent the stack: %d", row.StackCount)
		}
	}
}
