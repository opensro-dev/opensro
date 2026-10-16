/*
===========================================================================

petlootrules_test.go - persistent, validated DB pet drop factor

===========================================================================
*/
package store

import (
	"math"
	"testing"
)

/*
================
TestPetGearDropFactorDB
================
*/
func TestPetGearDropFactorDB(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	if value, err := s.PetGearDropFactor(); err != nil || value != 0.1 {
		t.Fatalf("default %v %v", value, err)
	}
	for _, value := range []float64{0, 0.25, 1} {
		if err := s.SetPetGearDropFactor(value); err != nil {
			t.Fatal(err)
		}
		if got, err := s.PetGearDropFactor(); err != nil || got != value {
			t.Fatalf("stored %v %v", got, err)
		}
	}
	for _, value := range []float64{-1, 1.1, math.NaN(), math.Inf(1)} {
		if err := s.SetPetGearDropFactor(value); err == nil {
			t.Fatalf("accepted invalid %v", value)
		}
	}
	s.Close()
	var err error
	s, err = Open(dir, Options{RequireStore: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if value, err := s.PetGearDropFactor(); err != nil || value != 1 {
		t.Fatalf("restart lost factor: %v %v", value, err)
	}
	for _, value := range []string{"NaN", "Inf", "-1", "2", "bad"} {
		if _, err := s.db.Exec("UPDATE meta SET value=? WHERE key=?", value, PetGearDropFactorKey); err != nil {
			t.Fatal(err)
		}
		if _, err := s.PetGearDropFactor(); err == nil {
			t.Fatalf("accepted corrupt DB %q", value)
		}
	}
}
