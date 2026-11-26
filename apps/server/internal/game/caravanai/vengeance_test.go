package caravanai

import (
	"context"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestHiredTraderKeepsCloseAndNeverTeleports

A hired Trader that follows its hirer runs after it step by step (never a
jump) and then holds a spot within 6m of it instead of strolling.
================
*/
func TestHiredTraderKeepsCloseAndNeverTeleports(t *testing.T) {
	players := map[string]*hirePlayer{"Boss": {at: TownWaypoint(TownJangan), job: domain.JobTrader, online: true}}
	paid := map[string]int64{}
	rt := hireRuntime(t, players, &paid)
	if reply := rt.HireAgent("division", "Boss", domain.JobTrader, "[AI]Trader_Jin", 1000); !strings.Contains(reply, "joins you") {
		t.Fatalf("hire refused: %s", reply)
	}
	rt.HandlePlayerCommand("division", "Boss", domain.JobTrader, "!leed", 1000)
	rt.mu.Lock()
	jin := rt.findAgentLocked("division", "[AI]Trader_Jin")
	if jin.pendingTeleport != nil {
		rt.mu.Unlock()
		t.Fatal("a hired Trader teleports to its hirer")
	}
	maxStep := float64(stepDistanceFor(jin)) + 0.5
	rt.mu.Unlock()

	players["Boss"].at = offsetPoint(TownWaypoint(TownJangan), 0, 1500)
	var last Waypoint
	now := int64(2000)
	steps := 0
	for ; steps < 2000; steps++ {
		_, unit, _ := rt.StepAgent(context.Background(), "division", "[AI]Trader_Jin", nil, now)
		here := position(unit)
		if steps > 0 && WaypointDistance(last, here) > maxStep {
			t.Fatalf("step %d jumped %.0f units", steps, WaypointDistance(last, here))
		}
		last = here
		now += 100
		if WaypointDistance(here, players["Boss"].at) <= 65 {
			break
		}
	}
	if WaypointDistance(last, players["Boss"].at) > 65 {
		t.Fatalf("the Trader never caught up: %.0f units away", WaypointDistance(last, players["Boss"].at))
	}
	for i := 0; i < 300; i++ {
		_, unit, _ := rt.StepAgent(context.Background(), "division", "[AI]Trader_Jin", nil, now)
		if d := WaypointDistance(position(unit), players["Boss"].at); d > 65 {
			t.Fatalf("the Trader strolled %.0f units from a standing hirer", d)
		}
		now += 100
	}
}

/*
================
TestHiredTraderWalksAroundWhatTheHirerWalkedAround

A wall stands between the Trader and its hirer's new spot; the hirer walks
around it, and the Trader, never teleported, follows the hirer's footsteps
around it instead of pushing into the wall.
================
*/
func TestHiredTraderWalksAroundWhatTheHirerWalkedAround(t *testing.T) {
	base := Waypoint{RegionID: 25239, X: 1000, Z: 1000}
	players := map[string]*hirePlayer{"Boss": {at: TownWaypoint(TownJangan), job: domain.JobTrader, online: true}}
	paid := map[string]int64{}
	rt := hireRuntime(t, players, &paid)
	if reply := rt.HireAgent("division", "Boss", domain.JobTrader, "[AI]Trader_Jin", 1000); !strings.Contains(reply, "joins you") {
		t.Fatalf("hire refused: %s", reply)
	}
	rt.HandlePlayerCommand("division", "Boss", domain.JobTrader, "!leed", 1000)
	// The wall: x 1040..1060 for z below 1100.
	wall := func(p Waypoint) bool { return p.RegionID == base.RegionID && p.X > 1040 && p.X < 1060 && p.Z < 1100 }
	rt.hooks.Move = func(_ string, from, to Waypoint, distance float32) (Waypoint, bool) {
		region, x, z, arrived := StepWaypoint(from.RegionID, from.X, from.Z, to, distance)
		next := Waypoint{RegionID: region, X: x, Z: z}
		if wall(next) {
			return from, false
		}
		return next, arrived
	}
	rt.mu.Lock()
	rt.findAgentLocked("division", "[AI]Trader_Jin").place(base, 0)
	rt.mu.Unlock()
	path := []Waypoint{base, offsetPoint(base, 0, 150), offsetPoint(base, 200, 150), offsetPoint(base, 200, 0)}
	now := int64(2000)
	step := func() *AgentUnit {
		_, unit, _ := rt.StepAgent(context.Background(), "division", "[AI]Trader_Jin", nil, now)
		now += 100
		return unit
	}
	for leg := 1; leg < len(path); leg++ {
		// The hirer outpaces the Trader: a straight line from where the
		// Trader lags to the hirer's end runs through the wall.
		for f := float32(0); f <= 1; f += 0.25 {
			a, b := path[leg-1], path[leg]
			players["Boss"].at = Waypoint{RegionID: a.RegionID, X: a.X + (b.X-a.X)*f, Z: a.Z + (b.Z-a.Z)*f}
			step()
		}
	}
	var jin *AgentUnit
	for i := 0; i < 300; i++ {
		jin = step()
	}
	if d := WaypointDistance(position(jin), players["Boss"].at); d > 65 {
		t.Fatalf("the Trader stalled at the wall: %.0f units from its hirer at %+v", d, position(jin))
	}
}

/*
================
TestKilledThiefLeadsItsGangBackAfterTheTrader
================
*/
func TestKilledThiefLeadsItsGangBackAfterTheTrader(t *testing.T) {
	players := map[string]*hirePlayer{}
	paid := map[string]int64{}
	rt := hireRuntime(t, players, &paid)
	field := Waypoint{RegionID: 25239, X: 1000, Z: 1000}
	pairEscort(rt, "[AI]Trader_Jin", "[AI]Hunter_Kwan")
	rt.mu.Lock()
	jin := rt.findAgentLocked("division", "[AI]Trader_Jin")
	jin.place(field, 0)
	thief := rt.findAgentLocked("division", "[AI]Thief_Wisp")
	thief.place(offsetPoint(field, 50, 0), 0)
	rt.mu.Unlock()

	_, defeated, _ := rt.RecordPlayerAttack("division", "[AI]Thief_Wisp", "[AI]Hunter_Kwan", 1_000_000, 1000)
	if !defeated {
		t.Fatal("the Thief was not defeated")
	}
	rt.mu.Lock()
	if thief.Vengeance != "[AI]Trader_Jin" || thief.RunKind != RunVengeance {
		rt.mu.Unlock()
		t.Fatalf("no hunt: %q %q", thief.Vengeance, thief.RunKind)
	}
	gang := 0
	for _, u := range rt.agents["division"] {
		if u.RunKind == RunVengeance && u.EscortingTrader == thief.Name {
			gang++
		}
	}
	start := WaypointDistance(position(thief), position(jin))
	rt.mu.Unlock()
	if gang != VengeanceGang {
		t.Fatalf("the Thief called %d friends, want %d", gang, VengeanceGang)
	}

	now := int64(2000)
	for i := 0; i < 50; i++ {
		rt.StepAgent(context.Background(), "division", "[AI]Thief_Wisp", nil, now)
		now += 100
	}
	rt.mu.Lock()
	if d := WaypointDistance(position(thief), position(jin)); d >= start {
		rt.mu.Unlock()
		t.Fatalf("the Thief is not riding back: %.0f -> %.0f", start, d)
	}
	jin.CargoStacks = 0 // robbed
	rt.mu.Unlock()
	rt.StepAgent(context.Background(), "division", "[AI]Thief_Wisp", nil, now)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for _, u := range rt.agents["division"] {
		if u.RunKind == RunVengeance || u.Vengeance != "" {
			t.Fatalf("%s still hunts after the Trader was robbed", u.Name)
		}
	}
}

func TestBlockedVengeanceReturnsTheGangToDen(t *testing.T) {
	rt := NewRuntime(Config{Enabled: true}, nil, nil, WorldHooks{
		Move: func(_ string, from, _ Waypoint, _ float32) (Waypoint, bool) { return from, false },
	})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true
	leader := rt.findAgentLocked("division", "[AI]Thief_Wisp")
	trader := rt.findAgentLocked("division", "[AI]Trader_Jin")
	field := Waypoint{RegionID: 25239, X: 1000, Z: 1000}
	trader.place(field, 1000)
	leader.place(offsetPoint(field, 500, 0), 1000)
	rt.startVengeanceLocked(leader, trader.Name, position(leader), 1000)
	rt.stepVengeanceLocked(leader, 1000)
	rt.stepVengeanceLocked(leader, 1000+stuckTeleportMs)
	if leader.RunKind != RunNone || !leader.AvailableForHire || leader.RegionID != TownWaypoint(TownThiefDen).RegionID {
		t.Fatal("a blocked thief leader stayed on the road")
	}
	for _, member := range rt.agents["division"] {
		if member.RunKind == RunVengeance || member.EscortingTrader == leader.Name {
			t.Fatalf("thief gang member %s stayed on the road", member.Name)
		}
	}
}
