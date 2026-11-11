/*
===========================================================================

tradetiers.go - trade stars by cargo gold, and Jangan route profit

Port-only, not native (owner decision 2026-10-14, for the balance advisor).
With SRO_TRADE_TIERS on:

Stars. A caravan's tier (1-5 stars) is its cargo's gold value, the sum of
count x each good's reference price (the quotation basis at its home
town; what was actually paid is not stored), against tradeTierGold:
under the first mark is 1 star, and each mark crossed adds a star. The
5-star mark is a brim-full ordinary transport (owner 2026-11-08, #2791);
on the advisor's first ladder only a mall transport's biggest bag
crossed the top. Natively the tier
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

Stakes (#2958). The fifth star is danger, not only money. A thief party
selling a 5-star caravan's worth of goods splits a larger gold share, and
each hunter escorting a trader's 5-star sale earns a larger gold share
(applyFiveStarTradeBonus, in the sale's reward distribution). A hunter
who kills a thief marked for attacking a 5-star caravan - its transport
or its owner, within caravanRobWindowMs - earns raised job EXP (the
bounty, in pkreward.go). The three percents are live tunables (game/
tunables, the Trade group). The caravan's star itself shows on the
transport's nameplate, client side.

Off, the default here, every tier and price is native; the GameWorld's
Nomad job turns it on.

===========================================================================
*/
package action

import (
	"math"
	"os"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/caravan"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/tunables"
	"opensro.online/server/internal/game/world/simulation"
)

// EnvTradeTiers turns the rule on ("on", "1", "true").
const EnvTradeTiers = "SRO_TRADE_TIERS"

// tradeTierGold are the cargo gold values that reach 2..5 stars; anything
// carried below the first is 1 star. The 5-star mark (#2791, owner
// 2026-11-08) is a brim-full ordinary transport - any one of them, so the
// mark is the smallest ordinary bag the shipped characterdata authors
// (COS_T_DONKEY, 32 slots), full of 40-stacks at the flag's scaled price
// 7,308: 32 x 40 x 7,308 = 9,354,240 (#3142: the old 101-slot calibration
// left every smaller ordinary transport short of five stars however full
// it stood). The marks below keep the ladder's ratios to the top (1/5,
// 2/5, 7/10), so bigger bags crown earlier in their fill and earn their
// extra as profit, never a higher tier.
var tradeTierGold = [...]uint64{1_870_848, 3_741_696, 6_547_968, 9_354_240}

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

// caravanRobWindowMs is how long an attack on a 5-star caravan keeps its
// thief marked for the hunter bounty: a robbery - chase, kill the
// transport, fight over the goods - resolves inside two minutes; past
// it, a hunter's kill is an ordinary road kill again.
const caravanRobWindowMs = 120_000

// caravanThiefMark is one thief's last attack on a 5-star caravan
// (runtime.go's caravanThiefMarks; session only, like the aggression
// indexes - a relog clears it and the thief simply robs again).
type caravanThiefMark struct {
	at int64
}

/*
================
noteCaravanAttack

Port-only, not native (#2958): a dressed thief's landed hit on a 5-star
caravan - its transport or its owner - marks the thief for the hunter
bounty. Called from the two strike doors once the hit is in, so refused
or whiffed casts never mark. The tier is the owner's own caravan tier at
the hit; with SRO_TRADE_TIERS off nothing is ever marked.
================
*/
func (rt *Runtime) noteCaravanAttack(attacker, owner *enterworld.Character, now int64) {
	if !rt.TradeTiers || attacker == nil || owner == nil || attacker.ID == owner.ID {
		return
	}
	if enterworld.DressedJob(attacker) != domain.JobThief {
		return
	}
	if tier, _ := rt.caravanTier(owner); tier != 5 {
		return
	}
	rt.caravanThiefMarks.Store(attacker.ID, caravanThiefMark{at: now})
}

