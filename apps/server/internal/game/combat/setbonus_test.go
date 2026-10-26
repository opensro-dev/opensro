/*
===========================================================================

setbonus_test.go - tests for setbonus.go

===========================================================================
*/

package combat

import (
	"math"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
)

// setCatalogueFake is the hand-built SetCatalogueSource these tests
// resolve tiers and options through.
/*
================
setCatalogueFake
================
*/
type setCatalogueFake struct {
	tiers   map[int64][]enterworld.SetTier
	options map[int][]enterworld.SetOption
}

/*
================
SetGroupTiers
================
*/
func (f setCatalogueFake) SetGroupTiers(setID int64) []enterworld.SetTier {
	return f.tiers[setID]
}

/*
================
MagicGroupOptions
================
*/
func (f setCatalogueFake) MagicGroupOptions(magicGroupID int) []enterworld.SetOption {
	return f.options[magicGroupID]
}

/*
================
setPieceRef

One wear-armour set piece at variance 0: physical defence 100 + 10 per
plus, magical defence 50 + 5 per plus, so the tier's opt-level value is
readable straight off the derived stats.
================
*/
func setPieceRef(refObjID uint32, codename string, setID int64) *enterworld.ItemRef {
	return &enterworld.ItemRef{
		RefObjID: refObjID,
		Codename: codename,
		SetID:    setID,
		TypeIDs:  [4]int64{3, 1, 1, 1},
		Combat: &enterworld.ItemCombatRef{
			PhysicalDefense: enterworld.ItemStatRange{Min: 100, Max: 100, PerPlus: 10},
			MagicalDefense:  enterworld.ItemStatRange{Min: 50, Max: 50, PerPlus: 5},
			EvasionRate:     enterworld.ItemStatRange{Min: 5, Max: 5},
		},
	}
}

/*
================
setCharacter

setCharacter equips count pieces of one set group in slots 0..count-1.
broken marks the last piece zero-durability; plus is every piece's own
plus.
================
*/
func setCharacter(refs []*enterworld.ItemRef, plus int64, broken bool) *domain.Character {
	character := &domain.Character{
		Level:     pointer(1),
		MaxLevel:  pointer(1),
		Strength:  pointer(20),
		Intellect: pointer(20),
	}
	for i, ref := range refs {
		durability := int64(76)
		if broken && i == len(refs)-1 {
			durability = 0
		}
		character.MissionInventory = append(character.MissionInventory, domain.InventoryRow{
			Slot:         int64(i),
			RefObjID:     ref.RefObjID,
			Codename:     ref.Codename,
			TypeFlags:    ref.TypeFlags(),
			VarianceBits: "0",
			Durability:   durability,
			Plus:         plus,
		})
	}
	return character
}

/*
================
setRefsFor

setRefsFor builds count distinct pieces of one group plus the item
source that resolves them. prefix keeps two groups' codenames apart.
================
*/
func setRefsFor(count int, setID int64, prefix string) ([]*enterworld.ItemRef, itemRefs) {
	letters := []string{"A", "B", "C", "D", "E", "F"}
	refs := make([]*enterworld.ItemRef, 0, count)
	source := itemRefs{}
	for i := 0; i < count; i++ {
		ref := setPieceRef(uint32(1000+i), prefix+letters[i], setID)
		refs = append(refs, ref)
		source[ref.Codename] = ref
	}
	return refs, source
}

/*
================
reinforceCatalogue

reinforceCatalogue is the traced armour shape: tiers at 2/4/6 pieces
carrying option 389 values 1/2/4 under magic groups 2/3/4.
================
*/
func reinforceCatalogue() setCatalogueFake {
	return setCatalogueFake{
		tiers: map[int64][]enterworld.SetTier{
			13: {
				{RequiredCount: 2, MagicGroupID: 2},
				{RequiredCount: 4, MagicGroupID: 3},
				{RequiredCount: 6, MagicGroupID: 4},
			},
		},
		options: map[int][]enterworld.SetOption{
			2: {{OptionID: enterworld.SetOptionReinforceItem, Value: 1}},
			3: {{OptionID: enterworld.SetOptionReinforceItem, Value: 2}},
			4: {{OptionID: enterworld.SetOptionReinforceItem, Value: 4}},
		},
	}
}

