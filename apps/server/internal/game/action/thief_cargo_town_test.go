/*
===========================================================================

thief_cargo_town_test.go - a thief's transport stays out of towns (#721)

A thief in job mode with a summoned transport may not cross from a
battlefield region into a protected one (v1.188; 0x3058 kind 1,
category 0x11 code 1), and may not summon a transport in a protected
region (item use error 149, "Cannot recall transport in
the Thieves' Den"). Thief town (region 0x60B6) is one protected region:
walking inside it is never refused, summoning there is.

===========================================================================
*/
package action

import (
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/townarea"
)

/*
================
regionsByCombat

The first known town (player combat disallowed) and battlefield regions.
================
*/
func regionsByCombat(t *testing.T) (town, field uint16) {
	t.Helper()
	foundTown, foundField := false, false
	for id := 0; id <= 0xffff && !(foundTown && foundField); id++ {
		if townRegion(uint16(id)) && !foundTown {
			town, foundTown = uint16(id), true
		} else if allowed, known := worldgeom.RegionPlayerCombat(uint16(id)); known && allowed && !foundField {
			field, foundField = uint16(id), true
		}
	}
	if !foundTown || !foundField {
		t.Fatal("no town or battlefield region in the combat table")
	}
	return town, field
}

/*
================
dressThief
================
*/
func dressThief(c *enterworld.Character) {
	c.Job = domain.CharacterJob{Type: domain.JobThief, Grade: 1, Alias: "Thief"}
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: enterworld.JobSuitSlot,
		RefObjID: 9102, Codename: "ITEM_CH_M_TRADE_THIEF_04", TypeFlags: wire.PackTypeFlags(3, 1, 7, 2), StackCount: 1})
}

// thiefTown is the Thieves' Den's one region, where its traders stand
// (npcpos.txt NPC_TD_THIEF_SELL/BUY).
const thiefTown = 0x60B6

/*
================
TestThiefWithTransportCannotEnterATown

Only the combination is refused: a thief in job mode, its transport
summoned, crossing from a battlefield into a protected region. A trader
with a transport, a thief without one, a move into a battlefield, or a move
within a protected region all pass.
================
*/
func TestThiefWithTransportCannotEnterATown(t *testing.T) {
	town, field := regionsByCombat(t)
	thief := testCharacter()
	dressThief(thief)
	rt, _ := newTestRuntime(thief, testCosSource(testItems()))
	if rt.ThiefCargoTownBlocked(thief, field, town) {
		t.Fatal("a thief without a transport was refused a town")
	}
	gid, _ := enterworld.CosObjectIDForCharacter(thief)
	thief.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100, Summoned: true}
	if !rt.ThiefCargoTownBlocked(thief, field, town) {
		t.Fatal("a thief with a transport entered a town")
	}
	if rt.ThiefCargoTownBlocked(thief, town, field) || rt.ThiefCargoTownBlocked(thief, field, field) {
		t.Fatal("a thief with a transport was refused a battlefield")
	}
	// Inside a protected region nothing is crossed: the thief walks the
	// Thieves' Den (and a town it is already in) to sell its goods.
	if townRegion(thiefTown) == false {
		t.Fatalf("thief town %#x is not a protected region", thiefTown)
	}
	if rt.ThiefCargoTownBlocked(thief, thiefTown, thiefTown) || rt.ThiefCargoTownBlocked(thief, town, town) {
		t.Fatal("a thief with a transport was refused a move within a protected region")
	}
	trader := testCharacter()
	dressTrader(trader)
	trader.ActiveCOS = thief.ActiveCOS
	if rt.ThiefCargoTownBlocked(trader, field, town) {
		t.Fatal("a trader with a transport was refused a town")
	}
}

/*
================
TestThiefWithTransportStopsAtTownWalls

#1985, port-only (SRO_THIEF_CART_TOWN_AREAS): the town walls apply the
same thief-and-transport test as the region rule. The list is one town
whose core is the protected region (152,102) and whose outer part is
x 0..600 of the battlefield region (154,102).
================
*/
func TestThiefWithTransportStopsAtTownWalls(t *testing.T) {
	areas, err := townarea.Parse(strings.NewReader("#TOWN\ttown\n152\t102\tALL\n154\t102\tRECT\t0\t0\t600\t1920\n"))
	if err != nil || areas.Len() != 1 {
		t.Fatalf("town list: %d parts, %v", areas.Len(), err)
	}
	outer := worldgeom.RegionIDForSectors(154, 102)
	from := worldgeom.RegionXZ{RegionID: outer, X: 900, Z: 900}
	to := worldgeom.RegionXZ{RegionID: outer, X: 300, Z: 900}

	thief := testCharacter()
	dressThief(thief)
	rt, _ := newTestRuntime(thief, testCosSource(testItems()))
	gid, _ := enterworld.CosObjectIDForCharacter(thief)
	thief.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100, Summoned: true}
	if rt.ThiefCargoTownAreaEntered(thief, from, to) {
		t.Fatal("the walls stopped a thief with the switch off")
	}
	rt.ThiefCartTownAreas = areas
	if !rt.ThiefCargoTownAreaEntered(thief, from, to) {
		t.Fatal("a thief with a transport walked through a town wall")
	}
	if rt.ThiefCargoTownAreaEntered(thief, to, from) {
		t.Fatal("a thief with a transport was stopped leaving a town")
	}
	trader := testCharacter()
	dressTrader(trader)
	trader.ActiveCOS = thief.ActiveCOS
	if rt.ThiefCargoTownAreaEntered(trader, from, to) {
		t.Fatal("a trader with a transport was stopped at a town wall")
	}
	thief.ActiveCOS = nil
	if rt.ThiefCargoTownAreaEntered(thief, from, to) {
		t.Fatal("a thief without a transport was stopped at a town wall")
	}
}

/*
================
TestThiefCannotSummonATransportInTown

The default start position is a town: a thief's transport summon is
refused with error 149 and the scroll stays; a trader's is not refused
for this reason.
================
*/
func TestThiefCannotSummonATransportInTown(t *testing.T) {
	thief := testCharacter()
	dressThief(thief)
	thief.MissionInventory = append(thief.MissionInventory, enterworld.InventoryRow{Slot: 22, RefObjID: 3905,
		Codename: "ITEM_COS_T_DHORSE3", TypeFlags: wire.PackTypeFlags(3, 3, 3, 2), StackCount: 1})
	rt, _ := newTestRuntime(thief, testCosSource(testItems()))
	at := rt.liveSpawn(simulation.WorldKey(testDivision, thief.Name), thief, rt.Now().UnixMilli())
	if !townRegion(at.RegionID) {
		t.Fatalf("the default start region %#x is not a town", at.RegionID)
	}
	assertItemUseRefusedUnchanged(t, rt, thief, []byte{22, 0xec, 0x11}, errCodeThiefCartInTown)
	if thief.ActiveCOS != nil {
		t.Fatal("the refused summon left a transport")
	}
}
