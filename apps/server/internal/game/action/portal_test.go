package action

import (
	"bytes"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"strings"
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
	if got := portalAdmission(c, source, catalog.links[[2]uint32{1, 2}], portalTraveller{questMask: 0x40000}); got != 0x17 {
		t.Fatalf("quest gate refusal %x", got)
	}
	source = catalog.destinations[3]
	if source.building {
		t.Fatal("ferry misclassified as building")
	}
	if got := portalAdmission(c, source, catalog.links[[2]uint32{3, 9}], portalTraveller{questMask: 0x40000}); got != 0 {
		t.Fatalf("quest flag blocked ferry %x", got)
	}
	link := portalLink{conditions: []portalCondition{{kind: 2}}}
	if got := portalAdmission(c, source, link, portalTraveller{transportCOS: true}); got != 0x10 {
		t.Fatalf("unmounted transport admitted: %x", got)
	}
	c.Gold = nil
	if got := portalAdmission(c, source, link, portalTraveller{}); got != 0 {
		t.Fatalf("free link required gold: %x", got)
	}
	link.fee = 1
	if got := portalAdmission(c, source, link, portalTraveller{}); got != 7 {
		t.Fatalf("paid link admitted without gold: %x", got)
	}
}

/*
================
TestPortalThiefSuitFerryAdmission

Restrict kind 7, the Alexandria ferry arrivals' condition: a traveller
wearing a thief suit with a transport summoned is refused 0x1C10; either
fact missing admits. The port-only job-goods admission belongs to kind 2
and does not reach kind 7, and the conditions decide in the row's order.
================
*/
func TestPortalThiefSuitFerryAdmission(t *testing.T) {
	c := &enterworld.Character{}
	hp, gold, level := int64(100), int64(50000), int64(30)
	c.CurrentHP, c.Gold, c.Level = &hp, &gold, &level
	thiefSuit := []domain.InventoryRow{{Slot: enterworld.JobSuitSlot, TypeFlags: 0x3ac | 2<<11}}
	traderSuit := []domain.InventoryRow{{Slot: enterworld.JobSuitSlot, TypeFlags: 0x3ac | 1<<11}}
	ferry := portalLink{conditions: []portalCondition{{kind: portalConditionThiefSuitTransport}}}
	for _, tc := range []struct {
		name      string
		worn      []domain.InventoryRow
		traveller portalTraveller
		want      byte
	}{
		{"thief suit and a transport", thiefSuit, portalTraveller{transportCOS: true}, 0x10},
		{"thief suit on foot", thiefSuit, portalTraveller{}, 0},
		{"trader suit and a transport", traderSuit, portalTraveller{transportCOS: true}, 0},
		{"no suit and a transport", nil, portalTraveller{transportCOS: true}, 0},
		{"job-goods admission does not reach kind 7", thiefSuit, portalTraveller{transportCOS: true, transportAdmitted: true}, 0x10},
	} {
		c.MissionInventory = tc.worn
		if got := portalAdmission(c, portalDestination{}, ferry, tc.traveller); got != tc.want {
			t.Errorf("%s: %#x, want %#x", tc.name, got, tc.want)
		}
	}
	c.MissionInventory = thiefSuit
	ordered := portalLink{conditions: []portalCondition{
		{kind: portalConditionThiefSuitTransport},
		{kind: 1, minimum: 50, maximum: 90},
	}}
	if got := portalAdmission(c, portalDestination{}, ordered, portalTraveller{transportCOS: true}); got != 0x10 {
		t.Errorf("kind 7 first: %#x, want 0x10", got)
	}
	ordered.conditions[0], ordered.conditions[1] = ordered.conditions[1], ordered.conditions[0]
	if got := portalAdmission(c, portalDestination{}, ordered, portalTraveller{transportCOS: true}); got != 0x15 {
		t.Errorf("level first: %#x, want 0x15", got)
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

// TestFortressGateFeeTaxFillsTheTreasury: a gate bound to a fortress
// (teleportbuilding column 55) shows the holder's ratio on select, charges
// it on the fee and credits the tax to the treasury once the move holds
// (as the native teleport use does).
func TestFortressGateFeeTaxFillsTheTreasury(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, _, _ := returnFixture(t, 30000)
	rt.NpcSpawn.Enabled, rt.NpcSpawn.AtPlayer = true, true
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 252094, RefObjID: 2094, Codename: "STORE_CH", FortressID: 1,
		Teleport: &simulation.TeleportGateBounds{Radius: 10, Height: 25, FortressID: 1}}}
	if err := rt.ConfigurePortals(gamedatatest.TextdataDir(t)); err != nil {
		t.Fatal(err)
	}
	bindMerchantTax(t, rt, 2094, 10, 0)
	out := rt.HandleObjectSelect(testDivision, c, wire.NewWriter(4).U32(252094).Payload())
	want := wire.NewWriter(11).U8(1).U32(252094).U32(0xc0).U16(10).Payload()
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, want) {
		t.Fatalf("select did not carry the fortress ratio: %+v", out)
	}
	base := uint64(rt.portals.links[[2]uint32{1, 2}].fee)
	fee, ok := commerce.AdjustPrice(base, commerce.Tax{Percent: 10, Precision: fortressTaxPrecision}, true)
	if !ok || fee <= base {
		t.Fatalf("expected a taxed fee above %d, got %d", base, fee)
	}
	// Gold covering the base fee but not its tax is refused (portalAdmission 7).
	*c.Gold = int64(fee) - 1
	request := wire.NewWriter(9).U32(252094).U8(2).U32(2).Payload()
	refused := rt.HandlePortal(testDivision, c, request)
	if len(refused.Frames) != 1 || !bytes.Equal(refused.Frames[0].Payload, []byte{2, 7}) {
		t.Fatalf("taxed fee above the purse was not refused: %+v", refused)
	}
	*c.Gold = int64(fee) + 1000
	before := *c.Gold
	result := rt.HandlePortal(testDivision, c, request)
	if len(result.Frames) == 0 || result.Frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("gate travel with gold %d, fee %d: %+v", before, fee, result)
	}
	record, _ := rt.Fortresses.Get(testDivision, 1)
	if *c.Gold != before-int64(fee) || record.TaxGold != int64(fee-base) {
		t.Fatalf("fee %d of base %d: gold %d -> %d, treasury %d", fee, base, before, *c.Gold, record.TaxGold)
	}
}

