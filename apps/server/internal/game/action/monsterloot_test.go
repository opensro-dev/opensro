/*
===========================================================================

monsterloot_test.go - loot behavior and lifecycle verification

===========================================================================
*/

package action

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
installSmallGoldRef
================
*/
func installSmallGoldRef(rt *Runtime) {
	items := rt.deps.ItemReferences().(staticItemSource)
	items[inventory.GoldHeapSmall] = &enterworld.ItemRef{
		RefObjID: 61, Codename: inventory.GoldHeapSmall,
		TypeIDs: [4]int64{3, 3, 5, 1},
	}
}

/*
================
TestEuropeanCrossbowFatalCommitsAmmoAndOwnedLootTogether
================
*/
func TestEuropeanCrossbowFatalCommitsAmmoAndOwnedLootTogether(t *testing.T) {
	rt, _, character, target := newCombatTestRuntime(t, 1)
	*character.RaceIndex = enterworld.RaceEurope
	character.ModelCodename = "CHAR_EU_MAN_NOBLE"
	items := rt.deps.ItemReferences().(staticItemSource)
	weapon := items[character.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 12
	weapon.Combat.ActionRange = 180
	character.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	bolt := &enterworld.ItemRef{
		RefObjID: 62002, Codename: "ITEM_ETC_AMMO_BOLT_01",
		TypeIDs: [4]int64{3, 3, 4, 2},
	}
	items[bolt.Codename] = bolt
	character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{
		Slot: 7, RefObjID: bolt.RefObjID, Codename: bolt.Codename,
		TypeFlags: bolt.TypeFlags(), StackCount: 2,
	})
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Codename = "SKILL_EU_CROSSBOW_BASE_01"
	skill.RequiredWeaponKinds = [2]uint8{12, 0xff}
	skills[2] = skill
	installSmallGoldRef(rt)
	rt.DropRoll = goldOnlyMonsterDropRoll(0, 0)

	result := rt.HandleTargetInteract(testDivision, character, wire.SkillAction{
		ActionId: 2, HasTarget: true, TargetGid: target.Gid,
	}.Encode())
	result = assertAndSeparateActionSession(t, result)
	assertOpcodes(t, result.Frames, wire.OpSkillCastResult, wire.OpObjectStateRefresh, wire.OpSingleObjectSpawn, wire.OpAvatarInventorySlot7StackCount)
	if !bytes.Equal(result.Frames[3].Payload, []byte{1, 0}) {
		t.Fatalf("EU fatal ammo = %v, want 1", result.Frames[3].Payload)
	}
	assertOpcodes(t, result.Broadcast, wire.OpSkillCastResult, wire.OpObjectStateRefresh, wire.OpSingleObjectSpawn)
	if character.MissionInventory[1].StackCount != 1 || rt.Ground.Count(testDivision) != 1 {
		t.Fatalf("EU fatal state split: ammo=%d ground=%d", character.MissionInventory[1].StackCount, rt.Ground.Count(testDivision))
	}
}

/*
================
dropRollSequence
================
*/
func dropRollSequence(values ...uint32) func() (uint32, error) {
	index := 0
	return func() (uint32, error) {
		value := values[index]
		index++
		return value, nil
	}
}

/*
================
assignedDropMisses

The rolls that make every assigned reward of the combat fixture's monster
miss. Assigned rewards roll before gold (7245C0), and the fixture monster
(MOB_CH_MANGNYANG) carries its own characterdata materials: a constant
32767 keeps each count at its minimum and lands each million roll at 741823,
above every authored chance, so its consumption is measured, not assumed.
================
*/
func assignedDropMisses() []uint32 {
	n := 0
	loot.AssignedDrops("MOB_CH_MANGNYANG", 255, func() (uint32, error) {
		n++
		return 32767, nil
	})
	values := make([]uint32, n)
	for i := range values {
		values[i] = 32767
	}
	return values
}

/*
================
goldOnlyMonsterDropRoll
================
*/
func goldOnlyMonsterDropRoll(amountRoll, admissionRoll uint32) func() (uint32, error) {
	// assigned misses; gold chance+amount; ordinary equipment rarity+two-draw
	// class miss; sixteen class/family draws that reject all consumables;
	// gold admission.
	values := append(assignedDropMisses(), 0, amountRoll, 0, 32767, 32767)
	for i := 0; i < 16; i++ {
		values = append(values, 32767)
	}
	values = append(values, admissionRoll, 0, 0, 0)
	return dropRollSequence(values...)
}

/*
================
constantDropRoll
================
*/
func constantDropRoll(value uint32) func() (uint32, error) {
	return func() (uint32, error) { return value, nil }
}

