package action

import (
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

func TestUniqueFatalCommitsItsAssignedTableOnlyOnce(t *testing.T) {
	licensed.RequireGameData(t)
	rt, _, c, target := newCombatTestRuntimeAtLevel(t, 1, 20)

	ref := target.Ref
	ref.Codename = "MOB_CH_TIGERWOMAN"
	ref.MonsterType = 3
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{ref.RefObjID: ref}, []monster.NestRow{{SpawnPoint: target.Spawn, RetailEvidence: true, MaxCount: 1}}))
	rt.Monsters.SetTimeSource(rt.Now)
	rt.Monsters.StartDivision(testDivision)
	rt.Monsters.AdvancePopulation(rt.Monsters.CurrentTimeMillis())
	target = rt.Monsters.InstancesInRegions(testDivision, []uint16{target.Spawn.RegionID})[0]
	items := enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	for _, chosen := range loot.AssignedDrops(ref.Codename, 60, func() (uint32, error) { return 0, nil }) {
		item, ok := items.ItemRefByCodename(chosen.Codename)
		if !ok {
			t.Fatal("missing unique item")
		}
		rt.deps.ItemReferences().(staticItemSource)[chosen.Codename] = item
	}
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	drops := rt.Ground.All(testDivision)
	if len(drops) != 3 {
		t.Fatalf("unique assigned drops: %+v frames=%v", drops, opcodesOf(r.Frames))
	}
	refsSeen := false
	spawns := 0
	for _, f := range r.Frames {
		if f.Opcode == opCommerceItemReferences {
			refsSeen = true
		}
		if f.Opcode == wire.OpSingleObjectSpawn {
			if !refsSeen {
				t.Fatal("unique spawn before reference")
			}
			spawns++
		}
	}
	if spawns != 3 {
		t.Fatal("unique assigned spawn count")
	}
	for _, drop := range drops {
		if drop.OwnerJID != enterworld.ObjectIDForCharacter(c) {
			t.Fatal("unique drop not owned")
		}
	}
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	if rt.Ground.Count(testDivision) != 3 {
		t.Fatal("replayed unique fatal changed loot")
	}
}

func TestAlchemyFatalPublishesReferenceSpawnAndPickup(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntimeAtLevel(t, 1, 80)
	chosen, ok := loot.SelectConsumable(8, 80, 0, func() (uint32, error) { return 0, nil })
	if !ok {
		t.Fatal("no source alchemy selection")
	}
	ref := &enterworld.ItemRef{RefObjID: 50081, Codename: chosen.Codename, TypeIDs: [4]int64{3, 3, 11, 1}, NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 20})}
	rt.deps.ItemReferences().(staticItemSource)[ref.Codename] = ref
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	assertOpcodes(t, r.Frames, wire.OpSkillCastResult, wire.OpObjectStateRefresh, opCommerceItemReferences, wire.OpSingleObjectSpawn)
	assertOpcodes(t, r.Broadcast, wire.OpSkillCastResult, wire.OpObjectStateRefresh, opCommerceItemReferences, wire.OpSingleObjectSpawn)
	row, err := wire.DecodeGroundItemRow(r.Frames[3].Payload, ref.TypeFlags(), true)
	if err != nil || row.RefObjID != ref.RefObjID || row.OwnerJID != enterworld.ObjectIDForCharacter(c) {
		t.Fatalf("ground row=%+v/%v", row, err)
	}
	rt.Ground.Restore(rt.Ground.Snapshot())
	r = rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: row.Gid}.Encode())
	// Scattered loot can require an approach; advance the real pending movement.
	if r.Pending == nil {
		t.Fatal("scattered drop did not require approach")
	}
	clock.Advance(r.Pending.Eta + time.Millisecond)
	r = rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: row.Gid}.Encode())
	assertOpcodes(t, r.Frames, wire.OpActionState, wire.OpPickupAnim, wire.OpItemMoveResponse, wire.OpObjectDespawn)
	count := 0
	for _, item := range c.MissionInventory {
		if item.RefObjID == ref.RefObjID {
			count++
			if item.StackCount != 1 {
				t.Fatal("stone count changed")
			}
		}
	}
	if count != 1 || rt.Ground.Count(testDivision) != 0 {
		t.Fatal("stone pickup not committed")
	}
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	if rt.Ground.Count(testDivision) != 0 {
		t.Fatal("fatal replay regenerated alchemy")
	}
}

func TestConsumablePlansKeepActualShippedTypeQuantityAndPersistence(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(dir)
	for _, code := range []string{"ITEM_ETC_HP_POTION_01", "ITEM_ETC_MP_POTION_01", "ITEM_ETC_CURE_ALL_01", "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A", "ITEM_ETC_ARCHEMY_MAGICSTONE_STR_01", "ITEM_ETC_ARCHEMY_ATTRSTONE_PA_01", "ITEM_MALL_GLOBAL_CHATTING"} {
		t.Run(code, func(t *testing.T) {
			rt, _, c, target := newCombatTestRuntime(t, 1)
			ref, ok := items.ItemRefByCodename(code)
			if !ok {
				t.Fatalf("missing shipped ref %s", code)
			}
			rt.deps.ItemReferences().(staticItemSource)[code] = ref
			at := simulation.Spawn{RegionID: target.Spawn.RegionID, X: target.Spawn.X, Y: target.Spawn.Y, Z: target.Spawn.Z}
			drop, ok := rt.prepareSelectedDrop(loot.DropItem{Codename: code, Count: 2}, at, c.Name, rt.Now())
			if !ok {
				t.Fatal("plan refused")
			}
			drop.OwnerJID = enterworld.ObjectIDForCharacter(c)
			if drop.TypeFlags != ref.TypeFlags() || drop.StackCount != 2 {
				t.Fatal("plan lost native type or quantity")
			}
			stored := rt.Ground.Add(testDivision, drop)
			snapshot := rt.Ground.Snapshot()
			rt.Ground = grounditem.NewRegistry()
			rt.Ground.Restore(snapshot)
			r := rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: stored.Gid}.Encode())
			if rt.Ground.Count(testDivision) != 0 && rt.maxStackFor(ref.TypeFlags(), code) == 1 {
				remaining, ok := rt.Ground.Get(testDivision, stored.Gid)
				if !ok || remaining.StackCount != 1 {
					t.Fatal("partial pickup lost remainder")
				}
				r = rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: stored.Gid}.Encode())
			}
			if len(r.Frames) == 0 || rt.Ground.Count(testDivision) != 0 {
				t.Fatal("restored consumable pickup failed")
			}
			count := int64(0)
			for _, row := range c.MissionInventory {
				if row.RefObjID == ref.RefObjID {
					count += row.StackCount
				}
			}
			if count < 2 {
				t.Fatal("picked item absent from inventory")
			}
		})
	}
}
