/*
===========================================================================

qinshi_tomb_test.go - verification of Qin-Shi Emperor Tomb teleport links and walk-in zones

Tests both SRO_QINSHI_TOMB off (native default) and on: 13 Floor 5-6
teleport destinations, 77 teleport links (including Floor 4 snake gate 125
and Floor 6 exit buildings 131..134), and 72 walk-in TELEPORT event zones
(normalized to uppercase to match WaterValidator.WalkInZoneAt). The
flag-off catalog is proven byte-identical to the shipped one, the tomb's
link graph is proven closed, and the Jangan door is walked at levels 69
and 70 (GH #1013).

===========================================================================
*/

package action

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestQinShiTombEmbeddedTeleportLinks
================
*/
func TestQinShiTombEmbeddedTeleportLinks(t *testing.T) {
	dataRows := monster.SplitTabbedText(qinshiTombTeleportDataTSV)
	if got, want := len(dataRows), 13; got != want {
		t.Fatalf("embedded tomb teleportdata rows = %d, want %d", got, want)
	}
	for i, row := range dataRows {
		if len(row) != 13 {
			t.Fatalf("teleportdata row %d column count = %d, want 13", i+1, len(row))
		}
	}

	rows := monster.SplitTabbedText(qinshiTombTeleportLinkTSV)
	if got, want := len(rows), 77; got != want {
		t.Fatalf("embedded tomb teleportlink rows = %d, want %d", got, want)
	}
	for i, row := range rows {
		if len(row) != 21 {
			t.Fatalf("row %d column count = %d, want 21", i+1, len(row))
		}
	}
}

/*
================
TestQinShiTombPortalsAndWalkIns
================
*/
func TestQinShiTombPortalsAndWalkIns(t *testing.T) {
	clientdata.RequireGameData(t)
	dir := clientdata.RetailTextdataDir(t)

	t.Setenv(monster.EnvQinShiTomb, "off")
	baseCatalog, err := loadPortalCatalog(dir)
	if err != nil {
		t.Fatalf("loadPortalCatalog(off): %v", err)
	}
	if _, exists := baseCatalog.walkIns["GATE_JINSI_IN"]; exists {
		t.Fatal("GATE_JINSI_IN must not be active when SRO_QINSHI_TOMB is off")
	}

	t.Setenv(monster.EnvQinShiTomb, "on")
	tombCatalog, err := loadPortalCatalog(dir)
	if err != nil {
		t.Fatalf("loadPortalCatalog(on): %v", err)
	}
	if got, want := len(tombCatalog.destinations)-len(baseCatalog.destinations), 13; got != want {
		t.Fatalf("added tomb teleport destinations = %d, want %d", got, want)
	}
	if got, want := len(tombCatalog.links)-len(baseCatalog.links), 77; got != want {
		t.Fatalf("added tomb teleport links = %d, want %d", got, want)
	}
	if got, want := len(tombCatalog.walkIns)-len(baseCatalog.walkIns), 72; got != want {
		t.Fatalf("added tomb walk-in zones = %d, want %d", got, want)
	}

	// Verify outer entrance walk-in GATE_JINSI_OUT (49 -> 50, requires level 70..999),
	// inner exit walk-in GATE_JINSI_IN (50 -> 49), and uppercase normalization of
	// mixed-case zones (GATE_JINSI_01X01_12, GATE_JINSI_05X01-1..3, GATE_JINSI_06X01_06..06X05_03).
	outLink, ok := tombCatalog.walkIns["GATE_JINSI_OUT"]
	if !ok || outLink.source != 49 || outLink.target != 50 {
		t.Fatalf("GATE_JINSI_OUT link = %+v, ok=%v; want 49->50", outLink, ok)
	}
	inLink, ok := tombCatalog.walkIns["GATE_JINSI_IN"]
	if !ok || inLink.source != 50 || inLink.target != 49 {
		t.Fatalf("GATE_JINSI_IN link = %+v, ok=%v; want 50->49", inLink, ok)
	}
	for zone, want := range map[string][2]uint32{
		"GATE_JINSI_01X01_12": {51, 53},
		"GATE_JINSI_05X01-1":  {136, 139},
		"GATE_JINSI_05X01-2":  {137, 140},
		"GATE_JINSI_05X01-3":  {138, 141},
		"GATE_JINSI_06X01_06": {144, 143},
		"GATE_JINSI_06X02_06": {145, 142},
		"GATE_JINSI_06X03_09": {146, 142},
		"GATE_JINSI_06X04_12": {147, 142},
		"GATE_JINSI_06X05_03": {148, 142},
	} {
		link, ok := tombCatalog.walkIns[zone]
		if !ok || link.source != want[0] || link.target != want[1] {
			t.Fatalf("walk-in %s = %+v, ok=%v; want %d->%d", zone, link, ok, want[0], want[1])
		}
	}

	// Verify Floor 4 snake gate (125 -> 130) and Floor 6 exit building links (131..134 -> {53, 129, 120, 50}).
	for _, pair := range [][2]uint32{{125, 130}, {131, 53}, {132, 129}, {133, 120}, {134, 50}} {
		if _, ok := tombCatalog.links[pair]; !ok {
			t.Fatalf("missing Tomb building link %v", pair)
		}
		src := tombCatalog.destinations[pair[0]]
		if !src.building {
			t.Fatalf("Tomb building gate %d (%s) must be marked as a building gate", pair[0], src.code)
		}
	}

	// Verify Floor 6 center -> North wing trigger portal radius fallback (the native portal operation).
	rt := &Runtime{}
	atPortal := simulation.Spawn{RegionID: 0x8002, X: -131, Y: 1890, Z: 2535}
	if got := rt.walkInZoneAt(atPortal); got != "GATE_JINSI_06X01_06" {
		t.Fatalf("walkInZoneAt(Floor 6 North portal) = %q, want GATE_JINSI_06X01_06", got)
	}
	atCenterArrival := simulation.Spawn{RegionID: 0x8002, X: 0, Y: 1785, Z: 59}
	if got := rt.walkInZoneAt(atCenterArrival); got != "" {
		t.Fatalf("walkInZoneAt(Floor 6 center arrival) = %q, want empty", got)
	}
	t.Setenv(monster.EnvQinShiTomb, "off")
	if got := rt.walkInZoneAt(atPortal); got != "" {
		t.Fatalf("walkInZoneAt(off) = %q, want empty", got)
	}
}

