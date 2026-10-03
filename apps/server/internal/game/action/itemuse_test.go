/*
===========================================================================

itemuse_test.go - tests for itemuse.go

===========================================================================
*/

package action

import (
	"encoding/binary"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
cosTestItemSource
================
*/
type cosTestItemSource struct {
	staticItemSource
	characters map[string]*enterworld.CharacterRef
}

/*
================
CharacterRefByCodename
================
*/
func (s cosTestItemSource) CharacterRefByCodename(codename string) (*enterworld.CharacterRef, bool) {
	ref, ok := s.characters[codename]
	return ref, ok
}

/*
================
SummonableCharacterRefs
================
*/
func (s cosTestItemSource) SummonableCharacterRefs() []enterworld.CharacterRef {
	rows := make([]enterworld.CharacterRef, 0, len(s.characters))
	for _, ref := range s.characters {
		rows = append(rows, *ref)
	}
	return rows
}

/*
================
testCosSource
================
*/
func testCosSource(items staticItemSource) cosTestItemSource {
	items["ITEM_COS_T_DHORSE3"] = &enterworld.ItemRef{
		RefObjID: 3905, Codename: "ITEM_COS_T_DHORSE3",
		TypeIDs:                     [4]int64{3, 3, 3, 2},
		AssociatedCharacterCodename: "COS_T_DHORSE3",
		NativeFields:                enterworld.NewNativeFields(map[string]float64{"canUse": 1}),
	}
	return cosTestItemSource{
		staticItemSource: items,
		characters: map[string]*enterworld.CharacterRef{
			"COS_T_DHORSE3": {
				RefObjID: 3914, TidWord: 0x11C6, Codename: "COS_T_DHORSE3",
				Name: "Red Horse", WalkSpeed: 20, RunSpeed: 40, Scale: 100, CanRide: true,
				Level: 105, MaxHP: 87829, MountedAttackCapability210: 3000,
			},
		},
	}
}

/*
================
TestHorseLevelRefusalPreservesItemAndAllowsUseAtRequirement
================
*/
func TestHorseLevelRefusalPreservesItemAndAllowsUseAtRequirement(t *testing.T) {
	c := testCharacter()
	level := int64(5)
	c.Level = &level
	items := testCosSource(testItems())
	ref := items.staticItemSource["ITEM_COS_T_DHORSE3"]
	ref.ReqQuadTypes[0], ref.ReqQuadValues[0] = 1, 10
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 22, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 1})
	rt, _ := newTestRuntime(c, items)
	body := []byte{22, 0xec, 0x11}
	assertItemUseRefusedUnchanged(t, rt, c, body, wire.ErrCodeItemUseLevelRequired)
	if c.ActiveCOS != nil {
		t.Fatal("rejected summon created a horse")
	}
	level = 10
	result := rt.HandleItemUse(testDivision, c, body)
	if result.Frames[0].Payload[0] != 1 || c.ActiveCOS == nil {
		t.Fatalf("eligible horse refused: %+v", result)
	}
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 22, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 1})
	assertItemUseRefusedUnchanged(t, rt, c, body, wire.ErrCodeMultipleCOS)
}

/*
================
TestHandleItemUseConsumesPotionRecoversHPAndAnswersNativeBurst
================
*/
func TestHandleItemUseConsumesPotionRecoversHPAndAnswersNativeBurst(t *testing.T) {
	character := testCharacter()
	currentHP := int64(40)
	currentMP := int64(80)
	character.CurrentHP = &currentHP
	character.CurrentMP = &currentMP
	character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{
		Slot:         21,
		RefObjID:     3630,
		Codename:     "ITEM_ETC_HP_POTION_01",
		TypeFlags:    wire.PackTypeFlags(3, 3, 1, 1),
		VarianceBits: "0",
		StackCount:   2,
	})
	rt, _ := newTestRuntime(character, testItems())

	result := rt.HandleItemUse(testDivision, character, []byte{21, 0xEC, 0x08})
	assertOpcodes(t, result.Frames, wire.OpItemUseResponse, simulation.OpVitalsUpdate)
	if got, want := result.Frames[0].Payload,
		wire.EncodeItemUseSuccess(21, 1, 0x08EC); !reflect.DeepEqual(got, want) {
		t.Fatalf("0xB5BD = % X, want % X", got, want)
	}
	// 49AA70 sizes 125 HP; 49A5B0 credits 25 now and queues four steps.
	if character.CurrentHP == nil || *character.CurrentHP != 65 {
		t.Fatalf("CurrentHP = %v, want first-pulse HP 65", character.CurrentHP)
	}
	if character.CurrentMP == nil || *character.CurrentMP != 80 {
		t.Fatalf("CurrentMP = %v, want untouched 80", character.CurrentMP)
	}
	for _, row := range character.MissionInventory {
		if row.Slot == 21 && row.StackCount != 1 {
			t.Fatalf("remaining stack = %d, want 1", row.StackCount)
		}
	}
}

