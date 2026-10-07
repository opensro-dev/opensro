/*
===========================================================================

entry_placement_test.go - the stand enter-world publishes and commits

A saved spawn is taken whole, a stranded one prefers the character's town
over the race start, and every stand the entry moved reaches the authority.

===========================================================================
*/
package enterworld

import (
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
worldSpawnAt
================
*/
func worldSpawnAt(regionID int64, x, y, z float64) *WorldSpawn {
	angle := int64(0)
	return &WorldSpawn{RegionID: &regionID, X: &x, Y: &y, Z: &z, Angle: &angle}
}

/*
================
TestWorldStateForCharacterTakesSpawnWhole

A spawn without a region must not keep its coordinates under the start
region: that is a stand in Jangan the character never had.
================
*/
func TestWorldStateForCharacterTakesSpawnWhole(t *testing.T) {
	character := chinaSpearman()
	character.World = &CharacterWorld{Spawn: worldSpawnAt(0x679A, 973.9, -42, 182.7)}
	character.World.Spawn.RegionID = nil
	start := StartProfileForRace(RaceKeyChina)
	got := WorldStateForCharacter(character, RaceKeyChina).Spawn
	if got.RegionID != start.RegionID || got.X != start.X || got.Z != start.Z {
		t.Fatalf("incomplete spawn mixed with the start profile: %+v", got)
	}
	character.World.Spawn = worldSpawnAt(0x679A, 640, -109.9, 61.5)
	if got := WorldStateForCharacter(character, RaceKeyChina).Spawn; got.RegionID != 0x679A || got.X != 640 {
		t.Fatalf("complete spawn not kept: %+v", got)
	}
}

/*
================
TestRescueStrandedSpawnPrefersTheAppointedTown

With no rescue point nearby, an open appointed town wins over the race
start; a town the relocator also refuses falls through to the race start.
================
*/
func TestRescueStrandedSpawnPrefersTheAppointedTown(t *testing.T) {
	town := worldSpawnAt(0x6699, 957, -80, 1508)
	strandedAt := func() LocalPlayerEntry {
		return LocalPlayerEntry{RaceKey: RaceKeyChina, StartProfile: StartProfile{RegionID: 0x679A, X: 640, Y: -109.9, Z: 61.5}}
	}
	onlyTownOpen := func(s simulation.Spawn) (simulation.Spawn, bool, bool) { return s, s.RegionID != 0x6699, false }
	entry := strandedAt()
	if !RescueStrandedSpawn(&entry, town, onlyTownOpen) || entry.StartProfile.RegionID != 0x6699 || entry.StartProfile.Z != 1508 {
		t.Fatalf("open town not taken: %+v", entry.StartProfile)
	}
	nothingOpen := func(s simulation.Spawn) (simulation.Spawn, bool, bool) { return s, true, false }
	entry = strandedAt()
	start := StartProfileForRace(RaceKeyChina)
	if !RescueStrandedSpawn(&entry, town, nothingOpen) || entry.StartProfile.RegionID != start.RegionID {
		t.Fatalf("stranded town did not fall through to the race start: %+v", entry.StartProfile)
	}
}

/*
================
TestBuildCommitsTheStandItPublished

A rescued entry hands its stand to AdoptEntrySpawn with the published
region; an untouched entry commits nothing.
================
*/
func TestBuildCommitsTheStandItPublished(t *testing.T) {
	for _, stranded := range []bool{true, false} {
		character := chinaSpearman()
		character.World = &CharacterWorld{Spawn: worldSpawnAt(0x679A, 640, -109.9, 61.5), SpawnSet: true}
		deps := testDeps(character)
		rescue := simulation.Spawn{RegionID: 0x679B, X: 905, Y: -42.9, Z: 81}
		deps.RelocateStrandedSpawn = func(s simulation.Spawn) (simulation.Spawn, bool, bool) {
			if stranded {
				return rescue, true, true
			}
			return s, false, false
		}
		var adopted []simulation.Spawn
		deps.AdoptEntrySpawn = func(_, name string, spawn simulation.Spawn) {
			if name != character.Name {
				t.Fatalf("adopted for %q", name)
			}
			adopted = append(adopted, spawn)
		}
		result := Build(deps, BootstrapRequest{CharacterName: character.Name})
		if result.NativeResult != 1 {
			t.Fatalf("build failed: %+v", result.Reason)
		}
		if !stranded {
			if len(adopted) != 0 {
				t.Fatalf("untouched entry committed %+v", adopted)
			}
			continue
		}
		if len(adopted) != 1 || adopted[0].RegionID != rescue.RegionID || adopted[0].X != rescue.X || adopted[0].Z != rescue.Z {
			t.Fatalf("rescued stand not committed: %+v", adopted)
		}
	}
}
