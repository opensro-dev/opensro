/*
===========================================================================

public_market.go - the website market's GameWorld half (opensro-web#148)

Port-only, not native (SRO_CONSIGNMENT, off by default): with the flag on,
the public listener serves the market routes (publicstats/market.go)
through a game/market.Service over the authority store, the item
references, the exchange's trade rule and the live session list.

===========================================================================
*/
package main

import (
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/market"
)

/*
================
marketService
================
*/
func marketService(gameplay *gameplayPlane, authority *store.Store, shard string, online func(int64) bool) *market.Service {
	return &market.Service{
		Store:    authority,
		Division: shard,
		Item: func(codename string) (market.ItemInfo, bool) {
			items := gameplay.deps.ItemReferences()
			if items == nil {
				return market.ItemInfo{}, false
			}
			ref, ok := items.ItemRefByCodename(codename)
			if !ok || ref == nil {
				return market.ItemInfo{}, false
			}
			return market.ItemInfo{Name: ref.Name, TypeIDs: ref.TypeIDs, Degree: enterworld.EquipmentDegree(ref)}, true
		},
		Tradeable: func(row domain.InventoryRow) bool {
			return gameplay.items.Tradeable(row.Codename, row.ForgeBound, row.Summon)
		},
		Characters: func(account string) []*domain.Character {
			var out []*domain.Character
			authority.ReadCharacters(shard, func(characters []*domain.Character) {
				for _, c := range characters {
					if c.AccountID == account && !c.DeletePending {
						out = append(out, c)
					}
				}
			})
			return out
		},
		Online: online,
		WhileOffline: func(c *domain.Character, write func() error) (bool, error) {
			return gameplay.items.WhileOffline(shard, c.Name, write)
		},
	}
}
