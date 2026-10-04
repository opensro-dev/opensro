/*
===========================================================================

resurrectionscroll_test.go - scrolls and fireworks through HandleItemUse

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
resurrectionScrollFixture

A dead level-one character (200 HP/MP maxima) holding two 60% scrolls in
slot 21, with the published Param1..3 (0, 100, 60).
================
*/
func resurrectionScrollFixture(hp int64) (*enterworld.Character, staticItemSource, []byte) {
	c := rebirthTestCharacter(1, hp)
	c.LastExpLoss = -1001
	items := testItems()
	ref := &enterworld.ItemRef{
		RefObjID: 3783, Codename: "ITEM_MALL_RESURRECTION_60P_SCROLL", TypeIDs: [4]int64{3, 3, 13, 6},
		ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{
			"maxStack": 10, "canUse": 1, "itemParam1_29c": 0, "itemParam2_2a0": 100, "itemParam3_2a4": 60,
		}),
	}
	items[ref.Codename] = ref
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: 21, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 2,
	})
	return c, items, wire.NewWriter(3).U8(21).U16(ref.TypeFlags()).Payload()
}

/*
================
TestResurrectionScrollExpRoundsUp

49FFE8..49FFF9: a float32 remainder above 1e-6 adds one, where the skill's
refund truncates.
================
*/
func TestResurrectionScrollExpRoundsUp(t *testing.T) {
	for _, tc := range []struct {
		loss     int64
		percent  uint32
		murderer bool
		want     int64
	}{
		{1001, 60, false, 601},
		{-1001, 60, false, 601},
		{1000, 60, false, 600},
		{1001, 60, true, 301},
		{1001, 100, false, 1001},
		{0, 100, false, 0},
	} {
		if got := resurrectionScrollExp(tc.loss, tc.percent, tc.murderer); got != tc.want {
			t.Errorf("loss %d at %d%% murderer %v: %d, want %d", tc.loss, tc.percent, tc.murderer, got, tc.want)
		}
	}
	if resurrectionExp(1001, 60, false) != 600 {
		t.Fatal("the skill refund must keep truncating")
	}
}

/*
================
TestResurrectionScrollRevivesTheDeadWhereTheyLie
================
*/
func TestResurrectionScrollRevivesTheDeadWhereTheyLie(t *testing.T) {
	c, items, body := resurrectionScrollFixture(0)
	region, x, z := *c.World.Spawn.RegionID, *c.World.Spawn.X, *c.World.Spawn.Z
	rt, _ := newTestRuntime(c, items)
	refunded := int64(-1)
	rt.RefundExperience = func(_ *enterworld.Character, exp int64) ([]wire.Frame, bool) {
		refunded = exp
		return nil, true
	}
	result := rt.HandleItemUse(testDivision, c, body)
	if !enterworld.CharacterAlive(c) || *c.CurrentHP != 200 || *c.CurrentMP != 200 {
		t.Fatalf("revived to %d/%d HP/MP, want 200/200", *c.CurrentHP, *c.CurrentMP)
	}
	if refunded != 601 || c.LastExpLoss != 0 {
		t.Fatalf("refunded %d EXP, loss now %d; want 601 and 0", refunded, c.LastExpLoss)
	}
	if *c.World.Spawn.RegionID != region || *c.World.Spawn.X != x || *c.World.Spawn.Z != z {
		t.Fatalf("revived at %d %v %v, not where the corpse lay %d %v %v",
			*c.World.Spawn.RegionID, *c.World.Spawn.X, *c.World.Spawn.Z, region, x, z)
	}
	if c.MissionInventory[len(c.MissionInventory)-1].StackCount != 1 {
		t.Fatal("the scroll was not consumed")
	}
	if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpItemUseResponse || len(result.Broadcast) == 0 {
		t.Fatalf("missing item-use success or observer revival: %d frames, %d broadcast",
			len(result.Frames), len(result.Broadcast))
	}
}

/*
================
TestResurrectionScrollRefusesTheLiving

49FF35: anything but life state 2 answers 0x1887.
================
*/
func TestResurrectionScrollRefusesTheLiving(t *testing.T) {
	c, items, body := resurrectionScrollFixture(50)
	rt, _ := newTestRuntime(c, items)
	assertItemUseRefusedUnchanged(t, rt, c, body, errCodeOnlyDeadResurrect)
}

/*
================
TestFireworkPublishesItsVisualToEveryone

49ACA0 family 6 succeeds for a living user; 510980 then sends the item's
0x305C (v1.150 0x3449) to the user and the nearby sessions.
================
*/
func TestFireworkPublishesItsVisualToEveryone(t *testing.T) {
	c := testCharacter()
	items := testItems()
	ref := &enterworld.ItemRef{
		RefObjID: 2700, Codename: "ITEM_ETC_FIREWORK_BOOMB_R", TypeIDs: [4]int64{3, 3, 6, 1},
		ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 50, "canUse": 1}),
	}
	items[ref.Codename] = ref
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: 21, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 3,
	})
	rt, _ := newTestRuntime(c, items)
	result := rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(21).U16(ref.TypeFlags()).Payload())
	assertOpcodes(t, result.Frames, wire.OpItemUseResponse, wire.OpItemUseVisual)
	assertOpcodes(t, result.Broadcast, opCommerceItemReferences, wire.OpItemUseVisual)
	visual := result.Frames[1].Payload
	if binary.LittleEndian.Uint32(visual) != enterworld.ObjectIDForCharacter(c) ||
		binary.LittleEndian.Uint32(visual[4:]) != ref.RefObjID {
		t.Fatalf("visual %x does not name the user and the firework", visual)
	}
	if c.MissionInventory[len(c.MissionInventory)-1].StackCount != 2 {
		t.Fatal("the firework was not consumed")
	}
}
