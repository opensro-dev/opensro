/*
===========================================================================

cossell_test.go - the stable keeper buys a standing transport back

===========================================================================
*/
package action

import (
	"bytes"
	"math"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

// stableNpcGid is the fixture stable keeper's object id.
const stableNpcGid = 23

/*
================
stableFixture

A character beside a selected stable keeper, with a sellable donkey
(CanSell 1, SellPrice 240, as shipped) and an unsellable ride in the
character references.
================
*/
func stableFixture(t *testing.T) (*Runtime, *enterworld.Character, cosTestItemSource) {
	t.Helper()
	c := testCharacter()
	refs := testCosSource(testItems())
	refs.characters["COS_T_DONKEY"] = &enterworld.CharacterRef{
		Codename: "COS_T_DONKEY", RefObjID: 3910, TidWord: 0x11c6,
		CanRide: true, CanSell: true, SellPrice: 240,
	}
	refs.characters["COS_C_HORSE1"] = &enterworld.CharacterRef{
		Codename: "COS_C_HORSE1", RefObjID: 2191, TidWord: 0x09c6, CanRide: true,
	}
	rt, _ := newTestRuntime(c, refs)
	npc := simulation.NpcDef{ObjectID: stableNpcGid, RefObjID: 2100, Codename: "NPC_CH_HORSE",
		AuthoredSpawn: true, Spawn: simulation.SeedWorldState(c).Spawn}
	npc.Services = simulation.ResolveNpcServices(npc)
	rt.NpcSpawn.Enabled = true
	rt.NpcRoster = []simulation.NpcDef{npc}
	rt.Selected.Set(testDivision, c.Name, stableNpcGid)
	return rt, c, refs
}

/*
================
standVehicle

The character's active vehicle: summoned, standing, empty.
================
*/
func standVehicle(t *testing.T, c *enterworld.Character, rt *Runtime, refObjID uint32, codename string) {
	t.Helper()
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: refObjID, Codename: codename,
		CurrentHP: 100, Summoned: true}
	rt.BindPetSession(testDivision, c, 1)
}

func sellRequest(npc uint32, kind uint8) []byte {
	return wire.NewWriter(5).U32(npc).U8(kind).Payload()
}

/*
================
TestShippedSellColumns

The shipped characterdata: only the trade transports can be sold, for
their authored price; rides and pets cannot.
================
*/
func TestShippedSellColumns(t *testing.T) {
	clientdata.RequireGameData(t)
	items := enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	for codename, want := range map[string]int64{
		"COS_T_DONKEY":   240,
		"COS_T_HORSE1":   360,
		"COS_T_CAMEL2":   630,
		"COS_T_BEHEMOTH": 1620,
	} {
		ref, found := items.CharacterRefByCodename(codename)
		if !found || !ref.CanSell || ref.SellPrice != want {
			t.Fatalf("%s sells for %+v, want CanSell at %d", codename, ref, want)
		}
	}
	for _, codename := range []string{"COS_C_HORSE1", "COS_P_BEAR_040"} {
		ref, found := items.CharacterRefByCodename(codename)
		if !found || ref.CanSell {
			t.Fatalf("%s can be sold: %+v", codename, ref)
		}
	}
}

