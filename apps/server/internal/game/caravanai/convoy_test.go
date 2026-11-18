/*
===========================================================================

convoy_test.go - escort ranges, hiring, leads, hire ends, grand caravan

===========================================================================
*/
package caravanai

import (
	"context"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
pairEscort

Puts hunter on trader's escorted run beside it (tests run runs by hand).
================
*/
func pairEscort(rt *Runtime, trader, hunter string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	tr := rt.findAgentLocked("division", trader)
	h := rt.findAgentLocked("division", hunter)
	tr.RunKind, tr.State = RunEscorted, StateTraveling
	h.RunKind, h.State = RunEscorted, StateTraveling
	h.EscortingTrader, h.DedicatedEscortFor = tr.Name, tr.Name
	h.OriginTown, h.DestTown = tr.OriginTown, tr.DestTown
	h.RegionID, h.X, h.Y, h.Z = tr.RegionID, tr.X+10, tr.Y, tr.Z
}

// hirePlayer is a dressed player whose position and job the test steers.
type hirePlayer struct {
	at     Waypoint
	job    uint8
	online bool
}

func hireRuntime(t *testing.T, players map[string]*hirePlayer, paid *map[string]int64) *Runtime {
	t.Helper()
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{
		Player: func(_, name string) (PlayerPosition, bool) {
			p := players[name]
			if p == nil || !p.online {
				return PlayerPosition{}, false
			}
			return PlayerPosition{ObservedPlayer: ObservedPlayer{Name: name, JobType: p.job, HPPercent: 100}, At: p.at}, true
		},
		OnHireFee: func(_, name string, fee int64) bool {
			(*paid)[name] -= fee
			return true
		},
		OnHirePayout: func(_, name string, gold int64) bool {
			(*paid)[name] += gold
			return true
		},
	})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true
	return rt
}

/*
================
TestFollowerRoamsWithin50mRunsPast120mTeleportsPast200m
================
*/
func TestFollowerRoamsWithin50mRunsPast120mTeleportsPast200m(t *testing.T) {
	field := Waypoint{RegionID: 25239, X: 1000, Z: 1000}
	players := map[string]*hirePlayer{"Hirer": {at: TownWaypoint(TownJangan), job: domain.JobTrader, online: true}}
	paid := map[string]int64{}
	var lastTarget Waypoint
	rt := hireRuntime(t, players, &paid)
	rt.hooks.Move = func(_ string, from, to Waypoint, distance float32) (Waypoint, bool) {
		lastTarget = to
		region, x, z, arrived := StepWaypoint(from.RegionID, from.X, from.Z, to, distance)
		return Waypoint{RegionID: region, X: x, Z: z}, arrived
	}
	if reply := rt.HireAgent("division", "Hirer", domain.JobTrader, "[AI]Hunter_Kwan", 1000); !strings.Contains(reply, "joins you") {
		t.Fatalf("hire refused: %s", reply)
	}
	players["Hirer"].at = field
	step := func(now int64) *AgentUnit {
		_, unit, _ := rt.StepAgent(context.Background(), "division", "[AI]Hunter_Kwan", nil, now)
		return unit
	}
	step(1100) // the hire teleport lands beside the hirer in town, then it is >200m behind

	set := func(dx float32) {
		rt.mu.Lock()
		kwan := rt.findAgentLocked("division", "[AI]Hunter_Kwan")
		kwan.RegionID, kwan.X, kwan.Z = field.RegionID, field.X+dx, field.Z
		kwan.catchingUp = false
		rt.mu.Unlock()
	}

	// Within 50m it roams around the hirer, never straight onto it.
	set(300)
	for now := int64(2000); now < 12_000; now += 100 {
		unit := step(now)
		if d := WaypointDistance(position(unit), field); d > EscortRoamRadius+1 {
			t.Fatalf("roaming escort drifted %.0f units away", d)
		}
	}
	if WaypointDistance(lastTarget, field) < 140 {
		t.Fatalf("roam target %.0f units from the hirer: it should circle, not stand on it", WaypointDistance(lastTarget, field))
	}

	// 150m behind: it runs straight after the hirer.
	set(-1500)
	unit := step(20_000)
	if !unit.catchingUp || lastTarget != field {
		t.Fatalf("escort 150m behind did not run after the hirer (target %+v)", lastTarget)
	}
	if !rt.EscortMustRejoin("division", "[AI]Hunter_Kwan") {
		t.Fatal("escort 150m behind must stop fighting to rejoin")
	}

	// 250m behind: teleported beside the hirer.
	set(-1900)
	players["Hirer"].at = Waypoint{RegionID: 25240, X: 400, Z: 1000}
	unit = step(20_100)
	if d := WaypointDistance(position(unit), players["Hirer"].at); d > 31 {
		t.Fatalf("escort 250m behind was not teleported (%.0f away)", d)
	}
}

