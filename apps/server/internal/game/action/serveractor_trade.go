/*
===========================================================================

serveractor_trade.go - what an AI caravan Trader's goods are worth
(port-only, SRO_AI_CARAVANS)

A hired Trader's goods are priced the way a player's own are: the stars
follow the cargo's gold against a full load of the same goods in the same
transport (TradeTierForLoad's fractions), and the sale at a town's shop is
the native foreign sale of each row (tradeSaleValue: the shop's trade
quotation, the Jangan route profit, the town tax).

===========================================================================
*/
package action

import (
	"strings"

	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/tunables"
)

// starLoadPermille is the least share of a full load (per mille) that earns
// each star under TradeTierForLoad; a first star asks a tenth.
var starLoadPermille = [6]uint64{0, 100, 200, 400, 700, 1000}

/*
================
ServerActorStarGoods

The gold of goods that rates stars on the AI Trader's transport: the
least cargo TradeTierForLoad gives that star, a full load being the
transport's slots of its goods' full stacks. Zero while the transport or
its goods are unknown (the caller keeps its own price).
================
*/
func (rt *Runtime) ServerActorStarGoods(division, name string, stars uint8) int64 {
	if stars < 1 || stars > 5 {
		return 0
	}
	c := rt.findCharacter(division, name)
	if c == nil || !c.RuntimeOnly {
		return 0
	}
	var full uint64
	rt.deps.Read(division, func() { full = rt.caravanFullValue(c) })
	return int64(full * starLoadPermille[stars] / 1000)
}

/*
================
ServerActorCargoSale

What the AI Trader's goods sell for at the named shop, row by row as a
player's sale is valued (tradeSaleValue: quotation, route profit, tax).
False when the shop or the goods cannot be valued.
================
*/
func (rt *Runtime) ServerActorCargoSale(division, name, shopCodename string) (int64, bool) {
	c := rt.findCharacter(division, name)
	if c == nil || !c.RuntimeOnly || rt.Commerce == nil {
		return 0, false
	}
	shop := -1
	for i := range rt.NpcRoster {
		if strings.EqualFold(rt.NpcRoster[i].Codename, shopCodename) {
			shop = i
			break
		}
	}
	if shop < 0 {
		return 0, false
	}
	npc := rt.NpcRoster[shop]
	settings := tunables.Current()
	var total int64
	valued := false
	rt.deps.Read(division, func() {
		if c.ActiveCOS == nil || c.ActiveCOS.Container == nil {
			return
		}
		for _, row := range c.ActiveCOS.Container.Rows {
			if row.StackCount <= 0 || !inventory.IsTradeGoods(row.TypeFlags) {
				continue
			}
			item := inventory.Item{RefObjID: row.RefObjID, Codename: row.Codename, TypeFlags: row.TypeFlags, Quantity: uint16(row.StackCount), TradeOwner: row.TradeOwner}
			if credit, _, _, ok := rt.tradeSaleValue(division, c, npc, item, settings); ok {
				total += int64(credit)
				valued = true
			}
		}
	})
	return total, valued
}
