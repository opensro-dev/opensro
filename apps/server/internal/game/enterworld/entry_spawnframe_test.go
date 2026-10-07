package enterworld

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

// Regression for the re-enter-far-from-enter-region incident (2026-07-26,
// character asd2): a world record persisted before goal-frame normalization
// carries the enter-world region with multi-sector overflow locals. The
// bootstrap must ship the CANONICAL frame or the client seeds its terrain
// residency around the stale sector and the player enters an unloaded void.
func TestWorldStateForCharacterFoldsStaleSpawnFrame(t *testing.T) {
	regionID := int64(0x5E9E)
	x := 5682.786447160981
	y := 817.0863865487756
	z := 5119.23114342619
	angle := int64(24576)
	character := &Character{
		ID:            2,
		Name:          "asd2",
		ModelCodename: "CHAR_CH_MAN_SCHOLAR",
		World: &CharacterWorld{
			Spawn:    &WorldSpawn{RegionID: &regionID, X: &x, Y: &y, Z: &z, Angle: &angle},
			SpawnSet: true,
		},
	}

	world := WorldStateForCharacter(character, RaceKeyChina)

	if world.Spawn.RegionID != 0x60A0 {
		t.Fatalf("bootstrap must ship canonical region 0x60A0, got 0x%04X", world.Spawn.RegionID)
	}
	if math.Abs(world.Spawn.X-1842.786447160981) > 1e-9 || math.Abs(world.Spawn.Z-1279.23114342619) > 1e-9 {
		t.Fatalf("bootstrap locals must fold into range: got x=%v z=%v", world.Spawn.X, world.Spawn.Z)
	}
	if world.Spawn.Y != y {
		t.Fatalf("fold must not touch height: got y=%v", world.Spawn.Y)
	}
	if world.Spawn.Angle != angle {
		t.Fatalf("fold must not touch angle: got %d", world.Spawn.Angle)
	}
	if !world.SpawnSet {
		t.Fatalf("spawnSet must survive the fold")
	}
}

// Regression for the second half of the asd2 incident: after the frame fold
// the player re-entered at the right x/z but with the stale creation-time
// height (817) while the terrain there sits at ~866 - INSIDE the mountain.
// The lift raises an underground spawn onto the surface, never lowers a
// legal above-terrain height (bridge decks), and skips dungeon regions.
func TestLiftSpawnAboveTerrain(t *testing.T) {
	heightAt := func(regionID uint16, x, z float64) (float64, bool) {
		if regionID == 0x60A0 {
			return 866.0, true
		}
		return 0, false
	}
	entryAt := func(regionID int64, y float64) LocalPlayerEntry {
		return LocalPlayerEntry{StartProfile: StartProfile{RegionID: regionID, X: 1842.8, Y: y, Z: 1279.2}}
	}

	underground := entryAt(0x60A0, 817.09)
	if !LiftSpawnAboveTerrain(&underground, heightAt, nil) {
		t.Fatalf("underground spawn must lift")
	}
	if underground.StartProfile.Y != 866.0 {
		t.Fatalf("lifted height = %v, want terrain 866", underground.StartProfile.Y)
	}

	deck := entryAt(0x60A0, 900.0)
	if LiftSpawnAboveTerrain(&deck, heightAt, nil) {
		t.Fatalf("above-terrain spawn must never be lowered")
	}

	grounded := entryAt(0x60A0, 865.0)
	if LiftSpawnAboveTerrain(&grounded, heightAt, nil) {
		t.Fatalf("within-tolerance ground stander must not jitter")
	}

	dungeon := entryAt(0x8001, -500.0)
	if LiftSpawnAboveTerrain(&dungeon, heightAt, nil) {
		t.Fatalf("dungeon spawn must be exempt")
	}

	noAuthority := entryAt(0x5E9E, 10.0)
	if LiftSpawnAboveTerrain(&noAuthority, heightAt, nil) {
		t.Fatalf("no height coverage must mean no lift")
	}
	if LiftSpawnAboveTerrain(&noAuthority, nil, nil) {
		t.Fatalf("nil sampler must mean no lift")
	}
}

