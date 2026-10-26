/*
===========================================================================

entry_plane_test.go - legacy-parity tests for the spawn-lift and stranded
rescue plane gates in entry.go (#1646, #1765)

LiftSpawnAboveTerrain and RescueStrandedSpawn used to test the dungeon
region bit themselves; the gate now reads the planes' feature rows
(simulation.PlaneFeatures.SpawnTerrainLift / SpawnStrandedRescue). The
legacy copies below are the pre-move bodies verbatim. Swapping the cave
column's rows on must fail the dungeon cases here.

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
legacyLiftSpawnAboveTerrain

The pre-#1765 body of LiftSpawnAboveTerrain, with the region bit as the
gate. Reference for the parity assertions below; not called by product code.
================
*/
func legacyLiftSpawnAboveTerrain(entry *LocalPlayerEntry, heightAt func(regionID uint16, x, z float64) (float64, bool), surfaceAt func(regionID uint16, x, authoredY, z float64) (float64, bool)) bool {
	if entry == nil || heightAt == nil {
		return false
	}
	profile := &entry.StartProfile
	regionID := uint16(profile.RegionID)
	if simulation.IsDungeonRegion(regionID) {
		return false
	}
	terrainY, ok := heightAt(regionID, profile.X, profile.Z)
	if !ok {
		return false
	}
	if profile.Y >= terrainY-spawnUndergroundToleranceUnits {
		return false
	}
	if surfaceAt != nil {
		if surfaceY, covered := surfaceAt(regionID, profile.X, profile.Y, profile.Z); covered &&
			math.Abs(surfaceY-profile.Y) < math.Abs(terrainY-profile.Y) {
			return false
		}
	}
	profile.Y = terrainY
	return true
}

/*
================
TestLiftSpawnAboveTerrainLegacyParity

Both the old and the new gate refuse dungeon spawns, lift buried outdoor
spawns, and leave above-terrain and deck-covered spawns alone.
================
*/
func TestLiftSpawnAboveTerrainLegacyParity(t *testing.T) {
	heightAt := func(regionID uint16, x, z float64) (float64, bool) {
		if regionID == 0x60A0 {
			return 866.0, true
		}
		return 0, false
	}
	deck := func(regionID uint16, x, authoredY, z float64) (float64, bool) {
		return 800.0, true
	}
	entryAt := func(regionID int64, y float64) *LocalPlayerEntry {
		return &LocalPlayerEntry{StartProfile: StartProfile{RegionID: regionID, X: 1842.8, Y: y, Z: 1279.2}}
	}

	cases := []struct {
		name       string
		entry      *LocalPlayerEntry
		heightAt   func(regionID uint16, x, z float64) (float64, bool)
		surfaceAt  func(regionID uint16, x, authoredY, z float64) (float64, bool)
		dungeonArm bool
	}{
		{"nil entry", nil, heightAt, nil, false},
		{"nil sampler", entryAt(0x60A0, 817.09), nil, nil, false},
		{"dungeon exempt", entryAt(0x8001, -500.0), heightAt, nil, true},
		{"dungeon deck-covered", entryAt(0x8001, -500.0), heightAt, deck, true},
		{"underground lifts", entryAt(0x60A0, 817.09), heightAt, nil, false},
		{"above terrain untouched", entryAt(0x60A0, 900.0), heightAt, nil, false},
		{"within tolerance untouched", entryAt(0x60A0, 865.0), heightAt, nil, false},
		{"no height coverage", entryAt(0x5E9E, 10.0), heightAt, nil, false},
		{"deck-covered untouched", entryAt(0x60A0, 817.09), heightAt, deck, false},
	}
	dungeonArms, lifts := 0, 0
	for _, tc := range cases {
		legacyIn, newIn := tc.entry, tc.entry
		if tc.entry != nil {
			dup := *tc.entry
			legacyIn, newIn = tc.entry, &dup
		}
		legacyGot := legacyLiftSpawnAboveTerrain(legacyIn, tc.heightAt, tc.surfaceAt)
		got := LiftSpawnAboveTerrain(newIn, tc.heightAt, tc.surfaceAt)
		if got != legacyGot {
			t.Errorf("%s: lift=%v, legacy %v", tc.name, got, legacyGot)
		}
		if legacyIn != nil && newIn.StartProfile != legacyIn.StartProfile {
			t.Errorf("%s: profile %+v, legacy %+v", tc.name, newIn.StartProfile, legacyIn.StartProfile)
		}
		if tc.dungeonArm {
			dungeonArms++
		}
		if got {
			lifts++
		}
	}
	if dungeonArms == 0 || lifts == 0 {
		t.Fatalf("arms unexercised: dungeon gates %d, lifts %d", dungeonArms, lifts)
	}
}