/*
================
TestHireRulesFeesGoodsAndAvailability
================
*/
func TestHireRulesFeesGoodsAndAvailability(t *testing.T) {
	town := TownWaypoint(TownJangan)
	players := map[string]*hirePlayer{
		"Thief":  {at: TownWaypoint(TownThiefDen), job: domain.JobThief, online: true},
		"Hunter": {at: town, job: domain.JobHunter, online: true},
		"Busy":   {at: town, job: domain.JobTrader, online: true},
	}
	paid := map[string]int64{}
	rt := hireRuntime(t, players, &paid)

	for _, offer := range rt.HireOffers("division", domain.JobThief) {
		if offer.Role != RoleThief {
			t.Fatalf("a Thief was offered %s", offer.Name)
		}
	}
	if reply := rt.HireAgent("division", "Thief", domain.JobThief, "[AI]Hunter_Kwan", 1000); !strings.Contains(reply, "don't work for thieves") {
		t.Fatalf("a Thief hired a Hunter: %s", reply)
	}
	// Thieves are hired in the Thief Den only, where idle ones wait.
	players["Thief"].at = town
	if reply := rt.HireAgent("division", "Thief", domain.JobThief, "[AI]Thief_Wisp", 1000); !strings.Contains(reply, "Thief Den") {
		t.Fatalf("a Thief was hired in a city: %s", reply)
	}
	players["Thief"].at = TownWaypoint(TownThiefDen)
	rt.mu.Lock()
	if wisp := rt.findAgentLocked("division", "[AI]Thief_Wisp"); wisp.OriginTown != TownThiefDen || WaypointDistance(position(wisp), TownWaypoint(TownThiefDen)) > denRadius {
		t.Fatalf("an idle Thief does not wait in the Den: %+v", position(wisp))
	}
	rt.mu.Unlock()
	if reply := rt.HireAgent("division", "Thief", domain.JobThief, "[AI]Thief_Wisp", 1000); !strings.Contains(reply, "joins you") {
		t.Fatalf("a Thief could not hire a Thief in the Den: %s", reply)
	}
	players["Hunter"].at = TownWaypoint(TownThiefDen)
	if reply := rt.HireAgent("division", "Hunter", domain.JobHunter, "[AI]Hunter_Lei", 1000); !strings.Contains(reply, "specialty shop") {
		t.Fatalf("a Hunter was hired in the Thief Den: %s", reply)
	}
	players["Hunter"].at = town

	rt.mu.Lock()
	jin := rt.findAgentLocked("division", "[AI]Trader_Jin")
	goods := hiredGoodsCost(jin)
	rt.mu.Unlock()
	if reply := rt.HireAgent("division", "Hunter", domain.JobHunter, "[AI]Trader_Jin", 1000); !strings.Contains(reply, "joins you") {
		t.Fatalf("a Hunter could not hire a Trader: %s", reply)
	}
	if paid["Hunter"] != -(HireFeeGold + goods) {
		t.Fatalf("hiring a Trader charged %d, want fee %d + goods %d", -paid["Hunter"], HireFeeGold, goods)
	}
	for _, offer := range rt.HireOffers("division", domain.JobTrader) {
		if offer.Name == "[AI]Trader_Jin" {
			t.Fatal("a hired Trader is still offered")
		}
	}

	// An agent out on a run cannot be hired.
	pairEscort(rt, "[AI]Trader_Mei", "[AI]Hunter_Wei")
	if reply := rt.HireAgent("division", "Busy", domain.JobTrader, "[AI]Hunter_Wei", 1000); !strings.Contains(reply, "on the road") {
		t.Fatalf("an escort on a run was hired: %s", reply)
	}
	if paid["Busy"] != 0 {
		t.Fatal("a refused hire was charged")
	}

	// At most MaxHiresPerPlayer at once.
	hired := 0
	for _, offer := range rt.HireOffers("division", domain.JobTrader) {
		if strings.Contains(rt.HireAgent("division", "Busy", domain.JobTrader, offer.Name, 1000), "joins you") {
			hired++
		}
	}
	if hired != MaxHiresPerPlayer {
		t.Fatalf("hired %d agents, want the %d cap", hired, MaxHiresPerPlayer)
	}
}