/*
================
TestStableKeeperBuysAStandingTransport

The sale pays the row's SellPrice, lets the record go and answers
[1, kind]; observers see the despawn.
================
*/
func TestStableKeeperBuysAStandingTransport(t *testing.T) {
	rt, c, _ := stableFixture(t)
	standVehicle(t, c, rt, 3910, "COS_T_DONKEY")
	before := goldOf(c)

	result := rt.HandleCosSell(testDivision, c, sellRequest(stableNpcGid, 3))
	last := result.Frames[len(result.Frames)-1]
	if last.Opcode != opCosSellResponse || !bytes.Equal(last.Payload, []byte{1, 3}) {
		t.Fatalf("the sale answered %v % X", last.Opcode, last.Payload)
	}
	if c.ActiveCOS != nil {
		t.Fatal("the sold transport is still out")
	}
	if goldOf(c) != before+240 {
		t.Fatalf("gold %d, want %d (the donkey's 240)", goldOf(c), before+240)
	}
	var payed, noticed bool
	for _, frame := range result.Frames {
		payed = payed || frame.Opcode == wire.OpPointsUpdate
		noticed = noticed || frame.Opcode == cosPetUpdateOpcode
	}
	if !payed || !noticed {
		t.Fatal("the owner missed the gold or removal frames", result.Frames)
	}
	if len(result.Broadcast) != 1 || result.Broadcast[0].Opcode != wire.OpObjectDespawn {
		t.Fatal("observers missed the despawn", result.Broadcast)
	}
	// The record is gone: a second sale finds no vehicle.
	again := rt.HandleCosSell(testDivision, c, sellRequest(stableNpcGid, 3))
	if !bytes.Equal(again.Frames[0].Payload, []byte{2, cosSellErrNoVehicle}) {
		t.Fatalf("a second sale answered % X", again.Frames[0].Payload)
	}
}

/*
================
TestStableKeeperRefusals

In the reference server's order: a ride cannot be sold even ridden
(CanSell first), a loaded transport keeps its goods' refusal even
ridden (goods second), a ridden empty transport third.
================
*/
func TestStableKeeperRefusals(t *testing.T) {
	rt, c, _ := stableFixture(t)

	// A ride (CanSell 0), ridden: the not-for-sale refusal wins.
	standVehicle(t, c, rt, 2191, "COS_C_HORSE1")
	c.ActiveCOS.Mounted = true
	if out := rt.HandleCosSell(testDivision, c, sellRequest(stableNpcGid, 3)); !bytes.Equal(out.Frames[0].Payload, []byte{2, cosSellErrNotForSale}) {
		t.Fatalf("a ridden ride answered % X", out.Frames[0].Payload)
	}

	// A loaded transport, ridden as well: the goods refusal wins.
	c.ActiveCOS.Codename = "COS_T_DONKEY"
	c.ActiveCOS.RefObjID = 3910
	c.ActiveCOS.Container = &domain.COSContainer{Capacity: 5,
		Rows: []domain.InventoryRow{{Slot: 0, RefObjID: 100, StackCount: 1}}}
	if out := rt.HandleCosSell(testDivision, c, sellRequest(stableNpcGid, 3)); !bytes.Equal(out.Frames[0].Payload, []byte{2, cosSellErrHasGoods}) {
		t.Fatalf("a loaded transport answered % X", out.Frames[0].Payload)
	}
	if c.ActiveCOS == nil {
		t.Fatal("a refused sale let the transport go")
	}

	// Empty but ridden: the riding refusal.
	c.ActiveCOS.Container = nil
	if out := rt.HandleCosSell(testDivision, c, sellRequest(stableNpcGid, 3)); !bytes.Equal(out.Frames[0].Payload, []byte{2, cosSellErrRiding}) {
		t.Fatalf("a ridden transport answered % X", out.Frames[0].Payload)
	}

	// A kind other than 3 finds no vehicle action at all.
	c.ActiveCOS.Mounted = false
	if out := rt.HandleCosSell(testDivision, c, sellRequest(stableNpcGid, 1)); !bytes.Equal(out.Frames[0].Payload, []byte{2, cosSellErrNoVehicle}) {
		t.Fatalf("kind 1 answered % X", out.Frames[0].Payload)
	}
}

