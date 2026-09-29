/*
===========================================================================

monsterunique_test.go - unique rewards and recovery loot through real kill doors

Selection, reference publication, ownership, persistence, pickup and activation
are exercised together, with client references and deterministic native draws.

===========================================================================
*/
package action

import (
	"reflect"
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
installLootReferences
================
*/
func installLootReferences(t *testing.T, rt *Runtime) {
	t.Helper()
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(dir)
	source := rt.deps.ItemReferences().(staticItemSource)
	for _, code := range loot.CatalogItemCodenames() {
		ref, ok := items.ItemRefByCodename(code)
		if !ok {
			t.Fatal("missing published loot reference", code)
		}
		if source[code] == nil {
			source[code] = ref
		}
	}
	rt.deps.(*enterworld.Deps).MagicOptions = enterworld.NewTextdataMagicOptions(dir)
	if err := rt.ValidateLootReferences(); err != nil {
		t.Fatal(err)
	}
}

/*
================
TestUniqueNativeCountExceptions
================
*/
func TestUniqueNativeCountExceptions(t *testing.T) {
	rt, _, c, target := newCombatTestRuntimeAtLevel(t, 1, 20)
	installLootReferences(t, rt)
	for _, tc := range []struct {
		code                      string
		rarity                    uint8
		equipment, recipe, scroll int
	}{
		{"MOB_CH_TIGERWOMAN", 3, 5, 0, 10},
		{"MOB_TK_BONELORD", 3, 5, 0, 10},
		{"MOB_TQ_WHITESNAKE", 3, 6, 1, 10},
		{"MOB_RM_ROC", 3, 60, 50, 100},
		{"M", 8, 1, 1, 0},
		{"M", 0x18, 5, 0, 10},
	} {
		t.Run(tc.code+string(rune(tc.rarity+'0')), func(t *testing.T) {
			target.Ref.Codename, target.Ref.MonsterType = tc.code, tc.rarity
			target.Nest.HasRarityOverride, target.Nest.RarityOverride = true, tc.rarity
			rt.DropRoll = constantDropRoll(0)
			drops := rt.prepareUniqueDrops(uniqueDropContext{mob: target, owner: c.Name, now: rt.Now()})
			var equipment, recipe, scroll int
			for _, drop := range drops {
				switch {
				case drop.TypeFlags&0x7c == 0x2c:
					equipment++
					if drop.Plus != 1 || len(drop.MagicOptions) == 0 {
						t.Fatal("special constructor lost plus/blues", drop)
					}
				case drop.Codename == "ITEM_ETC_SCROLL_RETURN_02":
					scroll++
				case drop.Codename == "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_B":
					recipe++
				default:
					t.Fatal("unexpected special reward", drop.Codename)
				}
			}
			if equipment != tc.equipment || recipe != tc.recipe || scroll != tc.scroll {
				t.Fatalf("counts %d/%d/%d want %d/%d/%d", equipment, recipe, scroll, tc.equipment, tc.recipe, tc.scroll)
			}
		})
	}
}

/*
================
pickupKillReward
================
*/
func pickupKillReward(t *testing.T, rt *Runtime, c *enterworld.Character, clock *fakeClock, drop grounditem.Item) enterworld.InventoryRow {
	t.Helper()
	rt.Ground.Restore(rt.Ground.Snapshot())
	result := rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: drop.Gid}.Encode())
	if result.Pending != nil {
		clock.Advance(result.Pending.Eta + time.Millisecond)
		rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: drop.Gid}.Encode())
	}
	if _, exists := rt.Ground.Get(testDivision, drop.Gid); exists {
		t.Fatal("kill reward remained on ground")
	}
	for _, row := range c.MissionInventory {
		if row.Codename == drop.Codename && row.Slot >= 13 {
			return row
		}
	}
	t.Fatal("kill reward missing from inventory")
	return enterworld.InventoryRow{}
}

