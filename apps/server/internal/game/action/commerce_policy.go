/*
===========================================================================

commerce_policy.go - what a merchant charges on top of its list price, and
each shopper's buyback ledger

The adjustment is the fortress tax of the fortress a merchant or gate is
bound to; the tax it collects fills that fortress's treasury. The ledger
of sold items lives as long as the player's logical session.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/world/simulation"
)

// fortressTaxPrecision is the x87 precision CGObj_CalculateFortressTax
// (486390) divides at: the server process keeps the CRT's default control
// word 0x27F, 53-bit.
const fortressTaxPrecision = 53

/*
================
commerceTax

A merchant's or gate's adjustment, from the fortress it is bound to
(NpcDef.FortressID). CGObj_GetFortressTaxRate (4862A0): an unbound object
charges nothing, a ratio of zero or below applies to everyone, and a
positive ratio spares the holder guild and its allies (fortressDefender).
commerce.AdjustPrice applies the exemption to a positive ratio only.
================
*/
func (rt *Runtime) commerceTax(division string, npc simulation.NpcDef, c *enterworld.Character) commerce.Tax {
	if npc.FortressID == 0 || rt.Fortresses == nil {
		return commerce.Tax{}
	}
	record, ok := rt.Fortresses.Get(division, npc.FortressID)
	if !ok || record.TaxRate == 0 {
		return commerce.Tax{}
	}
	tax := commerce.Tax{Percent: record.TaxRate, Precision: fortressTaxPrecision}
	if c != nil && c.GuildID != nil {
		tax.Exempt = rt.fortressDefender(division, record, *c.GuildID)
	}
	return tax
}

/*
================
collectFortressTax

CGObj_AccumulateFortressTax (486330): the tax a purchase, a gate fee or a
trade goods sale actually paid goes to the bound fortress's treasury.
Callers pass the taxed amount minus the untaxed one, as
CShopApp_ChargePurchaseAndAccumulateTax (6186D0) does; the authority ignores
anything not positive.
================
*/
func (rt *Runtime) collectFortressTax(division string, npc simulation.NpcDef, paid int64) {
	if npc.FortressID == 0 || rt.Fortresses == nil {
		return
	}
	rt.Fortresses.AccumulateTax(division, npc.FortressID, paid)
}

// A logical player lifetime owns the ledger. Rebinding/resuming the same
// transport lifetime preserves it; replacing that lifetime clears it. The
// native server clears PC+2008 during initialization and pooled PC reset.
func (rt *Runtime) BeginCommerceSession(division string, c *enterworld.Character, session uint64) {
	if c == nil || session == 0 {
		return
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	rt.deps.Update(c, "buyback-session-begin", func() bool {
		if c.BuybackSession == session {
			return false
		}
		c.Buyback = nil
		c.BuybackSession = session
		return true
	})
}
func (rt *Runtime) EndCommerceSession(division string, c *enterworld.Character, session uint64) {
	if c == nil || session == 0 {
		return
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	rt.deps.Update(c, "buyback-session-end", func() bool {
		if c.BuybackSession != session {
			return false
		}
		c.Buyback = nil
		c.BuybackSession = 0
		return true
	})
}
