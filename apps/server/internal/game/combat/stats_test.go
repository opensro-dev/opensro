/*
===========================================================================

stats_test.go - tests for stats.go

===========================================================================
*/

package combat

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

type itemRefs map[string]*enterworld.ItemRef

func (refs itemRefs) ItemRefByCodename(codename string) (*enterworld.ItemRef, bool) {
	ref, ok := refs[codename]
	return ref, ok
}

func pointer(value int64) *int64 { return &value }

func TestPlayerStatsBuildsBaseGraphAndWeaponVariance(t *testing.T) {
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
	character := &domain.Character{
		Level:     pointer(1),
		MaxLevel:  pointer(1),
		Strength:  pointer(20),
		Intellect: pointer(20),
		MissionInventory: []domain.InventoryRow{{
			Slot:         6,
			RefObjID:     sword.RefObjID,
			Codename:     sword.Codename,
			TypeFlags:    sword.TypeFlags(),
			VarianceBits: "0",
			Durability:   76,
			StackCount:   1,
		}},
	}

	stats, loadout, err := PlayerStats(character, Catalogs{Items: itemRefs{sword.Codename: sword}})
	if err != nil {
		t.Fatalf("PlayerStats: %v", err)
	}
	if !loadout.HasWeapon || loadout.WeaponKind != 2 || loadout.ActionRange != 6 {
		t.Fatalf("loadout = %+v, want TID4 2 at range 6", loadout)
	}
	// The server's stat factor constants are float32. Each dependent and final
	// parameter stores float32, including the equipment addition.
	if stats.Level != 1 || stats.MaxLevel != 1 ||
		stats.PhysicalDefense != float64(float32(20*0.19)) ||
		stats.MagicalDefense != float64(float32(20*0.305)) ||
		stats.EvasionRate != 11 || stats.HitRate != 35 ||
		stats.CriticalRate != 3 ||
		stats.PhysicalAttackMin != float64(float32(float64(float32(20*float64(float32(30.5999985))/100))+15)) ||
		stats.PhysicalAttackMax != float64(float32(float64(float32(20*float64(float32(34.2000008))/100))+16)) ||
		stats.MagicalAttackMin != float64(float32(float64(float32(20*0.49))+25)) ||
		stats.MagicalAttackMax != float64(float32(float64(float32(20*float64(float32(54.7999992))/100))+28)) {
		t.Fatalf("derived player stats = %+v", stats)
	}
}

func TestPlayerBaseStatsProjectsCharacterPaneFields(t *testing.T) {
	sword := &enterworld.ItemRef{
		RefObjID: 71,
		Codename: "ITEM_CH_SWORD_01_A",
		TypeIDs:  [4]int64{3, 1, 6, 2},
		Combat: &enterworld.ItemCombatRef{
			PhysicalAttack: enterworld.ItemAttackRange{
				Minimum: enterworld.ItemStatRange{Min: 15, Max: 16},
				Maximum: enterworld.ItemStatRange{Min: 16, Max: 18},
			},
			MagicalAttack: enterworld.ItemAttackRange{
				Minimum: enterworld.ItemStatRange{Min: 25, Max: 26},
				Maximum: enterworld.ItemStatRange{Min: 28, Max: 31},
			},
			HitRate: enterworld.ItemStatRange{Min: 24, Max: 30},
		},
	}
	character := &domain.Character{
		Level: pointer(1), MaxLevel: pointer(1),
		Strength: pointer(20), Intellect: pointer(20),
		MissionInventory: []domain.InventoryRow{{
			Slot: 6, RefObjID: sword.RefObjID, Codename: sword.Codename,
			TypeFlags: sword.TypeFlags(), VarianceBits: "0", Durability: 76,
		}},
	}

	got, err := PlayerBaseStats(character, Catalogs{Items: itemRefs{sword.Codename: sword}})
	if err != nil {
		t.Fatalf("PlayerBaseStats: %v", err)
	}
	if got.PhysicalAttackMin != 21 || got.PhysicalAttackMax != 22 ||
		got.MagicalAttackMin != 34 || got.MagicalAttackMax != 38 ||
		got.PhysicalDefense != 3 || got.MagicalDefense != 6 ||
		got.HitRate != 35 || got.ParryRate != 11 {
		t.Fatalf("character-pane projection = %+v", got)
	}
}

