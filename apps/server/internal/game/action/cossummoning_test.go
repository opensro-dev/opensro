/*
===========================================================================

cossummoning_test.go - persistent ownership across companion lifetimes

===========================================================================
*/
package action

import (
	"encoding/binary"
	"encoding/json"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
persistentSummonFixture
================
*/
func persistentSummonFixture() (*enterworld.Character, cosTestItemSource) {
	c := testCharacter()
	level := int64(5)
	c.Level = &level
	refs := testCosSource(testItems())
	for i, name := range []string{"ATTACK", "PICKUP"} {
		ref := &enterworld.ItemRef{Codename: "SUMMON_" + name, RefObjID: uint32(900 + i), TypeIDs: [4]int64{3, 2, 1, int64(i + 1)}, Country: 3, AssociatedCharacterCodename: name,
			NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "itemParam1_29c": 60})}
		refs.staticItemSource[ref.Codename] = ref
		refs.characters[name] = &enterworld.CharacterRef{Codename: name, RefObjID: uint32(950 + i), TidWord: 0x1c6 | uint16(i+3)<<11, Level: 1, MaxHP: 100, RunSpeed: 60, WalkSpeed: 20, Scale: 100, InventoryCapacity: uint8(i * 28)}
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: int64(23 + i), RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 1})
	}
	return c, refs
}

/*
================
useSummonerFixture
================
*/
func useSummonerFixture(t *testing.T, rt *Runtime, c *enterworld.Character, slot uint8, ref *enterworld.ItemRef) {
	t.Helper()
	result := rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(slot).U16(ref.TypeFlags()).Payload())
	if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpItemUseResponse || result.Frames[0].Payload[0] != 1 {
		t.Fatalf("summon refused: %+v", result)
	}
	// Client 854CD0: the owner's summon row ends with sub-state 1 (a fresh summon).
	for _, frame := range result.Frames {
		if frame.Opcode == wire.OpSingleObjectSpawn && frame.Payload[len(frame.Payload)-1] != cosSpawnFresh {
			t.Fatalf("summon spawn sub-state = %d, want %d", frame.Payload[len(frame.Payload)-1], cosSpawnFresh)
		}
	}
	// The peer COS lane is the only door that introduces a pet to viewers.
	for _, frame := range result.Broadcast {
		if frame.Opcode == wire.OpSingleObjectSpawn {
			t.Fatal("summon spawned the pet for viewers outside the peer COS lane")
		}
	}
}