/*
================
TestUniqueKillPreservesSpecialPropertiesAndPrepassOrder
================
*/
func TestUniqueKillPreservesSpecialPropertiesAndPrepassOrder(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntimeAtLevel(t, 1, 20)
	installLootReferences(t, rt)
	ref := target.Ref
	ref.Codename, ref.MonsterType = "MOB_CH_TIGERWOMAN", 3
	ref.TidWord = 0xc6
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{ref.RefObjID: ref}, []monster.NestRow{{SpawnPoint: target.Spawn, RetailEvidence: true, MaxCount: 1}}))
	rt.Monsters.SetTimeSource(rt.Now)
	rt.Monsters.StartDivision(testDivision)
	rt.Monsters.AdvancePopulation(rt.Monsters.CurrentTimeMillis())
	target = rt.Monsters.InstancesInRegions(testDivision, []uint16{target.Spawn.RegionID})[0]
	rt.DropRoll = constantDropRoll(0)
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	drops := rt.Ground.All(testDivision)
	if len(drops) != 60 || drops[5].Codename != "ITEM_ETC_SCROLL_RETURN_02" || drops[15].Codename != "ITEM_MALL_GLOBAL_CHATTING" {
		t.Fatalf("unique reward order/capacity: %d drops", len(drops))
	}
	drop := drops[0]
	if drop.OwnerJID != enterworld.ObjectIDForCharacter(c) || drop.Plus != 1 || len(drop.MagicOptions) != 2 || uint16(drop.MagicOptions[1]) != 65 {
		t.Fatalf("special properties missing: %+v", drop)
	}
	item := pickupKillReward(t, rt, c, clock, drop)
	if item.Plus != int64(drop.Plus) || item.Durability != int64(drop.Durability) || !reflect.DeepEqual(item.MagicOptions, drop.MagicOptions) {
		t.Fatal("unique pickup lost instance properties")
	}
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	if rt.Ground.Count(testDivision) != len(drops)-1 {
		t.Fatal("replayed kill regenerated unique rewards")
	}
}

/*
================
TestRecoveryKillPickupAndUse
================
*/
func TestRecoveryKillPickupAndUse(t *testing.T) {
	for _, tc := range []struct {
		code      string
		selection uint32
		hp        bool
	}{
		{"ITEM_ETC_HP_POTION_01", 0, true},
		{"ITEM_ETC_MP_POTION_01", 101, false},
	} {
		t.Run(tc.code, func(t *testing.T) {
			rt, clock, c, target := newCombatTestRuntime(t, 1)
			installLootReferences(t, rt)
			// Skip equipment, admit recovery and choose its actual weighted row.
			prefix := []uint32{0, 0, 0, 32767, 32767, 0, 0, tc.selection, 0}
			calls := 0
			rt.DropRoll = func() (uint32, error) {
				calls++
				if calls <= len(prefix) {
					return prefix[calls-1], nil
				}
				return 0, nil
			}
			rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
			var found grounditem.Item
			for _, drop := range rt.Ground.All(testDivision) {
				if drop.Codename == tc.code {
					found = drop
					break
				}
			}
			if found.Gid == 0 || found.StackCount != 1 {
				t.Fatal("recovery not selected by kill", tc.code)
			}
			row := pickupKillReward(t, rt, c, clock, found)
			hp, mp := int64(1), int64(1)
			c.CurrentHP, c.CurrentMP = &hp, &mp
			result := rt.HandleItemUse(testDivision, c, []byte{byte(row.Slot), byte(row.TypeFlags), byte(row.TypeFlags >> 8)})
			if tc.hp && *c.CurrentHP <= 1 || !tc.hp && *c.CurrentMP <= 1 {
				t.Fatalf("dropped recovery did not recover: %+v", result.Frames)
			}
			for _, remaining := range c.MissionInventory {
				if remaining.Slot == row.Slot {
					t.Fatal("recovery use did not consume drop")
				}
			}
		})
	}
}

