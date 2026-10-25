/*
===========================================================================

profile_test.go - the species health curve interpolates between anchors

BaseHealth (profile.go) walks the design's healthByLevel anchors and
interpolates linearly inside a segment, truncating toward zero. The
shipped tests only ever met anchor levels, so a moved boundary (the
segment's end taken early, or the full segment width in place of the
level's progress through it) passed everything. These assertions meet
the curve at off-anchor levels for every species and recompute the
expected value from the embedded design's own anchors, so a balance
change in the design does not break them; a rule change does.

===========================================================================
*/
package companion

import "testing"

/*
================
interpolatedHealth

The curve's expected value at level, recomputed from the design's
anchors with the same integer truncation the rule promises.
================
*/
func interpolatedHealth(points []PetHealth, level uint8) uint32 {
	previous := points[0]
	for _, point := range points[1:] {
		if level <= point.Level {
			return previous.HP + uint32(uint64(point.HP-previous.HP)*uint64(level-previous.Level)/uint64(point.Level-previous.Level))
		}
		previous = point
	}
	return previous.HP
}

/*
================
TestBaseHealthInterpolatesBetweenAnchors
================
*/
func TestBaseHealthInterpolatesBetweenAnchors(t *testing.T) {
	for _, pet := range D.Pets {
		if len(pet.HealthByLevel) < 2 {
			t.Fatalf("%s: health curve has %d anchors, want at least 2", pet.Name, len(pet.HealthByLevel))
		}
		// The anchors themselves, the last anchor's plateau, and levels
		// strictly inside three different segments (early, middle, late).
		levels := []uint8{1, 5, 15, 55, pet.HealthByLevel[len(pet.HealthByLevel)-1].Level, 100}
		for _, level := range levels {
			want := interpolatedHealth(pet.HealthByLevel, level)
			if got := BaseHealth(pet.CodenamePrefix, level, 0); got != want {
				t.Fatalf("%s level %d: BaseHealth %d, want %d", pet.Name, level, got, want)
			}
		}
		for _, level := range []uint8{5, 15, 55} {
			// An off-anchor level sits strictly inside its segment, so
			// its health must sit strictly between the anchors: a rule
			// that jumps to either anchor early fails here even if the
			// recomputation above ever drifted with it.
			var below, above PetHealth
			for _, point := range pet.HealthByLevel {
				if point.Level < level {
					below = point
				}
				if point.Level > level && above.Level == 0 {
					above = point
				}
			}
			if got := BaseHealth(pet.CodenamePrefix, level, 0); got <= below.HP || got >= above.HP {
				t.Fatalf("%s level %d: BaseHealth %d, want strictly between anchors %d (level %d) and %d (level %d)",
					pet.Name, level, got, below.HP, below.Level, above.HP, above.Level)
			}
		}
	}
}

/*
================
TestBaseHealthFallsBackWithoutACurve
================
*/
func TestBaseHealthFallsBackWithoutACurve(t *testing.T) {
	// Level 0 and a codename no species claims keep the native health,
	// scaled by the profile's percent (100 for an unclaimed codename).
	if got := BaseHealth("COS_P_BEAR", 0, 1000); got != 2200 {
		t.Fatalf("level 0 Bear: BaseHealth %d, want 2200 (native x 220%%)", got)
	}
	if got := BaseHealth("COS_P_WOLF", 0, 1000); got != 1000 {
		t.Fatalf("level 0 Wolf: BaseHealth %d, want 1000 (native x 100%%)", got)
	}
	if got := BaseHealth("COS_P_JAGUAR", 30, 1000); got != 1000 {
		t.Fatalf("unclaimed codename: BaseHealth %d, want the native 1000", got)
	}
}
