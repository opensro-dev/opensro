/*
===========================================================================

gmwarp_plane_test.go - legacy-parity tests for the /warp destination
plane validation (#1646, #1765)

ResolveGMWarpDestination used to branch on the dungeon region bit
itself; the validation now dispatches to the plane owners in
gmwarp_plane.go. The legacy copy below is the pre-move body verbatim -
the entry checks plus the bit branch. Swapping the dispatcher arms
must fail the dungeon and outdoor cases here.

===========================================================================
*/
package enterworld

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
legacyResolveGMWarpDestination

The pre-#1765 body of ResolveGMWarpDestination, with the region bit
choosing between the two validations.
================
*/
func legacyResolveGMWarpDestination(d *Deps, c *Character, p simulation.Spawn) (simulation.Spawn, bool) {
	if c == nil || !c.GMPrivilege || d.CanEnterWorldRegion == nil || !d.CanEnterWorldRegion(c, p.RegionID) {
		return p, false
	}
	for _, v := range []float64{p.X, p.Y, p.Z} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return p, false
		}
	}
	if p.RegionID == 0 {
		return p, false
	}
	if simulation.IsDungeonRegion(p.RegionID) {
		if d.SpawnSurfaceHeight == nil {
			return p, false
		}
		_, ok := d.SpawnSurfaceHeight(p.RegionID, p.X, p.Y, p.Z)
		return p, ok
	}
	if p.X < 0 || p.X >= 1920 || p.Z < 0 || p.Z >= 1920 || d.SpawnTerrainHeight == nil {
		return p, false
	}
	h, ok := d.SpawnTerrainHeight(p.RegionID, p.X, p.Z)
	if !ok || math.IsNaN(h) || math.IsInf(h, 0) {
		return p, false
	}
	entry := LocalPlayerEntry{}
	entry.StartProfile.RegionID = int64(p.RegionID)
	entry.StartProfile.X = p.X
	entry.StartProfile.Y = p.Y
	entry.StartProfile.Z = p.Z
	LiftSpawnAboveTerrain(&entry, d.SpawnTerrainHeight, d.SpawnSurfaceHeight)
	p.Y = entry.StartProfile.Y
	if d.SpawnSurfaceHeight != nil {
		if _, ok := d.SpawnSurfaceHeight(p.RegionID, p.X, p.Y, p.Z); !ok {
			return p, false
		}
	}
	if d.RelocateStrandedSpawn != nil {
		_, stranded, _ := d.RelocateStrandedSpawn(p)
		if stranded {
			return p, false
		}
	}
	return p, true
}

/*
================
sameSpawn

Spawn equality for the parity loop: NaN coordinates must compare
equal (the NaN-refusal case returns its input untouched on both sides).
================
*/
func sameSpawn(a, b simulation.Spawn) bool {
	if a.RegionID != b.RegionID || a.Y != b.Y || a.Z != b.Z || a.Angle != b.Angle {
		return false
	}
	return a.X == b.X || (math.IsNaN(a.X) && math.IsNaN(b.X))
}

