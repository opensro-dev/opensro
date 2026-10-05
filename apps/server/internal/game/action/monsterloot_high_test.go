/*
===========================================================================

monsterloot_high_test.go - loot behavior and lifecycle verification

===========================================================================
*/

package action

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestHigherLevelFatalDropReferencePickupAndRestore
================
*/
func TestHigherLevelFatalDropReferencePickupAndRestore(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntimeAtLevel(t, 1, 80)
	ref := &enterworld.ItemRef{RefObjID: 50080, Codename: "ITEM_CH_SWORD_09_B", Name: "Level 80 sword", TypeIDs: [4]int64{3, 1, 6, 2}, MaxDurability: 100, NativeFields: enterworld.NewNativeFields(map[string]float64{"itemClass": 26})}
	rt.deps.ItemReferences().(staticItemSource)[ref.Codename] = ref
	rt.DropRoll = constantDropRoll(0)
	r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	r = assertAndSeparateActionSession(t, r)
	assertOpcodes(t, r.Frames, wire.OpSkillCastResult, wire.OpObjectStateRefresh, opCommerceItemReferences, wire.OpSingleObjectSpawn)
	assertOpcodes(t, r.Broadcast, wire.OpSkillCastResult, wire.OpObjectStateRefresh, opCommerceItemReferences, wire.OpSingleObjectSpawn)
	if !reflect.DeepEqual(r.Frames[2], r.Broadcast[2]) {
		t.Fatal("observer reference differs")
	}
	var refs struct {
		Items []struct {
			ID           uint32 `json:"refObjId"`
			Name         string
			NativeFields map[string]float64
		}
	}
	if err := json.Unmarshal(r.Frames[2].Payload, &refs); err != nil {
		t.Fatal(err)
	}
	if len(refs.Items) != 1 || refs.Items[0].ID != ref.RefObjID || refs.Items[0].NativeFields["itemClass"] != 26 {
		t.Fatalf("missing higher-grade metadata: %s", r.Frames[2].Payload)
	}
	row, err := wire.DecodeGroundItemRow(r.Frames[3].Payload, ref.TypeFlags(), true)
	if err != nil || row.RefObjID != ref.RefObjID || row.OwnerJID != enterworld.ObjectIDForCharacter(c) {
		t.Fatalf("spawn=%+v/%v", row, err)
	}
	stored, _ := rt.Ground.Get(testDivision, row.Gid)
	if stored.Codename != ref.Codename || stored.StackCount != 1 {
		t.Fatalf("stored %+v", stored)
	}
	// Reload ground authority before pickup: item identity and modifiers must
	// survive, and a new viewer's bounded bootstrap must include this actual drop.
	snapshot := rt.Ground.Snapshot()
	rt.Ground = grounditem.NewRegistry()
	rt.Ground.Restore(snapshot)
	if restored, _ := rt.Ground.Get(testDivision, row.Gid); !reflect.DeepEqual(stored, restored) {
		t.Fatal("restart lost drop fields")
	}
	if !slices.Contains(rt.GroundRefItemCodenames(testDivision), ref.Codename) {
		t.Fatal("rejoining viewer has no item reference")
	}
	if rows := enterworld.GroundObjectListRows(rt.Ground.All(testDivision)); len(rows) != 1 {
		t.Fatal("rejoining viewer has no spawn")
	}
	finishTestCast(t, rt, clock, c)
	pick := rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: row.Gid}.Encode())
	// Scattered loot can require an approach; advance the real pending movement.
	if pick.Pending == nil {
		t.Fatal("scattered drop did not require approach")
	}
	clock.Advance(pick.Pending.Eta + time.Millisecond)
	pick = rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: row.Gid}.Encode())
	assertOpcodes(t, pick.Frames, wire.OpPickupAnim, wire.OpItemMoveResponse, wire.OpObjectDespawn, wire.OpActionState)
	if rt.Ground.Count(testDivision) != 0 {
		t.Fatal("pickup left item on ground")
	}
	count := 0
	for _, item := range c.MissionInventory {
		if item.RefObjID == ref.RefObjID {
			count++
			if item.Codename != ref.Codename || item.Durability != int64(stored.Durability) {
				t.Fatal("pickup changed equipment")
			}
		}
	}
	if count != 1 {
		t.Fatal("pickup did not grant exactly one higher-level item")
	}
	rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: row.Gid}.Encode())
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	if rt.Ground.Count(testDivision) != 0 {
		t.Fatal("replayed fatal generated loot")
	}
}

/*
================
TestGroundReferencesAreBoundedAndDoNotInflateBootstrap
================
*/
func TestGroundReferencesAreBoundedAndDoNotInflateBootstrap(t *testing.T) {
	rt, _, _, _ := newCombatTestRuntime(t, 1)
	before := append(rt.StaticRefItemCodenames(), rt.GroundRefItemCodenames(testDivision)...)
	drop := grounditem.Item{RefObjID: 50080, Codename: "ITEM_CH_SWORD_09_B", TypeFlags: wire.PackTypeFlags(3, 1, 6, 2)}
	frames := rt.groundReferences([]grounditem.Item{drop, drop})
	if len(frames) != 1 {
		t.Fatal("duplicate reference frames")
	}
	var body struct{ Items []json.RawMessage }
	if err := json.Unmarshal(frames[0].Payload, &body); err != nil || len(body.Items) != 1 {
		t.Fatal("duplicate metadata")
	}
	after := append(rt.StaticRefItemCodenames(), rt.GroundRefItemCodenames(testDivision)...)
	if !reflect.DeepEqual(before, after) || slices.Contains(before, drop.Codename) {
		t.Fatal("possible loot bloated bootstrap")
	}
}

/*
================
TestFullEquipmentCatalogMatchesShippedMedia
================
*/
func TestFullEquipmentCatalogMatchesShippedMedia(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	raw, err := os.ReadFile(filepath.Join("..", "item", "loot", ".generated", "equipment.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Items []struct {
			Codename string
			Country  uint8
			Group    int
			Level    uint8
		}
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Items) != 5724 {
		t.Fatalf("equipment coverage=%d", len(catalog.Items))
	}
	items := enterworld.NewTextdataItems(dir)
	for _, r := range catalog.Items {
		ref, ok := items.ItemRefByCodename(r.Codename)
		if !ok || ref == nil || ref.Country != int64(r.Country) || ref.ReqQuadValues[0] != int64(r.Level) {
			t.Fatalf("non-v1.150 assignment: %+v", r)
		}
		clientGroup := int(ref.NativeFields.Get("itemClass")) - 1
		if clientGroup != r.Group {
			// A rare torso whose required level outlives its donor class joins
			// the next enabled class, while retaining its actual client degree.
			if ref.NativeFields.Get("rarity") != 2 || r.Group != clientGroup+1 || r.Group/3 != clientGroup/3 {
				t.Fatalf("assignment changed client degree or nonrare class: %+v", r)
			}
		}
	}
}
