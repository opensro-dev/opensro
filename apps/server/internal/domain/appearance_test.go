package domain

import (
	"math"
	"testing"
)

func TestStartProfileReturnsOwnedModelMap(t *testing.T) {
	first := StartProfileForRace(RaceKeyEurope)
	first.DefaultModelRefByGender[GenderMale] = 1

	second := StartProfileForRace(RaceKeyEurope)
	if second.DefaultModelRefByGender[GenderMale] == 1 {
		t.Fatal("start profile exposes mutable package-owned model data")
	}
}

/*
================
TestBodyScaleClampsIntoTheNativeRange

An explicit scale holds inside [0.5, 2]; an infinite one pins to the
ceiling and a NaN never escapes the clamp (it returns the floor).
================
*/
func TestBodyScaleClampsIntoTheNativeRange(t *testing.T) {
	for _, tc := range []struct {
		scale float64
		want  float64
	}{
		{0.1, 0.5},
		{1.25, 1.25},
		{5, 2},
		{math.Inf(1), 2},
	} {
		scale := tc.scale
		character := &Character{HeightScale: &scale, VolumeScale: &scale}
		if got := ResolveCharacterHeightScale(character); got != tc.want {
			t.Errorf("height scale %v = %v, want %v", tc.scale, got, tc.want)
		}
		if got := ResolveCharacterVolumeScale(character); got != tc.want {
			t.Errorf("volume scale %v = %v, want %v", tc.scale, got, tc.want)
		}
	}
	if got := clampFloat(math.NaN(), 0.5, 2); got != 0.5 {
		t.Errorf("clampFloat(NaN) = %v, want the floor 0.5", got)
	}
}
