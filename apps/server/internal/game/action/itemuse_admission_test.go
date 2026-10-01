/*
===========================================================================

itemuse_admission_test.go - tests for itemuse_admission.go

===========================================================================
*/

package action

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
recoveryFixture
================
*/
func recoveryFixture(tid int64) (*enterworld.Character, staticItemSource, []byte) {
	c := testCharacter()
	hp, mp := int64(1), int64(1)
	c.CurrentHP, c.CurrentMP = &hp, &mp
	items := testItems()
	ref := items["ITEM_ETC_HP_POTION_01"]
	ref.Country = 3
	ref.TypeIDs[3] = tid
	// Each lane's potion carries only its own params: HP (1), MP (2), both (3).
	ref.RecoveryHP, ref.RecoveryMP = 0, 0
	if tid != 2 {
		ref.RecoveryHP = 1
	}
	if tid != 1 {
		ref.RecoveryMP = 1
	}
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: 21, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 20,
	})
	return c, items, wire.NewWriter(3).U8(21).U16(ref.TypeFlags()).Payload()
}

/*
================
assertItemUseRefusedUnchanged
================
*/
func assertItemUseRefusedUnchanged(t *testing.T, rt *Runtime, c *enterworld.Character, body []byte, expected ...uint8) {
	t.Helper()
	code := wire.ErrCodeInvalidRequest
	if len(expected) != 0 {
		code = expected[0]
	}
	before := rt.characterSnapshot(testDivision, c)
	result := rt.HandleItemUse(testDivision, c, body)
	if len(result.Frames) != 1 || result.Frames[0].Opcode != wire.OpItemUseResponse ||
		!reflect.DeepEqual(result.Frames[0].Payload, wire.EncodeItemUseError(code)) || len(result.Broadcast) != 0 {
		t.Fatalf("refusal emitted success/effects: %+v", result)
	}
	if !reflect.DeepEqual(before, rt.characterSnapshot(testDivision, c)) {
		t.Fatal("refusal changed authoritative character state")
	}
}

/*
================
TestItemUseRecoveryCooldownLanesAndExactExpiry
================
*/
func TestItemUseRecoveryCooldownLanesAndExactExpiry(t *testing.T) {
	for _, race := range []string{"CHAR_CH_MAN_ADVENTURER", "CHAR_EU_MAN_ADVENTURER"} {
		for _, percent := range []bool{false, true} {
			for tid := int64(1); tid <= 3; tid++ {
				t.Run(fmt.Sprintf("%s/percent=%t/lane=%d", race, percent, tid), func(t *testing.T) {
					c, items, body := recoveryFixture(tid)
					c.ModelCodename = race
					ref := items["ITEM_ETC_HP_POTION_01"]
					duration := int64(1100)
					if race == "CHAR_EU_MAN_ADVENTURER" {
						duration = 15100
					}
					if percent {
						// 49AA70 takes the percentage arm only when both absolute
						// params are zero; that arm locks 4.1 s for every country.
						ref.RecoveryHPPercent, ref.RecoveryMPPercent = ref.RecoveryHP, ref.RecoveryMP
						ref.RecoveryHP, ref.RecoveryMP = 0, 0
						duration = 4100
					}
					rt, clock := newTestRuntime(c, items)
					result := rt.HandleItemUse(testDivision, c, body)
					if result.Frames[0].Payload[0] != 1 || c.ItemUseCooldowns[tid-1] != clock.NowMs()+duration {
						t.Fatalf("wrong acceptance/deadline: %+v / %v", result, c.ItemUseCooldowns)
					}
					// A different grade in a different slot shares the lane.
					other := *ref
					other.RefObjID, other.Codename = 99999, "ITEM_TEST_OTHER_GRADE"
					items[other.Codename] = &other
					c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
						Slot: 22, RefObjID: other.RefObjID, Codename: other.Codename, TypeFlags: other.TypeFlags(), StackCount: 10,
					})
					otherBody := wire.NewWriter(3).U8(22).U16(other.TypeFlags()).Payload()
					assertItemUseRefusedUnchanged(t, rt, c, otherBody, wire.ErrCodeItemReuseDelay)
					clock.Advance(time.Duration(duration-1) * time.Millisecond)
					assertItemUseRefusedUnchanged(t, rt, c, body, wire.ErrCodeItemReuseDelay)
					clock.Advance(time.Millisecond)
					if result := rt.HandleItemUse(testDivision, c, otherBody); result.Frames[0].Payload[0] != 1 {
						t.Fatal("exact expiry refused")
					}
				})
			}
		}
	}
}