/*
================
TestFirstTraderInPartyLeadsUntilTheHirerTypesLeed
================
*/
func TestFirstTraderInPartyLeadsUntilTheHirerTypesLeed(t *testing.T) {
	players := map[string]*hirePlayer{"Boss": {at: TownWaypoint(TownJangan), job: domain.JobTrader, online: true}}
	paid := map[string]int64{}
	rt := hireRuntime(t, players, &paid)
	for _, name := range []string{"[AI]Trader_Jin", "[AI]Trader_Bao", "[AI]Hunter_Kwan"} {
		if reply := rt.HireAgent("division", "Boss", domain.JobTrader, name, 1000); !strings.Contains(reply, "joins you") {
			t.Fatalf("hire of %s refused: %s", name, reply)
		}
	}
	rt.SetPartyOrder("division", "Boss", []string{"Boss", "[AI]Hunter_Kwan", "[AI]Trader_Bao", "[AI]Trader_Jin"})
	leaders := func() map[string]string {
		out := map[string]string{}
		for _, name := range []string{"[AI]Trader_Jin", "[AI]Trader_Bao", "[AI]Hunter_Kwan"} {
			_, unit, _ := rt.StepAgent(context.Background(), "division", name, nil, 2000)
			out[name] = unit.EscortingTrader
		}
		return out
	}
	got := leaders()
	if got["[AI]Trader_Bao"] != "" || got["[AI]Trader_Jin"] != "[AI]Trader_Bao" || got["[AI]Hunter_Kwan"] != "Boss" {
		t.Fatalf("party order lead wrong: %v", got)
	}
	if reply, _ := rt.HandlePlayerCommand("division", "Boss", domain.JobTrader, "!leed", 2000); !strings.Contains(reply, "You lead") {
		t.Fatalf("!leed refused: %s", reply)
	}
	got = leaders()
	if got["[AI]Trader_Bao"] != "Boss" || got["[AI]Trader_Jin"] != "Boss" {
		t.Fatalf("!leed did not hand the lead to the hirer: %v", got)
	}
}