/*
================
TestPersistentCompanionsRetainStateAcrossCancelAndRestart
================
*/
func TestPersistentCompanionsRetainStateAcrossCancelAndRestart(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, clock := newTestRuntime(c, refs)
	rt.BindPetSession(testDivision, c, 101)
	useSummonerFixture(t, rt, c, 23, refs.staticItemSource["SUMMON_ATTACK"])
	useSummonerFixture(t, rt, c, 24, refs.staticItemSource["SUMMON_PICKUP"])
	pets := c.Companions()
	if len(pets) != 2 || pets[0].GID == pets[1].GID || c.ActiveCOS != nil {
		t.Fatal("persistent families share an actor", pets)
	}
	attack, pickup := pets[0], pets[1]
	attack.Name = "Retained"
	attack.Experience = 123456
	attack.Satiety = 7654
	attack.CurrentHP = 43
	pickup.Container.Rows = []domain.InventoryRow{{Slot: 0, RefObjID: 7, StackCount: 2}}
	if got := rt.CompanionPresentations(testDivision, c.Name); len(got) != 2 {
		t.Fatalf("only %d companions published", len(got))
	}
	result := rt.HandleCosCancel(testDivision, c, wire.NewWriter(4).U32(attack.GID).Payload())
	if result.Frames[0].Payload[0] != 1 || attack.Summoned || !pickup.Summoned {
		t.Fatal("cancellation affected the wrong companion")
	}
	useSummonerFixture(t, rt, c, 23, refs.staticItemSource["SUMMON_ATTACK"])
	attack = c.CompanionByGID(attack.GID)
	if attack == nil || attack.Name != "Retained" || attack.Experience != 123456 || attack.Satiety != 7654 || attack.CurrentHP != 43 {
		t.Fatal("resummoning reset persistent progress", attack)
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var restored enterworld.Character
	if err = json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	restarted, _ := newTestRuntime(&restored, refs)
	restarted.Now = rt.Now
	restarted.restoreCharacterCOS(testDivision, restored.Name)
	restarted.BindPetSession(testDivision, &restored, 202)
	restarted.advancePets(clock.NowMs())
	if len(restarted.CompanionPresentations(testDivision, restored.Name)) != 2 || restored.Companions()[1].Container.Rows[0].StackCount != 2 {
		t.Fatal("restart lost companion or its bag")
	}
}

/*
================
TestPersistentSummonerRejectsDuplicatesAndExpiresOffline
================
*/
func TestPersistentSummonerRejectsDuplicatesAndExpiresOffline(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, _ := newTestRuntime(c, refs)
	ref := refs.staticItemSource["SUMMON_PICKUP"]
	useSummonerFixture(t, rt, c, 24, ref)
	pet := c.Companions()[0]
	duplicate := c.MissionInventory[len(c.MissionInventory)-1]
	duplicate.Slot, duplicate.Summon = 25, nil
	c.MissionInventory = append(c.MissionInventory, duplicate)
	assertItemUseRefusedUnchanged(t, rt, c, wire.NewWriter(3).U8(25).U16(ref.TypeFlags()).Payload(), 0xa9)
	pet.RentalExpiresAtUnix = rt.Now().Unix() - 1
	rt.restoreCharacterCOS(testDivision, c.Name)
	if pet.Summoned || pet.RentalRemainingSeconds != 0 {
		t.Fatal("offline expiry restored a live pickup pet")
	}
	assertItemUseRefusedUnchanged(t, rt, c, wire.NewWriter(3).U8(24).U16(ref.TypeFlags()).Payload(), 0xa4)
}

/*
================
TestDormantRevivalCannotOverwriteAnotherLivePet
================
*/
func TestDormantRevivalCannotOverwriteAnotherLivePet(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, _ := newTestRuntime(c, refs)
	useSummonerFixture(t, rt, c, 23, refs.staticItemSource["SUMMON_ATTACK"])
	dead := c.Companions()[0]
	dead.Summoned = false
	dead.CurrentHP = 0
	dead.StateFlags = 0
	duplicate := c.MissionInventory[len(c.MissionInventory)-2]
	duplicate.Slot, duplicate.Summon = 25, nil
	c.MissionInventory = append(c.MissionInventory, duplicate)
	useSummonerFixture(t, rt, c, 25, refs.staticItemSource["SUMMON_ATTACK"])
	live := c.CompanionByGID(dead.GID)
	if live == nil || live == dead {
		t.Fatal("missing replacement pet")
	}
	scroll := &enterworld.ItemRef{Codename: "REVIVE", RefObjID: 999, TypeIDs: [4]int64{3, 3, 1, 6}, Country: 3, NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1})}
	refs.staticItemSource[scroll.Codename] = scroll
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 26, RefObjID: 999, Codename: scroll.Codename, TypeFlags: scroll.TypeFlags(), StackCount: 1})
	result := rt.HandleItemUse(testDivision, c, wire.NewWriter(4).U8(26).U16(scroll.TypeFlags()).U8(23).Payload())
	if result.Frames[0].Payload[0] != 1 || dead.CurrentHP != 100 || dead.Summoned || !live.Summoned {
		t.Fatal("revival changed the wrong actor")
	}
	for _, frame := range result.Frames {
		if frame.Opcode == simulation.OpVitalsUpdate {
			t.Fatal("dormant revival published another pet's GID")
		}
	}
}

/*
================
TestPersistentPickupLeaseExpiresOnlineWithoutRemovingSibling
================
*/
func TestPersistentPickupLeaseExpiresOnlineWithoutRemovingSibling(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, clock := newTestRuntime(c, refs)
	rt.BindPetSession(testDivision, c, 101)
	useSummonerFixture(t, rt, c, 23, refs.staticItemSource["SUMMON_ATTACK"])
	useSummonerFixture(t, rt, c, 24, refs.staticItemSource["SUMMON_PICKUP"])
	pickup := c.Companions()[1]
	pickup.RentalExpiresAtUnix = clock.NowMs()/1000 + 1
	rt.advancePets(clock.NowMs() + 1000)
	if pickup.Summoned || !c.Companions()[0].Summoned || pickup.RentalRemainingSeconds != 0 {
		t.Fatal("lease expiration crossed companion ownership")
	}
}

