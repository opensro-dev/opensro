package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/gmcommand"
	"opensro.online/server/internal/game/item/wire"
)

type gmItemSource struct{ staticItemSource }

func (s gmItemSource) ItemRefByID(id uint32) (*enterworld.ItemRef, bool) {
	for _, ref := range s.staticItemSource {
		if ref.RefObjID == id {
			return ref, true
		}
	}
	return nil, false
}

func TestMakeItemAuthorityDropAndNormalPickup(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100)
	deps := rt.deps.(*enterworld.Deps)
	ref := &enterworld.ItemRef{RefObjID: 24198, Codename: "ITEM_ETC_SPEED_UP_BASIC", TypeIDs: [4]int64{3, 3, 13, 1}, NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 20})}
	refs := rt.deps.ItemReferences().(staticItemSource)
	refs[ref.Codename] = ref
	deps.Items = gmItemSource{refs}
	var published []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { published = append(published, frames...) }
	command := []byte{7, 0x86, 0x5e, 0, 0, 255}
	if out := gmcommand.HandleGmCommand(deps, nil, testDivision, c, command, rt); out.Ack != nil || rt.Ground.Count(testDivision) != 0 {
		t.Fatal("non-GM created item")
	}
	c.GMPrivilege = true
	out := gmcommand.HandleGmCommand(deps, nil, testDivision, c, command, rt)
	if !bytes.Equal(out.Ack, []byte{1, 7}) || rt.Ground.Count(testDivision) != 1 {
		t.Fatalf("creation: %+v", out)
	}
	if len(published) < 2 || published[0].Opcode != opCommerceItemReferences {
		t.Fatal("drop published before reference metadata")
	}
	item := rt.Ground.All(testDivision)[0]
	if item.RefObjID != ref.RefObjID || item.StackCount != 20 {
		t.Fatalf("invalid authored stack clamp: %+v", item)
	}
	rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: item.Gid}.Encode())
	if rt.Ground.Count(testDivision) != 0 {
		t.Fatal("normal pickup did not consume ground item")
	}
	for _, row := range c.MissionInventory {
		if row.RefObjID == ref.RefObjID && row.StackCount == 20 {
			return
		}
	}
	t.Fatal("created item did not reach inventory")
}

/*
================
TestMakeItemEquipmentStartsAtItsVarianceDurability

A GM-made weapon has zero variance bits, so it starts at the durability
that variance gives (the range floor), never above the maximum its
tooltip and repairs use. Its plus is capped at 8.
================
*/
func TestMakeItemEquipmentStartsAtItsVarianceDurability(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100)
	deps := rt.deps.(*enterworld.Deps)
	floor := int64(130)
	ref := &enterworld.ItemRef{RefObjID: 4012, Codename: "ITEM_CH_BLADE_09_C_RARE", TypeIDs: [4]int64{3, 1, 6, 3},
		MaxDurability: 159, VarianceIntMin1c0: &floor}
	refs := rt.deps.ItemReferences().(staticItemSource)
	refs[ref.Codename] = ref
	deps.Items = gmItemSource{refs}
	rt.PushCharacterFrames = func(_, _ string, _ []wire.Frame) {}
	c.GMPrivilege = true
	if !rt.MakeGMItem(testDivision, c.Name, ref.RefObjID, 11) {
		t.Fatal("the GM item was not made")
	}
	item := rt.Ground.All(testDivision)[0]
	if item.Durability != 130 || item.Plus != 8 {
		t.Fatalf("made durability %d plus %d, want 130 and 8", item.Durability, item.Plus)
	}
}