/*
================
TestItemUseIndependentRecoveryLanesAndConcurrentReplay
================
*/
func TestItemUseIndependentRecoveryLanesAndConcurrentReplay(t *testing.T) {
	c, items, body := recoveryFixture(1)
	rt, _ := newTestRuntime(c, items)
	var pending sync.WaitGroup
	results := make(chan OpResult, 32)
	for i := 0; i < 32; i++ {
		pending.Add(1)
		go func() { defer pending.Done(); results <- rt.HandleItemUse(testDivision, c, body) }()
	}
	pending.Wait()
	close(results)
	accepted := 0
	for result := range results {
		if result.Frames[0].Payload[0] == 1 {
			accepted++
		}
	}
	if accepted != 1 || *c.CurrentHP != 2 || bagRowByCodename(c, "ITEM_ETC_HP_POTION_01").StackCount != 19 {
		t.Fatalf("replay committed %d uses, hp=%d", accepted, *c.CurrentHP)
	}
	for tid := int64(2); tid <= 3; tid++ {
		ref := *items["ITEM_ETC_HP_POTION_01"]
		ref.TypeIDs[3], ref.RefObjID, ref.Codename = tid, uint32(90000+tid), fmt.Sprintf("ITEM_TEST_LANE_%d", tid)
		items[ref.Codename] = &ref
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
			Slot: 21 + tid, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 2,
		})
		result := rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(uint8(21+tid)).U16(ref.TypeFlags()).Payload())
		if result.Frames[0].Payload[0] != 1 {
			t.Fatalf("HP incorrectly blocked category %d", tid)
		}
	}
}

/*
================
TestItemUseCooldownSurvivesStoreRebootAndSnapshotMutation
================
*/
func TestItemUseCooldownSurvivesStoreRebootAndSnapshotMutation(t *testing.T) {
	c, _, body := recoveryFixture(1)
	d := openDoorRuntime(t, t.TempDir(), c)
	d.rt.deps.(*enterworld.Deps).UpdateCharacter = d.authority.UpdateCharacter
	if r := d.rt.HandleItemUse(testDivision, d.character, body); r.Frames[0].Payload[0] != 1 {
		t.Fatal("initial use refused")
	}
	deadline := d.character.ItemUseCooldowns[0]
	snapshot := d.rt.characterSnapshot(testDivision, d.character)
	snapshot.ItemUseCooldowns[0] = 0
	if d.character.ItemUseCooldowns[0] != deadline {
		t.Fatal("snapshot aliases cooldown authority")
	}
	d = d.reboot(t)
	d.rt.deps.(*enterworld.Deps).UpdateCharacter = d.authority.UpdateCharacter
	// ITEM_ETC_HP_POTION_01 (Param1 120) at level 1, STR 20:
	// The 125 HP potion credits 25 now. Queued pulses are session-only;
	// reboot preserves the committed HP 26, inventory debit and reuse lock.
	if d.character.ItemUseCooldowns[0] != deadline || *d.character.CurrentHP != 26 ||
		bagRowByCodename(d.character, "ITEM_ETC_HP_POTION_01").StackCount != 19 {
		t.Fatal("disk restoration lost part of the item-use transaction")
	}
	assertItemUseRefusedUnchanged(t, d.rt, d.character, body, wire.ErrCodeItemReuseDelay)
	d.clock.Advance(1100 * time.Millisecond)
	if r := d.rt.HandleItemUse(testDivision, d.character, body); r.Frames[0].Payload[0] != 1 {
		t.Fatal("restored cooldown did not expire")
	}
}