/*
================
TestGateRefusesAFarTransport

The native teleport use case 2: a summoned transport standing beyond gateTransportRange is
refused 0x1C13 on any link, after the quest bit and before the link's own
conditions. A near transport then meets the link's Restrict: kind 2 (the
town gates) refuses it 0x1C10, a link without it (a ferry) lets it pass.
================
*/
func TestGateRefusesAFarTransport(t *testing.T) {
	c := &enterworld.Character{}
	hp, gold := int64(100), int64(50000)
	c.CurrentHP, c.Gold = &hp, &gold
	town := portalDestination{building: true}
	restricted := portalLink{conditions: []portalCondition{{kind: portalConditionTransport}}}
	for _, tc := range []struct {
		name      string
		source    portalDestination
		link      portalLink
		traveller portalTraveller
		want      byte
	}{
		{"near transport through a town gate", town, restricted, portalTraveller{transportCOS: true}, 0x10},
		{"far transport through a town gate", town, restricted, portalTraveller{transportCOS: true, transportFar: true}, 0x13},
		{"far transport on a ferry", portalDestination{}, portalLink{}, portalTraveller{transportCOS: true, transportFar: true}, 0x13},
		{"near transport on a ferry", portalDestination{}, portalLink{}, portalTraveller{transportCOS: true}, 0},
		{"quest bit before range", town, restricted, portalTraveller{questMask: 0x40000, transportCOS: true, transportFar: true}, 0x17},
		{"no transport", town, restricted, portalTraveller{transportFar: true}, 0},
	} {
		if got := portalAdmission(c, tc.source, tc.link, tc.traveller); got != tc.want {
			t.Errorf("%s: %#x, want %#x", tc.name, got, tc.want)
		}
	}
}

/*
================
TestShippedTownGatesRefuseATransportAndFerriesAllowIt

The shipped links take Restrict kind 2 from the server databases
(portaltransport_generated.go), not from the client's teleportlink.txt:
the four town gate links (Jangan-Donwhang, Donwhang-Hotan) refuse a near summoned
transport 0x1C10, and all 30 ferry, flyship and tunnel links let it through, still
behind the 1000-unit range (#916).
================
*/
func TestShippedTownGatesRefuseATransportAndFerriesAllowIt(t *testing.T) {
	licensed.RequireGameData(t)
	catalog, err := loadPortalCatalog(gamedatatest.TextdataDir(t))
	if err != nil {
		t.Fatal(err)
	}
	c := &enterworld.Character{}
	hp, gold, level := int64(100), int64(50000), int64(30)
	c.CurrentHP, c.Gold, c.Level = &hp, &gold, &level
	near := portalTraveller{transportCOS: true}
	far := portalTraveller{transportCOS: true, transportFar: true}
	gates, crossings := 0, 0
	for key, link := range catalog.links {
		source, target := catalog.destinations[key[0]], catalog.destinations[key[1]]
		got := portalAdmission(c, source, link, near)
		switch {
		case isTownGate(source.code) && isTownGate(target.code):
			gates++
			if got != 0x10 {
				t.Errorf("%s -> %s admitted a transport: %#x", source.code, target.code, got)
			}
		case isCrossing(source.code):
			crossings++
			if got != 0 {
				t.Errorf("%s -> %s refused a near transport: %#x", source.code, target.code, got)
			}
			if got := portalAdmission(c, source, link, far); got != 0x13 {
				t.Errorf("%s -> %s admitted a far transport: %#x", source.code, target.code, got)
			}
		}
	}
	if gates != 4 || crossings != 30 {
		t.Fatalf("census: %d town gate links, %d crossings", gates, crossings)
	}
}

/*
================
isTownGate
================
*/
func isTownGate(code string) bool {
	return code == "GATE_CH" || code == "GATE_WC" || code == "GATE_KT"
}

/*
================
isCrossing

A ferry, flyship or tunnel teleport.
================
*/
func isCrossing(code string) bool {
	return strings.Contains(code, "_FERRY") || strings.Contains(code, "_FLYSHIP") || strings.Contains(code, "_TUNNEL")
}