/*
================
TestHandleItemUseLastPotionRemovesRow
================
*/
func TestHandleItemUseLastPotionRemovesRow(t *testing.T) {
	character := testCharacter()
	currentHP := int64(1)
	character.CurrentHP = &currentHP
	character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{
		Slot: 21, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01",
		TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), VarianceBits: "0", StackCount: 1,
	})
	rt, _ := newTestRuntime(character, testItems())

	result := rt.HandleItemUse(testDivision, character, []byte{21, 0xEC, 0x08})
	assertOpcodes(t, result.Frames, wire.OpItemUseResponse, simulation.OpVitalsUpdate)
	if got := result.Frames[0].Payload; !reflect.DeepEqual(
		got,
		wire.EncodeItemUseSuccess(21, 0, 0x08EC),
	) {
		t.Fatalf("0xB5BD = % X", got)
	}
	for _, row := range character.MissionInventory {
		if row.Slot == 21 {
			t.Fatalf("consumed row survived: %+v", row)
		}
	}
}

/*
================
TestHandleItemUseRejectsSpoofAndFullPoolWithoutMutation
================
*/
func TestHandleItemUseRejectsSpoofAndFullPoolWithoutMutation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		currentHP int64
		payload   []byte
	}{
		{name: "type-word-spoof", currentHP: 1, payload: []byte{21, 0xED, 0x08}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			character := testCharacter()
			character.CurrentHP = &tc.currentHP
			character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{
				Slot: 21, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01",
				TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), VarianceBits: "0", StackCount: 2,
			})
			rt, _ := newTestRuntime(character, testItems())

			result := rt.HandleItemUse(testDivision, character, tc.payload)
			assertOpcodes(t, result.Frames, wire.OpItemUseResponse)
			if got, want := result.Frames[0].Payload,
				wire.EncodeItemUseError(wire.ErrCodeInvalidRequest); !reflect.DeepEqual(got, want) {
				t.Fatalf("refusal = % X, want % X", got, want)
			}
			row, ok := func() (enterworld.InventoryRow, bool) {
				for _, candidate := range character.MissionInventory {
					if candidate.Slot == 21 {
						return candidate, true
					}
				}
				return enterworld.InventoryRow{}, false
			}()
			if !ok || row.StackCount != 2 || *character.CurrentHP != tc.currentHP {
				t.Fatalf("state changed on refusal: row=%+v hp=%d", row, *character.CurrentHP)
			}
		})
	}
}

/*
================
TestHandleItemUseCreatesAuthoritativeCosBeforeSpawn
================
*/
func TestHandleItemUseCreatesAuthoritativeCosBeforeSpawn(t *testing.T) {
	character := testCharacter()
	character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{
		Slot: 22, RefObjID: 3905, Codename: "ITEM_COS_T_DHORSE3",
		TypeFlags: wire.PackTypeFlags(3, 3, 3, 2), StackCount: 1,
	})
	rt, _ := newTestRuntime(character, testCosSource(testItems()))

	result := rt.HandleItemUse(testDivision, character, []byte{22, 0xEC, 0x11})
	assertOpcodes(t, result.Frames,
		wire.OpItemUseResponse,
		wire.OpCosRecordCreate,
		wire.OpSingleObjectSpawn,
		wire.OpCosRideState,
		movementSpeedOpcode,
	)
	assertOpcodes(t, result.Broadcast, wire.OpSingleObjectSpawn, wire.OpCosRideState, movementSpeedOpcode)
	if character.ActiveCOS == nil || character.ActiveCOS.GID != 0x00C00003 ||
		character.ActiveCOS.RefObjID != 3914 || !character.ActiveCOS.Summoned || !character.ActiveCOS.Mounted {
		t.Fatalf("active COS = %+v", character.ActiveCOS)
	}
	if got := binary.LittleEndian.Uint32(result.Frames[1].Payload[0:4]); got != character.ActiveCOS.GID {
		t.Fatalf("3158 gid = 0x%X, want 0x%X", got, character.ActiveCOS.GID)
	}
	for _, row := range character.MissionInventory {
		if row.Slot == 22 {
			t.Fatalf("summoner survived consumption: %+v", row)
		}
	}
}