/*
================
TestCompanionLeaseRenewalPreservesRecordAndRemainingTime
================
*/
func TestCompanionLeaseRenewalPreservesRecordAndRemainingTime(t *testing.T) {
	for _, minutes := range []int64{1440, 40320} {
		for _, tc := range []struct {
			name    string
			expired bool
			hp, mp  uint32
		}{
			{"unexpired", false, 43, 17},
			{"expired", true, 43, 17},
			{"expired-zero-hp", true, 0, 17},
		} {
			t.Run(tc.name, func(t *testing.T) {
				c, refs := persistentSummonFixture()
				refs.characters["PICKUP"].MaxMP = 80
				rt, _ := newTestRuntime(c, refs)
				useSummonerFixture(t, rt, c, 23, refs.staticItemSource["SUMMON_ATTACK"])
				useSummonerFixture(t, rt, c, 24, refs.staticItemSource["SUMMON_PICKUP"])
				pet := c.Companions()[1]
				pet.Name = "Retained"
				pet.CurrentHP, pet.CurrentMP = tc.hp, tc.mp
				gid, generation := pet.GID, pet.SummonGeneration
				pet.Container.Rows = []domain.InventoryRow{{Slot: 0, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 2}}
				base := pet.RentalExpiresAtUnix
				if tc.expired {
					pet.Summoned = false
					pet.StateFlags = 0
					pet.RentalExpiresAtUnix = rt.Now().Unix() - 60
					base = rt.Now().Unix()
				}
				ref := &enterworld.ItemRef{Codename: "EXTEND", RefObjID: 998, TypeIDs: [4]int64{3, 3, 13, 12}, Country: 3,
					NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "itemParam1_29c": float64(minutes)})}
				refs.staticItemSource[ref.Codename] = ref
				c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 25, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 2})
				request := wire.NewWriter(4).U8(25).U16(ref.TypeFlags()).U8(24).Payload()
				result := rt.HandleItemUse(testDivision, c, request)
				if len(result.Frames) == 0 || len(result.Frames[0].Payload) == 0 || result.Frames[0].Payload[0] != 1 || pet.RentalExpiresAtUnix != base+minutes*60 || pet.Name != "Retained" || len(pet.Container.Rows) != 1 || pet.Summoned == tc.expired || pet.StateFlags&1 == 0 {
					t.Fatalf("incorrect renewal: %+v %+v", pet, result)
				}
				if pet.CurrentHP != tc.hp || pet.CurrentMP != tc.mp || pet.GID != gid || pet.SummonGeneration != generation {
					t.Fatalf("renewal changed retained vitals or actor identity: HP/MP %d/%d, want %d/%d; GID/generation %d/%d, want %d/%d",
						pet.CurrentHP, pet.CurrentMP, tc.hp, tc.mp, pet.GID, pet.SummonGeneration, gid, generation)
				}
				for _, frames := range [][]wire.Frame{result.Frames, result.Broadcast} {
					for _, frame := range frames {
						if frame.Opcode == simulation.OpVitalsUpdate {
							t.Fatal("clock renewal published a vitals change", frame)
						}
					}
				}
				// Frames[1..2] are the item's reference and visual (publishItemUseVisual).
				if len(result.Frames) < 4 || result.Frames[3].Opcode != 0x3645 || len(result.Frames[3].Payload) != 7 || binary.LittleEndian.Uint32(result.Frames[3].Payload[3:]) != uint32(pet.RentalRemainingSeconds) {
					t.Fatal("renewal omitted native state/time delta", result)
				}
				assertItemUseRefusedUnchanged(t, rt, c, wire.NewWriter(4).U8(25).U16(ref.TypeFlags()).U8(23).Payload(), companionLeaseWrongTarget)
				assertItemUseRefusedUnchanged(t, rt, c, append(request, 0), companionLeaseWrongTarget)
				if tc.expired && tc.hp > 0 {
					useSummonerFixture(t, rt, c, 24, refs.staticItemSource["SUMMON_PICKUP"])
				}
			})
		}
	}
}

