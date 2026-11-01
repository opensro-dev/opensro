/*
===========================================================================

magicoptions_test.go - tests for magicoptions.go

===========================================================================
*/

package combat

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/domain/charactervitals"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
)

type magicRows map[uint32]*enterworld.MagicOptionRow

func (m magicRows) MagicOptionByParamID(id uint32) (*enterworld.MagicOptionRow, bool) {
	row, ok := m[id]
	return row, ok
}

func optionRows() magicRows {
	return magicRows{
		11:  {ParamID: 11, OptionName: "MATTR_STR", Tag: optionTag("str")},
		17:  {ParamID: 17, OptionName: "MATTR_RESIST_FROSTBITE", Tag: optionTag("fz")},
		23:  {ParamID: 23, OptionName: "MATTR_RESIST_ESHOCK", Tag: optionTag("es")},
		29:  {ParamID: 29, OptionName: "MATTR_RESIST_BURN", Tag: optionTag("bu")},
		92:  {ParamID: 92, OptionName: "MATTR_HR", Tag: optionTag("hr")},
		140: {ParamID: 140, OptionName: "MATTR_HP", Tag: optionTag("hp")},
		999: {ParamID: 999, OptionName: "MATTR_UNKNOWN", Tag: optionTag("zzzz")},
	}
}

func encodedOption(id, value uint32) uint64 { return uint64(value)<<32 | uint64(id) }

// The server's option pass: options write the keeper keyed by the item; frostbite
// resistance covers freezing too, burn/shock land on 1E/1D.
func TestMagicOptionsWriteTheKeeper(t *testing.T) {
	ref := &enterworld.ItemRef{RefObjID: 1, Codename: "TEST_ARMOR", TypeIDs: [4]int64{3, 1, 1, 1}, Combat: &enterworld.ItemCombatRef{}}
	row := domain.InventoryRow{Slot: 0, RefObjID: 1, Codename: ref.Codename, VarianceBits: "0", Durability: 1,
		MagicOptions: []uint64{encodedOption(11, 7), encodedOption(17, 20), encodedOption(23, 15), encodedOption(29, 9), encodedOption(140, 300)}}
	c := &domain.Character{Level: pointer(10), Strength: pointer(40), Intellect: pointer(30), MissionInventory: []domain.InventoryRow{row}}
	stats, _, err := PlayerStats(c, Catalogs{Items: itemRefs{ref.Codename: ref}, MagicOptions: optionRows()})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Strength != 47 {
		t.Fatal("str option", stats.Strength)
	}
	for id, want := range map[uint16]float32{0x1b: 20, 0x1c: 20, 0x1d: 15, 0x1e: 9, 0x1f: 0, 0x20: 0} {
		if v, _ := stats.Param(id); v != want {
			t.Fatalf("param %#x = %v want %v", id, v, want)
		}
	}
	hp, _ := stats.Param(3)
	base, _, _ := PlayerStats(&domain.Character{Level: pointer(10), Strength: pointer(47), Intellect: pointer(30)}, Catalogs{Items: itemRefs{}})
	baseHP, _ := base.Param(3)
	if hp != baseHP+300 {
		t.Fatal("hp option on the max-HP node", hp, baseHP)
	}
	row.MagicOptions = []uint64{encodedOption(999, 1)}
	c.MissionInventory = []domain.InventoryRow{row}
	if _, _, err = PlayerStats(c, Catalogs{Items: itemRefs{ref.Codename: ref}, MagicOptions: optionRows()}); err == nil {
		t.Fatal("unknown tag admitted")
	}
	if _, _, err = PlayerStats(c, Catalogs{Items: itemRefs{ref.Codename: ref}}); err == nil {
		t.Fatal("options without a definition source admitted")
	}
}

