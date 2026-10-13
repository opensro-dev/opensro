/*
===========================================================================

objectnav_reach_test.go - the reach skip never skips a placement that mattered

The chord scans skip a placement whose reach box the chord misses
(objectnav_reach.go). If a reach were ever too small, a skipped placement
would have produced a contact, an entry or a different winner. This scans
random chords over real authority data both ways, skipping and with every
placement scanned (a validator built with scanEveryPlacement, the
brute-force reference), and
requires identical answers from both scans and from the clip and the owner
walk built on them.

===========================================================================
*/
package movement

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

// reachChordRegions are object-dense towns and a field; chords start
// anywhere in the region, so many cross sector and region borders.
var reachChordRegions = []uint16{0x61A8, 0x6699, 0x694F, 0x6687, 0x6A2C, 0x6E4A}

const reachChordsPerRegion = 20000

/*
================
reachScans

Every answer the skip could change for one chord, as one comparable string.
================
*/
func reachScans(v *WaterValidator, from, to simulation.Spawn) string {
	fromGrid := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z})
	toGrid := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: to.RegionID, X: to.X, Z: to.Z})
	walk := v.ownerWalk(from, simulation.NavOwner{}, to)
	t, key, found, point := v.objectChordFirstContact(fromGrid.X, fromGrid.Z, from.Y, toGrid.X, toGrid.Z, to.Y, walk)
	contact := fmt.Sprintf("%v %v %v %+v", t, key, found, point)
	entry := "none"
	if stand, at, visit := v.terrainOutlineEntry(fromGrid.X, fromGrid.Z, toGrid.X, toGrid.Z, 0); stand != nil {
		entry = fmt.Sprintf("%v %v %v %v %v %v %v %v", at, visit, stand.surface.seedRegionID, stand.setDX, stand.setDZ, stand.objectIndex, stand.meshIndex, stand.cellIndex)
	}
	return fmt.Sprintf("contact %s | entry %s | walk %+v | clip %+v", contact, entry, walk.spans(), v.ClipMovementPathFrom(from, simulation.NavOwner{}, to))
}

/*
================
TestReachSkipMatchesEveryPlacementScanned
================
*/
func TestReachSkipMatchesEveryPlacementScanned(t *testing.T) {
	if testing.Short() {
		t.Skip("long: real-data chords")
	}
	v := realAuthorityValidator(t)
	every := realAuthorityValidator(t)
	every.scanEveryPlacement = true
	random := rand.New(rand.NewSource(831))
	chords, contacts, entries := 0, 0, 0
	for _, region := range reachChordRegions {
		for i := 0; i < reachChordsPerRegion; i++ {
			x, z := random.Float64()*simulation.NativeRegionSize, random.Float64()*simulation.NativeRegionSize
			y, ok := v.TerrainHeightAt(region, x, z)
			if !ok {
				continue
			}
			angle, length := random.Float64()*2*math.Pi, 1+random.Float64()*119
			from := simulation.Spawn{RegionID: region, X: x, Y: y, Z: z}
			to := simulation.NormalizeSpawnFrame(simulation.Spawn{RegionID: region, X: x + math.Cos(angle)*length, Y: y, Z: z + math.Sin(angle)*length})
			skipped := reachScans(v, from, to)
			scanned := reachScans(every, from, to)
			if skipped != scanned {
				t.Fatalf("chord %+v -> %+v:\n skipped %s\n every   %s", from, to, skipped, scanned)
			}
			chords++
			if !strings.Contains(scanned, "contact +Inf +Inf false") {
				contacts++
			}
			if !strings.Contains(scanned, "entry none") {
				entries++
			}
		}
	}
	// The sweep must have exercised both scans, or the equality proves little.
	if contacts < 1000 || entries < 100 {
		t.Fatalf("only %d contacts and %d entries in %d chords", contacts, entries, chords)
	}
	t.Logf("%d chords identical both ways; %d object contacts, %d outline entries", chords, contacts, entries)
}