/*
================
TestCompanionRetirementCancelsPreparedCastBeforeGIDReuse
================
*/
func TestCompanionRetirementCancelsPreparedCastBeforeGIDReuse(t *testing.T) {
	rt, clock, c, monster := newCombatTestRuntime(t, 100)
	pet := &enterworld.CharacterCOS{GID: 9001, RefObjID: 500, InventorySlot: 23, SummonGeneration: 1, Summoned: true, CurrentHP: 100, StateFlags: 3}
	c.ActiveCOS = pet
	skill := rt.deps.SkillData().(staticSkillSource)[2]
	skill.ActionCastingTimeMs = 1000
	result := rt.prepareMonsterCast(testDivision, monster, monsterCastRecipient{character: c, gid: pet.GID}, skill, clock.NowMs())
	if !result.Accepted {
		t.Fatal("cast was not prepared")
	}
	rt.retireCosRuntime(testDivision, c, pet.GID)
	// A different retained item can have the exact same first-generation tuple.
	replacement := *pet
	c.ActiveCOS = &replacement
	frames := rt.advanceMonsterCasts(clock.NowMs() + 1001)
	if replacement.CurrentHP != 100 || len(rt.pendingMonsterCasts) != 0 || len(frames) != 1 || len(frames[0].Frames) != 1 || frames[0].Frames[0].Opcode != 0xb505 || frames[0].Frames[0].Payload[0] != 2 {
		t.Fatal("retired cast reached replacement or failed to finalize", frames)
	}
}

/*
================
TestShippedPersistentSummonersCreateAndRetainTheirAuthoredFamily

Use the published v1.150 summoners, including the trial and Silk variants.
This catches reference-chain, minute-unit and inventory-capacity regressions
that synthetic fixtures cannot detect.
================
*/
func TestShippedPersistentSummonersCreateAndRetainTheirAuthoredFamily(t *testing.T) {
	items := shippedItems(t)
	for _, name := range []string{
		"ITEM_COS_P_FLUTE", "ITEM_COS_P_RABBIT_SCROLL", "ITEM_COS_P_SPOT_RABBIT_SCROLL",
		"ITEM_COS_P_RABBIT_SCROLL_SILK", "ITEM_COS_P_GOLDPIG_SCROLL", "ITEM_COS_P_GOLDPIG_SCROLL_SILK",
		"ITEM_COS_P_PINKPIG_SCROLL", "ITEM_COS_P_GGLIDER_SCROLL", "ITEM_COS_P_CAT_SCROLL",
		"ITEM_COS_P_RACCOONDOG_SCROLL", "ITEM_COS_P_BROWNIE_SCROLL", "ITEM_COS_P_MYOWON_SCROLL", "ITEM_COS_P_SEOWON_SCROLL",
	} {
		t.Run(name, func(t *testing.T) {
			ref, ok := items.ItemRefByCodename(name)
			if !ok {
				t.Fatal("missing shipped summoner", name)
			}
			c := testCharacter()
			level := int64(5)
			c.Level = &level
			c.MissionInventory = []enterworld.InventoryRow{{Slot: 23, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 1}}
			rt, _ := newTestRuntime(c, items)
			useSummonerFixture(t, rt, c, 23, ref)
			pet := c.MissionInventory[0].Summon
			if pet == nil || !pet.Summoned || pet.CurrentHP == 0 {
				t.Fatal("missing live retained record")
			}
			if ref.TypeIDs[3] == 2 {
				minutes, _ := ref.NativeFields.Lookup("itemParam1_29c")
				if pet.RentalExpiresAtUnix != rt.Now().Unix()+int64(minutes)*60 || pet.Container == nil || pet.Container.Capacity != 28 || pet.CommandMode != 7 {
					t.Fatal("incorrect pickup lease or bag", pet)
				}
			}
			if ref.TypeIDs[3] == 1 && (pet.CommandMode != 1 || pet.Container != nil) {
				t.Fatal("incorrect attack mode or bag", pet)
			}
			// Resummoning must not reapply creation defaults after an upgrade or
			// a player choice. Exercise both persistent families through this path.
			pet.CommandMode = 0
			if pet.Container != nil {
				pet.Container.Capacity = 56
			}
			useSummonerFixture(t, rt, c, 23, ref)
			if pet.Summoned {
				t.Fatal("toggle did not dismiss retained companion")
			}
			useSummonerFixture(t, rt, c, 23, ref)
			retained := c.MissionInventory[0].Summon
			if retained.CommandMode != 0 || retained.Container != nil && retained.Container.Capacity != 56 {
				t.Fatal("resummon reset retained mode or upgraded bag", retained)
			}
			if retained.SummonGeneration != 2 {
				t.Fatal("resummon did not retain its lifetime")
			}
		})
	}
}

