/*
===========================================================================

unique_nests_test.go - the unique rule

A nest is a unique's when its rarity's grade is 3 or 8: the nest's own
override first, else its reference's monster type.

===========================================================================
*/
package monster

import "testing"

/*
================
TestNestIsUnique
================
*/
func TestNestIsUnique(t *testing.T) {
	unique, plain := MonsterRef{MonsterType: 3}, MonsterRef{MonsterType: 0}
	lateUnique := MonsterRef{MonsterType: 8}
	cases := []struct {
		name string
		nest NestRow
		ref  MonsterRef
		want bool
	}{
		{"unique reference", NestRow{}, unique, true},
		{"late unique reference", NestRow{}, lateUnique, true},
		{"plain reference", NestRow{}, plain, false},
		{"override makes a unique", NestRow{HasRarityOverride: true, RarityOverride: 0x13}, plain, true},
		{"override makes a late unique", NestRow{HasRarityOverride: true, RarityOverride: 0x18}, plain, true},
		{"override unmakes a unique", NestRow{HasRarityOverride: true, RarityOverride: 0}, unique, false},
	}
	for _, c := range cases {
		if got := NestIsUnique(c.nest, c.ref); got != c.want {
			t.Errorf("%s: NestIsUnique = %v, want %v", c.name, got, c.want)
		}
	}
}

/*
================
TestRarityIsUnique
================
*/
func TestRarityIsUnique(t *testing.T) {
	for _, rarity := range []uint8{3, 8, 0x13, 0x18, 0x28} {
		if !RarityIsUnique(rarity) {
			t.Errorf("rarity %#x must name a unique", rarity)
		}
	}
	for _, rarity := range []uint8{0, 1, 2, 4, 5, 6, 7, 9, 0x10, 0x24} {
		if RarityIsUnique(rarity) {
			t.Errorf("rarity %#x must not name a unique", rarity)
		}
	}
}
