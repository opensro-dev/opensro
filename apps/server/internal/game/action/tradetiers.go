/*
===========================================================================

tradetiers.go - trade stars by cargo gold, and Jangan route profit

Port-only, not native (owner decision 2026-10-14, for the balance advisor).
With SRO_TRADE_TIERS on:

Stars. A caravan's tier (1-5 stars) is its cargo's gold value, the sum of
count x each good's reference price (the quotation basis at its home
town; what was actually paid is not stored), against 5M/10M/20M/35M/50M:
under 10M is 1 star, 10M 2, 20M 3, 35M 4, 50M and up 5. Natively the tier
is cargo "units" (count x 348 over the level's gold basis),
which barely moves with what the goods are worth. The tier drives the
bandit count (the native caravan bandit count, still capped by
SRO_CARAVAN_BANDIT_CAP), the 1-star trader EXP rate and the client's
TRADESCALE combo, published at world entry so both sides agree.

Routes. A foreign sale's PROFIT (sale minus the reference cost, the
advisor's word) between Jangan and another town is raised, both ways:
Donwhang +50%, Hotan +100%, Samarkand +200%, Constantinople +300%. Profit,
not sale value: the sale is about three times the cost on the long road,
so +300% of it would quadruple every run's gold. The trader's job EXP and
the hunters' share follow the raised profit as they do natively. A good's
home town is the one specialty shop that stocks it (STORE_<town>_SPECIAL),
so no buy town is stored; the outpost traders (SPECIAL2) and every other
pair stay native.

Off, the default here, every tier and price is native; the GameWorld's
Nomad job turns it on.

===========================================================================
*/
package action

import (
	"math"
	"os"
	"strings"

	"opensro.online/server/internal/game/caravan"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/tunables"
	"opensro.online/server/internal/game/world/simulation"
)

// EnvTradeTiers turns the rule on ("on", "1", "true").
const EnvTradeTiers = "SRO_TRADE_TIERS"

// tradeTierGold are the cargo gold values that reach 2..5 stars; anything
// carried below the first is 1 star (5M, the advisor's 1-star mark).
var tradeTierGold = [...]uint64{10_000_000, 20_000_000, 35_000_000, 50_000_000}

// tradeTown names a town's specialty trade.
type tradeTown uint8

const (
	townNone tradeTown = iota
	townJangan
	townDonwhang
	townHotan
	townSamarkand
	townConstantinople
)

// tradeTownShops are each town's specialty tab and trader.
var tradeTownShops = [...]struct {
	town     tradeTown
	tab, npc string
}{
	{townJangan, "STORE_CH_SPECIAL_TAB1", "NPC_CH_SPECIAL"},
	{townDonwhang, "STORE_WC_SPECIAL_TAB1", "NPC_WC_SPECIAL"},
	{townHotan, "STORE_KT_SPECIAL_TAB1", "NPC_KT_SPECIAL"},
	{townSamarkand, "STORE_CA_SPECIAL_TAB1", "NPC_CA_SPECIAL"},
	{townConstantinople, "STORE_EU_SPECIAL_TAB1", "NPC_EU_SPECIAL"},
}

// janganRoutePercent names the live tunable holding the profit bonus
// between Jangan and each town (game/tunables, the trade group; defaults
// 50, 100, 200 and 300).
var janganRoutePercent = map[tradeTown]string{
	townDonwhang:       tunables.TradeRouteDonwhangPercent,
	townHotan:          tunables.TradeRouteHotanPercent,
	townSamarkand:      tunables.TradeRouteSamarkandPercent,
	townConstantinople: tunables.TradeRouteConstantinoplePercent,
}

/*
================
TradeTiersFromEnv
================
*/
func TradeTiersFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvTradeTiers))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
tradeOriginTowns

Each trade good's home town: the specialty tab that stocks it.
================
*/
func tradeOriginTowns(c *commerce.Catalog) map[uint32]tradeTown {
	origins := map[uint32]tradeTown{}
	if c == nil {
		return origins
	}
	for _, shop := range tradeTownShops {
		for _, offer := range c.TabOffers(shop.tab) {
			if offer.Ref != nil {
				origins[offer.Ref.RefObjID] = shop.town
			}
			for _, content := range offer.Contents {
				if content.Ref != nil {
					origins[content.Ref.RefObjID] = shop.town
				}
			}
		}
	}
	return origins
}

