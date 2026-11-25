/*
===========================================================================

portal_skytemple_test.go - the Sky Temple fields open with SRO_SKYTEMPLE

Port-only, not native. The Sky Temple import (scripts/skytemple_gates.py)
lists the four fields on Jangan's own gate. A variant built with them
keeps them closed while the switch is off: the catalogue is the native
one, and the Jangan gate offers none of them.

===========================================================================
*/

package action

import (
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/alexandria"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/pharaohtemple"
	"opensro.online/server/internal/testsupport/alexandriatest"
	"opensro.online/server/internal/testsupport/clientdata"
)

// The fixture's Sky Temple field gate, its return building and Jangan's gate.
const (
	skytempleFieldTeleport   uint32 = 349
	skytempleFieldBuilding   uint32 = 43803
	skytempleFieldRegion     uint16 = 25567
	skytempleFieldGate              = "GATE_OTHER_SKYTEMPLE_A"
	skytempleFieldBuildingID        = "STORE_OTHER_SKYTEMPLE_A_TO_TOWN"
	skytempleJanganTeleport  uint32 = 1
)

/*
================
skytempleFixture

A copy of the retail textdata (with the Alexandria overlay file the shared
fixture writes) and one Sky Temple field appended the way the import
appends it: the field gate, its return building, and the Jangan gate's
link to it and back. Returns the directory.
================
*/
func skytempleFixture(t *testing.T) string {
	t.Helper()
	dir := alexandriatest.Variant(t, firstNpcPosRef(t)).Dir
	var buildingTemplate, teleportTemplate []string
	for _, row := range enterworld.ReadTextdataFile(filepath.Join(dir, "teleportbuilding.txt")) {
		if len(row) == templeBuildingRowColumns && row[0] == "1" && row[2] == alexandriatest.BuildingCode {
			buildingTemplate = append([]string(nil), row...)
		}
	}
	for _, row := range enterworld.ReadTextdataFile(filepath.Join(dir, "teleportdata.txt")) {
		if len(row) >= templeTeleportRowColumns && row[0] == "1" && row[2] == alexandriatest.TeleportCode {
			teleportTemplate = append([]string(nil), row...)
		}
	}
	if buildingTemplate == nil || teleportTemplate == nil {
		t.Fatal("the shared fixture's gate rows are absent")
	}
	building := append([]string(nil), buildingTemplate...)
	building[1], building[2] = strconv.Itoa(int(skytempleFieldBuilding)), skytempleFieldBuildingID
	building[templeBuildingRegionColumn] = strconv.Itoa(int(skytempleFieldRegion))
	alexandriatest.AppendRow(t, dir, "teleportbuilding.txt", building)

	teleport := append([]string(nil), teleportTemplate...)
	teleport[1], teleport[2], teleport[3] = strconv.Itoa(int(skytempleFieldTeleport)), skytempleFieldGate,
		strconv.Itoa(int(skytempleFieldBuilding))
	teleport[templeRegionColumn] = strconv.Itoa(int(skytempleFieldRegion))
	teleport[6], teleport[7], teleport[8] = "1295", "40", "1282"
	teleport[templeWorldColumn] = "1"
	alexandriatest.AppendRow(t, dir, "teleportdata.txt", teleport)

	link := func(source, target uint32) {
		row := make([]string, templeLinkRowColumns)
		for i := range row {
			row[i] = "0"
		}
		row[0], row[1], row[2] = "1", strconv.Itoa(int(source)), strconv.Itoa(int(target))
		// The native link shape: one run, no schedule.
		row[4], row[5] = "1", "0"
		alexandriatest.AppendRow(t, dir, "teleportlink.txt", row)
	}
	link(skytempleJanganTeleport, skytempleFieldTeleport)
	link(skytempleFieldTeleport, skytempleJanganTeleport)
	return dir
}

/*
================
TestSkytempleFieldsAreClosedWhileTheSwitchIsOff

Unset, "off" and a value the switch refuses all read the shard as native:
with a field's rows in the tables, the catalogue is the native one, so the
Jangan gate offers no Sky Temple row.
================
*/
func TestSkytempleFieldsAreClosedWhileTheSwitchIsOff(t *testing.T) {
	clientdata.RequireGameData(t)
	t.Setenv(alexandria.EnvAlexandria, "")
	t.Setenv(monster.EnvQinShiTomb, "")
	t.Setenv(pharaohtemple.EnvPharaohTemple, "")
	t.Setenv(monster.EnvSkytemple, "")
	native := mustPortalCatalog(t, clientdata.RetailTextdataDir(t))
	dir := skytempleFixture(t)
	for _, value := range []string{"", "off", "nonsense"} {
		t.Setenv(monster.EnvSkytemple, value)
		if got := mustPortalCatalog(t, dir); !reflect.DeepEqual(got, native) {
			t.Fatalf("SRO_SKYTEMPLE=%q: the catalogue differs from the native one", value)
		}
	}
}

/*
================
TestSkytempleFieldsOpenOnTheJanganGate

On, the Jangan gate links to the field and the field's return building
leads back to the Jangan gate.
================
*/
func TestSkytempleFieldsOpenOnTheJanganGate(t *testing.T) {
	clientdata.RequireGameData(t)
	t.Setenv(alexandria.EnvAlexandria, "")
	t.Setenv(monster.EnvQinShiTomb, "")
	t.Setenv(pharaohtemple.EnvPharaohTemple, "")
	t.Setenv(monster.EnvSkytemple, "on")
	catalog := mustPortalCatalog(t, skytempleFixture(t))
	if _, ok := catalog.links[[2]uint32{skytempleJanganTeleport, skytempleFieldTeleport}]; !ok {
		t.Fatal("the Jangan gate does not offer the Sky Temple field")
	}
	if _, ok := catalog.links[[2]uint32{skytempleFieldTeleport, skytempleJanganTeleport}]; !ok {
		t.Fatal("the Sky Temple field does not lead back to the Jangan gate")
	}
	if source, ok := catalog.sources[skytempleFieldBuilding]; !ok || source != skytempleFieldTeleport {
		t.Fatalf("the field's return building is the source of %d (%v)", source, ok)
	}
}
