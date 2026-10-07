/*
===========================================================================

entry_prepare_placement_test.go - detached re-entry placement stays read-only

Rebirth owns the division lock and commits the returned stand after packet
preparation. Preparing a lifted or rescued stand must not invoke login's
adoption hook or mutate the character before that transaction succeeds.

===========================================================================
*/
package enterworld

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestPrepareReentryResolvesPlacementWithoutAdopting
================
*/
func TestPrepareReentryResolvesPlacementWithoutAdopting(t *testing.T) {
	for _, rescue := range []bool{false, true} {
		name := "terrain-lift"
		if rescue {
			name = "stranded-rescue"
		}
		t.Run(name, func(t *testing.T) {
			character := chinaSpearman()
			character.MissionInventory = []InventoryRow{}
			character.World = &CharacterWorld{Spawn: worldSpawnAt(0x679A, 640, -109.9, 61.5), SpawnSet: true}
			before := character.Snapshot()
			deps := testDeps(character)
			want := simulation.Spawn{RegionID: 0x679A, X: 640, Y: -80, Z: 61.5}
			deps.SpawnTerrainHeight = func(uint16, float64, float64) (float64, bool) {
				return -80, true
			}
			if rescue {
				want = simulation.Spawn{RegionID: 0x679B, X: 905, Y: -42.9, Z: 81}
				deps.RelocateStrandedSpawn = func(simulation.Spawn) (simulation.Spawn, bool, bool) {
					return want, true, true
				}
			}
			adoptions := 0
			deps.AdoptEntrySpawn = func(string, string, simulation.Spawn) {
				adoptions++
			}
			prepared, ok := deps.PrepareReentry(DefaultDivisionID, character)
			if !ok || len(prepared.Packets) == 0 || prepared.Packets[0].NativeOpcode != OpcodeResetClient {
				t.Fatalf("placement preparation failed: %+v", prepared)
			}
			if prepared.Spawn != want {
				t.Fatalf("prepared stand = %+v, want %+v", prepared.Spawn, want)
			}
			if adoptions != 0 {
				t.Fatalf("detached preparation invoked login adoption %d times", adoptions)
			}
			if !reflect.DeepEqual(character.Snapshot(), before) {
				t.Fatal("detached preparation mutated the character")
			}
			if deps.AdoptEntrySpawn == nil {
				t.Fatal("detached preparation removed the live login adoption hook")
			}
		})
	}
}