/*
================
TestCosMountAndMountedAttackShareAuthorityOnlyAfterCosGates
================
*/
func TestCosMountAndMountedAttackShareAuthorityOnlyAfterCosGates(t *testing.T) {
	character := testCharacter()
	source := testCosSource(testItems())
	rt, _ := newTestRuntime(character, source)
	character.ActiveCOS = &enterworld.CharacterCOS{
		GID: 0x00C00003, RefObjID: 3914, Codename: "COS_T_DHORSE3",
		CurrentHP: 87829, Summoned: true,
	}

	mountBody := wire.NewWriter(5).U32(character.ActiveCOS.GID).U8(wire.CosCommandMountTag).Payload()
	mounted := rt.HandleCosCommand(testDivision, character, mountBody)
	assertOpcodes(t, mounted.Frames, wire.OpObjectSourceCorrection, wire.OpCosRideState, movementSpeedOpcode)
	assertOpcodes(t, mounted.Broadcast, wire.OpObjectSourceCorrection, wire.OpCosRideState, movementSpeedOpcode)
	if character.ActiveCOS == nil || !character.ActiveCOS.Mounted {
		t.Fatal("mount success did not persist ride state")
	}
	if got := mounted.Frames[1].Payload; !reflect.DeepEqual(
		got,
		wire.EncodeCosRideState(enterworld.ObjectIDForCharacter(character), true, character.ActiveCOS.GID),
	) {
		t.Fatalf("B4B5 = % X", got)
	}

	// This detached runtime has no monster authority, so a valid mounted
	// command reaches the shared beginBasicAttack admission and returns no
	// combat frames; changing any COS gate must produce the same fail-closed
	// wire result without changing ride state.
	attackBody := wire.NewWriter(9).
		U32(character.ActiveCOS.GID).
		U8(wire.CosCommandAttackTag).
		U32(400001).
		Payload()
	if result := rt.HandleCosCommand(testDivision, character, attackBody); len(result.Frames) != 0 {
		t.Fatalf("detached mounted attack emitted %+v", result)
	}
	character.ActiveCOS.Mounted = false
	if result := rt.HandleCosCommand(testDivision, character, attackBody); len(result.Frames) != 0 {
		t.Fatalf("unmounted attack emitted %+v", result)
	}
}

/*
================
TestMountedAttackOrderNeverMakesTheRiderFight

4D2200 hands the attack order to the vehicle's AI (event 0x19). A rider on a
horse or transport never swings its own weapon from the saddle: no strike,
no attack intent, whatever the rider carries (a bow included).
================
*/
func TestMountedAttackOrderNeverMakesTheRiderFight(t *testing.T) {
	rt, _, character, target := newCombatTestRuntime(t, 100)
	deps, ok := rt.deps.(*enterworld.Deps)
	if !ok {
		t.Fatalf("combat fixture deps = %T, want *enterworld.Deps", rt.deps)
	}
	items, ok := deps.Items.(staticItemSource)
	if !ok {
		t.Fatalf("combat fixture item source = %T, want staticItemSource", deps.Items)
	}
	deps.Items = testCosSource(items)
	character.ActiveCOS = &enterworld.CharacterCOS{
		GID: 0x00C00003, RefObjID: 3914, Codename: "COS_T_DHORSE3",
		CurrentHP: 87829, Summoned: true, Mounted: true,
	}

	result := rt.HandleCosCommand(testDivision, character, wire.NewWriter(9).
		U32(character.ActiveCOS.GID).
		U8(wire.CosCommandAttackTag).
		U32(target.Gid).
		Payload())
	if len(result.Frames) != 0 || len(result.Broadcast) != 0 {
		t.Fatalf("mounted attack order struck: %+v", result)
	}
	if intents := rt.combatIntentSnapshot(); len(intents) != 0 {
		t.Fatalf("mounted attack order installed rider intents %+v", intents)
	}
	if !character.ActiveCOS.Mounted {
		t.Fatal("the refused order changed ride state")
	}
}

