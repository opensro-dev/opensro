/*
===========================================================================

portal_pharaoh_temple_test.go - the Pharaoh Temple's gates, switch off and on

Port-only, not native: build_alexandria_variant.py appends four gates (two
clicked desert gates, two walk-in gates inside the Temple), their links, two
objects and two TELEPORT event zones to the retail tables. The fixture
appends the same shape over a copy of the retail textdata, so the catalogue
can be compared with the native one with SRO_PHARAOH_TEMPLE off, and the
round trip (a desert gate in, the walk-in gate back out) run with it on.

===========================================================================
*/
package action

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/alexandria"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/pharaohtemple"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/alexandriatest"
	"opensro.online/server/internal/testsupport/clientdata"
)

// The source's teleport ids, object ids and event zone ids for the temple.
const (
	templeDesertTeleport       uint32 = 171
	templeDesertThiefTeleport  uint32 = 172
	templeInnerTeleport        uint32 = 173
	templeInnerThiefTeleport   uint32 = 174
	templeDesertBuildingRef    uint32 = 27751
	templeDesertThiefBuilding  uint32 = 27752
	templeDesertRegion         uint16 = 22347
	templeThiefDesertRegion    uint16 = 21067
	templeMerchantZoneID              = "234"
	templeThiefZoneID                 = "235"
	templeEntryLevel                  = 105
	templeTeleportRowColumns          = 13
	templeEventZoneRowColumns         = 14
	templeBuildingRowColumns          = 58
	templeLinkRowColumns              = 21
	templeWorldColumn                 = 12
	templeRegionColumn                = 5
	templeBuildingRegionColumn        = 41
	templeNoObject                    = "0"
)

// templeInnerSpawns are the Temple arrival points the source authors.
var templeInnerSpawns = map[uint32][3]float64{
	templeInnerTeleport:      {-4027, 58, -3717},
	templeInnerThiefTeleport: {4376, 55, 4707},
}

/*
================
templeFixture

A copy of the retail textdata (with the Alexandria overlay file the shared
fixture writes, hiding nothing of the temple's) and the temple's rows
appended the way the variant builder appends them. Returns the directory.
================
*/
func templeFixture(t *testing.T) string {
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
	building := func(ref uint32, code string, region uint16) {
		row := append([]string(nil), buildingTemplate...)
		row[1], row[2], row[templeBuildingRegionColumn] = strconv.Itoa(int(ref)), code, strconv.Itoa(int(region))
		alexandriatest.AppendRow(t, dir, "teleportbuilding.txt", row)
	}
	building(templeDesertBuildingRef, pharaohtemple.DesertMerchantHunterBuilding, templeDesertRegion)
	building(templeDesertThiefBuilding, pharaohtemple.DesertThiefBuilding, templeThiefDesertRegion)

	teleport := func(id uint32, code string, object string, region int, at [3]float64) {
		row := append([]string(nil), teleportTemplate...)
		row[1], row[2], row[3], row[templeRegionColumn] = strconv.Itoa(int(id)), code, object, strconv.Itoa(region)
		row[6], row[7], row[8] = strconv.FormatFloat(at[0], 'f', -1, 64), strconv.FormatFloat(at[1], 'f', -1, 64), strconv.FormatFloat(at[2], 'f', -1, 64)
		// The variant builder flattens the source's world 60 to 1.
		row[templeWorldColumn] = "1"
		alexandriatest.AppendRow(t, dir, "teleportdata.txt", row)
	}
	teleport(templeDesertTeleport, pharaohtemple.DesertMerchantHunterGate, strconv.Itoa(int(templeDesertBuildingRef)), int(templeDesertRegion), [3]float64{965, 49, 423})
	teleport(templeDesertThiefTeleport, pharaohtemple.DesertThiefGate, strconv.Itoa(int(templeDesertThiefBuilding)), int(templeThiefDesertRegion), [3]float64{815, 20, 574})
	teleport(templeInnerTeleport, pharaohtemple.TempleMerchantHunterGate, templeNoObject, -32752, templeInnerSpawns[templeInnerTeleport])
	teleport(templeInnerThiefTeleport, pharaohtemple.TempleThiefGate, templeNoObject, -32752, templeInnerSpawns[templeInnerThiefTeleport])

	// Every link: level 105 and over, and no transport (kind 2 with no operands).
	link := func(source, target uint32) {
		row := make([]string, templeLinkRowColumns)
		for i := range row {
			row[i] = "0"
		}
		row[0], row[1], row[2] = "1", strconv.Itoa(int(source)), strconv.Itoa(int(target))
		row[4], row[5] = "1", "0"
		row[6], row[7], row[8], row[9] = "1", strconv.Itoa(templeEntryLevel), "999", "2"
		alexandriatest.AppendRow(t, dir, "teleportlink.txt", row)
	}
	link(templeDesertTeleport, templeInnerTeleport)
	link(templeDesertThiefTeleport, templeInnerThiefTeleport)
	link(templeInnerTeleport, templeDesertTeleport)
	link(templeInnerThiefTeleport, templeDesertThiefTeleport)

	zone := func(id, code string) {
		row := []string{"1", id, code, "TELEPORT", "0", "0", "0", "0", "0", code, "xxx", "xxx", "xxx", "xxx"}
		alexandriatest.AppendRow(t, dir, "eventzonedata.txt", row)
	}
	zone(templeMerchantZoneID, pharaohtemple.TempleMerchantHunterGate)
	zone(templeThiefZoneID, pharaohtemple.TempleThiefGate)
	return dir
}