/*
================
TestHireEndsWithTheRunTheHourOrTheHirer
================
*/
func TestHireEndsWithTheRunTheHourOrTheHirer(t *testing.T) {
	players := map[string]*hirePlayer{"Boss": {at: TownWaypoint(TownJangan), job: domain.JobHunter, online: true}}
	paid := map[string]int64{}
	var ended []string
	rt := hireRuntime(t, players, &paid)
	rt.hooks.OnHireEnded = func(_, player, agent string, end HireEnd, _ int64) { ended = append(ended, agent+":"+string(end)) }
	step := func(name string, now int64) *AgentUnit {
		_, unit, _ := rt.StepAgent(context.Background(), "division", name, nil, now)
		return unit
	}

	// A finished run: out of Jangan, into Donwhang. The Trader sells the goods.
	rt.HireAgent("division", "Boss", domain.JobHunter, "[AI]Trader_Jin", 1000)
	rt.mu.Lock()
	jin := rt.findAgentLocked("division", "[AI]Trader_Jin")
	cost, stars := hiredGoodsCost(jin), jin.CargoStars
	rt.mu.Unlock()
	players["Boss"].at = Waypoint{RegionID: 25239, X: 500, Z: 500}
	step("[AI]Trader_Jin", 2000)
	players["Boss"].at = TownWaypoint(TownDonwhang)
	if unit := step("[AI]Trader_Jin", 3000); unit.HiredBy != "" || !unit.AvailableForHire {
		t.Fatal("the hire outlived the finished run")
	}
	sale := cost * int64(100+HiredGoodsMarginPerStarPct*int(stars)) / 100
	if paid["Boss"] != -(HireFeeGold+cost)+sale {
		t.Fatalf("finished run paid %d, want the sale %d after fee and goods", paid["Boss"], sale)
	}

	// The hour runs out: the goods' cost comes back.
	paid["Boss"] = 0
	rt.HireAgent("division", "Boss", domain.JobHunter, "[AI]Trader_Bao", 10_000)
	rt.mu.Lock()
	cost = hiredGoodsCost(rt.findAgentLocked("division", "[AI]Trader_Bao"))
	rt.mu.Unlock()
	players["Boss"].at = Waypoint{RegionID: 25239, X: 500, Z: 500}
	step("[AI]Trader_Bao", 20_000)
	if unit := step("[AI]Trader_Bao", 10_000+HireMaxDurationMs); unit.HiredBy != "" {
		t.Fatal("the hire outlived its hour")
	}
	if paid["Boss"] != -HireFeeGold {
		t.Fatalf("a timed-out hire returned %d, want only the fee kept", paid["Boss"]+HireFeeGold)
	}

	// The hirer logs off: the agent waits, then goes home after two minutes.
	players["Boss"].at = TownWaypoint(TownDonwhang)
	rt.HireAgent("division", "Boss", domain.JobHunter, "[AI]Hunter_Kwan", 100_000)
	players["Boss"].online = false
	if unit := step("[AI]Hunter_Kwan", 110_000); unit.HiredBy != "Boss" {
		t.Fatal("a short disconnect ended the hire")
	}
	if unit := step("[AI]Hunter_Kwan", 110_000+HireLeaderMissingMs); unit.HiredBy != "" {
		t.Fatal("the hire outlived a long disconnect")
	}
	if len(ended) != 3 || ended[0] != "[AI]Trader_Jin:completed" || ended[1] != "[AI]Trader_Bao:timed out" || ended[2] != "[AI]Hunter_Kwan:ended" {
		t.Fatalf("hire ends reported %v", ended)
	}
}