/*
==================
TestCosSummonDoesNotFabricateABoardWindow

A COS summoner never drives the kind-3 board slot: sub_6E6E00 reads Param1
as SECONDS and a summoner's Param1 is MINUTES (ITEM_COS_P_EXTENSION_1D
carries 1440 for one day). The v1.150 family that satisfies the contract is
ITEM_MALL_PET_SKILL_*, raised on use below, so a summon must not fabricate a
0x3691 of its own.
==================
*/
/*
================
TestCosSummonDoesNotFabricateABoardWindow
================
*/
func TestCosSummonDoesNotFabricateABoardWindow(t *testing.T) {
	character := testCharacter()
	source := testCosSource(testItems())
	item := source.staticItemSource["ITEM_COS_T_DHORSE3"]
	// Summon admission must not interpret a summoner parameter as a timed
	// pet-skill window. Use an actual transport family, not a rabbit with
	// a fabricated transport type word.
	item.NativeFields = enterworld.NewNativeFields(map[string]float64{"itemParam1_29c": 40320, "canUse": 1})
	character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{
		Slot: 23, RefObjID: item.RefObjID, Codename: item.Codename, TypeFlags: item.TypeFlags(), StackCount: 1,
	})
	rt, _ := newTestRuntime(character, source)
	for _, frame := range rt.HandleItemUse(testDivision, character, []byte{23, 0xec, 0x11}).Frames {
		if frame.Opcode == wire.OpCosStateRefresh {
			t.Fatal("summon fabricated a kind-3 window")
		}
	}
	if character.ActiveCOS == nil || !character.ActiveCOS.Summoned {
		t.Fatal("the summon itself must still succeed")
	}
}

// ITEM_MALL_PET_SKILL_* authors Param1 in seconds, as 6E6E00 expects.
/*
================
petSkillSource
================
*/
func petSkillSource() cosTestItemSource {
	source := testCosSource(testItems())
	source.staticItemSource["ITEM_MALL_PET_SKILL_COLD"] = &enterworld.ItemRef{
		RefObjID: 24001, Codename: "ITEM_MALL_PET_SKILL_COLD",
		TypeIDs:      [4]int64{3, 3, 13, 15},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"itemParam1_29c": 1800, "canUse": 129}),
	}
	return source
}

