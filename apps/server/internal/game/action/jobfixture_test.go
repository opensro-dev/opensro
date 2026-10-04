/*
===========================================================================

jobfixture_test.go - a trader in job mode for transport tests

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
dressTrader

A trade transport answers only a trader or thief in job mode
(transportJob): the character joins the trader guild and wears its suit.
================
*/
func dressTrader(c *enterworld.Character) {
	c.Job = domain.CharacterJob{Type: domain.JobTrader, Grade: 1, Alias: "Trader"}
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: enterworld.JobSuitSlot,
		RefObjID: 9101, Codename: "ITEM_CH_M_TRADE_TRADER_04", TypeFlags: wire.PackTypeFlags(3, 1, 7, 1), StackCount: 1})
}