/*
================
requireNear
================
*/
func requireNear(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.01 {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

/*
================
TestSetBonusTierThresholds

Below two pieces nothing applies; at each threshold the tier's value
becomes every worn piece's extra opt level.
================
*/
func TestSetBonusTierThresholds(t *testing.T) {
	// Base physical defence at strength 20, as the base graph derives it.
	const baseDefense = 20 * 0.19
	cases := []struct {
		worn     int
		wantPlus float64
	}{
		{1, 0},
		{2, 1},
		{3, 1},
		{4, 2},
		{5, 2},
		{6, 4},
	}
	for _, tc := range cases {
		refs, source := setRefsFor(tc.worn, 13, "ITEM_SET_PIECE_")
		stats, _, err := PlayerStats(setCharacter(refs, 0, false),
			Catalogs{Items: source, Sets: reinforceCatalogue()})
		if err != nil {
			t.Fatalf("worn %d: PlayerStats: %v", tc.worn, err)
		}
		want := baseDefense + float64(tc.worn)*(100+10*tc.wantPlus)
		requireNear(t, "physical defense", stats.PhysicalDefense, want)
	}
}

/*
================
TestSetBonusHighestTierReplaces

Six worn pieces take the 6-piece tier's value 4 - not the sum of the
2/4/6 tiers (7) and not the lowest tier (1). The oracle is the same
character with a raw plus of 4 and no catalogue.
================
*/
func TestSetBonusHighestTierReplaces(t *testing.T) {
	refs, source := setRefsFor(6, 13, "ITEM_SET_PIECE_")
	boosted, _, err := PlayerStats(setCharacter(refs, 0, false),
		Catalogs{Items: source, Sets: reinforceCatalogue()})
	if err != nil {
		t.Fatalf("PlayerStats boosted: %v", err)
	}
	oracle, _, err := PlayerStats(setCharacter(refs, 4, false),
		Catalogs{Items: source})
	if err != nil {
		t.Fatalf("PlayerStats oracle: %v", err)
	}
	requireNear(t, "physical defense", boosted.PhysicalDefense, oracle.PhysicalDefense)
	requireNear(t, "magical defense", boosted.MagicalDefense, oracle.MagicalDefense)
}

/*
================
TestSetBonusBrokenPieceCounts

A zero-durability piece derives no stats of its own (native base loop)
but still counts toward its group's tier, so the healthy piece is
reinforced at two worn.
================
*/
func TestSetBonusBrokenPieceCounts(t *testing.T) {
	refs, source := setRefsFor(2, 13, "ITEM_SET_PIECE_")
	stats, _, err := PlayerStats(setCharacter(refs, 0, true),
		Catalogs{Items: source, Sets: reinforceCatalogue()})
	if err != nil {
		t.Fatalf("PlayerStats: %v", err)
	}
	// Base + the one healthy piece at 100 + 10*1; the broken piece adds
	// nothing. Without the count rule the tier stays off and this reads
	// 100 lower by the tier's 10.
	requireNear(t, "physical defense", stats.PhysicalDefense, 20*0.19+110)
}

/*
================
TestSetBonusResistAll

Option 390 at value 10: flat +10 on keeper params 0x1b..0x20 and +10
percent in the Sleep, Fear, Disease and Combustion buckets only.
================
*/
func TestSetBonusResistAll(t *testing.T) {
	catalogue := setCatalogueFake{
		tiers: map[int64][]enterworld.SetTier{
			13: {{RequiredCount: 2, MagicGroupID: 7}},
		},
		options: map[int][]enterworld.SetOption{
			7: {{OptionID: enterworld.SetOptionResistAll, Value: 10}},
		},
	}
	refs, source := setRefsFor(2, 13, "ITEM_SET_PIECE_")
	stats, _, err := PlayerStats(setCharacter(refs, 0, false),
		Catalogs{Items: source, Sets: catalogue})
	if err != nil {
		t.Fatalf("PlayerStats: %v", err)
	}
	for param := uint16(0x1b); param <= 0x20; param++ {
		value, ok := stats.Param(param)
		if !ok || value != 10 {
			t.Fatalf("param %#x = %v, %v; want flat 10", param, value, ok)
		}
	}
	raised := map[abnormal.Status]bool{
		abnormal.Sleep: true, abnormal.Fear: true,
		abnormal.Disease: true, abnormal.Combustion: true,
	}
	for _, sourceRow := range abnormal.Sources {
		if sourceRow.Resist < 0 {
			continue
		}
		bucket := stats.StatusResistance[sourceRow.Resist]
		if raised[sourceRow.Status] {
			if bucket.Percent != 10 {
				t.Fatalf("status %v percent = %d, want 10", sourceRow.Status, bucket.Percent)
			}
		} else if bucket.Percent != 0 {
			t.Fatalf("status %v percent = %d, want untouched 0", sourceRow.Status, bucket.Percent)
		}
		if bucket.Flat != 0 {
			t.Fatalf("status %v flat = %d, want untouched 0", sourceRow.Status, bucket.Flat)
		}
	}
}

/*
================
TestSetBonusGroupsAreIndependent

Two groups worn at once apply side by side: group 13's reinforce and
group 14's resist neither suppress nor merge into the other.
================
*/
func TestSetBonusGroupsAreIndependent(t *testing.T) {
	catalogue := reinforceCatalogue()
	catalogue.tiers[14] = []enterworld.SetTier{{RequiredCount: 2, MagicGroupID: 7}}
	catalogue.options[7] = []enterworld.SetOption{
		{OptionID: enterworld.SetOptionResistAll, Value: 10},
	}
	armour, source := setRefsFor(2, 13, "ITEM_SET_PIECE_")
	others, otherSource := setRefsFor(2, 14, "ITEM_SET_OTHER_")
	for codename, ref := range otherSource {
		source[codename] = ref
	}
	refs := append(armour, others...)
	stats, _, err := PlayerStats(setCharacter(refs, 0, false),
		Catalogs{Items: source, Sets: catalogue})
	if err != nil {
		t.Fatalf("PlayerStats: %v", err)
	}
	// Four pieces derive; only group 13's two are reinforced.
	requireNear(t, "physical defense", stats.PhysicalDefense, 20*0.19+2*110+2*100)
	if value, ok := stats.Param(0x1b); !ok || value != 10 {
		t.Fatalf("param 0x1b = %v, %v; want flat 10 from group 14", value, ok)
	}
}

/*
================
TestSetBonusOffIsTheBaseline

With the catalogue absent (the flag-off wiring) or with a group the
catalogue has no tiers for, the snapshot is the plain equipment
projection: same derived stats, no resistance params, empty buckets.
================
*/
func TestSetBonusOffIsTheBaseline(t *testing.T) {
	refs, source := setRefsFor(3, 99, "ITEM_SET_PIECE_")
	for _, sets := range []enterworld.SetCatalogueSource{nil, reinforceCatalogue()} {
		stats, _, err := PlayerStats(setCharacter(refs, 0, false),
			Catalogs{Items: source, Sets: sets})
		if err != nil {
			t.Fatalf("PlayerStats: %v", err)
		}
		requireNear(t, "physical defense", stats.PhysicalDefense, 20*0.19+3*100)
		if value, ok := stats.Param(0x1b); ok && value != 0 {
			t.Fatalf("param 0x1b = %v with sets %v, want zero", value, sets)
		}
		for i, bucket := range stats.StatusResistance {
			if bucket != (abnormal.Resistance{}) {
				t.Fatalf("resistance bucket %d = %+v, want zero", i, bucket)
			}
		}
	}
}