/*
================
TestPetSkillItemRaisesTheKind3WindowOnlyWithALivePet
================
*/
func TestPetSkillItemRaisesTheKind3WindowOnlyWithALivePet(t *testing.T) {
	character := testCharacter()
	character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{
		Slot: 24, RefObjID: 24001, Codename: "ITEM_MALL_PET_SKILL_COLD",
		TypeFlags: wire.PackTypeFlags(3, 3, 13, 15), StackCount: 2,
	})
	rt, _ := newTestRuntime(character, petSkillSource())

	typeWord := wire.PackTypeFlags(3, 3, 13, 15)
	body := []byte{24, byte(typeWord), byte(typeWord >> 8)}
	// A pet skill needs the pet it applies to.
	for _, frame := range rt.HandleItemUse(testDivision, character, body).Frames {
		if frame.Opcode == wire.OpCosStateRefresh {
			t.Fatal("a pet skill raised a window without a live COS")
		}
	}

	character.ActiveCOS = &enterworld.CharacterCOS{
		GID: 0x00C00003, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 1, Summoned: true,
	}
	result := rt.HandleItemUse(testDivision, character, body)
	var window []byte
	for _, frame := range result.Frames {
		if frame.Opcode == wire.OpCosStateRefresh {
			window = frame.Payload
		}
	}
	// A fresh use carries its whole 1800-second window, keyed by the ITEM.
	if want := wire.EncodeCosSummonTimer3691(24001, 1800, 0); !reflect.DeepEqual(window, want) {
		t.Fatalf("window = % X, want % X", window, want)
	}
	// The codename outlives the stack so world entry can re-seed the ref row.
	if len(character.PetSkillWindows) != 1 || character.PetSkillWindows[0].ItemRefObjID != 24001 ||
		character.PetSkillWindows[0].Codename != "ITEM_MALL_PET_SKILL_COLD" {
		t.Fatalf("windows = %+v", character.PetSkillWindows)
	}
	// The deadline is absolute, so a save keeps it.
	if remaining := petSkillWindowRemaining(character.PetSkillWindows[0], rt.Now().UnixMilli()); remaining != 1800 {
		t.Fatalf("remaining = %d, want 1800", remaining)
	}
	if spent := petSkillWindowRemaining(character.PetSkillWindows[0], rt.Now().UnixMilli()+1800_000); spent != 0 {
		t.Fatalf("spent remaining = %d", spent)
	}
	// The same item restarts its own row; sub_6E6150 keys on kind and id.
	character.PetSkillWindows = upsertPetSkillWindow(character.PetSkillWindows, 24001, "ITEM_MALL_PET_SKILL_COLD", 5)
	if len(character.PetSkillWindows) != 1 || character.PetSkillWindows[0].EndUnixMs != 5 {
		t.Fatalf("restart replaced the wrong row: %+v", character.PetSkillWindows)
	}
	character.PetSkillWindows = upsertPetSkillWindow(character.PetSkillWindows, 24002, "ITEM_MALL_PET_SKILL_FIRE", 7)
	if len(character.PetSkillWindows) != 2 {
		t.Fatalf("a distinct item must stack: %+v", character.PetSkillWindows)
	}
}

// 49B710 consumes a potion into a full gauge: only both amounts <= 0 is a
// refusal, and 4EF450 clamps the recovery to the maximum.
/*
================
TestHandleItemUseConsumesIntoAFullGauge
================
*/
func TestHandleItemUseConsumesIntoAFullGauge(t *testing.T) {
	character := testCharacter()
	full := enterworld.DerivedMaxHP(character)
	character.CurrentHP = &full
	character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{
		Slot: 21, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01",
		TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), VarianceBits: "0", StackCount: 2,
	})
	rt, _ := newTestRuntime(character, testItems())
	result := rt.HandleItemUse(testDivision, character, []byte{21, 0xEC, 0x08})
	if result.Frames[0].Payload[0] != 1 || *character.CurrentHP != full || bagRowByCodename(character, "ITEM_ETC_HP_POTION_01").StackCount != 1 {
		t.Fatalf("full-gauge use: %+v hp %d", result.Frames, *character.CurrentHP)
	}
}

/*
==================
TestComputePotionAmountFollowsNative

49AA70 against independently computed values: the absolute arm scales by
(stat/416+1) * 1.02^(level-1); the percentage arm multiplies the maximum by
the float32 fraction; a nonzero absolute param wins over percentages.
==================
*/
/*
================
TestComputePotionAmountFollowsNative
================
*/
func TestComputePotionAmountFollowsNative(t *testing.T) {
	for _, tc := range []struct {
		ref          enterworld.ItemRef
		level        uint8
		str, intel   float64
		maxHP, maxMP int64
		hp, mp       int64
		absolute     bool
	}{
		{enterworld.ItemRef{RecoveryHP: 120}, 1, 20, 20, 200, 200, 125, 0, true},
		{enterworld.ItemRef{RecoveryHP: 120}, 50, 200, 20, 5000, 5000, 468, 0, true},
		{enterworld.ItemRef{RecoveryMP: 30, RecoveryHPPercent: 50}, 1, 20, 20, 200, 200, 0, 31, true},
		{enterworld.ItemRef{RecoveryHPPercent: 25, RecoveryMPPercent: 33}, 1, 20, 20, 1000, 777, 250, 256, false},
	} {
		got, ok := computePotionAmount(&tc.ref, tc.level, tc.str, tc.intel, tc.maxHP, tc.maxMP)
		if !ok || got.hp != tc.hp || got.mp != tc.mp || got.absolute != tc.absolute {
			t.Fatalf("%+v -> %+v", tc.ref, got)
		}
	}
}
