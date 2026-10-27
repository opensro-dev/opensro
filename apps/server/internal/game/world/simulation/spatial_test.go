// Wire expectations corrected from client 852F80 -> 8535A0 (+pi/2), 2026-09-12.
package simulation

import (
	"math"
	"testing"
)

// headingWordCircularDelta is the distance between two heading words the short
// way round the circle. Needed because the codec's full circle is 65535 word
// units, so words 0 and 65535 are ADJACENT, not maximally distant.
func headingWordCircularDelta(a, b uint16) int {
	const fullCircle = int(HeadingWordScale) // 65535 units, not 65536
	d := int(a) - int(b)
	if d < 0 {
		d = -d
	}
	if d > fullCircle/2 {
		d = fullCircle - d
	}
	return d
}

// Pin cardinal and diagonal wire bearings independently of producer callers.
// Native model yaw is atan2(dx,-dz); serializer 853550 subtracts pi/2.
// The client's spawn decoder 852F80 calls 8535A0 to restore that offset.
func TestHeadingWordFromDeltaUsesNativeYawConvention(t *testing.T) {
	// 65535 word units per circle, so one eighth is 8191.875.
	for _, tc := range []struct {
		name   string
		dx, dz float64
		want   uint16
	}{
		{"north(-z) is yaw 0", 0, -1, 49151},
		{"northEast(+x,-z) is yaw pi/4", 1, -1, 57343},
		{"east(+x) is yaw pi/2", 1, 0, 0},
		{"southEast(+x,+z) is yaw 3pi/4", 1, 1, 8192},
		{"south(+z) is yaw pi", 0, 1, 16384},
		{"southWest(-x,+z) is yaw 5pi/4", -1, 1, 24576},
		{"west(-x) is yaw 3pi/2", -1, 0, 32768},
		{"northWest(-x,-z) is yaw 7pi/4", -1, -1, 40959},
	} {
		if got := headingWordFromDelta(tc.dx, tc.dz); got != tc.want {
			t.Errorf("%s: headingWordFromDelta(%v, %v) = %d, want %d", tc.name, tc.dx, tc.dz, got, tc.want)
		}
	}
}

// TestHeadingWordFromDeltaDependsOnlyOnDirection guards the property the three
// callers rely on without stating it: they pass deltas of wildly different
// magnitudes (the NPC patrol passes a 20-unit leg span, the player plane passes
// a real region-corrected displacement, the monster plane passes a segment
// chord), so a magnitude-sensitive encoder would face the three planes
// differently while every cardinal test still passed.
func TestHeadingWordFromDeltaDependsOnlyOnDirection(t *testing.T) {
	for _, dir := range [][2]float64{{0, -1}, {1, -1}, {1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}} {
		base := headingWordFromDelta(dir[0], dir[1])
		for _, scale := range []float64{0.001, 0.5, 20, 1920, 100000} {
			if got := headingWordFromDelta(dir[0]*scale, dir[1]*scale); got != base {
				t.Errorf("direction (%v, %v) scaled by %v gave heading %d, want %d - the encoder must depend on direction only",
					dir[0], dir[1], scale, got, base)
			}
		}
	}
}

// TestHeadingCodecsAgreeWithinOneWireUnit is the single-source-of-truth guard
// for the two rad -> heading-word codecs this package holds, and it is the test
// headingWordFromDelta's own comment points at.
//
// WHY THERE ARE STILL TWO. HeadingWordFromRadians (moverequest.go) TRUNCATES
// toward zero because it replicates the CLIENT's 0x7738 request serializer,
// client sub_877cc0 @0x877ceb, where the x87 rounding control is forced to truncate;
// that pin covers the C->S direction only. headingWordFromDelta ROUNDS, which is
// what the parity oracle and the human-verified BUG-11 fix already emit on the
// S->C side. How the retail SERVER converted radians into the S->C heading word
// is UNPINNED - there is no native server artifact - so neither form can claim
// the other's evidence, and switching would rewrite player-plane wire bytes for
// no evidenced gain.
//
// What CAN be asserted is that the disagreement stays inside one word unit
// (1/65535 of a circle, 0.0055 degrees - below the resolution of the client's
// own float32 parse, RadiansFromHeadingWord). Provably the bound is exactly 1:
// the rad->deg constant 57.295780181884766 (client qword @0xc13df8) sits a hair above
// the exact 180/pi, so the truncating form evaluates trunc(v*(1+1.17e-8)) where
// the rounding form evaluates round(v); those differ by 1 whenever the
// fractional part reaches 0.5 and by 0 otherwise. So this is NOT a loose
// tolerance hiding a defect - a delta of 2 would mean one of the two codecs
// genuinely changed, and that is what this catches.
func TestHeadingCodecsAgreeWithinOneWireUnit(t *testing.T) {
	const samples = 4096
	worst := 0
	for i := 0; i < samples; i++ {
		yaw := twoPi * float64(i) / float64(samples)
		// Feed the shared encoder a unit vector on that bearing. Native yaw
		// 0 faces -Z, so the direction for a given yaw is
		// {sin(yaw), -cos(yaw)} - the Math_YawToDirVec form itself.
		rounded := headingWordFromDelta(math.Sin(yaw), -math.Cos(yaw))
		truncated := HeadingWordFromRadians(math.Mod(yaw+3*math.Pi/2, twoPi))
		if d := headingWordCircularDelta(rounded, truncated); d > worst {
			worst = d
		}
	}
	if worst > 1 {
		t.Errorf("the rounding and truncating heading codecs diverge by %d word units; the native-pinned bound is 1 (0.0055 degrees). One of them has changed behaviour, which means this package now has two genuinely different wire encodings for one field", worst)
	}
}

// TestHeadingProducersAllAgree is the census assertion for item 3: every
// travel-direction heading producer in this package must return the same word
// for the same physical movement. Before the unification there were three
// hand-rolled copies of the conversion (geometry.go HeadingFromMovement,
// monstertick.go headingWordToward, npc.go ComputeNpcPatrolState) and two of
// them were mirrored against the third.
func TestHeadingProducersAllAgree(t *testing.T) {
	const region uint16 = 0x6B4F
	for _, d := range [][2]float64{{0, -7}, {5, -5}, {9, 0}, {5, 5}, {0, 11}, {-5, 5}, {-3, 0}, {-5, -5}} {
		from := Spawn{RegionID: region, X: 1200, Y: 80, Z: 350}
		to := Spawn{RegionID: region, X: from.X + d[0], Y: 80, Z: from.Z + d[1]}

		player, ok := HeadingFromMovement(from, to)
		if !ok {
			t.Fatalf("delta (%v, %v): player plane called it degenerate", d[0], d[1])
		}
		shared := headingWordFromDelta(d[0], d[1])
		if player != shared {
			t.Errorf("delta (%v, %v): HeadingFromMovement = %d but the shared encoder = %d", d[0], d[1], player, shared)
		}
	}
}
