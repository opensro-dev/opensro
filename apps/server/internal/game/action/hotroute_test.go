/*
===========================================================================

hotroute_test.go - the hourly hot route (#3530 slice 3)

The pair is a pure function of the hour; a sale on it scores at the
hot rate and its escort payout with it; the run cap and the account's
daily cap still bound every point.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/platform/jobhonor"
)

// hotRouteTestItem is the trade good the route tests sell.
const hotRouteTestItem = uint32(41000)

/*
================
TestTheHotRouteIsFixedWithinTheHour
================
*/
func TestTheHotRouteIsFixedWithinTheHour(t *testing.T) {
	start := time.Date(2026, 11, 23, 4, 0, 0, 0, time.UTC)
	want := hotRouteAt(start)
	for _, at := range []time.Time{start, start.Add(30 * time.Minute), start.Add(59*time.Minute + 59*time.Second)} {
		if got := hotRouteAt(at); got != want {
			t.Fatalf("the route changed inside the hour: %v then %v", want, got)
		}
	}
	if got := hotRouteAt(start.Add(time.Hour)); hotRouteAt(start.Add(time.Hour)) != got {
		t.Fatalf("the next hour's route is not stable: %v", got)
	}
}

/*
================
TestTheHotRoutePairIsPinned

The selector seed is a contract: changing it changes the route on every
player's screen at once, so pin one computed pair.
================
*/
func TestTheHotRoutePairIsPinned(t *testing.T) {
	at := time.Date(2026, 11, 23, 4, 0, 0, 0, time.UTC)
	if got := hotRouteAt(at); got != (hotRoute{townHotan, townConstantinople}) {
		t.Fatalf("hour %d hot route = %v, want Hotan to Constantinople", at.UTC().Unix()/3600, got)
	}
	from, to := HotRouteNames(at)
	if from != "Hotan" || to != "Constantinople" {
		t.Fatalf("announced names = %s to %s, want Hotan to Constantinople", from, to)
	}
}

/*
================
TestTheHotRouteRotatesOverHours
================
*/
func TestTheHotRouteRotatesOverHours(t *testing.T) {
	start := time.Date(2026, 11, 23, 4, 0, 0, 0, time.UTC)
	seen := map[hotRoute]bool{}
	for hour := 0; hour < 240; hour++ {
		route := hotRouteAt(start.Add(time.Duration(hour) * time.Hour))
		if route.from == route.to || route.from == townNone || route.to == townNone {
			t.Fatalf("hour %d names an invalid route: %v", hour, route)
		}
		if route.from > route.to {
			t.Fatalf("hour %d route is not in town order: %v", hour, route)
		}
		seen[route] = true
	}
	if len(seen) < 5 {
		t.Fatalf("240 hours named only %d distinct routes", len(seen))
	}
}

/*
================
TestTheHotRoutePaysOnePointFive

A trader sale of ten points' profit pays 10 points cold and 15 hot;
a hunter's held escort damage of 100 points pays 150 hot; the run
cap still binds the multiplied sale.
================
*/
func TestTheHotRoutePaysOnePointFive(t *testing.T) {
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	profit := int64(10) * honorTraderGoldPerPoint

	rt := honorRuntime(t, at)
	if got := rt.scoreTradeSale(honorCharacter(1, "cold"), domain.JobTrader, profit, false, at); got != 10 {
		t.Fatalf("cold sale kept %d, want 10", got)
	}

	rt = honorRuntime(t, at)
	if got := rt.scoreTradeSale(honorCharacter(1, "hot"), domain.JobTrader, profit, true, at); got != 15 {
		t.Fatalf("hot sale kept %d, want 15", got)
	}

	rt = honorRuntime(t, at)
	huge := int64(honorRunPointCap) * honorTraderGoldPerPoint
	if got := rt.scoreTradeSale(honorCharacter(1, "capped"), domain.JobTrader, huge, true, at); got != honorRunPointCap {
		t.Fatalf("a huge hot sale kept %d, want the run cap %d", got, honorRunPointCap)
	}
}

/*
================
TestAHotArrivalPaysEscortsOnePointFive
================
*/
func TestAHotArrivalPaysEscortsOnePointFive(t *testing.T) {
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	rt := honorRuntime(t, at)
	ledger := rt.JobHonor.(*jobhonor.Service)
	trader := honorDressed(1, "trader-account", domain.JobTrader)
	thief := honorDressed(2, "thief-account", domain.JobThief)
	hunter := honorDressed(3, "hunter-account", domain.JobHunter)
	const now = int64(1_000_000)
	rt.noteHonorCaravanAttack(thief, trader, now)
	rt.noteHonorEscortDamage(hunter, thief, 100*honorHunterDamagePerPoint, now)
	rt.payHonorEscorts(trader, true, at)
	if got := ledger.Used("hunter-account"); got != 150 {
		t.Fatalf("hot arrival paid the escort %d, want 150", got)
	}
	rt.payHonorEscorts(trader, true, at)
	if got := ledger.Used("hunter-account"); got != 150 {
		t.Fatalf("a second hot arrival paid again: %d", got)
	}
}

/*
================
TestSaleRouteHotReadsTheSameSelector

A Jangan good sold at Hotan's specialty shop is hot exactly in the
hours the selector names Jangan to Hotan; a same-town sale and an
unknown good are never hot.
================
*/
func TestSaleRouteHotReadsTheSameSelector(t *testing.T) {
	rt := &Runtime{tradeOrigins: map[uint32]tradeTown{hotRouteTestItem: townJangan}}
	item := inventory.Item{RefObjID: hotRouteTestItem}
	hotan := simulation.NpcDef{Codename: "NPC_KT_SPECIAL"}
	target := hotRoute{townJangan, townHotan}
	start := time.Date(2026, 11, 23, 4, 0, 0, 0, time.UTC)
	for hour := 0; hour < 500; hour++ {
		at := start.Add(time.Duration(hour) * time.Hour)
		want := hotRouteAt(at) == target
		if got := rt.saleRouteHot(hotan, item, at); got != want {
			t.Fatalf("hour %d: sale hot = %v, selector says %v", hour, got, want)
		}
	}
	jangan := simulation.NpcDef{Codename: "NPC_CH_SPECIAL"}
	if rt.saleRouteHot(jangan, item, start) {
		t.Fatal("a same-town sale is hot")
	}
	unknown := inventory.Item{RefObjID: 99999}
	if rt.saleRouteHot(hotan, unknown, start) {
		t.Fatal("a good with no home town is hot")
	}
}