/*
================
TestLiveCompanionRevivalPublishesAliveAfterVitals
================
*/
func TestLiveCompanionRevivalPublishesAliveAfterVitals(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, _ := newTestRuntime(c, refs)
	useSummonerFixture(t, rt, c, 23, refs.staticItemSource["SUMMON_ATTACK"])
	pet := c.Companions()[0]
	pet.CurrentHP, pet.StateFlags = 0, cosStateSummoned
	scroll := &enterworld.ItemRef{Codename: "REVIVE", RefObjID: 999, TypeIDs: [4]int64{3, 3, 1, 6}, Country: 3, NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1})}
	refs.staticItemSource[scroll.Codename] = scroll
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 26, RefObjID: 999, Codename: scroll.Codename, TypeFlags: scroll.TypeFlags(), StackCount: 1})
	result := rt.HandleItemUse(testDivision, c, wire.NewWriter(4).U8(26).U16(scroll.TypeFlags()).U8(23).Payload())
	// The broadcast ends with the item's reference and visual (publishItemUseVisual).
	if pet.CurrentHP != 100 || len(result.Broadcast) != 4 || result.Broadcast[0].Opcode != simulation.OpVitalsUpdate || result.Broadcast[1].Opcode != wire.OpObjectStateRefresh || result.Broadcast[3].Opcode != wire.OpItemUseVisual {
		t.Fatal("revival omitted world life/vitals", result)
	}
	// Native case 6 names no object, so the revival's sparkle stays on the owner.
	if gid := binary.LittleEndian.Uint32(result.Broadcast[3].Payload); gid != enterworld.ObjectIDForCharacter(c) {
		t.Fatalf("revival sparkle on %d, want the owner", gid)
	}
	life := result.Broadcast[1].Payload
	if len(life) != 6 || binary.LittleEndian.Uint32(life) != pet.GID || life[4] != wire.StateChannelLife || life[5] != wire.LifeStateAlive {
		t.Fatal("incorrect revival identity/state", life)
	}
}

/*
================
TestItemOwnedCompanionBagTransferCommitsBothCanonicalContainers
================
*/
func TestItemOwnedCompanionBagTransferCommitsBothCanonicalContainers(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, _ := newTestRuntime(c, refs)
	useSummonerFixture(t, rt, c, 24, refs.staticItemSource["SUMMON_PICKUP"])
	gid := c.Companions()[0].GID
	for _, q := range []wire.ItemMoveRequest{
		{MovementType: wire.MoveTypePlayerToCos, CosGID: gid, SourceSlot: 20, DestSlot: 0},
		{MovementType: wire.MoveTypeCosToPlayer, CosGID: gid, SourceSlot: 0, DestSlot: 20},
	} {
		payload, err := q.Encode()
		if err != nil {
			t.Fatal(err)
		}
		result := rt.HandleItemMove(testDivision, c, payload)
		if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 {
			t.Fatal("transfer refused", result)
		}
		pet := c.CompanionByGID(gid)
		inBag := len(pet.Container.Rows)
		inPlayer := 0
		for _, row := range c.MissionInventory {
			if row.RefObjID == 11459 {
				inPlayer++
			}
		}
		if inBag+inPlayer != 1 || (q.MovementType == wire.MoveTypePlayerToCos && inBag != 1) || (q.MovementType == wire.MoveTypeCosToPlayer && inPlayer != 1) {
			t.Fatal("transfer lost or duplicated item through detached summoner", inBag, inPlayer)
		}
		if _, err = enterworld.BuildCOSRecord(pet, refs.characters["PICKUP"], refs); err != nil {
			t.Fatal(err)
		}
	}
}

