/*
===========================================================================

traderepair.go - the port-only repair of trade goods stuck in a bag

Before the transfer lane refused it (costransfer.go, #3144), a trade
good could be dragged out of a companion's cargo into the personal
bag, where no native route removes it: the specialty shop buys from
the transport and the row's authored CanDrop bit bars the ground.
On character load this repair takes such rows out and refunds them,
so no bag stays permanently occupied.

===========================================================================
*/
package action

import (
	"math"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
repairBagTradeGoods

Port-only, not native: a one-time data repair for a state the
original never produces. On character load, every trade good in the
personal bag (the pickup lane's set, isPickupTradeGoods) is removed
and its owner credited the good's reference price per piece - the
cargoGoldValue basis of tradetiers.go; what was actually paid is not
stored. Each repaired row is logged with the character, codename,
count and refund. The entry path is closed now (costransfer.go), so
the repair converges: a later load finds nothing and changes
nothing. A refund that would overflow the gold balance is refused
the way the gold-credit lanes refuse it (the pickup grant checks the
same cap before it removes the heap): the rows stay and the repair
retries at the next load rather than wrapping the balance.
================
*/
func (rt *Runtime) repairBagTradeGoods(division, name string) {
	c := rt.findCharacter(division, name)
	if c == nil {
		return
	}
	items := rt.deps.ItemReferences()
	if items == nil {
		return
	}
	type repairedRow struct {
		codename string
		count    int64
		refund   uint64
	}
	var repaired []repairedRow
	rt.deps.Update(c, "bag-trade-goods-repair", func() bool {
		var refund uint64
		kept := make([]enterworld.InventoryRow, 0, len(c.MissionInventory))
		for _, row := range c.MissionInventory {
			if !isPickupTradeGoods(row.TypeFlags) {
				kept = append(kept, row)
				continue
			}
			rowRefund := uint64(0)
			if ref, found := items.ItemRefByCodename(row.Codename); found && ref != nil {
				if price, present := ref.NativeFields.Lookup("price"); present && price >= 1 {
					rowRefund = uint64(price) * uint64(row.StackCount)
				}
			}
			repaired = append(repaired, repairedRow{codename: row.Codename, count: row.StackCount, refund: rowRefund})
			refund += rowRefund
		}
		if len(repaired) == 0 {
			return false
		}
		if refund > 0 && goldOf(c) > uint64(math.MaxInt64)-refund {
			log.WithFields(log.Fields{
				"character": c.Name,
				"refund":    refund,
				"gold":      goldOf(c),
			}).Warn("bag trade-goods repair: refund would overflow the gold balance; rows kept until the next load")
			repaired = nil
			return false
		}
		c.MissionInventory = kept
		if refund > 0 {
			setGold(c, goldOf(c)+refund)
		}
		return true
	})
	for _, row := range repaired {
		log.WithFields(log.Fields{
			"character": c.Name,
			"codename":  row.codename,
			"count":     row.count,
			"refund":    row.refund,
		}).Info("bag trade-goods repair: removed a trade good from the personal bag and refunded its reference price (port-only, #3144)")
	}
}
