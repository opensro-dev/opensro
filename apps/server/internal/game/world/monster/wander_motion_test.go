package monster

import (
	"math"
	"testing"

	worldgeom "opensro.online/server/internal/game/world"
)

func TestNativeWanderDistanceAndFacingBranches(t *testing.T) {
	for _, sign := range []uint32{0, 1} {
		for _, turn := range []uint32{0, 90} {
			words := []uint32{sign, 39, turn, sign}
			i := 0
			m := NativeWanderMotion(0, func() uint32 { w := words[i]; i++; return w })
			wantDistance, wantAngle := 46.0, -float64(turn+45)/2
			if sign == 1 {
				wantDistance = 124
				wantAngle = -wantAngle
			}
			if i != 4 || m.Distance != wantDistance {
				t.Fatalf("draws=%d motion=%+v", i, m)
			}
			if angle := math.Atan2(m.Z, m.X) * 180 / math.Pi; math.Abs(angle-wantAngle) > 0.0001 {
				t.Fatalf("angle=%v want %v", angle, wantAngle)
			}
			if math.Abs(math.Hypot(m.X, m.Z)-1) > 1e-6 {
				t.Fatalf("not normalized: %+v", m)
			}
			for _, result := range []uint32{0, 2, NavResultClipped, NavResultBlocked, NavResultBlocked | NavResultClipped} {
				adjusted := m.AfterProbe(result)
				if result&(NavResultBlocked|NavResultClipped) == 0 {
					if adjusted != m {
						t.Fatal("clear probe changed direction")
					}
				} else if adjusted.X != -m.X || adjusted.Z != -m.Z || adjusted.Distance != m.Distance {
					t.Fatal("blocked probe must reverse, not shorten")
				}
			}
		}
	}
}

func TestNativeWanderRotatesWithActor(t *testing.T) {
	base := NativeWanderMotion(0, func() uint32 { return 0 })
	rotated := NativeWanderMotion(16384, func() uint32 { return 0 })
	if math.Abs(rotated.X+base.Z) > 0.0001 || math.Abs(rotated.Z-base.X) > 0.0001 {
		t.Fatalf("facing lost: %+v %+v", base, rotated)
	}
}

/*
================
TestNativeTacticsRelativeMatchesTheLegacyBranch

#1764: the tactics delta asks the world-space owner (SamePlane,
SectorOffset) instead of testing the dungeon bit; every outdoor, cave and
cross-plane pair answers the former body's bits.
================
*/
func TestNativeTacticsRelativeMatchesTheLegacyBranch(t *testing.T) {
	legacy := func(from, to Pose) (float32, float32, float32) {
		if (from.RegionID^to.RegionID)&worldgeom.DungeonRegionBit != 0 {
			far := math.Float32frombits(0x7e967699)
			return far, far, far
		}
		dx, dz := float64(float32(to.X))-float64(float32(from.X)), float64(float32(to.Z))-float64(float32(from.Z))
		if !worldgeom.IsDungeonRegion(from.RegionID) {
			dx += float64(worldgeom.SectorX(to.RegionID)-worldgeom.SectorX(from.RegionID)) * worldgeom.OutdoorRegionSize
			dz += float64(worldgeom.SectorY(to.RegionID)-worldgeom.SectorY(from.RegionID)) * worldgeom.OutdoorRegionSize
		}
		return float32(dx), 0, float32(dz)
	}
	regions := []uint16{0x62a8, 0x63a9, 0x8001, 0x8004}
	for _, a := range regions {
		for _, b := range regions {
			from, to := Pose{RegionID: a, X: 10.5, Z: 1900.25}, Pose{RegionID: b, X: 1800.125, Z: -3.5}
			gx, gy, gz := NativeTacticsRelative(from, to)
			wx, wy, wz := legacy(from, to)
			if math.Float32bits(gx) != math.Float32bits(wx) || math.Float32bits(gy) != math.Float32bits(wy) || math.Float32bits(gz) != math.Float32bits(wz) {
				t.Errorf("%#x -> %#x: got (%v, %v, %v), want (%v, %v, %v)", a, b, gx, gy, gz, wx, wy, wz)
			}
		}
	}
}