/*
================
TestItemUseRefusesDeadRestrictedAndMalformedRecovery
================
*/
func TestItemUseRefusesDeadRestrictedAndMalformedRecovery(t *testing.T) {
	for name, change := range map[string]func(*enterworld.Character, *enterworld.ItemRef){
		"dead":        func(c *enterworld.Character, _ *enterworld.ItemRef) { *c.CurrentHP = 0 },
		"negative-hp": func(c *enterworld.Character, _ *enterworld.ItemRef) { *c.CurrentHP = -1 },
		"deleted":     func(c *enterworld.Character, _ *enterworld.ItemRef) { c.DeletePending = true },
		"duplicate-slot": func(c *enterworld.Character, _ *enterworld.ItemRef) {
			c.MissionInventory = append(c.MissionInventory, c.MissionInventory[1])
		},
		"country":   func(_ *enterworld.Character, r *enterworld.ItemRef) { r.Country = 1 },
		"level":     func(_ *enterworld.Character, r *enterworld.ItemRef) { r.ReqQuadTypes[0], r.ReqQuadValues[0] = 1, 2 },
		"strength":  func(_ *enterworld.Character, r *enterworld.ItemRef) { r.RequiredStr = 999 },
		"intellect": func(_ *enterworld.Character, r *enterworld.ItemRef) { r.RequiredInt = 999 },
		"identity":  func(_ *enterworld.Character, r *enterworld.ItemRef) { r.Codename = "ITEM_OTHER" },
		"nan":       func(_ *enterworld.Character, r *enterworld.ItemRef) { r.RecoveryHP = math.NaN() },
		"infinite":  func(_ *enterworld.Character, r *enterworld.ItemRef) { r.RecoveryHP = math.Inf(1) },
		"negative":  func(_ *enterworld.Character, r *enterworld.ItemRef) { r.RecoveryHP = -1 },
		"overflow":  func(_ *enterworld.Character, r *enterworld.ItemRef) { r.RecoveryHP = 1e30 },
		"no-effect": func(_ *enterworld.Character, r *enterworld.ItemRef) { r.RecoveryHP = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			c, items, body := recoveryFixture(1)
			change(c, items["ITEM_ETC_HP_POTION_01"])
			rt, _ := newTestRuntime(c, items)
			code := wire.ErrCodeInvalidRequest
			switch name {
			case "dead", "negative-hp":
				code = wire.ErrCodeItemUseDead
			case "country":
				code = wire.ErrCodeCountryMismatch
			case "level":
				code = wire.ErrCodeItemUseLevelRequired
			case "strength":
				code = wire.ErrCodeStrengthRequired
			case "intellect":
				code = wire.ErrCodeIntellectRequired
			}
			assertItemUseRefusedUnchanged(t, rt, c, body, code)
		})
	}
}

/*
================
TestItemUseUnsupportedFamiliesCannotBorrowRecoveryEffects
================
*/
func TestItemUseUnsupportedFamiliesCannotBorrowRecoveryEffects(t *testing.T) {
	for _, tids := range [][4]int64{
		// Pet potions/cures (TID4 4 / 2.7) are the COS family and have their
		// own refusal codes (TestPetItemRefusalCodes).
		{3, 3, 3, 1}, {3, 3, 3, 3},
		{3, 3, 13, 1}, {3, 3, 9, 1}, {3, 1, 6, 2}, {3, 3, 1, 33},
	} {
		t.Run(fmt.Sprint(tids), func(t *testing.T) {
			c, items, _ := recoveryFixture(1)
			ref := items["ITEM_ETC_HP_POTION_01"]
			ref.TypeIDs = tids
			c.MissionInventory[1].TypeFlags = ref.TypeFlags()
			rt, _ := newTestRuntime(c, items)
			assertItemUseRefusedUnchanged(t, rt, c, wire.NewWriter(3).U8(21).U16(ref.TypeFlags()).Payload())
		})
	}
}

