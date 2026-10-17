package main

import "testing"

// TestParseLevelBandsSortsAndParses guards the -levels grammar and the level
// order (issue #171).
func TestParseLevelBandsSortsAndParses(t *testing.T) {
	bands, err := parseLevelBands("20=200,2,0,2;10=100,1,0,1")
	if err != nil {
		t.Fatal(err)
	}
	if len(bands) != 2 {
		t.Fatalf("bands = %d, want 2", len(bands))
	}
	if bands[0].Level != 10 || bands[0].Target.Region != 100 || bands[0].Target.X != 1 {
		t.Fatalf("band 0 = %+v, want level 10 region 100 x 1", bands[0])
	}
	if bands[1].Level != 20 || bands[1].Target.Region != 200 {
		t.Fatalf("band 1 = %+v, want level 20 region 200", bands[1])
	}
}

// TestParseLevelBandsRejectsBadInput guards the -levels validation
// (issue #171).
func TestParseLevelBandsRejectsBadInput(t *testing.T) {
	if _, err := parseLevelBands("nope"); err == nil {
		t.Fatal("a band without = was accepted")
	}
	if _, err := parseLevelBands("-1=100,1,0,1"); err == nil {
		t.Fatal("a negative level was accepted")
	}
	if _, err := parseLevelBands("10=100,1,0"); err == nil {
		t.Fatal("a band with three coordinates was accepted")
	}
	bands, err := parseLevelBands("")
	if err != nil || len(bands) != 0 {
		t.Fatalf("empty bands = %+v, %v, want none", bands, err)
	}
}