/*
================
TestPharaohTempleCatalogueIsNativeWhileTheSwitchIsOff

Unset, "off" and a value the switch refuses all read the shard as native:
with the temple's rows in the tables, the catalogue (destinations, sources,
buildings, links and walk-in zones) is the native one, deeply equal. A
shard that did not hide the zones would refuse to load them, for they name
teleports the catalogue lacks.
================
*/
func TestPharaohTempleCatalogueIsNativeWhileTheSwitchIsOff(t *testing.T) {
	clientdata.RequireGameData(t)
	t.Setenv(alexandria.EnvAlexandria, "")
	t.Setenv(monster.EnvQinShiTomb, "")
	t.Setenv(pharaohtemple.EnvPharaohTemple, "")
	native := mustPortalCatalog(t, clientdata.RetailTextdataDir(t))
	dir := templeFixture(t)
	for _, value := range []string{"", "off", "nonsense"} {
		t.Setenv(pharaohtemple.EnvPharaohTemple, value)
		if got := mustPortalCatalog(t, dir); !reflect.DeepEqual(got, native) {
			t.Fatalf("SRO_PHARAOH_TEMPLE=%q: the catalogue differs from the native one", value)
		}
	}
}

/*
================
TestPharaohTempleCatalogueAddsFourGatesWhileTheSwitchIsOn

On adds exactly the four gates, the four links, the two desert objects and
the two walk-in zones, and nothing else: each desert gate is the source of
its object, each temple gate lands in the Temple's region 0x8010 of the
persistent world, and each walk-in zone's one link leads back to the desert
gate at level 105.
================
*/
func TestPharaohTempleCatalogueAddsFourGatesWhileTheSwitchIsOn(t *testing.T) {
	clientdata.RequireGameData(t)
	t.Setenv(alexandria.EnvAlexandria, "")
	t.Setenv(monster.EnvQinShiTomb, "")
	t.Setenv(pharaohtemple.EnvPharaohTemple, "")
	native := mustPortalCatalog(t, clientdata.RetailTextdataDir(t))
	dir := templeFixture(t)
	t.Setenv(pharaohtemple.EnvPharaohTemple, "on")
	on := mustPortalCatalog(t, dir)
	if len(on.destinations) != len(native.destinations)+4 || len(on.links) != len(native.links)+4 ||
		len(on.buildings) != len(native.buildings)+2 || len(on.walkIns) != len(native.walkIns)+2 || len(on.sources) != len(native.sources)+2 {
		t.Fatalf("on: %d destinations %d links %d buildings %d zones %d sources, native %d %d %d %d %d",
			len(on.destinations), len(on.links), len(on.buildings), len(on.walkIns), len(on.sources),
			len(native.destinations), len(native.links), len(native.buildings), len(native.walkIns), len(native.sources))
	}
	for id, region := range map[uint32]uint16{templeInnerTeleport: pharaohtemple.TempleRegionID, templeInnerThiefTeleport: pharaohtemple.TempleRegionID,
		templeDesertTeleport: templeDesertRegion, templeDesertThiefTeleport: templeThiefDesertRegion} {
		destination, ok := on.destinations[id]
		if !ok || destination.spawn.RegionID != region || destination.world != 1 {
			t.Fatalf("on: teleport %d = %+v (present %v), want region 0x%04X in world 1", id, destination, ok, region)
		}
	}
	for id, at := range templeInnerSpawns {
		spawn := on.destinations[id].spawn
		if spawn.X != at[0] || spawn.Y != at[1] || spawn.Z != at[2] {
			t.Fatalf("on: teleport %d lands at %+v, want %v", id, spawn, at)
		}
	}
	if on.sources[templeDesertBuildingRef] != templeDesertTeleport || on.sources[templeDesertThiefBuilding] != templeDesertThiefTeleport {
		t.Fatalf("on: desert objects %v", on.sources)
	}
	for zone, want := range map[string]uint32{
		pharaohtemple.TempleMerchantHunterGate: templeDesertTeleport, pharaohtemple.TempleThiefGate: templeDesertThiefTeleport} {
		link, ok := on.walkIns[zone]
		if !ok || link.target != want || link.fee != 0 || len(link.conditions) != 2 ||
			link.conditions[0] != (portalCondition{kind: 1, minimum: templeEntryLevel, maximum: 999}) || link.conditions[1] != (portalCondition{kind: portalConditionTransport}) {
			t.Fatalf("on: walk-in zone %s = %+v (present %v)", zone, link, ok)
		}
	}
}

