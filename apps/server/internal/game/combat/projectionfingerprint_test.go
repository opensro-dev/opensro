/*
===========================================================================

projectionfingerprint_test.go - pins for the rekeyed equipment-band fingerprint (#1943, slice #2208)

The learned-projection cache (projectioncache.go) and the stats memo
(statscache.go) both key on the equipment band only, so a bag-only
change - a stack count, an added or removed bag row, churn on a summon
row - must keep the entry and return the identical full result, while
an equipment or skill-list change must miss. The sibling files pin the
field-by-field two-sided key (projectioncache_test.go) and the memo's
fresh-equality (statscache_test.go); this file pins what they do not:
row-count churn in the bag band, summon-row churn, the avatar flow that
lives outside the equipment fingerprint by design, and the set-effects
path under the same churn (the coupling hunt of #2208: the rank cache
keys on skills and source only, rankcache.go, and activeSetEffects
reads the equipment band only, setbonus.go - both stay clean here by
execution, not just by reading).

===========================================================================
*/
package combat

import (
	"reflect"
	"slices"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
)

/*
================
fingerprintBagRows

The bag band of the fingerprint fixtures: a stackable row, a summon row
carrying its companion, and a quest item row. Slots are the first three
past the equipment band.
================
*/
func fingerprintBagRows() []domain.InventoryRow {
	bag := int64(inventory.EquipmentSlotEnd)
	return []domain.InventoryRow{
		{Slot: bag, RefObjID: 9001, Codename: "ITEM_BAG_ARROWS", VarianceBits: "0", StackCount: 5},
		{Slot: bag + 1, RefObjID: 9002, Codename: "ITEM_COS_SCROLL_WOLF", VarianceBits: "0", StackCount: 1,
			Summon: &domain.CharacterCOS{GID: 4242, Codename: "COS_P_WOLF", CurrentHP: 500, CurrentMP: 100}},
		{Slot: bag + 2, RefObjID: 9003, Codename: "ITEM_QUEST_SCROLL", VarianceBits: "0", StackCount: 1},
	}
}

/*
================
TestLearnedProjectionsBagChurnKeepsEntry

#2208: bag-only churn never invalidates the learned-projection entry.
Each step must return the seeded result value for value and from the
same backing store (no recompute); the equipment break at the end is
the seeing-eye leg proving the harness notices a keyed field at all.
================
*/
func TestLearnedProjectionsBagChurnKeepsEntry(t *testing.T) {
	skills, items, sword := projectionFixture()
	c := &domain.Character{ID: 980030, Level: pointer(80), Strength: pointer(300), Intellect: pointer(200),
		Skills: []uint32{101, 102, 201, 202, 301, 302},
		MissionInventory: append([]domain.InventoryRow{
			{Slot: 6, RefObjID: sword.RefObjID, Codename: sword.Codename, TypeFlags: sword.TypeFlags(), VarianceBits: "0", Durability: 90},
		}, fingerprintBagRows()...)}
	defer ForgetLearnedProjections(c.ID)

	seed := assertProjectionFresh(t, "seeded", c, skills, items, 2)
	seeded := learnedProjection{writes: slices.Clone(seed.writes), power: seed.power,
		resistance: seed.resistance, damageReturn: seed.damageReturn, damageReturnSkill: seed.damageReturnSkill}
	keeps := func(step string, mutate func()) {
		t.Helper()
		mutate()
		got := assertProjectionFresh(t, step, c, skills, items, 2)
		if len(got.writes) == 0 || &got.writes[0] != &seed.writes[0] {
			t.Fatalf("%s: the bag churn recomputed the projections", step)
		}
		if !reflect.DeepEqual(got.writes, seeded.writes) || got.power != seeded.power ||
			got.resistance != seeded.resistance || got.damageReturn != seeded.damageReturn ||
			got.damageReturnSkill != seeded.damageReturnSkill {
			t.Fatalf("%s: cached %+v, seeded %+v", step, got, seeded)
		}
	}

	keeps("bag fields churn", func() {
		c.MissionInventory[1].StackCount = 99
		c.MissionInventory[1].Durability = 3
		c.MissionInventory[1].Plus = 4
		c.MissionInventory[3].Codename = "ITEM_QUEST_OTHER"
	})
	keeps("bag row added", func() {
		c.MissionInventory = append(c.MissionInventory,
			domain.InventoryRow{Slot: int64(inventory.EquipmentSlotEnd) + 7, RefObjID: 9004, Codename: "ITEM_BAG_NEW", VarianceBits: "0", StackCount: 2})
	})
	keeps("bag row removed", func() {
		c.MissionInventory = slices.Delete(c.MissionInventory, len(c.MissionInventory)-1, len(c.MissionInventory))
		c.MissionInventory = slices.Delete(c.MissionInventory, 3, 4)
	})
	keeps("summon row churn", func() {
		c.MissionInventory[2].Summon.CurrentHP = 1
		c.MissionInventory[2].Summon = &domain.CharacterCOS{GID: 7777, Codename: "COS_P_WOLF", CurrentHP: 900, CurrentMP: 40}
		c.MissionInventory[2].Codename = "ITEM_COS_SCROLL_WOLF_B"
	})
	summonRow := c.MissionInventory[2]
	keeps("summon row removed", func() {
		c.MissionInventory = slices.Delete(c.MissionInventory, 2, 3)
	})
	keeps("summon row restored at the tail", func() {
		c.MissionInventory = append(c.MissionInventory, summonRow)
	})

	// Seeing-eye: a keyed equipment field still misses, value and store.
	c.MissionInventory[0].Durability = 0
	broken := assertProjectionFresh(t, "gating weapon broken", c, skills, items, 2)
	if &broken.writes[0] == &seed.writes[0] {
		t.Fatal("a broken gating weapon kept the cached projections")
	}
	if broken.resistance != ([17]abnormal.Resistance{}) {
		t.Fatalf("a broken sword still gates the resistance in: %+v", broken.resistance)
	}
}

