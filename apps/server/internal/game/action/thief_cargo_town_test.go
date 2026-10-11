/*
===========================================================================

thief_cargo_town_test.go - a thief's transport stays out of towns (#721)

The v1.150 client's own messages state the rule: "in thief state you
cannot enter a town with a transport summoned" (0x3058 kind 1, category
0x11 code 1) and "a thief cannot summon a transport in a town" (item use
error 149).

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
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

/*
================
TestThiefWithTransportCannotEnterATown

Only the combination is refused: a thief in job mode, its transport
summoned, a town region. A trader with a transport, a thief without one,
or a battlefield region all pass.
================
*/
func TestThiefWithTransportCannotEnterATown(t *testing.T) {
	town, field := regionsByCombat(t)
	thief := testCharacter()
	dressThief(thief)
	rt, _ := newTestRuntime(thief, testCosSource(testItems()))
	if rt.ThiefCargoTownBlocked(thief, town) {
		t.Fatal("a thief without a transport was refused a town")
	}
	gid, _ := enterworld.CosObjectIDForCharacter(thief)
	thief.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100, Summoned: true}
	if !rt.ThiefCargoTownBlocked(thief, town) {
		t.Fatal("a thief with a transport entered a town")
	}
	if rt.ThiefCargoTownBlocked(thief, field) {
		t.Fatal("a thief with a transport was refused a battlefield")
	}
	trader := testCharacter()
	dressTrader(trader)
	trader.ActiveCOS = thief.ActiveCOS
	if rt.ThiefCargoTownBlocked(trader, town) {
		t.Fatal("a trader with a transport was refused a town")
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