// An item carrying both a base vital option and its avatar form sums the two
// in either option order: the keeper replaces per source, so the base option
// accumulates onto the item's own contribution exactly like the avatar form
// (magicoptions.go's own rule). The hpa-then-hp order failed that before the
// hp/mp cases accumulated: the later plain write replaced the avatar total.
func TestMagicOptionBaseAndAvatarVitalsSumInAnyOrder(t *testing.T) {
	rows := optionRows()
	rows[141] = &enterworld.MagicOptionRow{ParamID: 141, OptionName: "MATTR_HPA", Tag: optionTag("hpa")}
	rows[142] = &enterworld.MagicOptionRow{ParamID: 142, OptionName: "MATTR_MP", Tag: optionTag("mp")}
	rows[143] = &enterworld.MagicOptionRow{ParamID: 143, OptionName: "MATTR_MPA", Tag: optionTag("mpa")}
	statsFor := func(options ...uint64) Stats {
		ref := &enterworld.ItemRef{RefObjID: 1, Codename: "TEST_ARMOR", TypeIDs: [4]int64{3, 1, 1, 1}, Combat: &enterworld.ItemCombatRef{}}
		row := domain.InventoryRow{Slot: 0, RefObjID: 1, Codename: ref.Codename, VarianceBits: "0", Durability: 1,
			MagicOptions: options}
		c := &domain.Character{Level: pointer(10), Strength: pointer(40), Intellect: pointer(30), MissionInventory: []domain.InventoryRow{row}}
		stats, _, err := PlayerStats(c, Catalogs{Items: itemRefs{ref.Codename: ref}, MagicOptions: rows})
		if err != nil {
			t.Fatal(err)
		}
		return stats
	}
	base := statsFor()
	baseHP, _ := base.Param(3)
	baseMP, _ := base.Param(4)
	for _, test := range []struct {
		name    string
		options []uint64
		param   uint16
		base    float32
		want    float32
	}{
		{"hp alone", []uint64{encodedOption(140, 300)}, 3, baseHP, baseHP + 300},
		{"hpa alone", []uint64{encodedOption(141, 150)}, 3, baseHP, baseHP + 150},
		{"hp then hpa", []uint64{encodedOption(140, 300), encodedOption(141, 150)}, 3, baseHP, baseHP + 450},
		{"hpa then hp", []uint64{encodedOption(141, 150), encodedOption(140, 300)}, 3, baseHP, baseHP + 450},
		{"mp alone", []uint64{encodedOption(142, 200)}, 4, baseMP, baseMP + 200},
		{"mpa alone", []uint64{encodedOption(143, 120)}, 4, baseMP, baseMP + 120},
		{"mp then mpa", []uint64{encodedOption(142, 200), encodedOption(143, 120)}, 4, baseMP, baseMP + 320},
		{"mpa then mp", []uint64{encodedOption(143, 120), encodedOption(142, 200)}, 4, baseMP, baseMP + 320},
	} {
		t.Run(test.name, func(t *testing.T) {
			stats := statsFor(test.options...)
			if v, _ := stats.Param(test.param); v != test.want {
				t.Fatalf("param %d = %v, want %v (base %v)", test.param, v, test.want, test.base)
			}
		})
	}
}

// The magic-option pass scales the item's own hit rating before insertion, stored to float32.
func TestItemHitRateOptionScalesTheItem(t *testing.T) {
	item := Stats{HitRate: 57, EvasionRate: 31}
	applyItemMagicOptions(&item, []magicOption{{tag: optionTag("hr"), value: 20}, {tag: optionTag("er"), value: 10}})
	if item.HitRate != float64(float32(20.0/100*57+57)) || item.EvasionRate != float64(float32(10.0/100*31+31)) {
		t.Fatal(item.HitRate, item.EvasionRate)
	}
}

// The graph builder's gauge chains: the keeper's max HP/MP chain reproduces the adopted
// closed form trunc(1.02^(L-1) * stat * 10) across the level/stat range.
func TestKeeperMaxVitalsMatchClosedForm(t *testing.T) {
	for level := int64(1); level <= 140; level++ {
		for _, stat := range []int64{20, 21, 49, 173, 250, 333, 499, 777, 1024} {
			c := &domain.Character{Level: pointer(level), Strength: pointer(stat), Intellect: pointer(stat)}
			stats, _, err := PlayerStats(c, Catalogs{Items: itemRefs{}})
			if err != nil {
				t.Fatal(err)
			}
			hp, _ := stats.Param(3)
			if want := charactervitals.DerivedVitalMax(level, stat); int64(hp) != want {
				t.Fatalf("level %d stat %d: keeper %v closed form %d", level, stat, hp, want)
			}
		}
	}
}

// Abnormal writes join the keeper after every other owner: frostbite
// replaces 8C's built-in source, Division scales incoming damage (B4/B5).
func TestAbnormalBlockWritesThePlayerKeeper(t *testing.T) {
	c := &domain.Character{Level: pointer(20), Strength: pointer(60), Intellect: pointer(60)}
	block := &abnormal.Block{}
	block.Modifiers[0] = abnormal.Modifier{Used: true, Param: 0x8c, Channel: 0, Source: 0, Value: 200}
	block.Modifiers[1] = abnormal.Modifier{Used: true, Param: 0xb4, Channel: 2, Source: 5, Value: 20}
	stats, _, err := PlayerStatsWithModifiers(c, Catalogs{Items: itemRefs{}}, nil, block)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := stats.Param(0x8c); v != 200 {
		t.Fatal("frostbite base replacement", v)
	}
	plain, _, _ := PlayerStats(c, Catalogs{Items: itemRefs{}})
	if stats.PhysicalIncoming == plain.PhysicalIncoming {
		t.Fatal("division did not reach the incoming lane", stats.PhysicalIncoming)
	}
}