/*
================
TestResolveGMWarpDestinationLegacyParity

The entry checks, the cave surface-only rule (no frame check, no Y
rewrite), and the outdoor frame/terrain/lift/stranded pipeline all
match the legacy bit branch.
================
*/
func TestResolveGMWarpDestinationLegacyParity(t *testing.T) {
	gm := &Character{GMPrivilege: true}
	baseDeps := func() *Deps {
		return &Deps{
			CanEnterWorldRegion: func(*Character, uint16) bool { return true },
			SpawnTerrainHeight:  func(uint16, float64, float64) (float64, bool) { return 100, true },
			SpawnSurfaceHeight:  func(uint16, float64, float64, float64) (float64, bool) { return 0, true },
			RelocateStrandedSpawn: func(p simulation.Spawn) (simulation.Spawn, bool, bool) {
				return p, false, false
			},
		}
	}
	cave := simulation.Spawn{RegionID: 0x8001, X: 5000, Y: -30, Z: 7000}
	field := simulation.Spawn{RegionID: 0x60A0, X: 100, Y: 50, Z: 100}

	cases := []struct {
		name   string
		deps   func() *Deps
		char   *Character
		target simulation.Spawn
		arm    string
	}{
		{"nil character", baseDeps, nil, field, "entry"},
		{"non-GM", baseDeps, &Character{}, field, "entry"},
		{"no entry callback", func() *Deps {
			d := baseDeps()
			d.CanEnterWorldRegion = nil
			return d
		}, gm, field, "entry"},
		{"region refused", func() *Deps {
			d := baseDeps()
			d.CanEnterWorldRegion = func(*Character, uint16) bool { return false }
			return d
		}, gm, field, "entry"},
		{"NaN coordinate", baseDeps, gm, simulation.Spawn{RegionID: 0x60A0, X: math.NaN(), Y: 50, Z: 100}, "entry"},
		{"region zero", baseDeps, gm, simulation.Spawn{RegionID: 0, X: 100, Y: 50, Z: 100}, "entry"},
		{"cave surface covers", baseDeps, gm, cave, "cave"},
		{"cave no frame check", baseDeps, gm, simulation.Spawn{RegionID: 0x8001, X: -9000, Y: -30, Z: 99999}, "cave"},
		{"cave surface missing", func() *Deps {
			d := baseDeps()
			d.SpawnSurfaceHeight = nil
			return d
		}, gm, cave, "cave"},
		{"cave surface fails", func() *Deps {
			d := baseDeps()
			d.SpawnSurfaceHeight = func(uint16, float64, float64, float64) (float64, bool) { return 0, false }
			return d
		}, gm, cave, "cave"},
		{"field lifts onto terrain", baseDeps, gm, field, "outdoor"},
		{"field above terrain stands", baseDeps, gm, simulation.Spawn{RegionID: 0x60A0, X: 100, Y: 150, Z: 100}, "outdoor"},
		{"field frame edge", baseDeps, gm, simulation.Spawn{RegionID: 0x60A0, X: 1920, Y: 50, Z: 100}, "outdoor"},
		{"field negative local", baseDeps, gm, simulation.Spawn{RegionID: 0x60A0, X: -1, Y: 50, Z: 100}, "outdoor"},
		{"field no terrain", func() *Deps {
			d := baseDeps()
			d.SpawnTerrainHeight = nil
			return d
		}, gm, field, "outdoor"},
		{"field terrain fails", func() *Deps {
			d := baseDeps()
			d.SpawnTerrainHeight = func(uint16, float64, float64) (float64, bool) { return 0, false }
			return d
		}, gm, field, "outdoor"},
		{"field terrain NaN", func() *Deps {
			d := baseDeps()
			d.SpawnTerrainHeight = func(uint16, float64, float64) (float64, bool) { return math.NaN(), true }
			return d
		}, gm, field, "outdoor"},
		{"field bare probes skipped", func() *Deps {
			d := baseDeps()
			d.SpawnSurfaceHeight = nil
			d.RelocateStrandedSpawn = nil
			return d
		}, gm, field, "outdoor"},
		{"field surface fails", func() *Deps {
			d := baseDeps()
			d.SpawnSurfaceHeight = func(uint16, float64, float64, float64) (float64, bool) { return 0, false }
			return d
		}, gm, field, "outdoor"},
		{"field stranded refused", func() *Deps {
			d := baseDeps()
			d.RelocateStrandedSpawn = func(p simulation.Spawn) (simulation.Spawn, bool, bool) { return p, true, true }
			return d
		}, gm, field, "outdoor"},
	}
	arms := map[string]int{}
	for _, tc := range cases {
		deps := tc.deps()
		legacySpawn, legacyOK := legacyResolveGMWarpDestination(deps, tc.char, tc.target)
		gotSpawn, gotOK := deps.ResolveGMWarpDestination(tc.char, tc.target)
		if gotOK != legacyOK || !sameSpawn(gotSpawn, legacySpawn) {
			t.Errorf("%s: (%+v, %v), legacy (%+v, %v)", tc.name, gotSpawn, gotOK, legacySpawn, legacyOK)
		}
		arms[tc.arm]++
	}
	for _, arm := range []string{"entry", "cave", "outdoor"} {
		if arms[arm] == 0 {
			t.Fatalf("arm unexercised: %s", arm)
		}
	}
}
