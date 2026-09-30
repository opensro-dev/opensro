/*
===========================================================================

skillpassive_damage_test.go - passive damage programs

Whole-program admission of passive damage and the authored two-hand power
ranks with their consumers.

===========================================================================
*/
package enterworld

import (
	"testing"

	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestPassiveDamageWholeProgramAdmission
================
*/
func TestPassiveDamageWholeProgramAdmission(t *testing.T) {
	base := func() []string {
		f := criticalFields("1936028790", "1160926017", "27", "0", "0")
		for len(f) < 118 { // a shipped row's full width
			f = append(f, "0")
		}
		f[68] = "4"
		return f
	}
	for _, tc := range []struct {
		name   string
		change func([]string) []string
		want   bool
	}{
		{"authored", func(f []string) []string { return f }, true},
		{"active", func(f []string) []string { f[8] = "1"; return f }, false},
		{"wrong kind", func(f []string) []string { f[68] = "0"; return f }, false},
		{"chain", func(f []string) []string { f[9] = "1"; return f }, false},
		{"unknown channel", func(f []string) []string { f[70] = "1160926018"; return f }, false},
		{"auxiliary value", func(f []string) []string { f[72] = "1"; return f }, false},
		{"negative", func(f []string) []string { f[71] = "-1"; return f }, false},
		{"overflow", func(f []string) []string { f[71] = "4294967296"; return f }, false},
		{"truncated", func(f []string) []string { return f[:72] }, false},
		// A reqi gate is part of a Praise passive; combat evaluates it (59F0E0).
		{"reqi gate", func(f []string) []string { copy(f[73:], []string{"1919250793", "6", "8"}); return f }, true},
		{"duplicate setv", func(f []string) []string { copy(f[73:], []string{"1936028790", "1160926017", "27", "0"}); return f }, true},
		{"unported tag", func(f []string) []string { copy(f[73:], []string{"1685418593", "1000"}); return f }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := encodedPassiveParameters(tc.change(base())); got.Pinned != tc.want {
				t.Fatalf("%+v", got)
			}
		})
	}
	if encodedAttackParameters(criticalFields("6386804", "1734702198", "1160926017", "0", "0", "0")).Has(ParameterTwoHandPower) {
		t.Fatal("attack arguments became getv")
	}
}

/*
================
TestAuthoredTwoHandPowerRanksAndConsumers
================
*/
func TestAuthoredTwoHandPowerRanksAndConsumers(t *testing.T) {
	licensed.RequireGameData(t)
	s := NewTextdataSkills(licensed.RetailTextdataDir(t))
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	count, consumers := 0, 0
	for _, r := range s.rows.values() {
		if r.PassiveParameters.Pinned && r.PassiveParameters.Mask.Has(ParameterTwoHandPower) {
			count++
			if r.Group != 433 || r.PassiveParameters.Values[ParameterTwoHandPower] != uint32(r.Masteries[0].Level) || r.Level < 1 || r.Level > 22 || r.DirectOffensePinned {
				t.Fatalf("unexpected admission %+v", r)
			}
		}
		if r.Attack.Parameters.Has(ParameterTwoHandPower) {
			consumers++
			if r.RequiredWeaponKinds[0] != 8 {
				t.Fatalf("unexpected consumer %s", r.Codename)
			}
		}
	}
	if count != 22 || consumers == 0 {
		t.Fatalf("ranks=%d consumers=%d", count, consumers)
	}
	base, ok := s.SkillByID(7128)
	if !ok || !base.Attack.Parameters.Has(ParameterTwoHandPower) || !base.DirectOffensePinned {
		t.Fatal("ordinary two-hand attack lost its channel")
	}
}