/*
================
caravanThiefMarked

Whether the thief still carries a live 5-star mark; an expired mark is
dropped on read so the map holds only live quarrels.
================
*/
func (rt *Runtime) caravanThiefMarked(thief *enterworld.Character, now int64) bool {
	if thief == nil {
		return false
	}
	stamp, ok := rt.caravanThiefMarks.Load(thief.ID)
	if !ok {
		return false
	}
	mark := stamp.(caravanThiefMark)
	if now < mark.at || now-mark.at > caravanRobWindowMs {
		rt.caravanThiefMarks.Delete(thief.ID)
		return false
	}
	return true
}

/*
================
fiveStarBountyPercent

Port-only, not native (#2958): the bounty percent on a hunter's job-kill
EXP for killing a marked thief (pkreward.go), or 0. A paid bounty is
consumed: the thief must attack a 5-star caravan again to be worth it
twice.
================
*/
func (rt *Runtime) fiveStarBountyPercent(killer, victim *enterworld.Character, now int64) int64 {
	if !rt.TradeTiers || enterworld.DressedJob(killer) != domain.JobHunter ||
		enterworld.DressedJob(victim) != domain.JobThief || !rt.caravanThiefMarked(victim, now) {
		return 0
	}
	rt.caravanThiefMarks.Delete(victim.ID)
	return tunables.Current().Int(tunables.TradeFiveStarBountyPercent)
}

/*
================
applyFiveStarTradeBonus

Port-only, not native (#2958): the 5-star stakes in a sale's reward
distribution. A thief's party splits its gold share raised by the thief
percent (the solo thief's own credit is the same share), and each hunter
on a trader's sale earns its gold share raised by the hunter percent.
Only gold moves; job EXP on a sale stays as DistributeTradeReward and
SRO_TRADE_JOB_BONUS left it, the same posture as tradejobbonus.go. The
input's FiveStar is set only under SRO_TRADE_TIERS, so off is native.
================
*/
func applyFiveStarTradeBonus(out *commerce.TradeRewardDistribution, in commerce.TradeRewardInput, s *tunables.Settings) {
	if !in.FiveStar || in.Profit <= 0 {
		return
	}
	switch in.Job {
	case domain.JobThief:
		percent := s.Int(tunables.TradeFiveStarThiefPercent)
		out.Thief.Gold += out.Thief.Gold * percent / 100
		out.Actor.Gold += out.Actor.Gold * percent / 100
	case domain.JobTrader:
		percent := s.Int(tunables.TradeFiveStarHunterPercent)
		out.Hunter.Gold += out.Hunter.Gold * percent / 100
	}
}

// caravanStarPushIntervalMs paces the 5-star roster push: a nameplate
// mark is a slow view, the beta world map roster's own pace.
const caravanStarPushIntervalMs = 2000

/*
================
CaravanStarFrames

Port-only, not native (#2958): the division's 5-star caravan transport
gids (wire.OpCaravanStars), recomputed at the push pace from the same
tier the sales use, so a caravan that loads or sells its way across the
5-star mark gains and loses its nameplate star on its own. The wiring hook
pushes the frame to the whole division; it goes out even when empty so
a lost fifth star clears. Nil off the pace, and nil with the flag off,
when no client ever hears of the mark.
================
*/
func (rt *Runtime) CaravanStarFrames(division string, views []simulation.SessionView, nowMs int64) []simulation.Frame {
	if !rt.TradeTiers || nowMs < rt.caravanStarDueMs {
		return nil
	}
	rt.caravanStarDueMs = nowMs + caravanStarPushIntervalMs
	gids := []uint32{}
	for _, view := range views {
		if view.DivisionID != division {
			continue
		}
		c := rt.findCharacterByID(division, view.CharacterID)
		if c == nil || c.ActiveCOS == nil {
			continue
		}
		if tier, _ := rt.caravanTier(c); tier == 5 {
			gids = append(gids, c.ActiveCOS.GID)
		}
	}
	return []simulation.Frame{{Opcode: wire.OpCaravanStars, Payload: wire.EncodeCaravanStars(gids)}}
}
