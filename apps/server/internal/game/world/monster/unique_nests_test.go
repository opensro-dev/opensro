/*
===========================================================================

unique_nests_test.go - the unique rule

A nest is a unique's when its rarity's low nibble is 3: the nest's own
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
	cases := []struct {
		name string
		nest NestRow
		ref  MonsterRef
		want bool
	}{
		{"unique reference", NestRow{}, unique, true},
		{"plain reference", NestRow{}, plain, false},
		{"override makes a unique", NestRow{HasRarityOverride: true, RarityOverride: 0x13}, plain, true},
		{"override unmakes a unique", NestRow{HasRarityOverride: true, RarityOverride: 0}, unique, false},
	}
	for _, c := range cases {
		if got := NestIsUnique(c.nest, c.ref); got != c.want {
			t.Errorf("%s: NestIsUnique = %v, want %v", c.name, got, c.want)
		}
	}
}