/*
================
fingerprintValues

One stats call's full published answer: every scalar the rebuild
publishes, the loadout, the learned dictionary and buckets, and a
keeper read across the parameter closure. Compared whole, so a pin
sees any field that moved, not a hit/miss bit.
================
*/
type fingerprintValues struct {
	level, maxLevel   uint8
	strength          float64
	intellect         float64
	physicalDefense   float64
	magicalDefense    float64
	parryRate         float64
	magicalParry      float64
	evasionRate       float64
	blockRate         float64
	hitRate           float64
	criticalRate      float64
	physicalAttackMin float64
	physicalAttackMax float64
	magicalAttackMin  float64
	magicalAttackMax  float64
	rates             [8]float64
	berserk           bool
	stealthStrike     bool
	motionState       uint8
	abnormalMask      uint32
	skillParameters   enterworld.SkillParameterValues
	statusResistance  [17]abnormal.Resistance
	damageReturn      enterworld.SkillDamageReturn
	damageReturnSkill uint32
	masteries         []domain.CharacterMastery
	loadout           Loadout
	params            [15]float32
	paramsOK          [15]bool
}

/*
================
captureFingerprintValues
================
*/
func captureFingerprintValues(t *testing.T, c *domain.Character, catalogs Catalogs) fingerprintValues {
	t.Helper()
	stats, loadout, err := PlayerStatsWithModifiers(c, catalogs, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	values := fingerprintValues{
		level: stats.Level, maxLevel: stats.MaxLevel,
		strength: stats.Strength, intellect: stats.Intellect,
		physicalDefense: stats.PhysicalDefense, magicalDefense: stats.MagicalDefense,
		parryRate: stats.ParryRate, magicalParry: stats.MagicalParry,
		evasionRate: stats.EvasionRate, blockRate: stats.BlockRate,
		hitRate: stats.HitRate, criticalRate: stats.CriticalRate,
		physicalAttackMin: stats.PhysicalAttackMin, physicalAttackMax: stats.PhysicalAttackMax,
		magicalAttackMin: stats.MagicalAttackMin, magicalAttackMax: stats.MagicalAttackMax,
		rates: [8]float64{stats.PhysicalBasicRate, stats.PhysicalSkillRate, stats.MagicalBasicRate, stats.MagicalSkillRate,
			float64(stats.PhysicalOutgoing), float64(stats.MagicalOutgoing), float64(stats.PhysicalIncoming), float64(stats.MagicalIncoming)},
		berserk: stats.Berserk, stealthStrike: stats.StealthStrike,
		motionState: stats.MotionState, abnormalMask: stats.AbnormalMask,
		skillParameters: stats.SkillParameters, statusResistance: stats.StatusResistance,
		damageReturn: stats.DamageReturn, damageReturnSkill: stats.DamageReturnSkill,
		masteries: slices.Clone(stats.masteries), loadout: loadout,
	}
	for i, id := range []uint16{1, 2, 3, 4, 5, 6, 7, 9, 11, 13, 14, 15, 16, 0x21, 0x8c} {
		values.params[i], values.paramsOK[i] = stats.Param(id)
	}
	return values
}

/*
================
TestPlayerStatsBagChurnKeepsFullResult

#2208 at the funnel: cold and warm, every bag-only churn returns the
baseline's full result values, and every step still equals the
unmemoised rebuild. Breaking the gating sword is the seeing-eye leg.
================
*/
func TestPlayerStatsBagChurnKeepsFullResult(t *testing.T) {
	skills, items, sword := projectionFixture()
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
	items[armor.Codename] = armor
	c := &domain.Character{ID: 980040, Level: pointer(80), MaxLevel: pointer(80), Strength: pointer(300), Intellect: pointer(200),
		Skills: []uint32{101, 102, 201, 202, 301, 302},
		MissionInventory: append([]domain.InventoryRow{
			{Slot: 1, RefObjID: armor.RefObjID, Codename: armor.Codename, TypeFlags: armor.TypeFlags(), VarianceBits: "0", Durability: 80, Plus: 5, StackCount: 1},
			{Slot: 6, RefObjID: sword.RefObjID, Codename: sword.Codename, TypeFlags: sword.TypeFlags(), VarianceBits: "0", Durability: 90},
		}, fingerprintBagRows()...)}
	catalogs := Catalogs{Items: items, Skills: skills, MagicOptions: optionRows()}
	defer ForgetPlayerStatsMemo(c.ID)
	defer ForgetLearnedProjections(c.ID)

	ForgetPlayerStatsMemo(c.ID)
	ForgetLearnedProjections(c.ID)
	baseline := captureFingerprintValues(t, c, catalogs)
	if warm := captureFingerprintValues(t, c, catalogs); !reflect.DeepEqual(warm, baseline) {
		t.Fatalf("warm repeat moved the result:\n%+v\n%+v", warm, baseline)
	}
	keeps := func(step string, mutate func()) {
		t.Helper()
		mutate()
		if got := captureFingerprintValues(t, c, catalogs); !reflect.DeepEqual(got, baseline) {
			t.Fatalf("%s: bag churn moved the full result:\n%+v\nbaseline:\n%+v", step, got, baseline)
		}
		assertStatsFresh(t, step, c, catalogs, nil, nil)
	}

	keeps("stack count", func() { c.MissionInventory[2].StackCount = 99 })
	keeps("summon HP", func() { c.MissionInventory[3].Summon.CurrentHP = 1 })
	keeps("summon replaced", func() {
		c.MissionInventory[3].Summon = &domain.CharacterCOS{GID: 7777, Codename: "COS_P_WOLF", CurrentHP: 900, CurrentMP: 40}
	})
	keeps("bag row added", func() {
		c.MissionInventory = append(c.MissionInventory,
			domain.InventoryRow{Slot: int64(inventory.EquipmentSlotEnd) + 7, RefObjID: 9004, Codename: "ITEM_BAG_NEW", VarianceBits: "0", StackCount: 2})
	})
	keeps("bag rows removed", func() {
		c.MissionInventory = slices.Delete(c.MissionInventory, len(c.MissionInventory)-1, len(c.MissionInventory))
		c.MissionInventory = slices.Delete(c.MissionInventory, 4, 5)
	})

	// Seeing-eye: breaking the gating sword empties the learned
	// resistance and drops the weapon, so the full result must move;
	// repairing it returns the baseline exactly.
	c.MissionInventory[1].Durability = 0
	if broken := captureFingerprintValues(t, c, catalogs); reflect.DeepEqual(broken, baseline) {
		t.Fatal("a broken gating sword left the full result unchanged")
	}
	assertStatsFresh(t, "gating sword broken", c, catalogs, nil, nil)
	c.MissionInventory[1].Durability = 90
	if repaired := captureFingerprintValues(t, c, catalogs); !reflect.DeepEqual(repaired, baseline) {
		t.Fatalf("repairing the sword did not return the baseline:\n%+v\n%+v", repaired, baseline)
	}
}

/*
================
TestAvatarOptionChangeMovesStatsOutsideProjectionKey

#2208: avatarOptionWrites is outside the equipment fingerprint by
design. Changing a worn avatar's option moves the resulting numbers
(the stats memo rekeys on the avatar band and rebuilds) while the
learned-projection entry - which never reads an avatar row - is
untouched.
================
*/
func TestAvatarOptionChangeMovesStatsOutsideProjectionKey(t *testing.T) {
	c, catalogs := avatarFixture(encodedOption(200, 3))
	skills, projItems, sword := projectionFixture()
	merged := itemRefs{}
	for name, ref := range catalogs.Items.(itemRefs) {
		merged[name] = ref
	}
	for name, ref := range projItems {
		merged[name] = ref
	}
	catalogs.Items = merged
	catalogs.Skills = skills
	c.ID = 980050
	c.Skills = []uint32{101, 102, 201, 202, 301, 302}
	c.MissionInventory = []domain.InventoryRow{
		{Slot: 6, RefObjID: sword.RefObjID, Codename: sword.Codename, TypeFlags: sword.TypeFlags(), VarianceBits: "0", Durability: 90},
	}
	defer ForgetPlayerStatsMemo(c.ID)
	defer ForgetLearnedProjections(c.ID)

	baseline := captureFingerprintValues(t, c, catalogs)
	if baseline.strength != 43 {
		t.Fatalf("baseline strength %v, want 40 + the dress's 3", baseline.strength)
	}
	entry := assertProjectionFresh(t, "seeded", c, skills, merged, 2)
	if len(entry.writes) == 0 {
		t.Fatal("the fixture learned no projections; the entry witness is empty")
	}

	c.AvatarInventory.Rows[0].MagicOptions = []uint64{encodedOption(200, 9)}
	moved := captureFingerprintValues(t, c, catalogs)
	if moved.strength != 49 {
		t.Fatalf("strength after the option change %v, want 40 + 9", moved.strength)
	}
	if reflect.DeepEqual(moved, baseline) {
		t.Fatal("the avatar option change left the full result unchanged")
	}
	assertStatsFresh(t, "avatar option raised", c, catalogs, nil, nil)
	still := assertProjectionFresh(t, "projection after avatar change", c, skills, merged, 2)
	if &still.writes[0] != &entry.writes[0] {
		t.Fatal("an avatar option change recomputed the learned projections")
	}

	c.AvatarInventory = nil
	bare := captureFingerprintValues(t, c, catalogs)
	if bare.strength != 40 {
		t.Fatalf("strength without the dress %v, want the bare 40", bare.strength)
	}
	assertStatsFresh(t, "dress removed", c, catalogs, nil, nil)
}

/*
================
TestSetBonusBagChurnKeepsFullResult

#2208 coupling hunt, set effects: with the set catalogue live and a
tier active, bag churn still returns the baseline's full result -
activeSetEffects counts the equipment band only. Dropping a worn piece
below the tier is the seeing-eye leg.
================
*/
func TestSetBonusBagChurnKeepsFullResult(t *testing.T) {
	refs, source := setRefsFor(4, 13, "ITEM_SET_")
	c := setCharacter(refs, 0, false)
	c.ID = 980060
	c.MissionInventory = append(c.MissionInventory, fingerprintBagRows()...)
	catalogs := Catalogs{Items: source, Skills: passiveSkills{}, MagicOptions: optionRows(), Sets: reinforceCatalogue()}
	defer ForgetPlayerStatsMemo(c.ID)
	defer ForgetLearnedProjections(c.ID)

	baseline := captureFingerprintValues(t, c, catalogs)
	if baseline.physicalDefense <= 100 {
		t.Fatalf("physical defense %v: the 4-piece tier's opt level never landed", baseline.physicalDefense)
	}
	keeps := func(step string, mutate func()) {
		t.Helper()
		mutate()
		if got := captureFingerprintValues(t, c, catalogs); !reflect.DeepEqual(got, baseline) {
			t.Fatalf("%s: bag churn moved the set result:\n%+v\nbaseline:\n%+v", step, got, baseline)
		}
		assertStatsFresh(t, step, c, catalogs, nil, nil)
	}
	keeps("stack count", func() { c.MissionInventory[4].StackCount = 42 })
	keeps("bag row added", func() {
		c.MissionInventory = append(c.MissionInventory,
			domain.InventoryRow{Slot: int64(inventory.EquipmentSlotEnd) + 7, RefObjID: 9004, Codename: "ITEM_BAG_NEW", VarianceBits: "0", StackCount: 2})
	})
	keeps("bag rows removed", func() {
		c.MissionInventory = slices.Delete(c.MissionInventory, len(c.MissionInventory)-1, len(c.MissionInventory))
		c.MissionInventory = slices.Delete(c.MissionInventory, 4, 7)
	})

	// Seeing-eye: losing the fourth worn piece drops the tier from 4
	// pieces (opt level 2) to 2 pieces (opt level 1), so the derived
	// defense must move.
	c.MissionInventory = slices.Delete(c.MissionInventory, 3, 4)
	if dropped := captureFingerprintValues(t, c, catalogs); reflect.DeepEqual(dropped, baseline) {
		t.Fatal("dropping below the set tier left the full result unchanged")
	}
	assertStatsFresh(t, "set piece dropped", c, catalogs, nil, nil)
}