/*
================
TestMonsterGoldTableAndNativeRollBoundaries
================
*/
func TestMonsterGoldTableAndNativeRollBoundaries(t *testing.T) {
	if min, max, ok := monsterGoldRange(1); !ok || min != 28 || max != 59 {
		t.Fatalf("level 1 gold = %d..%d/%v, want 28..59", min, max, ok)
	}
	if min, max, ok := monsterGoldRange(140); !ok || min != 515 || max != 1080 {
		t.Fatalf("level 140 gold = %d..%d/%v, want 515..1080", min, max, ok)
	}

	rt, _, _, target := newCombatTestRuntime(t, 1)
	rt.DropRoll = dropRollSequence(0, 0)
	if amount, ok := rt.rollMonsterGoldAmount(target); !ok || amount != 28 {
		t.Fatalf("minimum roll = %d/%v, want 28", amount, ok)
	}
	rt.DropRoll = dropRollSequence(0, 32767)
	if amount, ok := rt.rollMonsterGoldAmount(target); !ok || amount != 59 {
		t.Fatalf("maximum roll = %d/%v, want 59", amount, ok)
	}
	rt.DropRoll = dropRollSequence(100)
	if rt.admitMonsterDrop(8, target) {
		t.Fatal("level-gap admission accepted roll 100 above threshold 72")
	}
}

/*
================
TestBetaGoldRateScalesTheNativeHeapWithoutExtraDraws

The rate multiplies the built heap; the two native draws stay the only RNG
consumed, and an oversized heap clamps instead of vanishing.
================
*/
func TestBetaGoldRateScalesTheNativeHeapWithoutExtraDraws(t *testing.T) {
	rt, _, _, target := newCombatTestRuntime(t, 1)
	rt.GoldRate = 50
	rt.DropRoll = dropRollSequence(0, 32767)
	if amount, ok := rt.rollMonsterGoldAmount(target); !ok || amount != 59*50 {
		t.Fatalf("beta maximum roll = %d/%v, want %d", amount, ok, 59*50)
	}
	rt.GoldRate = 1 << 30
	rt.DropRoll = dropRollSequence(0, 0)
	if amount, ok := rt.rollMonsterGoldAmount(target); !ok || amount != math.MaxInt32 {
		t.Fatalf("oversized beta heap = %d/%v, want the dword clamp", amount, ok)
	}
}

/*
================
TestFatalHitCommitsOwnedGoldAtMonsterLivePoseAndPublishesOnce
================
*/
func TestFatalHitCommitsOwnedGoldAtMonsterLivePoseAndPublishesOnce(t *testing.T) {
	rt, _, character, target := newCombatTestRuntime(t, 1)
	installSmallGoldRef(rt)
	rt.DropRoll = goldOnlyMonsterDropRoll(0, 0)

	result := rt.HandleTargetInteract(testDivision, character, wire.SkillAction{
		ActionId: 2, HasTarget: true, TargetGid: target.Gid,
	}.Encode())
	result = assertAndSeparateActionSession(t, result)
	assertSkillDamageOpen(t, result.Frames, 2, enterworld.ObjectIDForCharacter(character), target.Gid)
	if len(result.Frames) != 3 || result.Frames[1].Opcode != wire.OpObjectStateRefresh || result.Frames[2].Opcode != wire.OpSingleObjectSpawn ||
		len(result.Broadcast) != 3 || result.Broadcast[1].Opcode != wire.OpObjectStateRefresh || result.Broadcast[2].Opcode != wire.OpSingleObjectSpawn {
		t.Fatalf("fatal publication actor=%04X broadcast=%04X, want B245, LIFE-dead, then 30D7 on both", opcodesOf(result.Frames), opcodesOf(result.Broadcast))
	}
	row, err := wire.DecodeGroundItemRow(result.Frames[2].Payload,
		wire.PackTypeFlags(3, 3, 5, 1), true)
	if err != nil {
		t.Fatalf("fatal drop row: %v", err)
	}
	if row.GoldAmount != 28 || row.HasOwner != 1 || row.OwnerJID != enterworld.ObjectIDForCharacter(character) {
		t.Fatalf("fatal gold row = %+v, want 28 gold owned by %d", row, enterworld.ObjectIDForCharacter(character))
	}
	if row.RegionID != target.Spawn.RegionID || row.X != float32(target.Spawn.X+8) ||
		row.Y != float32(target.Spawn.Y) || row.Z != float32(target.Spawn.Z) {
		t.Fatalf("fatal drop pose = %+v, want monster live spawn %+v", row.Position, target.Spawn)
	}
	if drops := rt.Ground.All(testDivision); len(drops) != 1 || drops[0].Gid != row.Gid {
		t.Fatalf("ground authority = %+v, want published gid %d", drops, row.Gid)
	}

	again := rt.HandleTargetInteract(testDivision, character, wire.SkillAction{
		ActionId: 2, HasTarget: true, TargetGid: target.Gid,
	}.Encode())
	assertQueuedAction(t, again)
	if rt.Ground.Count(testDivision) != 1 {
		t.Fatalf("post-fatal replay = %+v ground=%d, want no duplicate", again, rt.Ground.Count(testDivision))
	}
}