/*
================
TestGrandCaravanMustersEveryIdleTraderAndHunter
================
*/
func TestGrandCaravanMustersEveryIdleTraderAndHunter(t *testing.T) {
	var chat []string
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{OnAgentChat: func(_, _ string, _ uint16, msg string) { chat = append(chat, msg) }})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.StepAgent(context.Background(), "division", "[AI]Thief_Wisp", nil, 1000) // the standing runs leave first
	if !rt.StartGrandCaravan("division", 2000) {
		t.Fatal("grand caravan did not muster")
	}
	rt.StepAgent(context.Background(), "division", "[AI]Thief_Wisp", nil, 2100)
	if len(chat) != 1 || !strings.Contains(chat[0], "Grand caravan") {
		t.Fatalf("muster was not announced: %v", chat)
	}
	rt.mu.Lock()
	var traders, hunters int
	var town TownID
	for _, u := range rt.agents["division"] {
		if u.RunKind != RunGrand {
			continue
		}
		if u.State != StateRecruiting || u.pendingTeleport == nil && WaypointDistance(position(u), rt.terminalLocked(u.OriginTown)) > townWanderRadius*2 {
			t.Fatalf("%s is not mustering at the shop: %+v", u.Name, u)
		}
		if town != "" && u.OriginTown != town {
			t.Fatal("the grand caravan musters in more than one town")
		}
		town = u.OriginTown
		if u.Role == RoleTrader {
			traders++
		} else {
			hunters++
		}
	}
	rt.mu.Unlock()
	if traders != RosterTraders-StandingSoloRuns-StandingEscortedRuns || hunters != RosterHunters-StandingEscortedRuns*EscortsPerRun {
		t.Fatalf("grand caravan mustered %d Traders and %d Hunters", traders, hunters)
	}

	rt.StepAgent(context.Background(), "division", "[AI]Thief_Wisp", nil, 2000+GrandCaravanMusterMs)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	leads := 0
	for _, u := range rt.agents["division"] {
		if u.RunKind != RunGrand {
			continue
		}
		if u.State != StateTraveling {
			t.Fatalf("%s did not leave with the grand caravan", u.Name)
		}
		if u.EscortingTrader == "" {
			leads++
		} else if leader := rt.findAgentLocked("division", u.EscortingTrader); leader == nil || leader.RunKind != RunGrand || leader.Role != RoleTrader {
			t.Fatalf("%s follows %q outside the caravan", u.Name, u.EscortingTrader)
		}
	}
	if leads != 1 {
		t.Fatalf("grand caravan has %d leaders", leads)
	}
}

/*
================
TestSoloScenicBendIsSkippedWhenUnreachable
================
*/
func TestSoloScenicBendIsSkippedWhenUnreachable(t *testing.T) {
	a := Waypoint{RegionID: 25001, X: 100, Z: 100}
	b := Waypoint{RegionID: 25003, X: 100, Z: 100}
	route := scenicRoute([]Waypoint{a, b})
	if len(route) != 3 || !route[1].Scenic {
		t.Fatalf("a long open leg did not bend: %+v", route)
	}
	if off := WaypointDistance(route[1], offsetPoint(a, 1920, 0)); off < 600 {
		t.Fatalf("the bend is only %.0f units off the road", off)
	}

	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{Move: func(_ string, from, _ Waypoint, _ float32) (Waypoint, bool) { return from, false }})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true
	rt.mu.Lock()
	jin := rt.findAgentLocked("division", "[AI]Trader_Jin")
	jin.RunKind, jin.State, jin.Route, jin.WaypointIndex = RunSolo, StateTraveling, route, 1
	jin.RegionID, jin.X, jin.Z = a.RegionID, a.X, a.Z
	rt.mu.Unlock()
	for now := int64(1000); now <= 1000+stuckSkipMs; now += 1000 {
		rt.StepAgent(context.Background(), "division", jin.Name, nil, now)
	}
	if _, unit, _ := rt.StepAgent(context.Background(), "division", jin.Name, nil, 2000+stuckSkipMs); unit.WaypointIndex != 2 {
		t.Fatalf("a stuck solo run kept walking at its bend (index %d)", unit.WaypointIndex)
	}
	for now := int64(3000 + stuckSkipMs); now <= 3000+stuckSkipMs+stuckTeleportMs; now += 1000 {
		rt.StepAgent(context.Background(), "division", jin.Name, nil, now)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if jin = rt.findAgentLocked("division", jin.Name); jin.RegionID != b.RegionID || jin.X != b.X {
		t.Fatalf("a run stuck for a minute was not set onto its waypoint: %+v", position(jin))
	}
}