/*
================
tradeTownOf

The town a specialty trader stands in, or townNone.
================
*/
func tradeTownOf(npcCodename string) tradeTown {
	for _, shop := range tradeTownShops {
		if npcCodename == shop.npc {
			return shop.town
		}
	}
	return townNone
}

/*
================
routeProfitPercent

The profit bonus for a good sold at npc: Jangan to or from another town.
================
*/
func (rt *Runtime) routeProfitPercent(npc simulation.NpcDef, item inventory.Item, s *tunables.Settings) int64 {
	if !rt.TradeTiers {
		return 0
	}
	return routePercent(rt.tradeOrigins, npc.Codename, item.RefObjID, s)
}

/*
================
routePercent
================
*/
func routePercent(origins map[uint32]tradeTown, npcCodename string, itemID uint32, s *tunables.Settings) int64 {
	from, to := origins[itemID], tradeTownOf(npcCodename)
	key := ""
	switch {
	case from == townJangan:
		key = janganRoutePercent[to]
	case to == townJangan:
		key = janganRoutePercent[from]
	}
	if key == "" {
		return 0
	}
	return s.Int(key)
}

/*
================
TradeRouteProfit

The rule's route bonus for the website's trade quotes: the profit percent
a trader (by codename) adds to a good, from the same shops the GameWorld
reads, at the percents running when it is asked.
================
*/
func TradeRouteProfit(c *commerce.Catalog) func(npcCodename string, itemID uint32) int64 {
	origins := tradeOriginTowns(c)
	return func(npcCodename string, itemID uint32) int64 {
		return routePercent(origins, npcCodename, itemID, tunables.Current())
	}
}

/*
================
RaiseRouteProfit

The sale's credit and profit with the route bonus added to both (before
the town tax, as the native profit is).
================
*/
func RaiseRouteProfit(credit, profit, percent int64) (int64, int64) {
	if percent <= 0 || profit <= 0 {
		return credit, profit
	}
	extra := profit * percent / 100
	if extra > math.MaxInt64-credit {
		return credit, profit
	}
	return credit + extra, profit + extra
}

/*
================
cargoGoldValue

The transport's trade goods valued at their reference prices.
================
*/
func (rt *Runtime) cargoGoldValue(c *enterworld.Character) uint64 {
	if c.ActiveCOS == nil || c.ActiveCOS.Container == nil {
		return 0
	}
	items := rt.deps.ItemReferences()
	total := uint64(0)
	for _, row := range c.ActiveCOS.Container.Rows {
		if row.StackCount <= 0 || !inventory.IsTradeGoods(row.TypeFlags) {
			continue
		}
		ref, found := items.ItemRefByCodename(row.Codename)
		if !found || ref == nil {
			continue
		}
		price, present := ref.NativeFields.Lookup("price")
		if !present || price < 1 {
			continue
		}
		total += uint64(price) * uint64(row.StackCount)
	}
	return total
}

/*
================
TradeTierForGold

The stars a cargo of this gold value earns: 0 for none, then 1..5.
================
*/
func TradeTierForGold(value uint64) uint8 {
	if value == 0 {
		return 0
	}
	tier := uint8(1)
	for _, mark := range tradeTierGold {
		if value >= mark {
			tier++
		}
	}
	return tier
}

/*
================
caravanTier

The cargo's difficulty tier and star rating: native from cargo units, or
both the gold tier with the flag on.
================
*/
func (rt *Runtime) caravanTier(c *enterworld.Character) (uint8, uint32) {
	if rt.TradeTiers {
		tier := TradeTierForGold(rt.cargoGoldValue(c))
		return tier, uint32(tier)
	}
	value := rt.caravanCargoValue(c)
	return caravan.DifficultyTier(value), caravan.StarRating(value, cargoHolds(c, cargoSpecialGoods))
}