/*
================
TestCancellationPublishesSelectedSummonerWithDormantGIDCollision
================
*/
func TestCancellationPublishesSelectedSummonerWithDormantGIDCollision(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, _ := newTestRuntime(c, refs)
	ref := refs.staticItemSource["SUMMON_ATTACK"]
	useSummonerFixture(t, rt, c, 23, ref)
	gid := c.Companions()[0].GID
	useSummonerFixture(t, rt, c, 23, ref)
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 25, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 1})
	useSummonerFixture(t, rt, c, 25, ref)
	result := rt.HandleCosCancel(testDivision, c, wire.NewWriter(4).U32(gid).Payload())
	if result.Frames[0].Payload[0] != 1 {
		t.Fatal("cancel refused", result)
	}
	seen := false
	for _, frame := range result.Frames {
		if frame.Opcode == cosItemStateOpcode {
			seen = true
			if frame.Payload[0] != 25 || frame.Payload[2] != 3 {
				t.Fatal("cancel updated dormant sibling item", frame)
			}
		}
	}
	if !seen {
		t.Fatal("cancel omitted summoner state")
	}
}

/*
================
TestGrowthPetsCannotBeSummonedInFreeBattle

The v1.150 client's item-use code 0xB9: "Under free battle situation,
growth pets cannot be summoned". An owner wearing a free-battle cape is
refused the attack (growth) pet and nothing changes; the pickup pet is still
summoned, and without the cape the growth pet is too.
================
*/
func TestGrowthPetsCannotBeSummonedInFreeBattle(t *testing.T) {
	c, refs := persistentSummonFixture()
	cape := &enterworld.ItemRef{Codename: "TEST_PVP_CAPE", RefObjID: 63000, TypeIDs: [4]int64{3, 1, 7, 5},
		NativeFields: enterworld.NewNativeFields(map[string]float64{freeBattleGroupField: 1})}
	refs.staticItemSource[cape.Codename] = cape
	capeRow := enterworld.InventoryRow{Slot: 8, RefObjID: cape.RefObjID, Codename: cape.Codename, TypeFlags: cape.TypeFlags(), StackCount: 1}
	c.MissionInventory = append(c.MissionInventory, capeRow)
	rt, _ := newTestRuntime(c, refs)
	attack := refs.staticItemSource["SUMMON_ATTACK"]
	assertItemUseRefusedUnchanged(t, rt, c, wire.NewWriter(3).U8(23).U16(attack.TypeFlags()).Payload(), cosSummonFreeBattle)
	useSummonerFixture(t, rt, c, 24, refs.staticItemSource["SUMMON_PICKUP"])
	c.MissionInventory = c.MissionInventory[:len(c.MissionInventory)-1]
	for i := range c.MissionInventory {
		if c.MissionInventory[i].Slot == 8 {
			t.Fatal("fixture: the cape is still worn")
		}
	}
	useSummonerFixture(t, rt, c, 23, attack)
	c.MissionInventory = append(c.MissionInventory, capeRow)
	useSummonerFixture(t, rt, c, 23, attack)
	row, ok := inventoryRowAt(c, 23)
	if !ok || row.Summon == nil || row.Summon.Summoned {
		t.Fatal("a caped owner could not dismiss its retained growth pet")
	}
	assertItemUseRefusedUnchanged(t, rt, c, wire.NewWriter(3).U8(23).U16(attack.TypeFlags()).Payload(), cosSummonFreeBattle)
}

