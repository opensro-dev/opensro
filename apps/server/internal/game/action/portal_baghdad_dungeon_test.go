/*
===========================================================================

portal_baghdad_dungeon_test.go - the Baghdad dungeon's gates, switch off and on

Port-only, not native (#4134): the Arabia variant appends the dungeon's
gates (Baghdad's city portals into the dungeon, the dungeon's gates
back) to the retail tables, their arrivals naming the port-only world
112. With SRO_BAGHDAD_DUNGEON off the catalogue must load as if those
rows were absent - the world is never registered - and with it on,
once bootstrap has registered the world, the round trip loads.

===========================================================================
*/
package action

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"opensro.online/server/internal/game/world/arabia"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

const (
	baghdadTownTeleport    uint32 = 49001
	baghdadDungeonTeleport uint32 = 49002
	hideoutEntryTeleport   uint32 = 49003
	hideoutExitTeleport    uint32 = 49004
	hideoutEntryBuilding   uint32 = 27751
	hideoutExitBuilding    uint32 = 27752
)

/*
================
baghdadGateFixture

A minimal textdata dir carrying the dungeon chain's shape: a town
arrival in the default world, a dungeon arrival naming world 112, and
the two links between them, the variant's rows reduced to their load
columns.
================
*/
func baghdadGateFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	link := func(source, target uint32) string {
		fields := []string{"1", strconv.Itoa(int(source)), strconv.Itoa(int(target)), "0", "1", "0"}
		for len(fields) < 21 {
			fields = append(fields, "0")
		}
		return strings.Join(fields, "\t")
	}
	files := map[string]string{
		"teleportbuilding.txt": "1\t27751\tGATE_BAGHDAD_FIXTURE",
		"teleportdata.txt": strings.Join([]string{
			"1\t49001\tGATE_BAGHDAD_TOWN\t0\t0\t25000\t100\t0\t200\t0\t0\t0\t1",
			"1\t49002\tGATE_BAGHDAD_DUNGEON\t0\t0\t23378\t-400\t58\t-300\t0\t0\t0\t112",
		}, "\n"),
		"teleportlink.txt": strings.Join([]string{
			link(baghdadTownTeleport, baghdadDungeonTeleport),
			link(baghdadDungeonTeleport, baghdadTownTeleport),
		}, "\n"),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

/*
================
TestBaghdadDungeonGatesFollowTheirSwitch
================
*/
func TestBaghdadDungeonGatesFollowTheirSwitch(t *testing.T) {
	dir := baghdadGateFixture(t)
	t.Setenv(monster.EnvBaghdadDungeon, "off")
	off, err := loadPortalCatalog(dir)
	if err != nil {
		t.Fatalf("off: the catalogue refused the hidden dungeon rows: %v", err)
	}
	if _, ok := off.destinations[baghdadDungeonTeleport]; ok {
		t.Fatal("off: the dungeon arrival loaded")
	}
	if _, ok := off.links[[2]uint32{baghdadTownTeleport, baghdadDungeonTeleport}]; ok {
		t.Fatal("off: the link into the dungeon loaded")
	}
	if _, ok := off.links[[2]uint32{baghdadDungeonTeleport, baghdadTownTeleport}]; ok {
		t.Fatal("off: the link out of the dungeon loaded")
	}
	if _, ok := off.destinations[baghdadTownTeleport]; !ok {
		t.Fatal("off: the town arrival was lost with the dungeon's rows")
	}
	if err := instance.RegisterPortOnly(instance.ArabiaDefinitions()); err != nil {
		t.Fatal(err)
	}
	t.Setenv(monster.EnvBaghdadDungeon, "on")
	on, err := loadPortalCatalog(dir)
	if err != nil {
		t.Fatalf("on: %v", err)
	}
	destination, ok := on.destinations[baghdadDungeonTeleport]
	if !ok {
		t.Fatal("on: the dungeon arrival is missing")
	}
	if destination.world != instance.DefinitionID(arabia.DungeonWorldID) {
		t.Fatalf("on: the dungeon arrival names world %d", destination.world)
	}
	if _, ok := on.links[[2]uint32{baghdadTownTeleport, baghdadDungeonTeleport}]; !ok {
		t.Fatal("on: the link into the dungeon is missing")
	}
	if _, ok := on.links[[2]uint32{baghdadDungeonTeleport, baghdadTownTeleport}]; !ok {
		t.Fatal("on: the link out of the dungeon is missing")
	}
}

/*
================
TestHideoutGatesFollowTheirSwitch

The Kalia hideout's own chain (#4134): the source's field gate
(GATE_MONG_TO_BOSS, world 1 as authored) and the exit on the
template's doorstep (GATE_BOSS_TO_MONG, world 113, its region spelled
signed as the later table spells it). With the dungeon switch off
neither row loads - the entry hides by its code beside its world-113
exit - and with it on the round trip loads, the signed region landing
on the hideout's own.
================
*/
func TestHideoutGatesFollowTheirSwitch(t *testing.T) {
	dir := t.TempDir()
	link := func(source, target uint32) string {
		fields := []string{"1", strconv.Itoa(int(source)), strconv.Itoa(int(target)), "0", "1", "0"}
		for len(fields) < 21 {
			fields = append(fields, "0")
		}
		return strings.Join(fields, "\t")
	}
	files := map[string]string{
		"teleportbuilding.txt": strings.Join([]string{
			"1\t27751\tSTORE_MONG_TO_BOSS_GATE",
			"1\t27752\tSTORE_BOSS_TO_MONG_GATE",
		}, "\n"),
		"teleportdata.txt": strings.Join([]string{
			"1\t49003\tGATE_MONG_TO_BOSS\t27751\t0\t18769\t961\t-341\t574\t0\t0\t0\t1",
			"1\t49004\tGATE_BOSS_TO_MONG\t27752\t0\t-32743\t0\t7\t360\t0\t0\t0\t113",
		}, "\n"),
		"teleportlink.txt": strings.Join([]string{
			link(hideoutEntryTeleport, hideoutExitTeleport),
			link(hideoutExitTeleport, hideoutEntryTeleport),
		}, "\n"),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(monster.EnvBaghdadDungeon, "off")
	off, err := loadPortalCatalog(dir)
	if err != nil {
		t.Fatalf("off: the catalogue refused the hidden hideout rows: %v", err)
	}
	for _, id := range []uint32{hideoutEntryTeleport, hideoutExitTeleport} {
		if _, ok := off.destinations[id]; ok {
			t.Fatalf("off: hideout gate %d loaded", id)
		}
	}
	for _, ref := range []uint32{hideoutEntryBuilding, hideoutExitBuilding} {
		if _, ok := off.sources[ref]; ok {
			t.Fatalf("off: hideout building %d answers clicks", ref)
		}
	}
	if len(off.links) != 0 {
		t.Fatalf("off: %d hideout links loaded", len(off.links))
	}
	if err := instance.RegisterPortOnly(instance.ArabiaDefinitions()); err != nil {
		t.Fatal(err)
	}
	t.Setenv(monster.EnvBaghdadDungeon, "on")
	on, err := loadPortalCatalog(dir)
	if err != nil {
		t.Fatalf("on: %v", err)
	}
	exit, ok := on.destinations[hideoutExitTeleport]
	if !ok {
		t.Fatal("on: the hideout exit is missing")
	}
	if exit.spawn.RegionID != arabia.HideoutRegionID {
		t.Fatalf("on: the hideout exit stands in region %d, want %d", exit.spawn.RegionID, arabia.HideoutRegionID)
	}
	if exit.world != instance.DefinitionID(arabia.HideoutWorldID) {
		t.Fatalf("on: the hideout exit names world %d", exit.world)
	}
	if on.sources[hideoutEntryBuilding] != hideoutEntryTeleport {
		t.Fatal("on: the field gate's building answers no clicks")
	}
	if _, ok := on.links[[2]uint32{hideoutEntryTeleport, hideoutExitTeleport}]; !ok {
		t.Fatal("on: the way in is missing")
	}
	if _, ok := on.links[[2]uint32{hideoutExitTeleport, hideoutEntryTeleport}]; !ok {
		t.Fatal("on: the way out is missing")
	}
}
