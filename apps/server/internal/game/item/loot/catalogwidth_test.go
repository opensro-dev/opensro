/*
===========================================================================

catalogwidth_test.go - version 2 catalogs: per-table class widths (#460)

ISRO-R widens RareEquip to 60 classes, the stones to 20 and Reinforce to 3;
a version 2 file names each table's width and the loader enforces it.

===========================================================================
*/

package loot

import (
	"strings"
	"testing"
)

/*
================
widthRows

180 level rows of width classes, with p at class group of the given level.
================
*/
func widthRows(width, level, group int, p float32) [][]float32 {
	rows := make([][]float32, catalogLevels)
	for i := range rows {
		rows[i] = make([]float32, width)
	}
	rows[level-1][group] = p
	return rows
}

/*
================
TestVersion2EquipmentReadsItsWidths

A 60-wide rare table admits an item in class 59 and rolls it; v1 keeps 36.
================
*/
func TestVersion2EquipmentReadsItsWidths(t *testing.T) {
	item := equipmentRef{Codename: "ITEM_RARE", Group: 59, Rare: true, Type: "weapon", Weight: 1, Absolute: 100, Level: 1}
	normal := equipmentRef{Codename: "ITEM_NORMAL", Group: 0, Type: "weapon", Weight: 1, Absolute: 100, Level: 1}
	source := equipmentSource{Version: 2, Widths: map[string]int{"normal": 36, "rare": 60},
		Items: []equipmentRef{item, normal}, Normal: widthRows(36, 80, 0, 0.5), Rare: widthRows(60, 80, 59, 0.25)}
	c, err := compileEquipment(source)
	if err != nil {
		t.Fatal(err)
	}
	if c.widths != [2]int{36, 60} || len(c.classes[1][79]) != 1 || c.classes[1][79][0].group != 59 {
		t.Fatalf("widths %v, rare row %+v", c.widths, c.classes[1][79])
	}
	got, ok := c.selectEquipment(0, 59, true, 80, func() (uint32, error) { return 0, nil })
	if !ok || got.Codename != "ITEM_RARE" {
		t.Fatalf("class 59 = %+v %v", got, ok)
	}
	v1 := equipmentSource{Version: 1, Items: []equipmentRef{normal}, Normal: widthRows(36, 1, 0, 0.1), Rare: widthRows(36, 1, 0, 0)}
	if c, err := compileEquipment(v1); err != nil || c.widths != [2]int{36, 36} {
		t.Fatalf("version 1: %v %v", c.widths, err)
	}
}

/*
================
TestVersion2CatalogRejections

Each table row has exactly its width, sums to at most 1, every item sits
below its table's width, and every rolled class reaches an item at or below
it, as selectEquipment's walk down from an empty class (724120) does.
================
*/
func TestVersion2CatalogRejections(t *testing.T) {
	normal := equipmentRef{Codename: "ITEM_NORMAL", Group: 0, Type: "weapon", Weight: 1, Absolute: 100, Level: 1}
	base := func() equipmentSource {
		return equipmentSource{Version: 2, Widths: map[string]int{"normal": 36, "rare": 60},
			Items: []equipmentRef{normal}, Normal: widthRows(36, 80, 0, 0.5), Rare: widthRows(60, 80, 0, 0)}
	}
	for name, mutate := range map[string]func(*equipmentSource){
		"row width":     func(s *equipmentSource) { s.Rare[3] = s.Rare[3][:59] },
		"sum over one":  func(s *equipmentSource) { s.Normal[79][1] = 0.6 },
		"item width":    func(s *equipmentSource) { s.Items[0].Group = 36 },
		"no item below": func(s *equipmentSource) { s.Items[0].Group = 6 },
		"version":       func(s *equipmentSource) { s.Version = 3 },
	} {
		source := base()
		source.Items = append([]equipmentRef(nil), source.Items...)
		mutate(&source)
		if _, err := compileEquipment(source); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	consumable := consumableSource{Version: 2, Widths: map[int]int{8: 20},
		Classes: map[int][][]float32{8: widthRows(20, 80, 15, 0.02)},
		Items: []consumableRef{{equipmentRef: equipmentRef{Codename: "ITEM_STONE_15", Group: 15, Count: 1,
			Type: "stone", Weight: 1, Absolute: 100, Level: 1}, Family: 8}}}
	if _, err := compileConsumables(consumable); err != nil {
		t.Fatalf("a 20-wide stone family: %v", err)
	}
	consumable.Classes[8][79][16] = 0.01
	if _, err := compileConsumables(consumable); err != nil {
		t.Fatalf("an empty stone class above an item falls to it: %v", err)
	}
	consumable.Classes[8][79][14] = 0.01
	if _, err := compileConsumables(consumable); err == nil || !strings.Contains(err.Error(), "no item at or below") {
		t.Fatalf("a rolled stone class with nothing below it: %v", err)
	}
}
