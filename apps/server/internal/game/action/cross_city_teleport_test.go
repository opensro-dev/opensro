/*
===========================================================================

cross_city_teleport_test.go - the direct town-to-town gate links

Port-only, not native: SRO_CROSS_CITY_TELEPORT links every Silk Road town
gate to the other four. Off, each gate keeps its native neighbours only.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

// crossCityTowns are the town gates in Silk Road order: Jangan, Donwhang,
// Hotan, Samarkand, Constantinople. Native links join neighbours only.
var crossCityTowns = []uint32{1, 2, 11, 34, 25}

// crossCityHopFee is what one native town gate trip costs.
const crossCityHopFee = 5000

/*
================
TestCrossCityTeleportLinksEveryTownPair

Off, a town gate reaches only its neighbours on the road. On, every
ordered pair is linked, for 5000 gold per native trip the link replaces,
with no level condition and the transport refusal of the town gates.
================
*/
func TestCrossCityTeleportLinksEveryTownPair(t *testing.T) {
	clientdata.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	t.Setenv(EnvCrossCityTeleport, "off")
	native, err := loadPortalCatalog(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvCrossCityTeleport, "on")
	catalog, err := loadPortalCatalog(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i, source := range crossCityTowns {
		for j, target := range crossCityTowns {
			if i == j {
				continue
			}
			hops := int64(i - j)
			if hops < 0 {
				hops = -hops
			}
			key := [2]uint32{source, target}
			if _, ok := native.links[key]; ok != (hops == 1) {
				t.Fatalf("off: link %d->%d present %v", source, target, ok)
			}
			link, ok := catalog.links[key]
			if !ok {
				t.Fatalf("on: link %d->%d missing", source, target)
			}
			if link.fee != hops*crossCityHopFee {
				t.Fatalf("link %d->%d fee %d, want %d", source, target, link.fee, hops*crossCityHopFee)
			}
			transport := false
			for _, condition := range link.conditions {
				if condition.kind != portalConditionTransport {
					t.Fatalf("link %d->%d conditions %+v", source, target, link.conditions)
				}
				transport = true
			}
			if !transport {
				t.Fatalf("link %d->%d admits a transport", source, target)
			}
		}
	}
}

/*
================
TestCrossCityTeleportTravel

The Jangan gate carries a character straight to the Constantinople gate
(teleport 25) for four trips' fee.
================
*/
func TestCrossCityTeleportTravel(t *testing.T) {
	clientdata.RequireGameData(t)
	t.Setenv(EnvCrossCityTeleport, "on")
	rt, c, _, _ := returnFixture(t, 30000)
	rt.NpcSpawn.Enabled, rt.NpcSpawn.AtPlayer = true, true
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 252094, RefObjID: 2094, Codename: "STORE_CH", Teleport: &simulation.TeleportGateBounds{Radius: 10, Height: 25, FortressID: 1}}}
	if err := rt.ConfigurePortals(gamedatatest.TextdataDir(t)); err != nil {
		t.Fatal(err)
	}
	rt.HandleObjectSelect(testDivision, c, wire.NewWriter(4).U32(252094).Payload())
	gold := int64(1000000)
	c.Gold = &gold
	result := rt.HandlePortal(testDivision, c, wire.NewWriter(9).U32(252094).U8(2).U32(25).Payload())
	if len(result.Frames) == 0 || result.Frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("gate travel: %+v", result)
	}
	if *c.Gold != 1000000-4*crossCityHopFee {
		t.Fatalf("fee: gold %d", *c.Gold)
	}
	arrival := rt.portals.destinations[25].spawn
	if c.World.Spawn == nil || c.World.Spawn.RegionID == nil || *c.World.Spawn.RegionID != int64(arrival.RegionID) {
		t.Fatalf("arrived at %+v, want region %d", c.World.Spawn, arrival.RegionID)
	}
}