/*
================
TestPetsRefuseSummonInBattle

The reference server refuses every persistent summon in battle with 0x78 before its
family branch, attack and pickup pets alike, and admits it once battle
ends.
================
*/
func TestPetsRefuseSummonInBattle(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, clock := newTestRuntime(c, refs)
	rt.BindPetSession(testDivision, c, 101)
	c.BattleUntilMs = clock.NowMs() + battleStateMs
	for i, name := range []string{"SUMMON_ATTACK", "SUMMON_PICKUP"} {
		ref := refs.staticItemSource[name]
		result := rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(uint8(23+i)).U16(ref.TypeFlags()).Payload())
		if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 2 || result.Frames[0].Payload[1] != cosSummonInBattle {
			t.Fatalf("%s in battle answered %+v, want the 0x78 refusal", name, result.Frames)
		}
	}
	c.BattleUntilMs = 0
	useSummonerFixture(t, rt, c, 24, refs.staticItemSource["SUMMON_PICKUP"])
}

/*
================
TestSummonedPetScrollMovesWithinBagAndUpdatesStateSlot

The native inventory slot move allows a summoned pet
scroll to move or swap within the owner's bag, and subsequent state updates
(the native summoner state notices) resolve its current bag slot.
================
*/
func TestSummonedPetScrollMovesWithinBagAndUpdatesStateSlot(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, _ := newTestRuntime(c, refs)
	attack := refs.staticItemSource["SUMMON_ATTACK"]
	pickup := refs.staticItemSource["SUMMON_PICKUP"]
	useSummonerFixture(t, rt, c, 23, attack)
	useSummonerFixture(t, rt, c, 24, pickup)

	movePayload, err := (wire.ItemMoveRequest{
		MovementType: wire.MoveTypeInventory,
		SourceSlot:   23,
		DestSlot:     30,
		Quantity:     1,
	}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	moved := rt.HandleItemMove(testDivision, c, movePayload)
	if len(moved.Frames) == 0 || moved.Frames[0].Opcode != wire.OpItemMoveResponse || moved.Frames[0].Payload[0] != 1 {
		t.Fatalf("moving summoned pet scroll to empty bag slot failed: %+v", moved.Frames)
	}
	if row30, ok := inventoryRowAt(c, 30); !ok || row30.Summon == nil || !row30.Summon.Summoned || row30.Summon.InventorySlot != 30 {
		t.Fatalf("moved scroll at slot 30 missing updated Summon state: %+v", row30)
	}

	swapPayload, err := (wire.ItemMoveRequest{
		MovementType: wire.MoveTypeInventory,
		SourceSlot:   30,
		DestSlot:     24,
		Quantity:     1,
	}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	swapped := rt.HandleItemMove(testDivision, c, swapPayload)
	if len(swapped.Frames) == 0 || swapped.Frames[0].Opcode != wire.OpItemMoveResponse || swapped.Frames[0].Payload[0] != 1 {
		t.Fatalf("swapping two summoned pet scrolls failed: %+v", swapped.Frames)
	}
	row24, ok24 := inventoryRowAt(c, 24)
	row30, ok30 := inventoryRowAt(c, 30)
	if !ok24 || row24.Codename != attack.Codename || row24.Summon == nil || row24.Summon.InventorySlot != 24 ||
		!ok30 || row30.Codename != pickup.Codename || row30.Summon == nil || row30.Summon.InventorySlot != 30 {
		t.Fatalf("swapped scrolls mismatch: 24=%+v 30=%+v", row24, row30)
	}

	// Dismissing the attack pet at its new slot 24 must publish 0x329C for slot 24.
	useSummonerFixture(t, rt, c, 24, attack)
	if row24, _ = inventoryRowAt(c, 24); row24.Summon == nil || row24.Summon.Summoned {
		t.Fatalf("attack pet at slot 24 was not dismissed: %+v", row24)
	}
}

/*
================
TestPetSummonInBattleAnswersBattleBeforeBusy

The reference server's pet summoner tests the battle state (answer 0x78)
before the casting and motion-lock tests, which answer 5. A player
casting in a fight must hear the battle notice
(UIIT_MSG_COS_CAN_NOT_CREATE_BATTLE), not the silent busy code; with the
battle state gone, the open cast still answers busy.
================
*/
func TestPetSummonInBattleAnswersBattleBeforeBusy(t *testing.T) {
	rt, _, c, target := newCombatTestRuntime(t, 100000)
	items := cosTestItemSource{staticItemSource: rt.deps.ItemReferences().(staticItemSource),
		characters: map[string]*enterworld.CharacterRef{}}
	flute := &enterworld.ItemRef{Codename: "SUMMON_ATTACK", RefObjID: 900, TypeIDs: [4]int64{3, 2, 1, 1}, Country: 3,
		AssociatedCharacterCodename: "ATTACK", NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1})}
	items.staticItemSource[flute.Codename] = flute
	items.characters["ATTACK"] = &enterworld.CharacterRef{Codename: "ATTACK", RefObjID: 950, TidWord: 0x1c6 | 3<<11,
		Level: 1, MaxHP: 100, RunSpeed: 60, WalkSpeed: 20, Scale: 100}
	rt.deps.(*enterworld.Deps).Items = items
	const fluteSlot = 23
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: fluteSlot, RefObjID: flute.RefObjID,
		Codename: flute.Codename, TypeFlags: flute.TypeFlags(), StackCount: 1})
	skill := shippedOffense(t, "SKILL_CH_SWORD_SMASH_A_01")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentMP = testInt64(10000)
	started := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if !rt.PlayerAttackLocked(testDivision, c.Name) || c.BattleUntilMs == 0 {
		t.Fatal("fixture did not open a casting instance in battle", started)
	}
	use := wire.NewWriter(3).U8(fluteSlot).U16(flute.TypeFlags()).Payload()
	assertItemUseRefusedUnchanged(t, rt, c, use, cosSummonInBattle)
	c.BattleUntilMs = 0
	assertItemUseRefusedUnchanged(t, rt, c, use, cosSummonBusy)

	// The reference server tests the seated motion before the battle
	// state: a player seated in a fight still answers the silent busy
	// code, not the battle notice.
	c.BattleUntilMs = 2_000_000
	key := simulation.WorldKey(testDivision, c.Name)
	seed := func() simulation.WorldState { return simulation.SeedWorldState(c) }
	rt.Worlds.Update(key, seed, func(w *simulation.WorldState) { w.Sitting = true })
	assertItemUseRefusedUnchanged(t, rt, c, use, cosSummonBusy)
}