/*
================
TestSelectedCurePickupAndActivation
================
*/
func TestSelectedCurePickupAndActivation(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 100)
	installLootReferences(t, rt)
	rt.DropRoll = constantDropRoll(0)
	at := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.NowMs())
	chosen, ok := rt.prepareConsumableDrop(target, 3, at, c.Name, rt.Now())
	if !ok {
		t.Fatal("ordinary cure selection failed")
	}
	chosen.OwnerJID = enterworld.ObjectIDForCharacter(c)
	row := pickupKillReward(t, rt, c, clock, rt.Ground.Add(testDivision, chosen))
	rt.deps.Update(c, "loot-cure-test", func() bool {
		rt.applyPlayerAbnormalInDoor(testDivision, c, false, []abnormal.Record{{Status: abnormal.Burn, DurationMs: 1000, Level: 1, SourceGID: target.Gid}}, clock.NowMs())
		return true
	})
	rt.HandleItemUse(testDivision, c, []byte{byte(row.Slot), byte(row.TypeFlags), byte(row.TypeFlags >> 8)})
	if block := rt.playerAbnormal(testDivision, c.Name); block != nil && block.Has(abnormal.Burn) {
		t.Fatal("selected and picked-up cure left burn active")
	}
	for _, item := range c.MissionInventory {
		if item.Slot == row.Slot {
			t.Fatal("cure was not consumed")
		}
	}
}

/*
================
TestSpecialReturnScrollPickupAndTimedActivation
================
*/
func TestSpecialReturnScrollPickupAndTimedActivation(t *testing.T) {
	rt, c, clock, _ := returnFixture(t, 30000)
	installLootReferences(t, rt)
	rt.DropRoll = constantDropRoll(0)
	ref, ok := rt.deps.ItemReferences().ItemRefByCodename("ITEM_ETC_SCROLL_RETURN_02")
	if !ok {
		t.Fatal("missing special return reference")
	}
	row := pickupPublishedLoot(t, rt, c, ref, 1)
	c.World.RebirthPoint = worldSpawnFromMission(simulation.ChinaStartProfile())
	result := rt.HandleItemUse(testDivision, c, []byte{byte(row.Slot), byte(row.TypeFlags), byte(row.TypeFlags >> 8)})
	if c.NativeTeleportMode == 0 {
		t.Fatalf("dropped special scroll did not begin return: %+v", result.Frames)
	}
	var sent []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { sent = append(sent, frames...) }
	clock.Advance(time.Minute)
	rt.TickHook()(clock.NowMs())
	if c.NativeTeleportMode != 0 || !saw(sent, enterworld.OpcodeResetClient) {
		t.Fatal("special return did not complete")
	}
	for _, item := range c.MissionInventory {
		if item.Codename == ref.Codename {
			t.Fatal("special return was not consumed")
		}
	}
}

/*
================
TestLootReferenceValidationRejectsIncompleteDeployment
================
*/
func TestLootReferenceValidationRejectsIncompleteDeployment(t *testing.T) {
	rt, _, _, _ := newCombatTestRuntime(t, 1)
	if err := rt.ValidateLootReferences(); err == nil {
		t.Fatal("incomplete reference plane accepted")
	}
	installLootReferences(t, rt)
	rt.deps.(*enterworld.Deps).MagicOptions = nil
	if err := rt.ValidateLootReferences(); err == nil {
		t.Fatal("missing magic plane accepted")
	}
}

/*
================
TestAssignedEquipmentDoesNotAcquireRandomBlues
================
*/
func TestAssignedEquipmentDoesNotAcquireRandomBlues(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 1)
	installLootReferences(t, rt)
	rt.DropRoll = constantDropRoll(32767)
	chosen := loot.DropItem{Codename: "ITEM_CH_SWORD_03_A", Count: 1, Assigned: true}
	drop, ok := rt.prepareSelectedDrop(chosen, simulation.Spawn{}, c.Name, rt.Now())
	if !ok || drop.Plus != 0 || len(drop.MagicOptions) != 0 || drop.VarianceBits != (uint64(1)<<35)-1 {
		t.Fatalf("assigned equipment property mismatch: %+v/%v", drop, ok)
	}
}
