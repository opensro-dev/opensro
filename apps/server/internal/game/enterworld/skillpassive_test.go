/*
===========================================================================

skillpassive_test.go - passive critical programs

Whole-program admission of the passive critical family as authored.

===========================================================================
*/
package enterworld

import (
	"testing"

	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestPassiveCriticalAdmissionIsWholeProgram
================
*/
func TestPassiveCriticalAdmissionIsWholeProgram(t *testing.T) {
	base := func() []string {
		f := criticalFields("25458", "2", "0", "1919250793", "6", "8", "0")
		f[68] = "4"
		return f
	}
	for _, tc := range []struct {
		name     string
		change   func([]string) []string
		admitted bool
	}{
		{"authored", func(f []string) []string { return f }, true},
		{"active cr is not passive", func(f []string) []string { f[68] = "1"; return f }, false},
		{"active lifecycle", func(f []string) []string { f[8] = "1"; return f }, false},
		{"original id is not activity", func(f []string) []string { f[6] = "123"; return f }, true},
		{"linked", func(f []string) []string { f[9] = "12"; return f }, false},
		{"percentage unpinned", func(f []string) []string { f[71] = "20"; return f }, false},
		{"shield requirement", func(f []string) []string { f[73] = "4"; return f }, false},
		{"missing equipment", func(f []string) []string { return f[:72] }, false},
		{"missing critical", func(f []string) []string { f[69] = "0"; f[70] = "0"; return f }, false},
		{"extra rider", func(f []string) []string { return append(f, "1685418593", "8000") }, false},
		{"duplicate", func(f []string) []string { return append(f, "25458", "2", "0") }, false},
		{"truncated", func(f []string) []string { return f[:74] }, false},
		{"negative", func(f []string) []string { f[70] = "-1"; return f }, false},
		{"overflow", func(f []string) []string { f[70] = "4294967296"; return f }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := encodedPassiveCritical(tc.change(base()))
			if got.Pinned != tc.admitted {
				t.Fatalf("%+v", got)
			}
		})
	}
}

/*
================
TestAuthoredPassiveCriticalFamily
================
*/
func TestAuthoredPassiveCriticalFamily(t *testing.T) {
	licensed.RequireGameData(t)
	dir := licensed.RetailTextdataDir(t)
	source := NewTextdataSkills(dir)
	if err := source.Load(); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, r := range source.rows.values() {
		if !r.PassiveCritical.Pinned {
			continue
		}
		count++
		if r.Group != 434 || r.PassiveCritical.WeaponKind != 8 || r.PassiveCritical.Flat != uint32(r.Level+1) || r.DirectOffensePinned {
			t.Fatalf("unexpected passive %+v", r)
		}
	}
	if count != 8 {
		t.Fatalf("admitted %d ranks, want all eight authored ranks", count)
	}
}
