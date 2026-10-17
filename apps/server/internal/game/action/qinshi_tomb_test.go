/*
===========================================================================

qinshi_tomb_test.go - verification of Qin-Shi Emperor Tomb teleport links and walk-in zones

Tests both SRO_QINSHI_TOMB off (native default) and on: 68 teleport links
(including Floor 6 exit buildings 131..134) and 64 walk-in TELEPORT event
zones (GATE_JINSI_*).

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestQinShiTombPortalsAndWalkIns
================
*/
func TestQinShiTombPortalsAndWalkIns(t *testing.T) {
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
	if got, want := len(tombCatalog.links)-len(baseCatalog.links), 68; got != want {
		t.Fatalf("added tomb teleport links = %d, want %d", got, want)
	}
	if got, want := len(tombCatalog.walkIns)-len(baseCatalog.walkIns), 64; got != want {
		t.Fatalf("added tomb walk-in zones = %d, want %d", got, want)
	}

	// Verify outer entrance walk-in GATE_JINSI_OUT (49 -> 50, requires level 70..999)
	// and inner exit walk-in GATE_JINSI_IN (50 -> 49).
	outLink, ok := tombCatalog.walkIns["GATE_JINSI_OUT"]
	if !ok || outLink.source != 49 || outLink.target != 50 {
		t.Fatalf("GATE_JINSI_OUT link = %+v, ok=%v; want 49->50", outLink, ok)
	}
	inLink, ok := tombCatalog.walkIns["GATE_JINSI_IN"]
	if !ok || inLink.source != 50 || inLink.target != 49 {
		t.Fatalf("GATE_JINSI_IN link = %+v, ok=%v; want 50->49", inLink, ok)
	}

	// Verify Floor 6 clickable exit building links (131..134 -> {53, 129, 120, 50}).
	for _, pair := range [][2]uint32{{131, 53}, {132, 129}, {133, 120}, {134, 50}} {
		if _, ok := tombCatalog.links[pair]; !ok {
			t.Fatalf("missing Floor 6 exit building link %v", pair)
		}
		src := tombCatalog.destinations[pair[0]]
		if !src.building {
			t.Fatalf("Floor 6 exit %d (%s) must be marked as a building gate", pair[0], src.code)
		}
	}
}
