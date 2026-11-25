package caravanai

import (
	"context"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestHiredLeadStopsAndResumes

The leading hired Trader stops while its hirer or another hired Trader is
out of range (HiredLeadStopDistance) and sets off again as soon as all
are back in range.
================
*/
func TestHiredLeadStopsAndResumes(t *testing.T) {
	players := map[string]*hirePlayer{"Boss": {at: TownWaypoint(TownJangan), job: domain.JobTrader, online: true}}
	paid := map[string]int64{}
	rt := hireRuntime(t, players, &paid)
	rt.hooks.Move = func(_ string, from, to Waypoint, distance float32) (Waypoint, bool) {
		region, x, z, arrived := StepWaypoint(from.RegionID, from.X, from.Z, to, distance)
		return Waypoint{RegionID: region, X: x, Z: z}, arrived
	}
	wp := func(x float32, town TownID) Waypoint { return Waypoint{RegionID: 25239, X: x, Z: 500, Town: town} }
	road := []Waypoint{wp(0, TownJangan), wp(600, ""), wp(1200, ""), wp(1800, TownDonwhang)}
	rt.cfg.Highway = map[[2]TownID][]Waypoint{{TownJangan, TownDonwhang}: road}
	rt.cfg.Terminals = map[TownID]Waypoint{}
	rt.mu.Lock()
	rt.findAgentLocked("division", "[AI]Trader_Bao").place(TownWaypoint(TownJangan), 0)
	rt.mu.Unlock()
	rt.HireAgent("division", "Boss", domain.JobTrader, "[AI]Trader_Jin", 1000)
	rt.HireAgent("division", "Boss", domain.JobTrader, "[AI]Trader_Bao", 1000)
	rt.SetPartyOrder("division", "Boss", []string{"Boss", "[AI]Trader_Jin", "[AI]Trader_Bao"}, 1000)
	place := func(name string, at Waypoint) {
		rt.mu.Lock()
		rt.findAgentLocked("division", name).place(at, 1000)
		rt.mu.Unlock()
	}
	x := func(name string) float32 {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		return rt.findAgentLocked("division", name).X
	}
	now := int64(2000)
	step := func() {
		rt.StepAgent(context.Background(), "division", "[AI]Trader_Jin", nil, now)
		now += 100
	}
	place("[AI]Trader_Jin", wp(300, ""))
	place("[AI]Trader_Bao", wp(290, ""))
	players["Boss"].at = wp(295, "")
	for range 5 {
		step()
	}
	if x("[AI]Trader_Jin") <= 300 {
		t.Fatal("the lead did not set off with everyone in range")
	}
	// The other Trader falls 90m behind: the lead stops.
	place("[AI]Trader_Bao", wp(x("[AI]Trader_Jin")-900, ""))
	step()
	stopped := x("[AI]Trader_Jin")
	for range 300 {
		step()
	}
	if x("[AI]Trader_Jin") != stopped {
		t.Fatal("the lead went on without its Trader")
	}
	// Everyone back in range: it goes again.
	place("[AI]Trader_Bao", wp(stopped-50, ""))
	players["Boss"].at = wp(stopped-40, "")
	for range 5 {
		step()
	}
	if x("[AI]Trader_Jin") <= stopped {
		t.Fatal("the lead did not set off again with everyone back in range")
	}
	// The hirer falls 85m behind: it stops; back in range, it goes.
	players["Boss"].at = wp(x("[AI]Trader_Jin")-850, "")
	place("[AI]Trader_Bao", wp(x("[AI]Trader_Jin")-20, ""))
	step()
	stopped = x("[AI]Trader_Jin")
	for range 20 {
		step()
	}
	if x("[AI]Trader_Jin") != stopped {
		t.Fatal("the lead went on without its hirer")
	}
	players["Boss"].at = wp(stopped-30, "")
	for range 5 {
		step()
	}
	if x("[AI]Trader_Jin") <= stopped {
		t.Fatal("the lead did not set off again once its hirer was back")
	}
}
