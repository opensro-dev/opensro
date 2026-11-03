/*
===========================================================================

portal_pharaoh_tomb_test.go - the Pharaoh Tomb gates resolve to their own worlds

Port-only (#1750): the variant's three carried tomb exits name worlds 57, 58
and 59 in the last teleportdata column. The portal catalogue refuses them
while the tomb worlds are unregistered, so a tomb-built variant only boots
with SRO_PHARAOH_TOMB on, and resolves each exit to its own world once they
are registered - the order the boot runs, registration in the bootstrap and
the catalogue load after it. The native catalogue never names those worlds.

===========================================================================
*/
package action

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/testsupport/alexandriatest"
	"opensro.online/server/internal/testsupport/clientdata"
)

/*
================
TestPharaohTombGateWorldsFollowRegistration
================
*/
func TestPharaohTombGateWorldsFollowRegistration(t *testing.T) {
	clientdata.RequireGameData(t)
	// Two fixture dirs: a catalogue parse is cached per directory (portal.go),
	// so the refused reading and the accepted one cannot share a directory.
	// The boot never reparses after registering; only this test does both.
	unregistered := alexandriatest.Variant(t, firstNpcPosRef(t))
	appendTombExit(t, unregistered.Dir)
	if _, err := loadPortalCatalog(unregistered.Dir); err == nil || !strings.Contains(err.Error(), "unknown world 57") {
		t.Fatalf("unregistered tomb worlds loaded: %v", err)
	}
	if err := instance.RegisterPortOnly(instance.PharaohTombDefinitions()); err != nil {
		t.Fatal(err)
	}
	fixture := alexandriatest.Variant(t, firstNpcPosRef(t))
	tombID := appendTombExit(t, fixture.Dir)
	catalog := mustPortalCatalog(t, fixture.Dir)
	destination, ok := catalog.destinations[tombID]
	if !ok || destination.world != instance.PharaohTomb1ID {
		t.Fatalf("tomb exit = %+v (present %v)", destination, ok)
	}
	if destination.code != "GATE_TOMB_GATE_OUT_1" {
		t.Fatalf("tomb exit code %q", destination.code)
	}
}

/*
================
appendTombExit

Appends one carried tomb exit (world 57) and its building to the fixture
tables, returning the new teleport id.
================
*/
func appendTombExit(t *testing.T, dir string) uint32 {
	t.Helper()
	const buildingRef = "29901"
	buildingFound := false
	for _, row := range enterworld.ReadTextdataFile(filepath.Join(dir, "teleportbuilding.txt")) {
		if len(row) >= 3 && row[1] == strconv.Itoa(alexandriatest.BuildingRef) {
			row = append([]string(nil), row...)
			row[1], row[2] = buildingRef, "STORE_TOMB_GATE_OUT_1"
			alexandriatest.AppendRow(t, dir, "teleportbuilding.txt", row)
			buildingFound = true
			break
		}
	}
	if !buildingFound {
		t.Fatal("the fixture building is absent")
	}
	var id uint32
	var exit []string
	for _, row := range enterworld.ReadTextdataFile(filepath.Join(dir, "teleportdata.txt")) {
		if len(row) < 13 || row[0] != "1" {
			continue
		}
		if parsed, err := strconv.ParseUint(row[1], 10, 32); err == nil && uint32(parsed) > id {
			id = uint32(parsed)
		}
		if row[2] == alexandriatest.TeleportCode {
			exit = append([]string(nil), row...)
		}
	}
	if exit == nil {
		t.Fatal("the fixture gate is absent")
	}
	id++
	exit[1] = strconv.FormatUint(uint64(id), 10)
	exit[2], exit[3], exit[12] = "GATE_TOMB_GATE_OUT_1", buildingRef, "57"
	alexandriatest.AppendRow(t, dir, "teleportdata.txt", exit)
	return id
}
