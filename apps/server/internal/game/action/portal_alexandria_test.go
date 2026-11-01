/*
===========================================================================

portal_alexandria_test.go - the portal catalogue with SRO_ALEXANDRIA off and on

Port-only, not native. Off, an Alexandria variant's catalogue equals the
native one exactly; on, its gate, building and both links are registered.

===========================================================================
*/
package action

import (
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/alexandria"
	"opensro.online/server/internal/testsupport/alexandriatest"
	"opensro.online/server/internal/testsupport/clientdata"
)

/*
================
mustPortalCatalog
================
*/
func mustPortalCatalog(t *testing.T, dir string) *portalCatalog {
	t.Helper()
	catalog, err := loadPortalCatalog(dir)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

/*
================
TestAlexandriaPortalCatalogFollowsTheFlag
================
*/
func TestAlexandriaPortalCatalogFollowsTheFlag(t *testing.T) {
	clientdata.RequireGameData(t)
	t.Setenv(alexandria.EnvAlexandria, "")
	native := mustPortalCatalog(t, clientdata.RetailTextdataDir(t))
	// The placement row does not matter here; any native npcpos ref will do.
	variant := alexandriatest.Variant(t, firstNpcPosRef(t))

	for _, value := range []string{"", "off"} {
		t.Setenv(alexandria.EnvAlexandria, value)
		if got := mustPortalCatalog(t, variant.Dir); !reflect.DeepEqual(got, native) {
			t.Fatalf("SRO_ALEXANDRIA=%q: the catalogue differs from the native one", value)
		}
	}

	t.Setenv(alexandria.EnvAlexandria, "on")
	on := mustPortalCatalog(t, variant.Dir)
	linked, err := strconv.ParseUint(variant.LinkedTeleport, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	gate, other := uint32(alexandriatest.TeleportID), uint32(linked)
	if destination, ok := on.destinations[gate]; !ok || destination.code != alexandriatest.TeleportCode {
		t.Fatalf("on: gate %d = %+v (present %v)", gate, destination, ok)
	}
	if on.buildings[alexandriatest.BuildingCode] != alexandriatest.BuildingRef || on.sources[alexandriatest.BuildingRef] != gate {
		t.Fatalf("on: building %s not registered", alexandriatest.BuildingCode)
	}
	_, out := on.links[[2]uint32{gate, other}]
	_, back := on.links[[2]uint32{other, gate}]
	if !out || !back || len(on.links) != len(native.links)+2 {
		t.Fatalf("on: links out %v back %v, %d links for the native %d", out, back, len(on.links), len(native.links))
	}
}

/*
================
TestAlexandriaPortalCatalogKind7

The ferry arrivals' Restrict kind 7 loads with its condition intact (the
server evaluates it, #1749 PR-C(ii)); kinds 3-6 still refuse the whole
catalogue, as they did before any evaluator existed.
================
*/
func TestAlexandriaPortalCatalogKind7(t *testing.T) {
	clientdata.RequireGameData(t)
	variant := alexandriatest.Variant(t, firstNpcPosRef(t))
	t.Setenv(alexandria.EnvAlexandria, "on")
	gate := uint32(alexandriatest.TeleportID)
	linked, err := strconv.ParseUint(variant.LinkedTeleport, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	arrival := [2]uint32{uint32(linked), gate}
	target := strconv.Itoa(alexandriatest.TeleportID)
	alexandriatest.SetLinkCondition(t, variant.Dir, variant.LinkedTeleport, target, portalConditionThiefSuitTransport, 0, 0)
	catalog := mustPortalCatalog(t, variant.Dir)
	link, ok := catalog.links[arrival]
	if !ok || len(link.conditions) == 0 || link.conditions[0].kind != portalConditionThiefSuitTransport {
		t.Fatalf("kind 7 arrival link: %+v (present %v)", link, ok)
	}
	c := &enterworld.Character{}
	hp, gold := int64(100), int64(1000000000)
	c.CurrentHP, c.Gold = &hp, &gold
	c.MissionInventory = []domain.InventoryRow{{Slot: enterworld.JobSuitSlot, TypeFlags: 0x3ac | 2<<11}}
	source := catalog.destinations[arrival[0]]
	// The loaded arrival also carries the kind 2 detail's condition from the
	// shipped transport table; the job-goods admission suppresses that one so
	// the refusal below can only come from the link's own kind 7.
	if got := portalAdmission(c, source, link, portalTraveller{transportCOS: true, transportAdmitted: true}); got != 0x10 {
		t.Fatalf("thief with a transport boarded: %#x", got)
	}
	if got := portalAdmission(c, source, link, portalTraveller{}); got != 0 {
		t.Fatalf("thief on foot refused: %#x", got)
	}
	for _, kind := range []uint32{3, 4, 5, 6} {
		alexandriatest.SetLinkCondition(t, variant.Dir, variant.LinkedTeleport, target, kind, 1, 0)
		if _, err := loadPortalCatalog(variant.Dir); err == nil {
			t.Fatalf("kind %d loaded", kind)
		}
	}
}

/*
================
firstNpcPosRef
================
*/
func firstNpcPosRef(t *testing.T) string {
	t.Helper()
	for _, row := range enterworld.ReadTextdataFile(filepath.Join(clientdata.RetailTextdataDir(t), "npcpos.txt")) {
		if len(row) >= 5 {
			return row[0]
		}
	}
	t.Fatal("npcpos has no rows")
	return ""
}
