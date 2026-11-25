/*
===========================================================================

caravan_highway_test.go - world-data proof for the caravan highway

The caravanai anchor chains (port-only, #3704) claim two things about the
shipped world: the terminals and ferry landings sit exactly on the
shipped NPC placements, and every consecutive pair of anchors is one
march chord on the shipped nav surface, in both directions. This test
checks both claims against the real authority data; the second is what
lets caravan agents walk the monster walk path instead of gliding.

===========================================================================
*/
package movement

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/caravanai"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

// caravanFerryCrossings are the shipped ferry portal pairs by landing
// region (the GATE_NPC_*_FERRY link pairs in the portal data).
var caravanFerryCrossings = map[[2]uint16]bool{
	{24993, 25761}: true, {25761, 24993}: true,
	{24734, 25244}: true, {25244, 24734}: true,
	{23439, 23436}: true, {23436, 23439}: true,
	{23183, 23180}: true, {23180, 23183}: true,
}

func caravanPose(wp caravanai.Waypoint) monster.Pose {
	return monster.Pose{RegionID: wp.RegionID, X: float64(wp.X), Y: float64(wp.Y), Z: float64(wp.Z)}
}

func caravanDist(p monster.Pose, wp caravanai.Waypoint) float64 {
	return worldgeom.Distance(
		worldgeom.RegionXZ{RegionID: p.RegionID, X: p.X, Z: p.Z},
		worldgeom.RegionXZ{RegionID: wp.RegionID, X: float64(wp.X), Z: float64(wp.Z)})
}

// caravanMarchReaches walks the runtime's own shape: plan a chord,
// ride it to its Rest, replan from there, until the goal is within
// arrival distance or a chord stalls (no progress). A single chord
// need not suffice — the runtime replans every tick the same way.
func caravanMarchReaches(v *WaterValidator, a, b caravanai.Waypoint) bool {
	cur := caravanPose(a)
	goal := caravanPose(b)
	for step := 0; step < 64; step++ {
		if caravanDist(cur, b) <= 24 {
			return true
		}
		path := v.PlanMonsterPath(cur, goal)
		if path == nil {
			return false
		}
		rest := path.Rest()
		if worldgeom.Distance(
			worldgeom.RegionXZ{RegionID: rest.RegionID, X: rest.X, Z: rest.Z},
			worldgeom.RegionXZ{RegionID: cur.RegionID, X: cur.X, Z: cur.Z}) < 1 {
			return false
		}
		cur = rest
	}
	return caravanDist(cur, b) <= 24
}

func caravanRoutes() [][]caravanai.Waypoint {
	hot := map[uint16]int{
		caravanai.RegionJanganFerry:       caravanai.DetourHeatThreshold,
		caravanai.RegionDonwhangFerryWest: caravanai.DetourHeatThreshold,
	}
	towns := []caravanai.TownID{caravanai.TownJangan, caravanai.TownDonwhang, caravanai.TownHotan}
	var routes [][]caravanai.Waypoint
	for _, heat := range []map[uint16]int{nil, hot} {
		for _, from := range towns {
			for _, to := range towns {
				if from != to {
					routes = append(routes, caravanai.SelectRoute(from, to, 5, caravanai.PersonalityGreedy, heat))
				}
			}
		}
	}
	return routes
}

/*
==================
TestCaravanStopsMatchShippedNpcs

Every terminal and ferry landing anchor must sit on the shipped NPC
placement of the same codename (specialty traders, ferry gates) in the
server's NPC roster, region and position alike.
==================
*/
func TestCaravanStopsMatchShippedNpcs(t *testing.T) {
	roster := simulation.LoadNpcWorldRoster(gamedatatest.TextdataDir(t))
	if len(roster) == 0 {
		t.Fatal("empty NPC roster")
	}
	byCodename := make(map[string]simulation.Spawn)
	for _, npc := range roster {
		if npc.AuthoredSpawn {
			byCodename[npc.Codename] = npc.Spawn
		}
	}

	for _, town := range []caravanai.TownID{caravanai.TownJangan, caravanai.TownDonwhang, caravanai.TownHotan} {
		terminal := caravanai.TownWaypoint(town)
		spawn, ok := byCodename[terminal.SpecialtyShop]
		if !ok {
			t.Fatalf("no shipped NPC %s for the %s terminal", terminal.SpecialtyShop, town)
		}
		if spawn.RegionID != terminal.RegionID ||
			math.Abs(spawn.X-float64(terminal.X)) > 0.5 ||
			math.Abs(spawn.Z-float64(terminal.Z)) > 0.5 {
			t.Fatalf("%s terminal at region %d (%.2f, %.2f), shipped %s at region %d (%.2f, %.2f)",
				town, terminal.RegionID, terminal.X, terminal.Z,
				terminal.SpecialtyShop, spawn.RegionID, spawn.X, spawn.Z)
		}
	}

	checked := make(map[uint16]bool)
	for _, route := range caravanRoutes() {
		for _, wp := range route {
			if !wp.Ferry || checked[wp.RegionID] {
				continue
			}
			checked[wp.RegionID] = true
			found := false
			for _, spawn := range byCodename {
				if spawn.RegionID != wp.RegionID {
					continue
				}
				if math.Abs(spawn.X-float64(wp.X)) <= 2 && math.Abs(spawn.Z-float64(wp.Z)) <= 2 {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("ferry landing %q (region %d, %.2f, %.2f) matches no shipped NPC placement", wp.Name, wp.RegionID, wp.X, wp.Z)
			}
		}
	}
	if len(checked) != 8 {
		t.Fatalf("expected 8 ferry landings across the routes, checked %d", len(checked))
	}
}

/*
==================
TestCaravanHighwayMarches

Every consecutive anchor pair in every route (both heat variants, all
six town directions) is either a shipped ferry crossing (two Ferry
anchors) or a chord the monster march walks in both directions.
==================
*/
func TestCaravanHighwayMarches(t *testing.T) {
	v := realAuthorityValidator(t)
	seen := make(map[[2]caravanai.Waypoint]bool)
	hops := 0
	for _, route := range caravanRoutes() {
		for i := 1; i < len(route); i++ {
			a, b := route[i-1], route[i]
			if a.Ferry && b.Ferry {
				if !caravanFerryCrossings[[2]uint16{a.RegionID, b.RegionID}] {
					t.Fatalf("anchors %q -> %q are both Ferry but not a shipped crossing", a.Name, b.Name)
				}
				continue
			}
			if seen[[2]caravanai.Waypoint{a, b}] {
				continue
			}
			seen[[2]caravanai.Waypoint{a, b}] = true
			hops++
			if !caravanMarchReaches(v, a, b) {
				t.Errorf("march cannot walk %q (region %d, %.1f, %.1f) -> %q (region %d, %.1f, %.1f)",
					a.Name, a.RegionID, a.X, a.Z, b.Name, b.RegionID, b.X, b.Z)
			}
			if !caravanMarchReaches(v, b, a) {
				t.Errorf("march cannot walk back %q (region %d, %.1f, %.1f) -> %q (region %d, %.1f, %.1f)",
					b.Name, b.RegionID, b.X, b.Z, a.Name, a.RegionID, a.X, a.Z)
			}
		}
	}
	if hops == 0 {
		t.Fatal("no walk hops checked")
	}
}
