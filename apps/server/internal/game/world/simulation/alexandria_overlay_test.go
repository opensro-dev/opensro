/*
===========================================================================

alexandria_overlay_test.go - the NPC world with SRO_ALEXANDRIA off and on

Port-only, not native. Off, an Alexandria variant's roster and gates equal
the native tables' exactly; on, its placement and gate are registered.

===========================================================================
*/
package simulation

import (
	"reflect"
	"strconv"
	"testing"

	"opensro.online/server/internal/game/world/alexandria"
	"opensro.online/server/internal/testsupport/alexandriatest"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
alexandriaWorld

The roster plus teleport gates LoadNpcWorldRoster and AppendTeleportGates
build over dir.
================
*/
func alexandriaWorld(t *testing.T, dir string) []NpcDef {
	t.Helper()
	world, err := AppendTeleportGates(dir, LoadNpcWorldRoster(dir))
	if err != nil {
		t.Fatal(err)
	}
	return world
}

/*
================
alexandriaFixture

A variant whose added placement repeats the first native NPC.
================
*/
func alexandriaFixture(t *testing.T) (alexandriatest.Fixture, []NpcDef) {
	t.Helper()
	clientdata.RequireGameData(t)
	t.Setenv(alexandria.EnvAlexandria, "")
	native := alexandriaWorld(t, clientdata.RetailTextdataDir(t))
	if len(native) == 0 {
		t.Skip("shipped NPC world data unavailable")
	}
	return alexandriatest.Variant(t, strconv.FormatUint(uint64(native[0].RefObjID), 10)), native
}

/*
================
TestAlexandriaOffKeepsTheNativeNpcWorld
================
*/
func TestAlexandriaOffKeepsTheNativeNpcWorld(t *testing.T) {
	tier.Long(t, "the native NPC world is unchanged with Alexandria off, over the whole shipped roster")
	variant, native := alexandriaFixture(t)
	for _, value := range []string{"", "off", "0", "false", "typo"} {
		t.Setenv(alexandria.EnvAlexandria, value)
		if got := alexandriaWorld(t, variant.Dir); !reflect.DeepEqual(got, native) {
			t.Fatalf("SRO_ALEXANDRIA=%q: %d rows differ from the native %d", value, len(got), len(native))
		}
	}
}

/*
================
TestAlexandriaOnRegistersTheCity
================
*/
func TestAlexandriaOnRegistersTheCity(t *testing.T) {
	variant, native := alexandriaFixture(t)
	t.Setenv(alexandria.EnvAlexandria, "on")
	world := alexandriaWorld(t, variant.Dir)
	if len(world) != len(native)+2 {
		t.Fatalf("on: %d rows, want the native %d plus a placement and a gate", len(world), len(native))
	}
	var placed, gate bool
	for _, row := range world {
		placed = placed || row.Teleport == nil && row.Spawn.RegionID == alexandriatest.Region
		gate = gate || row.Teleport != nil && row.Codename == alexandriatest.BuildingCode
	}
	if !placed || !gate {
		t.Fatalf("on: placement %v, gate %v", placed, gate)
	}
	// The added placement is appended, so every native identity is unchanged.
	byID := make(map[uint32]NpcDef, len(world))
	for _, row := range world {
		byID[row.ObjectID] = row
	}
	for _, row := range native {
		if got := byID[row.ObjectID]; got.Codename != row.Codename || got.Spawn != row.Spawn {
			t.Fatalf("native %s (%d) became %s at %+v", row.Codename, row.ObjectID, got.Codename, got.Spawn)
		}
	}
}