/*
================
canonicalPortalCatalog

One deterministic line per catalog row (destinations, sources, links,
buildings, walk-ins), so two catalogs compare byte for byte.
================
*/
func canonicalPortalCatalog(c *portalCatalog) string {
	linkLine := func(link portalLink) string {
		line := fmt.Sprintf("%d>%d|%d", link.source, link.target, link.fee)
		for _, cond := range link.conditions {
			line += fmt.Sprintf("|{%d,%d,%d}", cond.kind, cond.minimum, cond.maximum)
		}
		return line
	}
	var b strings.Builder
	ids := make([]uint32, 0, len(c.destinations))
	for id := range c.destinations {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		d := c.destinations[id]
		fmt.Fprintf(&b, "D %d|%d|%s|%t|%t|%d|%t|%d|%d|%v|%v|%v\n",
			d.id, d.ref, d.code, d.building, d.fortressGate, d.gateKind, d.recall,
			d.world, d.spawn.RegionID, d.spawn.X, d.spawn.Y, d.spawn.Z)
	}
	refs := make([]uint32, 0, len(c.sources))
	for ref := range c.sources {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i] < refs[j] })
	for _, ref := range refs {
		fmt.Fprintf(&b, "S %d>%d\n", ref, c.sources[ref])
	}
	keys := make([][2]uint32, 0, len(c.links))
	for key := range c.links {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	for _, key := range keys {
		fmt.Fprintf(&b, "L %s\n", linkLine(c.links[key]))
	}
	codes := make([]string, 0, len(c.buildings))
	for code := range c.buildings {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		fmt.Fprintf(&b, "B %s>%d\n", code, c.buildings[code])
	}
	zones := make([]string, 0, len(c.walkIns))
	for zone := range c.walkIns {
		zones = append(zones, zone)
	}
	sort.Strings(zones)
	for _, zone := range zones {
		fmt.Fprintf(&b, "W %s|%s\n", zone, linkLine(c.walkIns[zone]))
	}
	return b.String()
}

