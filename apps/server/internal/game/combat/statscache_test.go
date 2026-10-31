/*
===========================================================================

statscache_test.go - tests for statscache.go

The memo must be invisible: every answer equals the unmemoised rebuild
over the same inputs, whatever writer moved the character, the effect
writes or the abnormal block. Each content move below is checked
against playerStatsWithModifiers itself, so a key field the memo
forgot fails here.

===========================================================================
*/
package combat

import (
	"slices"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
)

/*
================
statsMemoFixture

A level-80 character with an ID, a sword and one armor piece, and the
catalogues that resolve them.
================
*/
func statsMemoFixture() (*domain.Character, Catalogs) {
	sword := &enterworld.ItemRef{
		RefObjID: 71,
		Codename: "ITEM_CH_SWORD_01_A",
		TypeIDs:  [4]int64{3, 1, 6, 2},
		Combat: &enterworld.ItemCombatRef{
			ActionRange: 6,
			PhysicalAttack: enterworld.ItemAttackRange{
				Minimum: enterworld.ItemStatRange{Min: 15, Max: 16, PerPlus: 2.4000001},
				Maximum: enterworld.ItemStatRange{Min: 16, Max: 18, PerPlus: 2.4000001},
			},
			MagicalAttack: enterworld.ItemAttackRange{
				Minimum: enterworld.ItemStatRange{Min: 25, Max: 26, PerPlus: 4.0999999},
				Maximum: enterworld.ItemStatRange{Min: 28, Max: 31, PerPlus: 4.0999999},
			},
			HitRate:      enterworld.ItemStatRange{Min: 24, Max: 30},
			CriticalRate: enterworld.ItemStatRange{Min: 3, Max: 15},
		},
	}
	armor := &enterworld.ItemRef{
		RefObjID: 101,
		Codename: "ITEM_CH_M_HEAVY_01_BA_A",
		TypeIDs:  [4]int64{3, 1, 1, 1},
		Combat: &enterworld.ItemCombatRef{
			PhysicalDefense: enterworld.ItemStatRange{Min: 12, Max: 15, PerPlus: 1.5},
			MagicalDefense:  enterworld.ItemStatRange{Min: 8, Max: 10, PerPlus: 1.2},
			EvasionRate:     enterworld.ItemStatRange{Min: 5, Max: 8},
		},
	}
	c := &domain.Character{
		ID:        990001,
		Level:     pointer(80),
		MaxLevel:  pointer(80),
		Strength:  pointer(300),
		Intellect: pointer(200),
		MissionInventory: []domain.InventoryRow{
			{Slot: 1, RefObjID: armor.RefObjID, Codename: armor.Codename, TypeFlags: armor.TypeFlags(), VarianceBits: "0", Durability: 80, Plus: 5, StackCount: 1},
			{Slot: 6, RefObjID: sword.RefObjID, Codename: sword.Codename, TypeFlags: sword.TypeFlags(), VarianceBits: "0", Durability: 76, Plus: 7, StackCount: 1},
		},
	}
	catalogs := Catalogs{
		Items:        itemRefs{sword.Codename: sword, armor.Codename: armor},
		Skills:       passiveSkills{},
		MagicOptions: optionRows(),
	}
	return c, catalogs
}

/*
================
assertStatsFresh

One memoised call against the unmemoised rebuild over the same inputs:
every published field, the learned dictionary and a keeper read across
the parameter closure must agree.
================
*/
func assertStatsFresh(
	t *testing.T,
	step string,
	c *domain.Character,
	catalogs Catalogs,
	modifiers []paramkeeper.Write,
	block *abnormal.Block,
) Stats {
	t.Helper()
	got, loadout, err := PlayerStatsWithModifiers(c, catalogs, modifiers, block)
	if err != nil {
		t.Fatalf("%s: memoised %v", step, err)
	}
	want, wantLoadout, err := playerStatsWithModifiers(c, catalogs, modifiers, block)
	if err != nil {
		t.Fatalf("%s: fresh %v", step, err)
	}
	if loadout != wantLoadout {
		t.Fatalf("%s: loadout %+v, fresh %+v", step, loadout, wantLoadout)
	}
	if got.Level != want.Level || got.Strength != want.Strength || got.Intellect != want.Intellect ||
		got.PhysicalDefense != want.PhysicalDefense || got.MagicalDefense != want.MagicalDefense ||
		got.PhysicalAttackMin != want.PhysicalAttackMin || got.PhysicalAttackMax != want.PhysicalAttackMax ||
		got.MagicalAttackMin != want.MagicalAttackMin || got.MagicalAttackMax != want.MagicalAttackMax ||
		got.Berserk != want.Berserk || got.StealthStrike != want.StealthStrike ||
		got.SkillParameters != want.SkillParameters || got.StatusResistance != want.StatusResistance ||
		got.DamageReturn != want.DamageReturn || !slices.Equal(got.masteries, want.masteries) {
		t.Fatalf("%s: memoised %+v, fresh %+v", step, got, want)
	}
	for _, id := range []uint16{1, 2, 3, 4, 5, 6, 7, 9, 11, 13, 14, 15, 16, 0x21, 0x8c} {
		g, gok := got.Param(id)
		w, wok := want.Param(id)
		if gok != wok || g != w {
			t.Fatalf("%s: param %#x memoised %v (%v), fresh %v (%v)", step, id, g, gok, w, wok)
		}
	}
	return got
}