func TestPlayerStatsUsesLevelWhenOptionalMaxLevelIsAbsent(t *testing.T) {
	level := int64(17)
	character := &domain.Character{
		Level:     &level,
		Strength:  pointer(36),
		Intellect: pointer(34),
	}

	stats, loadout, err := PlayerStats(character, Catalogs{Items: itemRefs{}})
	if err != nil {
		t.Fatalf("PlayerStats with omitted maxLevel: %v", err)
	}
	if stats.Level != 17 || stats.MaxLevel != 17 {
		t.Fatalf("level pair = %d/%d, want entered-wire fallback 17/17", stats.Level, stats.MaxLevel)
	}
	if loadout.HasWeapon {
		t.Fatalf("empty equipment produced a weapon loadout: %+v", loadout)
	}
}

func TestPlayerStatsFailsClosedOnUnpinnedEquipmentPlanes(t *testing.T) {
	base := &domain.Character{
		Level:     pointer(1),
		MaxLevel:  pointer(1),
		Strength:  pointer(20),
		Intellect: pointer(20),
	}
	ref := &enterworld.ItemRef{
		RefObjID: 1,
		Codename: "ITEM_TEST",
		TypeIDs:  [4]int64{3, 1, 6, 2},
		Combat:   &enterworld.ItemCombatRef{},
	}

	withOption := base.Snapshot()
	withOption.MissionInventory = []domain.InventoryRow{{
		Slot: 6, RefObjID: 1, Codename: ref.Codename,
		VarianceBits: "0", Durability: 1, MagicOptions: []uint64{1},
	}}
	if _, _, err := PlayerStats(withOption, Catalogs{Items: itemRefs{ref.Codename: ref}}); err == nil {
		t.Fatal("equipped magic-option stat plane was silently ignored")
	}

	incomplete := base.Snapshot()
	incomplete.MissionInventory = []domain.InventoryRow{{
		Slot: 6, RefObjID: 1, Codename: ref.Codename,
		VarianceBits: "0", Durability: 1,
	}}
	ref.Combat = nil
	if _, _, err := PlayerStats(incomplete, Catalogs{Items: itemRefs{ref.Codename: ref}}); err == nil {
		t.Fatal("incomplete item combat row was silently treated as zero")
	}
}

/*
================
BenchmarkPlayerStatsWithModifiers
================
*/
func BenchmarkPlayerStatsWithModifiers(b *testing.B) {
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
	character := &domain.Character{
		Level:     pointer(60),
		MaxLevel:  pointer(60),
		Strength:  pointer(140),
		Intellect: pointer(140),
		MissionInventory: []domain.InventoryRow{
			{
				Slot:         1,
				RefObjID:     armor.RefObjID,
				Codename:     armor.Codename,
				TypeFlags:    armor.TypeFlags(),
				VarianceBits: "0",
				Durability:   80,
				Plus:         5,
				StackCount:   1,
			},
			{
				Slot:         6,
				RefObjID:     sword.RefObjID,
				Codename:     sword.Codename,
				TypeFlags:    sword.TypeFlags(),
				VarianceBits: "0",
				Durability:   76,
				Plus:         7,
				StackCount:   1,
			},
		},
	}
	catalogs := Catalogs{Items: itemRefs{
		sword.Codename: sword,
		armor.Codename: armor,
	}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := PlayerStatsWithModifiers(character, catalogs, nil, nil); err != nil {
			b.Fatal(err)
		}
	}
}