/*
================
TestQinShiOffCatalogIsByteIdentical

The off state is the shipped catalog, proven byte for byte: the off
catalog serializes deterministically, and the on catalog is exactly the
off catalog plus the tomb's rows, so flag-off cannot drift (GH #1013).
================
*/
func TestQinShiOffCatalogIsByteIdentical(t *testing.T) {
	clientdata.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)

	t.Setenv(monster.EnvQinShiTomb, "off")
	off, err := loadPortalCatalog(dir)
	if err != nil {
		t.Fatalf("loadPortalCatalog(off): %v", err)
	}
	offAgain, err := loadPortalCatalog(dir)
	if err != nil {
		t.Fatalf("loadPortalCatalog(off) again: %v", err)
	}
	if canonicalPortalCatalog(off) != canonicalPortalCatalog(offAgain) {
		t.Fatal("off catalog load is not deterministic")
	}
	for zone := range off.walkIns {
		if strings.HasPrefix(zone, "GATE_JINSI") {
			t.Fatalf("tomb walk-in %s is active with the flag off", zone)
		}
	}

	t.Setenv(monster.EnvQinShiTomb, "on")
	on, err := loadPortalCatalog(dir)
	if err != nil {
		t.Fatalf("loadPortalCatalog(on): %v", err)
	}
	stripped := &portalCatalog{
		destinations: map[uint32]portalDestination{},
		sources:      map[uint32]uint32{},
		links:        map[[2]uint32]portalLink{},
		buildings:    map[string]uint32{},
		walkIns:      map[string]portalLink{},
	}
	for id, d := range on.destinations {
		if _, shipped := off.destinations[id]; shipped {
			stripped.destinations[id] = d
		}
	}
	for ref, id := range on.sources {
		if _, shipped := off.sources[ref]; shipped {
			stripped.sources[ref] = id
		}
	}
	for key, link := range on.links {
		if _, shipped := off.links[key]; shipped {
			stripped.links[key] = link
		}
	}
	stripped.buildings = on.buildings
	for zone, link := range on.walkIns {
		if _, shipped := off.walkIns[zone]; shipped {
			stripped.walkIns[zone] = link
		}
	}
	if canonicalPortalCatalog(stripped) != canonicalPortalCatalog(off) {
		t.Fatal("the on catalog is not the off catalog plus tomb rows")
	}
}

/*
================
TestQinShiTombClosedLinkGraph

With the flag on, every tomb link lands on a real destination with a
real landing point, every tomb walk-in is free, and the Jangan door
keeps its level-70..999 gate (GH #1013).
================
*/
func TestQinShiTombClosedLinkGraph(t *testing.T) {
	clientdata.RequireGameData(t)
	t.Setenv(monster.EnvQinShiTomb, "on")
	catalog, err := loadPortalCatalog(gamedatatest.TextdataDir(t))
	if err != nil {
		t.Fatalf("loadPortalCatalog(on): %v", err)
	}
	tombLinks := 0
	for _, link := range catalog.links {
		src, srcOK := catalog.destinations[link.source]
		dst, dstOK := catalog.destinations[link.target]
		if !srcOK || !dstOK {
			t.Fatalf("link %d>%d misses a destination", link.source, link.target)
		}
		if !strings.HasPrefix(src.code, "GATE_JINSI") && !strings.HasPrefix(dst.code, "GATE_JINSI") {
			continue
		}
		tombLinks++
		if dst.spawn.RegionID == 0 {
			t.Fatalf("tomb link %s>%s lands in region 0", src.code, dst.code)
		}
	}
	if tombLinks == 0 {
		t.Fatal("no tomb links in the on catalog")
	}
	for zone, link := range catalog.walkIns {
		if !strings.HasPrefix(zone, "GATE_JINSI") {
			continue
		}
		if link.fee != 0 {
			t.Fatalf("tomb walk-in %s charges %d", zone, link.fee)
		}
		if catalog.destinations[link.target].spawn.RegionID == 0 {
			t.Fatalf("tomb walk-in %s lands in region 0", zone)
		}
	}
	door, ok := catalog.walkIns["GATE_JINSI_OUT"]
	if !ok {
		t.Fatal("GATE_JINSI_OUT is not a walk-in with the flag on")
	}
	if got := catalog.destinations[door.target]; got.code != "GATE_JINSI_IN" || got.spawn.RegionID != 0x8007 {
		t.Fatalf("the tomb door lands on %+v, want GATE_JINSI_IN in 0x8007", got)
	}
	wantConditions := []portalCondition{{1, 70, 999}, {portalConditionTransport, 0, 0}}
	if len(door.conditions) != len(wantConditions) {
		t.Fatalf("door conditions %+v, want %+v", door.conditions, wantConditions)
	}
	for i, want := range wantConditions {
		if door.conditions[i] != want {
			t.Fatalf("door conditions %+v, want %+v", door.conditions, wantConditions)
		}
	}
}