/*
================
TestPlayerStatsMemoFollowsEveryContentWriter

Each way the inputs move - gear plus, a broken piece, a mastery, a
learned skill behind another source, an effect write, an abnormal
block, an avatar option, the body status - the memoised answer equals
a fresh rebuild.
================
*/
func TestPlayerStatsMemoFollowsEveryContentWriter(t *testing.T) {
	c, catalogs := statsMemoFixture()
	defer ForgetPlayerStatsMemo(c.ID)

	assertStatsFresh(t, "seeded", c, catalogs, nil, nil)
	assertStatsFresh(t, "identical repeat", c, catalogs, nil, nil)

	c.MissionInventory[1].Plus = 12
	assertStatsFresh(t, "sword plus", c, catalogs, nil, nil)

	c.MissionInventory[1].Durability = 0
	assertStatsFresh(t, "sword broken", c, catalogs, nil, nil)
	c.MissionInventory[1].Durability = 76

	c.Masteries = []domain.CharacterMastery{{ID: 257, Level: 3}}
	assertStatsFresh(t, "mastery learned", c, catalogs, nil, nil)

	skills, _, _ := projectionFixture()
	withSkills := catalogs
	withSkills.Skills = skills
	c.Skills = []uint32{101, 102}
	assertStatsFresh(t, "passives learned", c, withSkills, nil, nil)

	writes := []paramkeeper.Write{{Parameter: 5, Channel: paramkeeper.Flat, Source: 0x80000001, Value: 25}}
	assertStatsFresh(t, "effect write", c, withSkills, writes, nil)

	block := &abnormal.Block{}
	block.Modifiers[0] = abnormal.Modifier{Used: true, Param: 0x8c, Channel: 3, Source: 5, Value: 125}
	assertStatsFresh(t, "abnormal block", c, withSkills, writes, block)

	dress := &enterworld.ItemRef{RefObjID: 7, Codename: "TEST_AVATAR_DRESS", TypeIDs: [4]int64{3, 1, 13, 2}}
	items := itemRefs{}
	for name, ref := range catalogs.Items.(itemRefs) {
		items[name] = ref
	}
	items[dress.Codename] = dress
	rows := optionRows()
	for i, tag := range avatarOptionTags {
		id := uint32(200 + i)
		rows[id] = &enterworld.MagicOptionRow{ParamID: id, OptionName: "MATTR_AVATAR_" + tag, Tag: optionTag(tag)}
	}
	withDress := withSkills
	withDress.Items = items
	withDress.MagicOptions = rows
	c.AvatarInventory = &domain.AvatarInventory{Capacity: 4, Rows: []domain.InventoryRow{
		{Slot: 1, RefObjID: dress.RefObjID, Codename: dress.Codename, MagicOptions: []uint64{encodedOption(200, 7)}},
	}}
	assertStatsFresh(t, "avatar option", c, withDress, writes, block)

	c.NativeBodyStatus = 1
	assertStatsFresh(t, "berserk body", c, withDress, writes, block)
}

/*
================
TestPlayerStatsMemoSkipsCharactersWithoutID

Two characters without an ID never share an answer, however their
content differs: the memo keys on the ID first.
================
*/
func TestPlayerStatsMemoSkipsCharactersWithoutID(t *testing.T) {
	c, catalogs := statsMemoFixture()
	c.ID = 0
	assertStatsFresh(t, "first without ID", c, catalogs, nil, nil)
	_, first, err := PlayerStatsWithModifiers(c, catalogs, nil, nil)
	if err != nil || !first.HasWeapon {
		t.Fatalf("first without ID: %v / %+v", err, first)
	}
	c.MissionInventory = c.MissionInventory[:1]
	assertStatsFresh(t, "second without ID", c, catalogs, nil, nil)
	_, second, err := PlayerStatsWithModifiers(c, catalogs, nil, nil)
	if err != nil || second.HasWeapon {
		t.Fatalf("second without ID inherited another answer: %v / %+v", err, second)
	}
}

/*
================
BenchmarkPlayerStatsWithModifiersMemoHit

The tick pattern the memo exists for: the same snapshot, writes and
block rebuilt per hit and per effect refresh. After the first call
every iteration is a memo hit.
================
*/
func BenchmarkPlayerStatsWithModifiersMemoHit(b *testing.B) {
	c, catalogs := statsMemoFixture()
	defer ForgetPlayerStatsMemo(c.ID)
	modifiers := []paramkeeper.Write{
		{Parameter: 1, Channel: paramkeeper.Flat, Source: 0x80000001, Value: 25},
		{Parameter: 5, Channel: paramkeeper.PercentSum, Source: 0x80000002, Value: 10},
	}
	block := &abnormal.Block{}
	block.Modifiers[0] = abnormal.Modifier{Used: true, Param: 0x8c, Channel: 3, Source: 5, Value: 125}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stats, _, err := PlayerStatsWithModifiers(c, catalogs, modifiers, block)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkStatsSink = stats
	}
}

/*
================
BenchmarkPlayerStatsWithModifiersMemoMiss

The miss path: one write's value rotates every iteration, so the memo
can never hit and each call pays the rebuild plus the memo insert.
Guards the memo against taxing the calls it cannot serve.
================
*/
func BenchmarkPlayerStatsWithModifiersMemoMiss(b *testing.B) {
	c, catalogs := statsMemoFixture()
	defer ForgetPlayerStatsMemo(c.ID)
	modifiers := []paramkeeper.Write{
		{Parameter: 1, Channel: paramkeeper.Flat, Source: 0x80000001, Value: 25},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		modifiers[0].Value = float32(i)
		stats, _, err := PlayerStatsWithModifiers(c, catalogs, modifiers, nil)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkStatsSink = stats
	}
}
