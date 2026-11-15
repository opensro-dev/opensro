/*
===========================================================================

jangan_tomb_teleport_test.go - the Jangan gate's Qin-Shi Tomb floor rows

Port-only, not native: SRO_JANGAN_TOMB_TELEPORT adds seven links from the
Jangan town gate (teleport 1) to the tomb floors 1-4. Off, or with the
tomb itself closed, the gate keeps its native menu.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

// janganTombFloors are the tomb floor 1-4 arrival rows the gate links to:
// floors 1-3, then the four floor 4 corners (NW, NE, SW, SE).
var janganTombFloors = []uint32{50, 53, 120, 129, 126, 128, 127}

// janganTombFee is the Alexandria rows' fee from the same gate.
const janganTombFee = 40000

/*
================
TestJanganTombTeleportLinksFollowBothFlags

The links exist only with both SRO_JANGAN_TOMB_TELEPORT and
SRO_QINSHI_TOMB on, and carry the Alexandria terms: 40000 gold, level
70-999, no transport.
================
*/
func TestJanganTombTeleportLinksFollowBothFlags(t *testing.T) {
	clientdata.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	for _, posture := range []struct{ teleport, tomb string }{{"off", "on"}, {"on", "off"}} {
		t.Setenv(EnvJanganTombTeleport, posture.teleport)
		t.Setenv(monster.EnvQinShiTomb, posture.tomb)
		catalog, err := loadPortalCatalog(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, floor := range janganTombFloors {
			if _, ok := catalog.links[[2]uint32{1, floor}]; ok {
				t.Fatalf("teleport %s, tomb %s: link 1->%d present", posture.teleport, posture.tomb, floor)
			}
		}
	}

	t.Setenv(EnvJanganTombTeleport, "on")
	t.Setenv(monster.EnvQinShiTomb, "on")
	catalog, err := loadPortalCatalog(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, floor := range janganTombFloors {
		link, ok := catalog.links[[2]uint32{1, floor}]
		if !ok {
			t.Fatalf("link 1->%d missing", floor)
		}
		if link.fee != janganTombFee {
			t.Fatalf("link 1->%d fee %d", floor, link.fee)
		}
		level, transport := false, false
		for _, condition := range link.conditions {
			switch condition.kind {
			case 1:
				level = condition.minimum == 70 && condition.maximum == 999
			case portalConditionTransport:
				transport = true
			}
		}
		if !level || !transport {
			t.Fatalf("link 1->%d conditions %+v", floor, link.conditions)
		}
	}
}

/*
================
TestJanganTombTeleportTravel

A level 69 traveller is refused and keeps the gold; at level 70 the
Jangan gate carries the character to the floor 2 arrival point (the
floor 1->2 passage, teleport 53) for the fee.
================
*/
func TestJanganTombTeleportTravel(t *testing.T) {
	clientdata.RequireGameData(t)
	t.Setenv(EnvJanganTombTeleport, "on")
	t.Setenv(monster.EnvQinShiTomb, "on")
	rt, c, _, _ := returnFixture(t, 30000)
	rt.NpcSpawn.Enabled, rt.NpcSpawn.AtPlayer = true, true
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 252094, RefObjID: 2094, Codename: "STORE_CH", Teleport: &simulation.TeleportGateBounds{Radius: 10, Height: 25, FortressID: 1}}}
	if err := rt.ConfigurePortals(gamedatatest.TextdataDir(t)); err != nil {
		t.Fatal(err)
	}
	rt.HandleObjectSelect(testDivision, c, wire.NewWriter(4).U32(252094).Payload())
	gold := int64(1000000)
	c.Gold = &gold
	request := wire.NewWriter(9).U32(252094).U8(2).U32(53).Payload()

	level := int64(69)
	c.Level = &level
	if refused := rt.HandlePortal(testDivision, c, request); len(refused.Frames) == 0 || refused.Frames[0].Opcode == enterworld.OpcodeResetClient {
		t.Fatalf("level 69 travelled: %+v", refused)
	}
	if *c.Gold != 1000000 {
		t.Fatalf("refusal charged: %d", *c.Gold)
	}

	level = 70
	result := rt.HandlePortal(testDivision, c, request)
	if len(result.Frames) == 0 || result.Frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("gate travel: %+v", result)
	}
	if *c.Gold != 1000000-janganTombFee {
		t.Fatalf("fee: gold %d", *c.Gold)
	}
	arrival := rt.portals.destinations[53].spawn
	if c.World.Spawn == nil || c.World.Spawn.RegionID == nil || *c.World.Spawn.RegionID != int64(arrival.RegionID) {
		t.Fatalf("arrived at %+v, want region %d", c.World.Spawn, arrival.RegionID)
	}
}
