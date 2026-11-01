package enterworld

import (
	"strconv"
	"testing"
)

func TestPassiveDefenseCompleteProgram(t *testing.T) {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0] = "1"
	fields[68] = "4"
	fields[69] = "1684366960"
	fields[70] = "7"
	fields[71] = "17"
	got := encodedPassiveDefense(fields)
	if !got.Pinned || got.Physical != 7 || got.Magical != 17 {
		t.Fatal(got)
	}
	for _, c := range []struct {
		column int
		value  string
	}{{0, "0"}, {8, "2"}, {9, "1"}, {68, "3"}, {72, "50"}, {73, "1919246697"}, {73, "99"}, {70, "4294967296"}, {70, "bad"}} {
		t.Run(strconv.Itoa(c.column)+"/"+c.value, func(t *testing.T) {
			f := append([]string(nil), fields...)
			f[c.column] = c.value
			if encodedPassiveDefense(f).Pinned {
				t.Fatal("partial program admitted", c)
			}
		})
	}
}

func TestPassiveDefenseWeaponRequirements(t *testing.T) {
	f := make([]string, 118)
	for i := range f {
		f[i] = "0"
	}
	f[0] = "1"
	f[68] = "4"
	f[69] = strconv.FormatUint(0x64656670, 10)
	f[70] = "3"
	f[73] = strconv.FormatUint(0x72657169, 10)
	f[74] = "6"
	f[75] = "7"
	// Any reqi/reqn pins: the requirement is row.Reqi, which combat evaluates
	// with the equipment, e.g. Glory's armour set + staff under reqn.
	if d := encodedPassiveDefense(f); !d.Pinned {
		t.Fatal(d)
	}
	g := append([]string(nil), f...)
	g[73], g[74], g[75] = strconv.FormatUint(0x72657169, 10), "10", "0"
	g[76], g[77], g[78] = strconv.FormatUint(0x72657169, 10), "6", "15"
	g[79] = strconv.FormatUint(0x7265716e, 10)
	if !encodedPassiveDefense(g).Pinned {
		t.Fatal("armour set + staff under reqn refused")
	}
	g[79] = strconv.FormatUint(0x64757261, 10) // an unported operation still refuses
	g[80] = "1000"
	if encodedPassiveDefense(g).Pinned {
		t.Fatal("dura admitted into a passive")
	}
}