func TestSpawnLiftArbitratesAgainstAuthoredHeight(t *testing.T) {
	terrain := func(uint16, float64, float64) (float64, bool) { return 15, true }
	for _, tc := range []struct {
		name          string
		surface       float64
		covered, lift bool
	}{
		{"deck below terrain", 10, true, false},
		{"terrain wins tie", 15, true, true},
		{"more distant object", 20, true, true},
		{"missing object coverage", 0, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := LocalPlayerEntry{StartProfile: StartProfile{RegionID: 257, X: 25, Y: 10, Z: 50}}
			surface := func(region uint16, x, y, z float64) (float64, bool) {
				if region != 257 || x != 25 || y != 10 || z != 50 {
					t.Fatal("arbitration lost authored pose")
				}
				return tc.surface, tc.covered
			}
			if got := LiftSpawnAboveTerrain(&entry, terrain, surface); got != tc.lift {
				t.Fatalf("lift=%v want %v", got, tc.lift)
			}
			want := 10.
			if tc.lift {
				want = 15
			}
			if entry.StartProfile.Y != want {
				t.Fatalf("height=%v", entry.StartProfile.Y)
			}
		})
	}
}

// Regression for the later acts of the asd2 incident: after the frame fold
// and terrain lift, the character stood at the RIGHT place with the RIGHT
// height - first on a blocked mountain face (every move rejected at the
// source-resolve gate), then on a walkable island plateau (moves clipped at
// the island edge). Retail characters can never BE in either state; only
// the frame bug's void-walk recorded such positions.
func TestRescueStrandedSpawn(t *testing.T) {
	entryAt := func(regionID int64, x, y, z float64) LocalPlayerEntry {
		return LocalPlayerEntry{
			RaceKey:      RaceKeyChina,
			StartProfile: StartProfile{RegionID: regionID, X: x, Y: y, Z: z, Angle: 24576},
		}
	}
	rescuePoint := simulation.Spawn{RegionID: 0x60A0, X: 1500, Y: 700, Z: 1100, Angle: 24576}

	relocateToPoint := func(simulation.Spawn) (simulation.Spawn, bool, bool) { return rescuePoint, true, true }
	relocateMainland := func(s simulation.Spawn) (simulation.Spawn, bool, bool) { return s, false, false }
	relocateNoRescue := func(s simulation.Spawn) (simulation.Spawn, bool, bool) { return s, true, false }

	stranded := entryAt(0x60A0, 1842.79, 843.6, 1279.23)
	if !RescueStrandedSpawn(&stranded, relocateToPoint) {
		t.Fatal("stranded spawn must rescue")
	}
	if stranded.StartProfile.X != 1500 || stranded.StartProfile.Y != 700 || stranded.StartProfile.Z != 1100 {
		t.Fatalf("rescue point not applied: %+v", stranded.StartProfile)
	}

	fine := entryAt(0x60A0, 1500, 700, 1100)
	if RescueStrandedSpawn(&fine, relocateMainland) {
		t.Fatal("mainland spawn must pass through untouched")
	}

	desperate := entryAt(0x60A0, 1842.79, 843.6, 1279.23)
	if !RescueStrandedSpawn(&desperate, relocateNoRescue) {
		t.Fatal("no-rescue case must still relocate (start profile fallback)")
	}
	start := StartProfileForRace(RaceKeyChina)
	if desperate.StartProfile.RegionID != start.RegionID || desperate.StartProfile.X != start.X {
		t.Fatalf("fallback must be the race start profile, got %+v", desperate.StartProfile)
	}

	dungeon := entryAt(0x8001, 5000, -30, 7000)
	if RescueStrandedSpawn(&dungeon, relocateToPoint) {
		t.Fatal("dungeon spawn must be exempt")
	}
	if RescueStrandedSpawn(&stranded, nil) {
		t.Fatal("nil relocator must mean no rescue")
	}
}

// An in-range record must round-trip byte-identically through the fold (the
// parity goldens pin these values).
func TestWorldStateForCharacterKeepsInRangeSpawn(t *testing.T) {
	regionID := int64(24222)
	x := 1342.78
	y := 817.09
	z := 779.23
	angle := int64(24576)
	character := &Character{
		ID:            2,
		Name:          "asd2",
		ModelCodename: "CHAR_CH_MAN_SCHOLAR",
		World: &CharacterWorld{
			Spawn:    &WorldSpawn{RegionID: &regionID, X: &x, Y: &y, Z: &z, Angle: &angle},
			SpawnSet: true,
		},
	}

	world := WorldStateForCharacter(character, RaceKeyChina)

	if world.Spawn.RegionID != regionID || world.Spawn.X != x || world.Spawn.Y != y || world.Spawn.Z != z {
		t.Fatalf("in-range spawn must pass through untouched: got %+v", world.Spawn)
	}
}
