package simulation

import (
	"math"
	"testing"
)

func TestDefaultNpcRosterReturnsOwnedCopy(t *testing.T) {
	first := DefaultNpcRoster()
	first[0].Name = "mutated"
	first = append(first, NpcDef{Name: "extra"})
	if len(first) != len(defaultNpcRoster)+1 {
		t.Fatalf("mutated caller roster length = %d, want %d", len(first), len(defaultNpcRoster)+1)
	}

	second := DefaultNpcRoster()
	if len(second) != len(defaultNpcRoster) {
		t.Fatalf("default roster length = %d after caller append, want %d", len(second), len(defaultNpcRoster))
	}
	if second[0].Name == "mutated" {
		t.Fatal("default roster shares mutable storage with its caller")
	}
}

// npcPatrolLegTicks re-derives the triangle wave's leg length the same way
// ComputeNpcPatrolState does, so retuning NpcPatrolSpanZ / NpcPatrolStepPerTick
// cannot leave a stale hardcoded period in these tests.
func npcPatrolLegTicks() int64 {
	return int64(math.Max(1, math.Ceil(math.Abs(NpcPatrolSpanZ)/NpcPatrolStepPerTick)))
}

// playerPlaneWordForZHop asks the PLAYER plane what a pure +/-z step faces.
// The expected value is taken from the other plane rather than written as a
// literal - that is what makes the assertions below cross-plane AGREEMENT
// rather than a restatement of npc.go's own arithmetic against itself.
func playerPlaneWordForZHop(t *testing.T, anchor Spawn, dz float64) uint16 {
	t.Helper()
	from := Spawn{RegionID: anchor.RegionID, X: anchor.X, Y: anchor.Y, Z: anchor.Z}
	to := Spawn{RegionID: anchor.RegionID, X: anchor.X, Y: anchor.Y, Z: anchor.Z + dz}
	word, ok := HeadingFromMovement(from, to)
	if !ok {
		t.Fatalf("HeadingFromMovement rejected a %v-unit z hop as degenerate", dz)
	}
	return word
}

func npcPatrolTestAnchors() map[string]Spawn {
	return map[string]Spawn{
		// The two anchors the parity fixture also exercises.
		"shop":        NpcShopSpawn(),
		"europeStart": {RegionID: 0x6B4F, X: 1201, Y: 80, Z: 355},
	}
}

// TestNpcPatrolFacesItsTravelDirectionLikeThePlayerPlane is the regression
// guard for the mirrored patrol yaw (board seq742/752, G-SRV co-sign seq763).
//
// ComputeNpcPatrolState documented and emitted "+z -> 0, -z -> pi". Native is
// the mirror of that: yaw 0 faces -Z, so travelling +z is pi. The patrol is
// pure +/-Z motion, which is precisely the axis where the mirrored and native
// forms differ by a FULL pi rather than agreeing - so NPC_EU_SMITH faced
// exactly backwards on every leg.
//
// THE SHAPE OF THIS TEST IS THE POINT. It does not pin a literal heading word,
// because a literal table is exactly what let this survive: on the +/-x rows
// the mirrored and native forms AGREE EXACTLY (coordinator seq734 made the
// same observation on the monster plane). Instead it derives the direction the
// NPC is actually travelling from the POSES the function itself returns, and
// derives the expected word from HeadingFromMovement - the player plane. The
// two planes therefore cannot silently drift apart again, whatever either one
// is refactored into.
func TestNpcPatrolFacesItsTravelDirectionLikeThePlayerPlane(t *testing.T) {
	legTicks := npcPatrolLegTicks()
	period := 2 * legTicks

	for name, anchor := range npcPatrolTestAnchors() {
		forwardZ := playerPlaneWordForZHop(t, anchor, NpcPatrolStepPerTick)
		backwardZ := playerPlaneWordForZHop(t, anchor, -NpcPatrolStepPerTick)
		if forwardZ == backwardZ {
			t.Fatalf("%s: the player plane gives the same word (%d) for +z and -z travel - the oracle is broken, not the patrol", name, forwardZ)
		}

		interior := 0
		// Several periods, including NEGATIVE ticks, so the phase modulo is
		// exercised rather than just the first leg.
		for tick := -period; tick < 3*period; tick++ {
			prev := ComputeNpcPatrolState(anchor, tick-1)
			cur := ComputeNpcPatrolState(anchor, tick)
			next := ComputeNpcPatrolState(anchor, tick+1)

			var want uint16
			switch {
			case prev.Z < cur.Z && cur.Z < next.Z:
				want = forwardZ // strictly inside the A->B (+z) leg
			case prev.Z > cur.Z && cur.Z > next.Z:
				want = backwardZ // strictly inside the B->A (-z) leg
			default:
				// A turning point: the pose sequence does not name a single
				// travel direction here, so only assert the heading is one
				// of the two legal facings rather than which.
				if cur.HeadingWord != forwardZ && cur.HeadingWord != backwardZ {
					t.Errorf("%s tick %d (turning point): headingWord = %d, want one of %d/%d",
						name, tick, cur.HeadingWord, forwardZ, backwardZ)
				}
				continue
			}
			interior++
			if cur.HeadingWord != want {
				t.Errorf("%s tick %d: headingWord = %d, want %d - the patrol pose moves z %v -> %v, and the player plane faces that direction with %d",
					name, tick, cur.HeadingWord, want, cur.Z, next.Z, want)
			}
		}
		if interior == 0 {
			t.Fatalf("%s: no tick was strictly interior to a leg - the test asserted nothing", name)
		}
	}
}