/*
================
templeRoundTripFixture

A player at the desert gate's object with the shipped gates plus the
temple's, level 105. The zone lookup puts the merchant gate's walk-in zone
at x >= 100 of the Temple.
================
*/
func templeRoundTripFixture(t *testing.T) (*Runtime, *enterworld.Character, *[][]wire.Frame) {
	t.Helper()
	clientdata.RequireGameData(t)
	t.Setenv(alexandria.EnvAlexandria, "")
	t.Setenv(monster.EnvQinShiTomb, "")
	t.Setenv(pharaohtemple.EnvPharaohTemple, "on")
	dir := templeFixture(t)
	rt, c, _, _ := returnFixture(t, 30000)
	level := int64(templeEntryLevel)
	c.Level = &level
	rt.NpcSpawn.Enabled, rt.NpcSpawn.AtPlayer = true, true
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 277751, RefObjID: templeDesertBuildingRef, Codename: pharaohtemple.DesertMerchantHunterBuilding,
		Teleport: &simulation.TeleportGateBounds{Radius: 10, Height: 25}}}
	if err := rt.ConfigurePortals(dir); err != nil {
		t.Fatal(err)
	}
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(nil, nil))
	rt.WalkInZoneAt = func(at simulation.Spawn) string {
		if at.RegionID == pharaohtemple.TempleRegionID && at.X >= 100 {
			return pharaohtemple.TempleMerchantHunterGate
		}
		return ""
	}
	pushed := &[][]wire.Frame{}
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { *pushed = append(*pushed, frames) }
	if err := rt.AdmitCharacterSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	return rt, c, pushed
}