/*
================
TestItemUsePermissionIsIndependentOfFamilyAndSalePermission
================
*/
func TestItemUsePermissionIsIndependentOfFamilyAndSalePermission(t *testing.T) {
	for _, value := range []float64{0, 2, 128, -1, 256, 1.5, math.NaN(), math.Inf(1)} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			c, items, body := recoveryFixture(1)
			ref := items["ITEM_ETC_HP_POTION_01"]
			ref.NativeFields = ref.NativeFields.With("canUse", value).With("canSell", 1)
			rt, _ := newTestRuntime(c, items)
			assertItemUseRefusedUnchanged(t, rt, c, body)
		})
	}
	c, items, body := recoveryFixture(1)
	items["ITEM_ETC_HP_POTION_01"].NativeFields = items["ITEM_ETC_HP_POTION_01"].NativeFields.Without("canUse")
	rt, _ := newTestRuntime(c, items)
	assertItemUseRefusedUnchanged(t, rt, c, body)
	for _, permission := range []float64{1, 129} {
		c, items, body := recoveryFixture(1)
		ref := items["ITEM_ETC_HP_POTION_01"]
		ref.NativeFields = ref.NativeFields.With("canUse", permission).With("canSell", 0)
		rt, _ := newTestRuntime(c, items)
		if result := rt.HandleItemUse(testDivision, c, body); result.Frames[0].Payload[0] != 1 {
			t.Fatalf("sale prohibition blocked use with permission %v", permission)
		}
	}
}

/*
================
TestItemUsePetSkillRejectsDeadPetBadIdentityDurationAndFullBoard
================
*/
func TestItemUsePetSkillRejectsDeadPetBadIdentityDurationAndFullBoard(t *testing.T) {
	for _, scenario := range []string{"dead-owner", "dead-pet", "wrong-gid", "wrong-ref", "fractional", "nan", "overflow", "full"} {
		t.Run(scenario, func(t *testing.T) {
			c := testCharacter()
			c.ActiveCOS = &enterworld.CharacterCOS{GID: 0x00c00003, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 1, Summoned: true}
			source := petSkillSource()
			ref := source.staticItemSource["ITEM_MALL_PET_SKILL_COLD"]
			c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 24, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 2})
			switch scenario {
			case "dead-owner":
				hp := int64(0)
				c.CurrentHP = &hp
			case "dead-pet":
				c.ActiveCOS.CurrentHP = 0
			case "wrong-gid":
				c.ActiveCOS.GID++
			case "wrong-ref":
				c.ActiveCOS.RefObjID++
			case "fractional":
				ref.NativeFields = ref.NativeFields.With("itemParam1_29c", 1.5)
			case "nan":
				ref.NativeFields = ref.NativeFields.With("itemParam1_29c", math.NaN())
			case "overflow":
				ref.NativeFields = ref.NativeFields.With("itemParam1_29c", 1e30)
			case "full":
				for i := 0; i < petSkillWindowCapacity; i++ {
					c.PetSkillWindows = append(c.PetSkillWindows, domain.PetSkillWindow{ItemRefObjID: uint32(i + 1), EndUnixMs: 2000000})
				}
			}
			rt, _ := newTestRuntime(c, source)
			code := wire.ErrCodeInvalidRequest
			if scenario == "dead-owner" {
				code = wire.ErrCodeItemUseDead
			}
			assertItemUseRefusedUnchanged(t, rt, c, wire.NewWriter(3).U8(24).U16(ref.TypeFlags()).Payload(), code)
		})
	}
}