// TestNpcPatrolHeadingIsNotTheMirroredConvention is the explicit
// witnessed-red guard: it names the defect and the value the defect produced,
// so a future reader who reintroduces "+z -> 0" sees why it is wrong rather
// than only that a number moved.
//
// The old code emitted heading word 0 while travelling +z (yaw 0) and 32768
// while travelling -z (yaw pi). Native inverts both. The assertion is written
// against the OTHER PLANE rather than against 32768 so that it survives a
// change of rounding mode in the shared encoder - it is the mirror that is
// being forbidden, not a specific integer.
func TestNpcPatrolHeadingIsNotTheMirroredConvention(t *testing.T) {
	legTicks := npcPatrolLegTicks()
	if legTicks < 2 {
		t.Skip("patrol tuning leaves no tick strictly inside a leg")
	}

	for name, anchor := range npcPatrolTestAnchors() {
		forwardZ := playerPlaneWordForZHop(t, anchor, NpcPatrolStepPerTick)
		backwardZ := playerPlaneWordForZHop(t, anchor, -NpcPatrolStepPerTick)

		// Tick 1 is strictly inside the A->B leg for any legTicks >= 2.
		advancing := ComputeNpcPatrolState(anchor, 1)
		if advancing.Z <= ComputeNpcPatrolState(anchor, 0).Z {
			t.Fatalf("%s: tick 1 is not advancing in +z; the fixture assumption changed", name)
		}
		if advancing.HeadingWord == backwardZ {
			t.Errorf("%s: travelling +z produced heading %d, which is the word for -z travel - the mirrored '+z -> 0' convention has been reintroduced (native yaw 0 faces -Z; the client's Math_YawToDirVec negates cos; REV board seq742)",
				name, advancing.HeadingWord)
		}
		if advancing.HeadingWord != forwardZ {
			t.Errorf("%s: travelling +z produced heading %d, want the player plane's +z word %d",
				name, advancing.HeadingWord, forwardZ)
		}

		// And the returning leg, so a fix that merely swapped one literal
		// without deriving the direction cannot pass by fixing only one side.
		retreating := ComputeNpcPatrolState(anchor, legTicks+1)
		if retreating.Z >= ComputeNpcPatrolState(anchor, legTicks).Z {
			t.Fatalf("%s: tick %d is not retreating in -z; the fixture assumption changed", name, legTicks+1)
		}
		if retreating.HeadingWord != backwardZ {
			t.Errorf("%s: travelling -z produced heading %d, want the player plane's -z word %d",
				name, retreating.HeadingWord, backwardZ)
		}
	}
}