/*
================
TestStableKeeperNeedsTheKeeper

The named keeper must be the selected one, a stable keeper, and in
hit range; every miss answers the quiet NPC code and sells nothing.
================
*/
func TestStableKeeperNeedsTheKeeper(t *testing.T) {
	rt, c, _ := stableFixture(t)
	standVehicle(t, c, rt, 3910, "COS_T_DONKEY")
	before := goldOf(c)

	if out := rt.HandleCosSell(testDivision, c, sellRequest(stableNpcGid+1, 3)); !bytes.Equal(out.Frames[0].Payload, []byte{2, cosSellErrNpc}) {
		t.Fatalf("an unnamed keeper answered % X", out.Frames[0].Payload)
	}
	moveMerchantAway(rt, npcHitRange+10)
	if out := rt.HandleCosSell(testDivision, c, sellRequest(stableNpcGid, 3)); !bytes.Equal(out.Frames[0].Payload, []byte{2, cosSellErrNpc}) {
		t.Fatalf("a distant keeper answered % X", out.Frames[0].Payload)
	}
	if c.ActiveCOS == nil || goldOf(c) != before {
		t.Fatal("a keeper-less sale moved the vehicle or the gold")
	}
}

/*
================
TestStableKeeperHoldsThePriceInBand

The paid price is the row's SellPrice held to 1..10,000,000.
================
*/
func TestStableKeeperHoldsThePriceInBand(t *testing.T) {
	for _, tc := range []struct {
		price int64
		want  uint64
	}{
		{0, 1},
		{240, 240},
		{99_000_000, 10_000_000},
	} {
		rt, c, refs := stableFixture(t)
		refs.characters["COS_T_DONKEY"].SellPrice = tc.price
		standVehicle(t, c, rt, 3910, "COS_T_DONKEY")
		before := goldOf(c)
		out := rt.HandleCosSell(testDivision, c, sellRequest(stableNpcGid, 3))
		if last := out.Frames[len(out.Frames)-1]; !bytes.Equal(last.Payload, []byte{1, 3}) {
			t.Fatalf("price %d answered % X", tc.price, last.Payload)
		}
		if goldOf(c)-before != tc.want {
			t.Fatalf("price %d paid %d, want %d", tc.price, goldOf(c)-before, tc.want)
		}
	}
}

/*
================
TestStableKeeperCreditSaturates

The sale still lands at the top of the balance: the credit saturates
at the largest balance instead of wrapping it negative.
================
*/
func TestStableKeeperCreditSaturates(t *testing.T) {
	rt, c, _ := stableFixture(t)
	standVehicle(t, c, rt, 3910, "COS_T_DONKEY")
	full := int64(math.MaxInt64 - 100)
	c.Gold = &full
	out := rt.HandleCosSell(testDivision, c, sellRequest(stableNpcGid, 3))
	if last := out.Frames[len(out.Frames)-1]; !bytes.Equal(last.Payload, []byte{1, 3}) {
		t.Fatalf("a saturating sale answered % X", last.Payload)
	}
	if goldOf(c) != uint64(math.MaxInt64) || c.ActiveCOS != nil {
		t.Fatalf("gold %d, vehicle %+v", goldOf(c), c.ActiveCOS)
	}
}

/*
================
TestStableKeeperSaleTravelsOnTheClientsOpcodes

Through the hub: the v1.150 client's sale box sends 0x71DC (6FF980)
and reads the answer on 0xB1DC (7782E0). The numbers are the client's,
written here as literals - the v1.188 server's 0x70C7 never reaches it.
================
*/
func TestStableKeeperSaleTravelsOnTheClientsOpcodes(t *testing.T) {
	const clientSellRequest, clientSellAnswer uint16 = 0x71dc, 0xb1dc
	rt, c, _ := stableFixture(t)
	standVehicle(t, c, rt, 3910, "COS_T_DONKEY")
	srv := wireStartServer(t, rt)
	client := wireConnect(t, srv, testDivision, c.Name)

	client.send(t, clientSellRequest, sellRequest(stableNpcGid, 3))
	for range 64 {
		frame := client.readFrame(t)
		if frame.Opcode != clientSellAnswer {
			continue
		}
		if !bytes.Equal(frame.Payload, []byte{1, 3}) {
			t.Fatalf("the sale answered % X, want 01 03", frame.Payload)
		}
		return
	}
	t.Fatal("no 0xB1DC answer to the client's 0x71DC sale")
}
