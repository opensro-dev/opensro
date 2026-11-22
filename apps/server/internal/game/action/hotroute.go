/*
===========================================================================

hotroute.go - the hourly hot route (port-only, not native)

#3530 slice 3: with SRO_JOB_HONOR on, one town pair each hour pays
150% job honor, announced in chat so traders reroute and thieves and
hunters follow. The pair is a pure function of the UTC hour - a
splitmix64 of the hour index - so every division and every restart
agrees on it with no stored state, and the announcement and the
scorer can never disagree: both read hotRouteAt.

Which legs the multiplier touches is inference from the issue's
"traders reroute, thieves and hunters follow": the trader's foreign
sale on the route, and the escort payout that sale releases. A
thief's den sale ends at no town pair, and a job kill is tied to no
route, so neither is multiplied. The run cap and the account's daily
cap still bound everything, exactly as without the route.

===========================================================================
*/
package action

import (
	"time"

	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/world/simulation"
)

// hotRouteHonorPercent is the hot route's scoring rate, the issue's
// own number (x1.5).
const hotRouteHonorPercent = 150

// hotRoute is one unordered town pair (from before to in tradeTown
// order).
type hotRoute struct {
	from, to tradeTown
}

// hotRoutePairs is every pair of specialty towns a caravan can run
// between, in tradeTown order.
var hotRoutePairs = buildHotRoutePairs()

/*
================
buildHotRoutePairs
================
*/
func buildHotRoutePairs() []hotRoute {
	towns := []tradeTown{townJangan, townDonwhang, townHotan, townSamarkand, townConstantinople}
	pairs := make([]hotRoute, 0, 10)
	for i, from := range towns {
		for _, to := range towns[i+1:] {
			pairs = append(pairs, hotRoute{from, to})
		}
	}
	return pairs
}

// tradeTownNames names a specialty town for the announcement.
var tradeTownNames = map[tradeTown]string{
	townJangan:         "Jangan",
	townDonwhang:       "Donwhang",
	townHotan:          "Hotan",
	townSamarkand:      "Samarkand",
	townConstantinople: "Constantinople",
}

/*
================
hotRouteAt

The hour's hot pair. Pure in at: the same hour always names the same
pair, on any process, with no seed to keep.
================
*/
func hotRouteAt(at time.Time) hotRoute {
	hour := at.UTC().Unix() / 3600
	x := uint64(hour) + 0x9E3779B97F4A7C15
	x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9
	x = (x ^ (x >> 27)) * 0x94D049BB133111EB
	x ^= x >> 31
	return hotRoutePairs[x%uint64(len(hotRoutePairs))]
}

/*
================
HotRouteNames

The hot pair's town names at at, for the announcement the wiring
sends. Scoring reads the same selector, so the line never promises a
multiplier the scorer does not pay.
================
*/
func HotRouteNames(at time.Time) (string, string) {
	route := hotRouteAt(at)
	return tradeTownNames[route.from], tradeTownNames[route.to]
}

/*
================
saleRouteHot

Whether a sale of item at npc travels the hour's hot route: the good's
home town (tradeOriginTowns, the specialty shop that stocks it) to the
sale town. Same-town and unknown ends are never hot.
================
*/
func (rt *Runtime) saleRouteHot(npc simulation.NpcDef, item inventory.Item, at time.Time) bool {
	from, to := rt.tradeOrigins[item.RefObjID], tradeTownOf(npc.Codename)
	if from == townNone || to == townNone || from == to {
		return false
	}
	if from > to {
		from, to = to, from
	}
	return hotRoute{from, to} == hotRouteAt(at)
}
