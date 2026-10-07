/*
===========================================================================

cerberus_spawn_test.go - shipped Cerberus startup and respawn locations

Exercise the real nest catalog through the population lifecycle. A restart
uses the first eligible anchor; deaths select an alternate without creating
extra occupants or allowing observers to bypass the respawn timer.

===========================================================================
*/
package monster_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestShippedCerberusSpawnLifecycle

Native 55EC10 tries eligible nests in order when no alternate is selected.
55EEEE..55EF0A chooses rand() % (members + 1), folding the extra bucket
onto the final member. The previous member remains a valid choice.
================
*/
func TestShippedCerberusSpawnLifecycle(t *testing.T) {
	const (
		codename     = "MOB_EU_KERBEROS"
		division     = "cerberus-audit"
		memberCount  = 13
		crtWordRange = 32768
	)
	dir := licensed.RetailTextdataDir(t)
	if _, err := os.Stat(filepath.Join(dir, "npcpos.txt")); err != nil {
		t.Skip("shipped textdata not present in this checkout")
	}
	shipped := monster.LoadTemplate(dir)
	var nests []monster.NestRow
	var regions []uint16
	for _, nest := range shipped.Nests {
		if shipped.Refs[nest.RefObjID].Codename == codename {
			nests = append(nests, nest)
			regions = append(regions, nest.RegionID)
		}
	}
	if len(nests) != memberCount {
		t.Fatalf("Cerberus anchors = %d, want %d", len(nests), memberCount)
	}
	for _, nest := range nests {
		if !nest.RetailEvidence || nest.HiveMaxCount != 1 || nest.HiveKey != nests[0].HiveKey {
			t.Fatalf("Cerberus anchor escaped its single-occupant hive: %+v", nest)
		}
	}
	template := monster.TemplateFromParts(shipped.Refs, nests)
	now := time.Unix(1_784_000_000, 0)
	word := uint32(0)
	state := simulation.NewMonsterState(template)
	state.SetTimeSource(func() time.Time { return now })
	state.SetRandomSource(func() float64 { return (float64(word) + 0.5) / crtWordRange })
	state.StartDivision(division)
	state.AdvancePopulation(now.UnixMilli())
	live := state.MaterializedInstances(division)
	if len(live) != 1 || live[0].Nest.HiveOrder != 0 {
		t.Fatalf("eligible startup anchors must fill in native order: %+v", live)
	}

	// Visit every member, the native extra bucket, and a consecutive repeat.
	for selection := 0; selection <= memberCount+1; selection++ {
		word = uint32(selection)
		if selection == memberCount+1 {
			word = memberCount - 1
		}
		wantOrder := int(word)
		if wantOrder >= memberCount {
			wantOrder = memberCount - 1
		}
		oldGID := live[0].Gid
		if !state.Defeat(division, oldGID, now) || state.Defeat(division, oldGID, now) {
			t.Fatal("a death must release the shared occupant exactly once")
		}
		now = now.Add(time.Second)
		state.StartDivision(division)
		state.AdvancePopulation(now.UnixMilli())
		if got := state.InstancesInRegions(division, regions); len(got) != 0 {
			t.Fatalf("selection %d: region observation bypassed the timer: %+v", selection, got)
		}
		// Cross every member's authored delay, without changing the data.
		now = now.Add(24 * time.Hour)
		state.AdvancePopulation(now.UnixMilli())
		live = state.MaterializedInstances(division)
		if len(live) != 1 || live[0].Gid == oldGID || live[0].Nest.HiveOrder != wantOrder {
			t.Fatalf("selection %d: want one new Cerberus at anchor %d, got %+v", selection, wantOrder, live)
		}
		t.Logf("draw %d respawned Cerberus at anchor %d, region %d", word, wantOrder, live[0].Nest.RegionID)
	}
}