/*
================
legacyRescueStrandedSpawn

The pre-#1765 body of RescueStrandedSpawn, with the region bit as the
gate.
================
*/
func legacyRescueStrandedSpawn(entry *LocalPlayerEntry, relocate func(simulation.Spawn) (simulation.Spawn, bool, bool)) bool {
	if entry == nil || relocate == nil {
		return false
	}
	profile := &entry.StartProfile
	regionID := uint16(profile.RegionID)
	if simulation.IsDungeonRegion(regionID) {
		return false
	}
	rescued, stranded, rescueFound := relocate(simulation.Spawn{
		RegionID: regionID,
		X:        profile.X,
		Y:        profile.Y,
		Z:        profile.Z,
		Angle:    uint16(profile.Angle),
	})
	if !stranded {
		return false
	}
	if !rescueFound {
		start := StartProfileForRace(entry.RaceKey)
		profile.RegionID = start.RegionID
		profile.X = start.X
		profile.Y = start.Y
		profile.Z = start.Z
		profile.Angle = start.Angle
		return true
	}
	profile.RegionID = int64(rescued.RegionID)
	profile.X = rescued.X
	profile.Y = rescued.Y
	profile.Z = rescued.Z
	return true
}

/*
================
TestRescueStrandedSpawnLegacyParity

Both gates exempt dungeon spawns, take the rescue point, fall back to
the race start, and pass mainland spawns through.
================
*/
func TestRescueStrandedSpawnLegacyParity(t *testing.T) {
	entryAt := func(regionID int64) *LocalPlayerEntry {
		return &LocalPlayerEntry{
			RaceKey:      RaceKeyChina,
			StartProfile: StartProfile{RegionID: regionID, X: 1842.79, Y: 843.6, Z: 1279.23, Angle: 24576},
		}
	}
	rescuePoint := simulation.Spawn{RegionID: 0x60A0, X: 1500, Y: 700, Z: 1100, Angle: 24576}
	toPoint := func(simulation.Spawn) (simulation.Spawn, bool, bool) { return rescuePoint, true, true }
	mainland := func(s simulation.Spawn) (simulation.Spawn, bool, bool) { return s, false, false }
	noRescue := func(s simulation.Spawn) (simulation.Spawn, bool, bool) { return s, true, false }

	cases := []struct {
		name       string
		entry      *LocalPlayerEntry
		relocate   func(simulation.Spawn) (simulation.Spawn, bool, bool)
		dungeonArm bool
	}{
		{"nil entry", nil, toPoint, false},
		{"nil relocator", entryAt(0x60A0), nil, false},
		{"dungeon exempt", entryAt(0x8001), toPoint, true},
		{"stranded rescued to point", entryAt(0x60A0), toPoint, false},
		{"mainland untouched", entryAt(0x60A0), mainland, false},
		{"stranded falls back to race start", entryAt(0x60A0), noRescue, false},
	}
	dungeonArms, rescues := 0, 0
	for _, tc := range cases {
		legacyIn, newIn := tc.entry, tc.entry
		if tc.entry != nil {
			dup := *tc.entry
			legacyIn, newIn = tc.entry, &dup
		}
		legacyGot := legacyRescueStrandedSpawn(legacyIn, tc.relocate)
		got := RescueStrandedSpawn(newIn, tc.relocate)
		if got != legacyGot {
			t.Errorf("%s: rescue=%v, legacy %v", tc.name, got, legacyGot)
		}
		if legacyIn != nil && newIn.StartProfile != legacyIn.StartProfile {
			t.Errorf("%s: profile %+v, legacy %+v", tc.name, newIn.StartProfile, legacyIn.StartProfile)
		}
		if tc.dungeonArm {
			dungeonArms++
		}
		if got {
			rescues++
		}
	}
	if dungeonArms == 0 || rescues == 0 {
		t.Fatalf("arms unexercised: dungeon gates %d, rescues %d", dungeonArms, rescues)
	}
}
