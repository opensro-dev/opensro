/*
===========================================================================

petaffixes_test.go - operator blue policy persists and rejects invalid edits

===========================================================================
*/
package store

import "testing"

/*
================
TestPetAffixRulesDB
================
*/
func TestPetAffixRulesDB(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	defaults := []PetAffixRule{{Code: 77, Name: "Cooldown", Group: "Cooldown", Minimum: 13, Maximum: 50, Weight: 40, RollPower: 3}}
	if rows, err := s.PetAffixRules(defaults); err != nil || len(rows) != 1 || rows[0].Maximum != 50 {
		t.Fatalf("seed: %v %v", rows, err)
	}
	if _, err := s.db.Exec("UPDATE pet_affix_rules SET maximum=35,roll_power=5 WHERE code=77"); err != nil {
		t.Fatal(err)
	}
	if rows, err := s.PetAffixRules(defaults); err != nil || rows[0].Maximum != 35 || rows[0].RollPower != 5 {
		t.Fatalf("operator edit overwritten: %v %v", rows, err)
	}
	if _, err := s.db.Exec("UPDATE pet_affix_rules SET maximum=2 WHERE code=77"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PetAffixRules(defaults); err == nil {
		t.Fatal("invalid range accepted")
	}
}