/*
================
TestQinShiTombEveryFloorReachable

The closed-graph test proves every tomb link lands somewhere real; this
proves the chain is walkable end to end (#1800 step 2): from the Jangan
door's landing on Floor 1 (0x8007), following tomb link edges region to
region, every one of the six floors (0x8002..0x8007) is reachable. The
qinshi family's nests stand on those floors; a floor no edge reaches
would be population no player can ever meet. Intra-floor walking is the
dungeon navmesh's proof (movement.TestPublishedDungeonPortalTraversal),
not this one's.
================
*/
func TestQinShiTombEveryFloorReachable(t *testing.T) {
	clientdata.RequireGameData(t)
	t.Setenv(monster.EnvQinShiTomb, "on")
	catalog, err := loadPortalCatalog(gamedatatest.TextdataDir(t))
	if err != nil {
		t.Fatalf("loadPortalCatalog(on): %v", err)
	}
	edges := map[uint16][]uint16{}
	for _, link := range catalog.links {
		src, srcOK := catalog.destinations[link.source]
		dst, dstOK := catalog.destinations[link.target]
		if !srcOK || !dstOK {
			t.Fatalf("link %d>%d misses a destination", link.source, link.target)
		}
		if !strings.HasPrefix(src.code, "GATE_JINSI") && !strings.HasPrefix(dst.code, "GATE_JINSI") {
			continue
		}
		from, to := src.spawn.RegionID, dst.spawn.RegionID
		if from != 0 && to != 0 && from != to {
			edges[from] = append(edges[from], to)
		}
	}
	reached := map[uint16]bool{0x8007: true}
	queue := []uint16{0x8007}
	for len(queue) > 0 {
		region := queue[0]
		queue = queue[1:]
		for _, next := range edges[region] {
			if !reached[next] {
				reached[next] = true
				queue = append(queue, next)
			}
		}
	}
	for floor := uint16(0x8002); floor <= 0x8007; floor++ {
		if !reached[floor] {
			t.Fatalf("floor %#x is not reachable from the tomb door's landing on 0x8007", floor)
		}
	}
}

/*
================
placeTombDoorWalker

Stands the player at the tomb door's zone (region 0x66AC).
================
*/
func placeTombDoorWalker(rt *Runtime, c *enterworld.Character) {
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = simulation.Spawn{RegionID: 0x66ac, X: 961, Y: 150.4, Z: 1828}
			w.SpawnSet = true
			w.MoveSegment = nil
		})
}

/*
================
placeOutsideTombDoor

Stands the player west of the tomb door's zone, still in region 0x66AC.
================
*/
func placeOutsideTombDoor(rt *Runtime, c *enterworld.Character) {
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = simulation.Spawn{RegionID: 0x66ac, X: 900, Y: 150.4, Z: 1800}
			w.SpawnSet = true
			w.MoveSegment = nil
		})
}

/*
================
TestWalkingIntoTheTombDoorLevels

The reference server's NPC teleport command at the tomb's own door: a level-69 player is refused by the
door link's level condition (the teleport answer's 0x15) and stays; a
level-70 player arrives on Floor 1 (region 0x8007) with nothing charged.
================
*/
func TestWalkingIntoTheTombDoorLevels(t *testing.T) {
	clientdata.RequireGameData(t)
	t.Setenv(monster.EnvQinShiTomb, "on")
	rt, c, _, _ := returnFixture(t, 30000)
	if err := rt.ConfigurePortals(gamedatatest.TextdataDir(t)); err != nil {
		t.Fatal(err)
	}
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(nil, nil))
	// The door's event zone sits at the east of the region, like the cave
	// mouth's lookup in walkin_test.go.
	rt.WalkInZoneAt = func(at simulation.Spawn) string {
		if at.RegionID == 0x66ac && at.X > 940 {
			return "GATE_JINSI_OUT"
		}
		return ""
	}
	pushed := &[][]wire.Frame{}
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { *pushed = append(*pushed, frames) }
	placeOutsideTombDoor(rt, c)
	if err := rt.AdmitCharacterSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	rt.advanceWalkIns(1000)
	if len(*pushed) != 0 {
		t.Fatalf("outside the door pushed %+v", *pushed)
	}

	level := int64(69)
	c.Level = &level
	placeTombDoorWalker(rt, c)
	rt.advanceWalkIns(1100)
	if len(*pushed) != 1 || !bytes.Equal((*pushed)[0][0].Payload, []byte{2, 0x15}) {
		t.Fatalf("level-69 door entry answered %+v, want the level refusal", *pushed)
	}
	rt.advanceWalkIns(1200)
	if len(*pushed) != 1 {
		t.Fatal("standing on at the door asked again")
	}

	level = 70
	gold := *c.Gold
	placeOutsideTombDoor(rt, c)
	rt.advanceWalkIns(1300)
	placeTombDoorWalker(rt, c)
	rt.advanceWalkIns(1400)
	if len(*pushed) != 2 || (*pushed)[1][0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("level-70 door entry answered %+v, want the teleport reset", *pushed)
	}
	if uint16(*c.World.Spawn.RegionID) != 0x8007 {
		t.Fatalf("arrived in region %v, want Floor 1 (0x8007)", *c.World.Spawn.RegionID)
	}
	if *c.Gold != gold {
		t.Fatal("the tomb door charged a fee")
	}
}