/*
================
TestFatalHitCommitsAndPublishesEveryPreparedDropInNativeOrder
================
*/
func TestFatalHitCommitsAndPublishesEveryPreparedDropInNativeOrder(t *testing.T) {
	rt, _, character, target := newCombatTestRuntime(t, 1)
	installSmallGoldRef(rt)
	// Zero selects gold minimum, ordinary equipment group 1, the first CH
	// weighted row (sword), +0, zero variance, then admits both rows.
	rt.DropRoll = constantDropRoll(0)

	result := rt.HandleTargetInteract(testDivision, character, wire.SkillAction{
		ActionId: 2, HasTarget: true, TargetGid: target.Gid,
	}.Encode())
	result = assertAndSeparateActionSession(t, result)
	if len(result.Frames) != 4 || len(result.Broadcast) != 4 ||
		result.Frames[0].Opcode != wire.OpSkillCastResult ||
		result.Frames[1].Opcode != wire.OpObjectStateRefresh ||
		result.Frames[2].Opcode != wire.OpSingleObjectSpawn ||
		result.Frames[3].Opcode != wire.OpSingleObjectSpawn {
		t.Fatalf("multi-drop publication actor=%04X broadcast=%04X, want B245, LIFE-dead, gold 30D7, item 30D7",
			opcodesOf(result.Frames), opcodesOf(result.Broadcast))
	}
	drops := rt.Ground.All(testDivision)
	if len(drops) != 2 || drops[0].GoldAmount != 28 ||
		drops[1].Codename != "ITEM_CH_SWORD_01_A" {
		t.Fatalf("prepared list committed as %+v, want gold then CH sword", drops)
	}
	for _, drop := range drops {
		if drop.OwnerJID != enterworld.ObjectIDForCharacter(character) ||
			drop.Position.RegionID != target.Spawn.RegionID ||
			drop.Position.X != float32(target.Spawn.X+8) ||
			drop.Y != float32(target.Spawn.Y) ||
			drop.Position.Z != float32(target.Spawn.Z) {
			t.Fatalf("drop lost owner/monster-pose contract: %+v", drop)
		}
	}
}

/*
================
TestLowGradeEquipmentCountryBucketsCoverChinaAndEurope
================
*/
func TestLowGradeEquipmentCountryBucketsCoverChinaAndEurope(t *testing.T) {
	for _, tc := range []struct {
		country  uint8
		codename string
		types    [4]int64
	}{
		{0, "ITEM_CH_SWORD_01_A", [4]int64{3, 1, 6, 2}},
		{1, "ITEM_EU_DAGGER_01_A", [4]int64{3, 1, 6, 13}},
	} {
		rt, _, character, target := newCombatTestRuntime(t, 1)
		installSmallGoldRef(rt)
		target.Ref.Country = tc.country
		items := rt.deps.ItemReferences().(staticItemSource)
		items[tc.codename] = &enterworld.ItemRef{
			RefObjID: 90000 + uint32(tc.country), Codename: tc.codename,
			TypeIDs: tc.types,
		}
		rt.DropRoll = constantDropRoll(0)
		drops := rt.planMonsterKillLoot(character, target, monster.Pose{
			RegionID: target.Spawn.RegionID,
			X:        target.Spawn.X,
			Y:        target.Spawn.Y,
			Z:        target.Spawn.Z,
		}, rt.Now().UnixMilli())
		if len(drops) != 2 || drops[1].Codename != tc.codename {
			t.Fatalf("country %d prepared %+v, want gold then %s", tc.country, drops, tc.codename)
		}
	}
}

/*
================
TestMonsterDropBootstrapCatalogResolvesAgainstShippedV1150Media
================
*/
func TestMonsterDropBootstrapCatalogResolvesAgainstShippedV1150Media(t *testing.T) {
	dir := licensed.RetailTextdataDir(t)
	if _, err := os.Stat(filepath.Join(dir, "itemdata_5000.txt")); err != nil {
		t.Skip("shipped v1.150 itemdata not present")
	}
	items := enterworld.NewTextdataItems(dir)
	codenames := loot.MonsterDropRefItemCodenames()
	if len(codenames) != 318 {
		t.Fatalf("monster drop preload roster = %d, want 318 CH+EU references that level-1/2 tables can emit", len(codenames))
	}
	var missing []string
	for _, codename := range codenames {
		if _, ok := items.ItemRefByCodename(codename); !ok {
			missing = append(missing, codename)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("v1.188 natural drop keys missing from v1.150 media: %v", missing)
	}
}

/*
================
TestDropAdmissionNegativeThresholdAndGradeSeven

4C1840: thirty levels above the monster the threshold is -20 and even
roll 0 admits nothing; a grade-7 monster quadruples a 60 threshold to the
cap of 100.
================
*/
func TestDropAdmissionNegativeThresholdAndGradeSeven(t *testing.T) {
	rt, _, _, target := newCombatTestRuntime(t, 1)
	rt.DropRoll = dropRollSequence(0)
	if rt.admitMonsterDrop(31, target) {
		t.Fatal("a negative threshold admitted roll 0")
	}
	target.Nest.HasRarityOverride, target.Nest.RarityOverride = true, 7
	rt.DropRoll = dropRollSequence(100)
	if !rt.admitMonsterDrop(11, target) {
		t.Fatal("a grade-7 monster's quadrupled threshold refused roll 100")
	}
}