/*
================
TestItemUseDeathDoesNotResetCooldownOrAdmitSummoning
================
*/
func TestItemUseDeathDoesNotResetCooldownOrAdmitSummoning(t *testing.T) {
	c, items, body := recoveryFixture(1)
	source := testCosSource(items)
	rt, clock := newTestRuntime(c, source)
	rt.HandleItemUse(testDivision, c, body)
	deadline := c.ItemUseCooldowns[0]
	*c.CurrentHP = 0
	ref := source.staticItemSource["ITEM_COS_T_DHORSE3"]
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 22, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 1})
	assertItemUseRefusedUnchanged(t, rt, c, wire.NewWriter(3).U8(22).U16(ref.TypeFlags()).Payload(), wire.ErrCodeItemUseDead)
	// Exercise the actual rebirth owner, then damage the revived character so
	// a full-pool refusal cannot conceal a lost cooldown.
	if result := rt.HandleLocalRebirth(testDivision, c, []byte{2}); len(result.Frames) == 0 || !enterworld.CharacterAlive(c) {
		t.Fatal("rebirth did not complete")
	}
	if c.ItemUseCooldowns[0] != deadline {
		t.Fatal("rebirth reset cooldown")
	}
	*c.CurrentHP = 1
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var restored enterworld.Character
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	reconnected, _ := newTestRuntime(&restored, source)
	reconnected.Now = clock.Now
	assertItemUseRefusedUnchanged(t, reconnected, &restored, body, wire.ErrCodeItemReuseDelay)
	if restored.ItemUseCooldowns[0] != deadline {
		t.Fatal("death/reconnect reset cooldown")
	}
}

// The native server's lock deliberately outlasts the client icon by 100ms.
/*
================
TestMPRecoveryServerGuardAfterClientIconExpiry
================
*/
func TestMPRecoveryServerGuardAfterClientIconExpiry(t *testing.T) {
	c, items, body := recoveryFixture(2)
	c.ModelCodename = "CHAR_CH_MAN_ADVENTURER"
	rt, clock := newTestRuntime(c, items)
	if result := rt.HandleItemUse(testDivision, c, body); result.Frames[0].Payload[0] != 1 {
		t.Fatal("first potion refused")
	}
	clock.Advance(1000 * time.Millisecond)
	assertItemUseRefusedUnchanged(t, rt, c, body, wire.ErrCodeItemReuseDelay)
	clock.Advance(99 * time.Millisecond)
	assertItemUseRefusedUnchanged(t, rt, c, body, wire.ErrCodeItemReuseDelay)
	clock.Advance(time.Millisecond)
	if result := rt.HandleItemUse(testDivision, c, body); result.Frames[0].Payload[0] != 1 {
		t.Fatal("native reuse guard did not expire at 1100ms")
	}
}

// 49D240: without a live COS, a pet potion refuses with 0x1871 and a pet
// cure with 3; neither borrows the player's recovery or cure.
/*
================
TestPetItemRefusalCodes
================
*/
func TestPetItemRefusalCodes(t *testing.T) {
	for _, tc := range []struct {
		tids [4]int64
		code uint8
	}{{[4]int64{3, 3, 1, 4}, wire.ErrCodeCosTarget}, {[4]int64{3, 3, 2, 7}, wire.ErrCodeCosRefused}} {
		t.Run(fmt.Sprint(tc.tids), func(t *testing.T) {
			c, items, _ := recoveryFixture(1)
			ref := items["ITEM_ETC_HP_POTION_01"]
			ref.TypeIDs = tc.tids
			c.MissionInventory[1].TypeFlags = ref.TypeFlags()
			rt, _ := newTestRuntime(c, items)
			body := wire.NewWriter(7).U8(21).U16(ref.TypeFlags()).U32(0x00C00099).Payload()
			assertItemUseRefusedUnchanged(t, rt, c, body, tc.code)
		})
	}
}
