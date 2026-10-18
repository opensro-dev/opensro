/*
===========================================================================

qinshi_tomb_test.go - verification of Qin-Shi Emperor Tomb teleport links and walk-in zones

Tests both SRO_QINSHI_TOMB off (native default) and on: 13 Floor 5-6
teleport destinations, 77 teleport links (including Floor 4 snake gate 125
and Floor 6 exit buildings 131..134), and 72 walk-in TELEPORT event zones
(normalized to uppercase to match WaterValidator.WalkInZoneAt).

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/licensed"
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
	licensed.RequireGameData(t)
	dir := licensed.RetailTextdataDir(t)

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

	// Verify Floor 6 center -> North wing trigger portal radius fallback (CAction_OperatePortal 0x005f0d40).
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