/*
================
TestTransportSummonInBattleAnswersBattleBeforeBusy

The transport summoner carries the same refusal order as the
persistent lane: the battle state is tested before the casting and
motion-change locks, so a player casting in a fight hears the battle
notice for a horse scroll too; the open cast answers busy once the
fight is over.
================
*/
func TestTransportSummonInBattleAnswersBattleBeforeBusy(t *testing.T) {
	rt, _, c, target := newCombatTestRuntime(t, 100000)
	items := cosTestItemSource{staticItemSource: rt.deps.ItemReferences().(staticItemSource),
		characters: map[string]*enterworld.CharacterRef{}}
	horse := &enterworld.ItemRef{Codename: "ITEM_COS_T_DHORSE3", RefObjID: 3905, TypeIDs: [4]int64{3, 3, 3, 2}, Country: 3,
		AssociatedCharacterCodename: "COS_T_DHORSE3", NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1})}
	items.staticItemSource[horse.Codename] = horse
	items.characters["COS_T_DHORSE3"] = &enterworld.CharacterRef{Codename: "COS_T_DHORSE3", RefObjID: 3914, TidWord: 0x11C6,
		Name: "Red Horse", WalkSpeed: 20, RunSpeed: 40, Scale: 100, CanRide: true, Level: 105, MaxHP: 87829,
		MountedAttackCapability210: 3000}
	rt.deps.(*enterworld.Deps).Items = items
	const horseSlot = 23
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: horseSlot, RefObjID: horse.RefObjID,
		Codename: horse.Codename, TypeFlags: horse.TypeFlags(), StackCount: 1})
	skill := shippedOffense(t, "SKILL_CH_SWORD_SMASH_A_01")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentMP = testInt64(10000)
	started := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if !rt.PlayerAttackLocked(testDivision, c.Name) || c.BattleUntilMs == 0 {
		t.Fatal("fixture did not open a casting instance in battle", started)
	}
	use := wire.NewWriter(3).U8(horseSlot).U16(horse.TypeFlags()).Payload()
	assertItemUseRefusedUnchanged(t, rt, c, use, cosSummonInBattle)
	c.BattleUntilMs = 0
	assertItemUseRefusedUnchanged(t, rt, c, use, cosSummonBusy)
}
