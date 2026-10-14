/*
===========================================================================

withhold.go - goods a port-only rule keeps off every NPC shelf

A shard rule that keeps an item out of NPC shops (progression.SunEventOnly)
removes its offers from the loaded catalogue once, before any shop reads
it, so the shop window, the purchase, the trade sale and the website's NPC
goods all agree. The removed offers' slots stay unused: every other offer
keeps the tab and slot the client's own refshopgoods gives it.

===========================================================================
*/
package commerce

import "opensro.online/server/internal/game/enterworld"

/*
================
Catalog.WithholdGoods

Removes every offer whose item, or any item its package holds, excluded
names; the count is the offers removed.
================
*/
func (c *Catalog) WithholdGoods(excluded func(*enterworld.ItemRef) bool) int {
	removed := 0
	for tab, offers := range c.Tabs {
		kept := offers[:0]
		for _, offer := range offers {
			if offerHolds(offer, excluded) {
				removed++
				continue
			}
			kept = append(kept, offer)
		}
		c.Tabs[tab] = kept
	}
	return removed
}

/*
================
offerHolds
================
*/
func offerHolds(offer Offer, excluded func(*enterworld.ItemRef) bool) bool {
	if offer.Ref != nil && excluded(offer.Ref) {
		return true
	}
	for _, content := range offer.Contents {
		if content.Ref != nil && excluded(content.Ref) {
			return true
		}
	}
	return false
}
