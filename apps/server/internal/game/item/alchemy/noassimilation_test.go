/*
===========================================================================

noassimilation_test.go - stones with and without assimilation

Off (native) a successful attribute stone with a 100% assimilation chance
rerolls another white stat, and a magic stone can reroll a white stat too;
on, either stone changes only its own stat and draws nothing more.

===========================================================================
*/

package alchemy

import (
	"reflect"
	"testing"
)

/*
================
TestParseNoAssimilation
================
*/
func TestParseNoAssimilation(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
		bad  bool
	}{
		{"", false, false},
		{"off", false, false},
		{" on ", true, false},
		{"true", true, false},
		{"yes", false, true},
	} {
		got, err := ParseNoAssimilation(tc.text)
		if got != tc.want || (err != nil) != tc.bad {
			t.Fatalf("%q = %v, %v", tc.text, got, err)
		}
	}
}

/*
================
TestAttributeStoneAssimilatesOnlyWhenNative

The draws: 0 succeeds the stone, 5 5 5 5 roll its own stat; natively 0
then hits the 100% assimilation chance, 0 picks white stat 0 and 1 1 1 1
reroll it.
================
*/
func TestAttributeStoneAssimilatesOnlyWhenNative(t *testing.T) {
	c, items := stoneFixture(false)
	items[1].Plus = 100
	native, err := c.Stone(items, []uint8{13, 14}, false, 0, sequence(t, 0, 5, 5, 5, 5, 0, 0, 1, 1, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if native.Items[0].VarianceBits&31 == items[0].VarianceBits&31 {
		t.Fatal("off: the native stone did not assimilate stat 0")
	}

	c.NoAssimilation = true
	// sequence fails the test on any draw past the stone's own five.
	on, err := c.Stone(items, []uint8{13, 14}, false, 0, sequence(t, 0, 5, 5, 5, 5))
	if err != nil {
		t.Fatal(err)
	}
	if !on.Success || on.Items[0].VarianceBits>>20&31 != native.Items[0].VarianceBits>>20&31 {
		t.Fatalf("on: the stone's own stat %d, want %d", on.Items[0].VarianceBits>>20&31, native.Items[0].VarianceBits>>20&31)
	}
	mask := uint64(31) << 20
	if on.Items[0].VarianceBits&^mask != items[0].VarianceBits&^mask ||
		!reflect.DeepEqual(on.Items[0].MagicOptions, items[0].MagicOptions) {
		t.Fatal("on: another stat moved")
	}
}

/*
================
TestMagicStoneAssimilatesOnlyWhenNative

The magic-stone branch can land on a white stat. Draws: 0 succeeds the
STR stone and 1 rolls its value; natively 0 hits the 100% assimilation
chance, 0 picks white stat 0 and 1 1 1 1 reroll it. On, the stone draws
nothing past its own two.
================
*/
func TestMagicStoneAssimilatesOnlyWhenNative(t *testing.T) {
	c, items := stoneFixture(true)
	items[1].Plus = 100
	native, err := c.Stone(items, []uint8{13, 14}, true, 0, sequence(t, 0, 1, 0, 0, 1, 1, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if native.Items[0].VarianceBits == items[0].VarianceBits {
		t.Fatal("off: the native magic stone did not assimilate a white stat")
	}

	c.NoAssimilation = true
	on, err := c.Stone(items, []uint8{13, 14}, true, 0, sequence(t, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if !on.Success || !reflect.DeepEqual(on.Items[0].MagicOptions, native.Items[0].MagicOptions) {
		t.Fatalf("on: magic options %v, want the stone's own %v", on.Items[0].MagicOptions, native.Items[0].MagicOptions)
	}
	if on.Items[0].VarianceBits != items[0].VarianceBits {
		t.Fatal("on: a white stat moved")
	}
}
