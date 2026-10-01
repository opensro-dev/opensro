package action

import (
	"bytes"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"
)

func TestPortalPrimaryCatalogAndQuestAdmission(t *testing.T) {
	licensed.RequireGameData(t)
	catalog, err := loadPortalCatalog(gamedatatest.TextdataDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.links) != 85 {
		t.Fatalf("primary link census %d", len(catalog.links))
	}
	c := &enterworld.Character{}
	hp, gold, level := int64(100), int64(50000), int64(30)
	c.CurrentHP = &hp
	c.Gold = &gold
	c.Level = &level
	source := catalog.destinations[1]
	if !source.building {
		t.Fatal("town gate lacks authored building classification")
	}
	if got := portalAdmission(c, source, catalog.links[[2]uint32{1, 2}], 0x40000, false); got != 0x17 {
		t.Fatalf("quest gate refusal %x", got)
	}
	source = catalog.destinations[3]
	if source.building {
		t.Fatal("ferry misclassified as building")
	}
	if got := portalAdmission(c, source, catalog.links[[2]uint32{3, 9}], 0x40000, false); got != 0 {
		t.Fatalf("quest flag blocked ferry %x", got)
	}
	link := portalLink{conditions: []portalCondition{{kind: 2}}}
	if got := portalAdmission(c, source, link, 0, true); got != 0x10 {
		t.Fatalf("unmounted transport admitted: %x", got)
	}
	c.Gold = nil
	if got := portalAdmission(c, source, link, 0, false); got != 0 {
		t.Fatalf("free link required gold: %x", got)
	}
	link.fee = 1
	if got := portalAdmission(c, source, link, 0, false); got != 7 {
		t.Fatalf("paid link admitted without gold: %x", got)
	}
}

type portalRejectEntry struct{ Dependencies }

func (portalRejectEntry) ReentryPackets(string, string) ([]enterworld.Packet, bool) {
	return nil, false
}
func TestPortalSelectedNpcTransactionAndRollback(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, _, _ := returnFixture(t, 30000)
	dir := gamedatatest.TextdataDir(t)
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 2001, RefObjID: 2011, Codename: "NPC_CH_FERRY", TalkFlags: 2}}
	rt.NpcSpawn.Enabled = true
	if err := rt.ConfigurePortals(dir); err != nil {
		t.Fatal(err)
	}
	if rt.NpcRoster[0].TalkFlags&0x80 == 0 {
		t.Fatal("destination menu capability absent")
	}
	body := wire.NewWriter(9).U32(2001).U8(2).U32(9).Payload()
	original := c.Snapshot()
	rt.HandlePortal(testDivision, c, body)
	if *c.Gold != *original.Gold {
		t.Fatal("unselected portal charged")
	}
	rt.Selected.Set(testDivision, c.Name, 2001)
	before := c.Snapshot()
	deps := rt.deps
	rt.deps = portalRejectEntry{deps}
	out := rt.HandlePortal(testDivision, c, body)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, []byte{2, 2}) || *c.Gold != *before.Gold || missionSpawnFromWorld(c.World.Spawn, simulation.Spawn{}) != missionSpawnFromWorld(before.World.Spawn, simulation.Spawn{}) {
		t.Fatal("failed re-entry did not roll back")
	}
	rt.deps = deps
	out = rt.HandlePortal(testDivision, c, body)
	if len(out.Frames) == 0 || out.Frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("portal did not enter: %+v", out)
	}
	if *c.Gold != *before.Gold-500 || *c.World.Spawn.RegionID != 25761 {
		t.Fatal("wrong authoritative portal destination or fee")
	}
	paid := *c.Gold
	rt.HandlePortal(testDivision, c, body)
	if *c.Gold != paid {
		t.Fatal("duplicate request charged twice")
	}
}

func TestPortalStructureGrantAndDestinationTransaction(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, _, _ := returnFixture(t, 30000)
	// The fixture gate stands at the player, inside its hit range.
	rt.NpcSpawn.Enabled, rt.NpcSpawn.AtPlayer = true, true
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 252094, RefObjID: 2094, Codename: "STORE_CH", Teleport: &simulation.TeleportGateBounds{Radius: 10, Height: 25, FortressID: 1}}}
	if err := rt.ConfigurePortals(gamedatatest.TextdataDir(t)); err != nil {
		t.Fatal(err)
	}
	out := rt.HandleObjectSelect(testDivision, c, wire.NewWriter(4).U32(252094).Payload())
	want := wire.NewWriter(11).U8(1).U32(252094).U32(0xc0).U16(0).Payload()
	if out.Refusal != "" || len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, want) {
		t.Fatalf("native structure grant: %+v", out)
	}
	before := *c.Gold
	result := rt.HandlePortal(testDivision, c, wire.NewWriter(9).U32(252094).U8(2).U32(2).Payload())
	if len(result.Frames) == 0 || result.Frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("gate travel: %+v", result)
	}
	if *c.Gold != before-int64(rt.portals.links[[2]uint32{1, 2}].fee) {
		t.Fatal("city fee not committed")
	}
}