/*
================
templeEnter

Clicks the desert gate's object and asks for the Temple, the way the
client's gate window does.
================
*/
func templeEnter(rt *Runtime, c *enterworld.Character) OpResult {
	rt.HandleObjectSelect(testDivision, c, wire.NewWriter(4).U32(277751).Payload())
	return rt.HandlePortal(testDivision, c, wire.NewWriter(9).U32(277751).U8(2).U32(templeInnerTeleport).Payload())
}

/*
================
TestPharaohTempleDesertGateAdmitsLevel105AndOver

The desert gate's one link carries the level condition and, in this slice,
nothing else: a level-104 player is refused with the teleport answer's 0x15
and stays; a level-105 one enters. The job check is not here (slice 2, see
the comment in walkin.go and portalAdmission's kind 7), so any job passes.
================
*/
func TestPharaohTempleDesertGateAdmitsLevel105AndOver(t *testing.T) {
	rt, c, _ := templeRoundTripFixture(t)
	level := int64(templeEntryLevel - 1)
	c.Level = &level
	before := *c.World.Spawn.RegionID
	out := templeEnter(rt, c)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, []byte{2, 0x15}) || *c.World.Spawn.RegionID != before {
		t.Fatalf("level 104 answered %+v and stands in region %v", out, *c.World.Spawn.RegionID)
	}
	level = templeEntryLevel
	out = templeEnter(rt, c)
	if len(out.Frames) == 0 || out.Frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("level 105 did not enter: %+v", out)
	}
}

/*
================
TestPharaohTempleRoundTrip

A level-105 player clicks the desert gate, arrives at the Temple's authored
point in region 0x8010 of world 1 with nothing charged, stands there without
being bounced (the arrival is not in a walk-in zone), walks into the zone of
GATE_TEMPLE_MERCHANT_HUNTER_GATE and arrives back at the desert gate's own
point in region 22347.
================
*/
func TestPharaohTempleRoundTrip(t *testing.T) {
	rt, c, pushed := templeRoundTripFixture(t)
	gold := *c.Gold
	out := templeEnter(rt, c)
	if len(out.Frames) == 0 || out.Frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("the desert gate did not enter: %+v", out)
	}
	spawn := templeInnerSpawns[templeInnerTeleport]
	if uint16(*c.World.Spawn.RegionID) != pharaohtemple.TempleRegionID || *c.World.Spawn.X != spawn[0] || *c.World.Spawn.Z != spawn[2] {
		t.Fatalf("arrived at %+v, want the Temple's point %v", c.World.Spawn, spawn)
	}
	if *c.Gold != gold {
		t.Fatal("the gate charged a fee")
	}

	// The first sample only seeds the zone; standing on outside it is quiet.
	rt.advanceWalkIns(1000)
	rt.advanceWalkIns(1100)
	if len(*pushed) != 0 || uint16(*c.World.Spawn.RegionID) != pharaohtemple.TempleRegionID {
		t.Fatalf("the arrival bounced the player: %+v", *pushed)
	}

	// Walk into the zone: out through the way the source authors.
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = simulation.Spawn{RegionID: pharaohtemple.TempleRegionID, X: 500, Y: 58, Z: 0}
			w.SpawnSet = true
			w.MoveSegment = nil
		})
	rt.advanceWalkIns(1200)
	if len(*pushed) != 1 || (*pushed)[0][0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("walking into the zone answered %+v", *pushed)
	}
	if uint16(*c.World.Spawn.RegionID) != templeDesertRegion || *c.World.Spawn.X != 965 || *c.World.Spawn.Z != 423 {
		t.Fatalf("came out at %+v, want the desert gate's point in region %d", c.World.Spawn, templeDesertRegion)
	}
	if *c.Gold != gold {
		t.Fatal("the walk-in charged a fee")
	}
}
