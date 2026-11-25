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
	return hireRuntimeWith(t, nil, players, paid)
}

func hireRuntimeWith(t *testing.T, store MemoryStore, players map[string]*hirePlayer, paid *map[string]int64) *Runtime {
	t.Helper()
	rt := NewRuntime(Config{Enabled: true}, store, WorldHooks{
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
TestFollowerRoamsNearbyAndRejoinsInAStraightLine
================
*/
func TestFollowerRoamsNearbyAndRejoinsInAStraightLine(t *testing.T) {
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

	set(300)
	if rt.EscortMustRejoin("division", "[AI]Hunter_Kwan") || !step(1900).catchingUp || lastTarget != field {
		t.Fatalf("escort 30m away should run straight to the hirer, got %+v", lastTarget)
	}
	// A nearby escort roams around the stationary hirer.
	set(170)
	for now := int64(2000); now < 12_000; now += 100 {
		unit := step(now)
		if d := WaypointDistance(position(unit), field); d > EscortRoamRadius+1 {
			t.Fatalf("roaming escort drifted %.0f units away", d)
		}
	}
	if WaypointDistance(lastTarget, field) < 140 {
		t.Fatalf("roam target %.0f units from the hirer: it should circle, not stand on it", WaypointDistance(lastTarget, field))
	}

	// 120m behind (between 90m follow and 150m teleport): it runs straight after the hirer.
	set(-1200)
	unit := step(20_000)
	if !unit.catchingUp || lastTarget != field {
		t.Fatalf("escort 120m behind did not run after the hirer (target %+v)", lastTarget)
	}
	if !rt.EscortMustRejoin("division", "[AI]Hunter_Kwan") {
		t.Fatal("escort 120m behind must stop fighting to rejoin")
	}

	set(-600)
	if !rt.EscortMustRejoin("division", "[AI]Hunter_Kwan") {
		t.Fatal("a returning escort should ignore combat at 60m")
	}
	set(-300)
	if rt.EscortMustRejoin("division", "[AI]Hunter_Kwan") {
		t.Fatal("a returning escort should be able to fight again at 30m")
	}
	set(-1000)
	if rt.EscortMustRejoin("division", "[AI]Hunter_Kwan") {
		t.Fatal("a rejoined escort should be able to fight at 100m")
	}

	// Beyond the teleport range: return beside the hirer.
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
	// Traders are hired in the town they wait in: bring Bao to Jangan.
	rt.mu.Lock()
	rt.findAgentLocked("division", "[AI]Trader_Bao").place(TownWaypoint(TownJangan), 0)
	rt.mu.Unlock()
	for _, name := range []string{"[AI]Trader_Jin", "[AI]Trader_Bao", "[AI]Hunter_Kwan"} {
		if reply := rt.HireAgent("division", "Boss", domain.JobTrader, name, 1000); !strings.Contains(reply, "joins you") {
			t.Fatalf("hire of %s refused: %s", name, reply)
		}
	}
	rt.SetPartyOrder("division", "Boss", []string{"Boss", "[AI]Hunter_Kwan", "[AI]Trader_Bao", "[AI]Trader_Jin"}, 1000)
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
TestHandsOffHireEndsWhenTheTradersSellOrThePartyGoes

A hired Trader that left town rides to the next town's shop, sells there
and stays (no teleport home); the hired Hunter is released at the next
town once the Traders all sold. The hirer disbanding the party ends a
hire (goods' cost back), and a long logout ends it too. No hour limit.
================
*/
func TestHandsOffHireEndsWhenTheTradersSellOrThePartyGoes(t *testing.T) {
	players := map[string]*hirePlayer{"Boss": {at: TownWaypoint(TownJangan), job: domain.JobHunter, online: true}}
	paid := map[string]int64{}
	var ended []string
	rt := hireRuntime(t, players, &paid)
	rt.hooks.OnHireEnded = func(_, player, agent string, end HireEnd, _ int64) { ended = append(ended, agent+":"+string(end)) }
	step := func(name string, now int64) *AgentUnit {
		_, unit, _ := rt.StepAgent(context.Background(), "division", name, nil, now)
		return unit
	}
	place := func(name string, at Waypoint, now int64) {
		rt.mu.Lock()
		u := rt.findAgentLocked("division", name)
		u.place(at, now)
		u.pendingTeleport = nil
		rt.mu.Unlock()
	}
	field := Waypoint{RegionID: 25239, X: 500, Z: 500}
	donwhang := TownWaypoint(TownDonwhang)
	// 40m from the shop, still in the town region.
	street := Waypoint{RegionID: donwhang.RegionID, X: donwhang.X + 400, Y: donwhang.Y, Z: donwhang.Z}
	if street.X > 1900 {
		street.X = donwhang.X - 400
	}

	rt.HireAgent("division", "Boss", domain.JobHunter, "[AI]Trader_Jin", 1000)
	rt.HireAgent("division", "Boss", domain.JobHunter, "[AI]Hunter_Kwan", 1000)
	rt.SetPartyOrder("division", "Boss", []string{"Boss", "[AI]Trader_Jin", "[AI]Hunter_Kwan"}, 1000)
	rt.mu.Lock()
	jin := rt.findAgentLocked("division", "[AI]Trader_Jin")
	cost, stars := hiredGoodsCost(jin), jin.CargoStars
	rt.mu.Unlock()

	// Out of Jangan. The hirer coming back into a town ends nothing.
	players["Boss"].at = field
	place("[AI]Trader_Jin", field, 2000)
	place("[AI]Hunter_Kwan", field, 2000)
	step("[AI]Trader_Jin", 2000)
	step("[AI]Hunter_Kwan", 2000)
	players["Boss"].at = donwhang
	if unit := step("[AI]Hunter_Kwan", 3000); unit.HiredBy != "Boss" {
		t.Fatal("the hirer entering a town ended the hire")
	}
	if unit := step("[AI]Trader_Jin", 3000); unit.HiredBy != "Boss" {
		t.Fatal("the hire ended before the Trader reached a town")
	}

	// Into Donwhang: Jin rides to the shop before it sells.
	place("[AI]Trader_Jin", street, 4000)
	unit := step("[AI]Trader_Jin", 4000)
	if unit.HiredBy != "Boss" || WaypointDistance(position(unit), donwhang) >= WaypointDistance(street, donwhang) {
		t.Fatalf("the Trader sold away from the shop or stood still: hired by %q at %+v", unit.HiredBy, position(unit))
	}
	for now := int64(4100); now < 30_000 && unit.HiredBy != ""; now += 100 {
		unit = step("[AI]Trader_Jin", now)
	}
	// Released, it may already take its first stroll step in town.
	if unit.HiredBy != "" || WaypointDistance(position(unit), donwhang) > 2*hireSellReach {
		t.Fatalf("the Trader did not sell at the shop: hired by %q at %+v (shop %+v) ends %v", unit.HiredBy, position(unit), donwhang, ended)
	}
	if at, _ := rt.placeAtLocked(position(unit)); at != TownDonwhang {
		t.Fatalf("the Trader left the town it sold in: %v", at)
	}
	sale := cost * int64(100+HiredGoodsMarginPerStarPct*int(stars)) / 100
	if paid["Boss"] != -(2*HireFeeGold+cost)+sale {
		t.Fatalf("finished run paid %d, want the sale %d after fees and goods", paid["Boss"], sale)
	}

	// The Hunter, done, goes when it reaches a town.
	place("[AI]Hunter_Kwan", field, 31_000)
	players["Boss"].at = field
	if unit := step("[AI]Hunter_Kwan", 31_000); unit.HiredBy != "Boss" {
		t.Fatal("the Hunter was released outside a town")
	}
	place("[AI]Hunter_Kwan", street, 32_000)
	if unit := step("[AI]Hunter_Kwan", 32_000); unit.HiredBy != "" || unit.RegionID != donwhang.RegionID {
		t.Fatalf("the Hunter was not released where it stands in town: %+v", unit)
	}

	// The party disbands: the hire ends, the goods' cost comes back.
	paid["Boss"] = 0
	players["Boss"].at = donwhang
	rt.HireAgent("division", "Boss", domain.JobHunter, "[AI]Trader_Mei", 40_000)
	rt.mu.Lock()
	cost = hiredGoodsCost(rt.findAgentLocked("division", "[AI]Trader_Mei"))
	rt.mu.Unlock()
	rt.SetPartyOrder("division", "Boss", nil, 41_000)
	if unit := step("[AI]Trader_Mei", 41_000); unit.HiredBy != "Boss" {
		t.Fatal("a hire ended before its agent joined the party")
	}
	rt.SetPartyOrder("division", "Boss", []string{"Boss", "[AI]Trader_Mei"}, 42_000)
	rt.SetPartyOrder("division", "Boss", nil, 43_000)
	if unit := step("[AI]Trader_Mei", 43_000); unit.HiredBy != "" {
		t.Fatal("the hire outlived the disbanded party")
	}
	if paid["Boss"] != -HireFeeGold {
		t.Fatalf("a disbanded hire returned %d, want only the fee kept", paid["Boss"]+HireFeeGold)
	}

	// The hirer logs off: the agent waits, then goes home after two minutes.
	rt.HireAgent("division", "Boss", domain.JobHunter, "[AI]Hunter_Kwan", 100_000)
	players["Boss"].online = false
	rt.SetPartyOrder("division", "Boss", nil, 105_000)
	if unit := step("[AI]Hunter_Kwan", 110_000); unit.HiredBy != "Boss" {
		t.Fatal("a short disconnect ended the hire")
	}
	if unit := step("[AI]Hunter_Kwan", 110_000+HireLeaderMissingMs); unit.HiredBy != "" {
		t.Fatal("the hire outlived a long disconnect")
	}
	want := []string{"[AI]Trader_Jin:completed", "[AI]Hunter_Kwan:completed", "[AI]Trader_Mei:ended", "[AI]Hunter_Kwan:ended"}
	if strings.Join(ended, ",") != strings.Join(want, ",") {
		t.Fatalf("hire ends reported %v, want %v", ended, want)
	}
}

/*
================
TestHirerPicksWhoLeadsTheTraders

The party board's lead button: the hirer or any hired Trader leads; the
rest of the Traders follow it, Hunters keep following the hirer.
================
*/
func TestHirerPicksWhoLeadsTheTraders(t *testing.T) {
	players := map[string]*hirePlayer{"Boss": {at: TownWaypoint(TownJangan), job: domain.JobTrader, online: true}}
	paid := map[string]int64{}
	rt := hireRuntime(t, players, &paid)
	rt.mu.Lock()
	rt.findAgentLocked("division", "[AI]Trader_Bao").place(TownWaypoint(TownJangan), 0)
	rt.mu.Unlock()
	if leader, who := rt.LeadState("division", "Boss"); leader != "" || who != nil {
		t.Fatalf("lead state without hires: %q %v", leader, who)
	}
	for _, name := range []string{"[AI]Trader_Jin", "[AI]Trader_Bao", "[AI]Hunter_Kwan"} {
		rt.HireAgent("division", "Boss", domain.JobTrader, name, 1000)
	}
	if leader, who := rt.LeadState("division", "Boss"); leader != "[AI]Trader_Jin" || strings.Join(who, ",") != "Boss,[AI]Trader_Jin,[AI]Trader_Bao" {
		t.Fatalf("default lead state %q %v", leader, who)
	}
	if reply := rt.SetLead("division", "Boss", "[AI]Hunter_Kwan"); !strings.Contains(reply, "Only you") {
		t.Fatalf("a Hunter was made lead: %s", reply)
	}
	escort := func(name string) string {
		_, unit, _ := rt.StepAgent(context.Background(), "division", name, nil, 2000)
		return unit.EscortingTrader
	}
	rt.SetLead("division", "Boss", "[AI]Trader_Bao")
	if leader, _ := rt.LeadState("division", "Boss"); leader != "[AI]Trader_Bao" || escort("[AI]Trader_Bao") != "" || escort("[AI]Trader_Jin") != "[AI]Trader_Bao" || escort("[AI]Hunter_Kwan") != "Boss" {
		t.Fatal("the chosen Trader does not lead")
	}
	rt.SetLead("division", "Boss", "boss")
	if leader, _ := rt.LeadState("division", "Boss"); leader != "Boss" || escort("[AI]Trader_Bao") != "Boss" || escort("[AI]Trader_Jin") != "Boss" {
		t.Fatal("the hirer does not lead")
	}
}

/*
================
TestGrandCaravanRunsHourlyWithTwoFiveStarTradersAndSixHunters
================
*/
func TestGrandCaravanRunsHourlyWithTwoFiveStarTradersAndSixHunters(t *testing.T) {
	var chat []string
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{OnAgentChat: func(_, _ string, _ uint16, msg string) { chat = append(chat, msg) }})
	t.Cleanup(func() { _ = rt.Close() })
	step := func(now int64) {
		rt.StepAgent(context.Background(), "division", "[AI]Thief_Wisp", nil, now)
	}
	const boot = 1000
	step(boot)
	step(boot + GrandCaravanFirstMs - 1)
	if len(chat) != 0 {
		t.Fatalf("grand caravan mustered before its first hour: %v", chat)
	}
	muster := boot + GrandCaravanFirstMs
	step(muster)
	if len(chat) != 1 || !strings.Contains(chat[0], "2 Traders and 6 Hunters") {
		t.Fatalf("the complete muster was not announced: %v", chat)
	}
	traders, hunters := 0, 0
	var town TownID
	for _, u := range rt.EnsureDivisionRoster("division") {
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
	if traders != 2 || hunters != 6 {
		t.Fatalf("grand caravan mustered %d Traders and %d Hunters", traders, hunters)
	}

	departure := boot + GrandCaravanIntervalMs
	step(departure - 1)
	if rt.grand["division"].departAtMs != departure {
		t.Fatal("grand caravan departed early")
	}
	step(departure)
	guards := map[string]int{}
	var leader string
	for _, u := range rt.EnsureDivisionRoster("division") {
		if u.RunKind != RunGrand {
			continue
		}
		if u.State != StateTraveling {
			t.Fatalf("%s did not leave with the grand caravan", u.Name)
		}
		if u.Role == RoleTrader {
			if u.CargoStars != 5 || u.CargoStacks != 10 {
				t.Fatalf("%s departed without a full five-star load", u.Name)
			}
			if u.EscortingTrader == "" {
				if leader != "" {
					t.Fatal("grand caravan has two leaders")
				}
				leader = u.Name
			}
		} else {
			guards[u.EscortingTrader]++
		}
	}
	if leader == "" || len(guards) != 2 || guards[leader] != 3 {
		t.Fatalf("grand caravan did not split the six guards between its two traders: %v", guards)
	}
	if rt.StartGrandCaravan("division", departure+1) {
		t.Fatal("a second grand caravan overlapped the active one")
	}
	rt.mu.Lock()
	rt.finishRunLocked(rt.findAgentLocked("division", leader), departure+1000)
	rt.mu.Unlock()
	step(muster + GrandCaravanIntervalMs - 1)
	if len(chat) != 1 {
		t.Fatal("the second grand caravan mustered before its hour")
	}
	step(muster + GrandCaravanIntervalMs)
	if len(chat) != 2 || !strings.Contains(chat[1], "2 Traders and 6 Hunters") {
		t.Fatalf("the grand caravan did not muster again on schedule: %v", chat)
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

/*
================
TestTraderRecruitsAfterRobberyAndTravelsSoloAfterDelivery
================
*/
func TestTraderRecruitsAfterRobberyAndTravelsSoloAfterDelivery(t *testing.T) {
	rt := hireRuntime(t, nil, nil)
	trader := rt.findAgentLocked("division", "[AI]Trader_Jin")
	trader.standingCargoStars = 5
	rt.dispatchTraderLocked(trader, RunSolo, nil, 1000)
	if trader.CargoStars != 2 {
		t.Fatalf("solo dispatch loaded %d stars", trader.CargoStars)
	}
	trader.RegionID, trader.X, trader.Z = 25239, 1000, 1000
	if _, defeated, _ := rt.RecordPlayerAttack("division", trader.Name, "[AI]Thief_Kael", 50000, 2000); !defeated {
		t.Fatal("test thief did not defeat the trader")
	}
	rt.ActorSynchronized("division", trader.Name)
	now := trader.CooldownUntilMs + TownRestDurationMs
	if trader.escortPreference != 1 || rt.eligibleTraderLocked("division", now, RunSolo) == trader {
		t.Fatal("robbed solo trader remained eligible to dispatch alone")
	}
	rt.manualRuns = false
	rt.scheduleRunsLocked("division", now)
	if trader.RunKind != RunEscorted || rt.assignedEscortCountLocked(trader) != 1 || trader.CargoStars != 5 {
		t.Fatalf("trader did not leave with one hunter and the usual cargo: run=%s escorts=%d stars=%d",
			trader.RunKind, rt.assignedEscortCountLocked(trader), trader.CargoStars)
	}
	rt.finishRunLocked(trader, now+1000)
	if trader.escortPreference != -1 || rt.assignedEscortCountLocked(trader) != 0 {
		t.Fatal("successful delivery should release the hunter and allow a solo next run")
	}
}

/*
================
TestRevivedTraderCarriesOnlyGoodsStillInItsWagon

A hired Trader waits dead for resurrection. When the world finds its
transport empty (the death scattered the goods) it carries none after; a
wagon that still holds them keeps them.
================
*/
func TestRevivedTraderCarriesOnlyGoodsStillInItsWagon(t *testing.T) {
	players := map[string]*hirePlayer{"Boss": {at: TownWaypoint(TownJangan), job: domain.JobTrader, online: true}}
	paid := map[string]int64{}
	rt := hireRuntime(t, players, &paid)
	rt.mu.Lock()
	rt.findAgentLocked("division", "[AI]Trader_Bao").place(TownWaypoint(TownJangan), 0)
	rt.mu.Unlock()
	rt.HireAgent("division", "Boss", domain.JobTrader, "[AI]Trader_Jin", 1000)
	rt.HireAgent("division", "Boss", domain.JobTrader, "[AI]Trader_Bao", 1000)
	cargo := func(name string) (int, bool) {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		u := rt.findAgentLocked("division", name)
		return u.CargoStacks, u.DeadUntilMs > 0
	}
	rt.RecordPlayerStrike("division", "[AI]Trader_Jin", "AIThief_Kael", 1, true, 2000)
	rt.RecordPlayerStrike("division", "[AI]Trader_Bao", "", 1, true, 2000)
	rt.NoteCargoGone("division", "[AI]Trader_Jin")
	if stacks, waiting := cargo("[AI]Trader_Jin"); !waiting || stacks != 0 {
		t.Fatalf("emptied wagon: waiting %v, still carries %d stacks", waiting, stacks)
	}
	if stacks, waiting := cargo("[AI]Trader_Bao"); !waiting || stacks == 0 {
		t.Fatalf("loaded wagon: waiting %v, lost its goods (%d stacks)", waiting, stacks)
	}
}

/*
================
TestOnlyAFatalStrikeKillsAnAgent

The agent's HP copy can lag the world's heals: a strike the combat
authority did not judge fatal never starts a death, a fatal one always does.
================
*/
func TestOnlyAFatalStrikeKillsAnAgent(t *testing.T) {
	players := map[string]*hirePlayer{"Boss": {at: TownWaypoint(TownJangan), job: domain.JobTrader, online: true}}
	paid := map[string]int64{}
	rt := hireRuntime(t, players, &paid)
	rt.HireAgent("division", "Boss", domain.JobTrader, "[AI]Hunter_Kwan", 1000)
	if _, defeated, _ := rt.RecordPlayerStrike("division", "[AI]Hunter_Kwan", "Thief", 1<<30, false, 2000); defeated {
		t.Fatal("a non-fatal strike killed the agent")
	}
	if _, defeated, _ := rt.RecordPlayerStrike("division", "[AI]Hunter_Kwan", "Thief", 1, true, 3000); !defeated {
		t.Fatal("a fatal strike did not kill the agent")
	}
}

/*
================
TestFollowersSetOffOnTheirOwnTime

Two Hunters hired by one player settle near their spots; when the player
walks off, they do not set off on the same tick: each has its own slack
and reaction (followerTemper).
================
*/
func TestFollowersSetOffOnTheirOwnTime(t *testing.T) {
	field := Waypoint{RegionID: 25239, X: 900, Z: 900}
	players := map[string]*hirePlayer{"Boss": {at: TownWaypoint(TownJangan), job: domain.JobTrader, online: true}}
	paid := map[string]int64{}
	rt := hireRuntime(t, players, &paid)
	rt.hooks.Move = func(_ string, from, to Waypoint, distance float32) (Waypoint, bool) {
		region, x, z, arrived := StepWaypoint(from.RegionID, from.X, from.Z, to, distance)
		return Waypoint{RegionID: region, X: x, Z: z}, arrived
	}
	names := []string{"[AI]Hunter_Kwan", "[AI]Hunter_Lei"}
	if a, b := names[0], names[1]; func() bool {
		sa, ra, _ := followerTemper(a)
		sb, rb, _ := followerTemper(b)
		return sa == sb && ra == rb
	}() {
		t.Fatal("two names share one temper")
	}
	for _, name := range names {
		rt.HireAgent("division", "Boss", domain.JobTrader, name, 1000)
	}
	players["Boss"].at = field
	now := int64(2000)
	step := func(name string) *AgentUnit {
		_, unit, _ := rt.StepAgent(context.Background(), "division", name, nil, now)
		return unit
	}
	// Settle beside the stationary player: both standing on their spots.
	for still := 0; still < 10 && now < 120_000; now += 100 {
		resting := true
		for _, name := range names {
			if u := step(name); u.escortMoving || u.catchingUp {
				resting = false
			}
		}
		if resting {
			still++
		} else {
			still = 0
		}
	}
	players["Boss"].at = Waypoint{RegionID: field.RegionID, X: field.X + 150, Z: field.Z}
	started := map[string]int64{}
	for deadline := now + 20_000; now < deadline && len(started) < len(names); now += 100 {
		for _, name := range names {
			if u := step(name); started[name] == 0 && (u.escortMoving || u.catchingUp) {
				started[name] = now
			}
		}
	}
	if len(started) != len(names) {
		t.Fatalf("not every follower set off after its leader: %v", started)
	}
	if started[names[0]] == started[names[1]] {
		t.Fatalf("both followers set off on the same tick %d", started[names[0]])
	}
}

/*
================
TestTakenLeadHeadsForTheNearestTownFromHere

A hired Trader that takes the lead plans from where it stands: of the
roads it knows, the one whose end is nearest along the road wins, joined
at the waypoint ahead of it, never back where its old route left off. The
lead waits while another hired Trader is more than 50m behind.
================
*/
func TestTakenLeadHeadsForTheNearestTownFromHere(t *testing.T) {
	players := map[string]*hirePlayer{"Boss": {at: TownWaypoint(TownJangan), job: domain.JobTrader, online: true}}
	paid := map[string]int64{}
	rt := hireRuntime(t, players, &paid)
	wp := func(x float32, town TownID) Waypoint { return Waypoint{RegionID: 25239, X: x, Z: 500, Town: town} }
	// One straight road: Jangan at x=0 ... Donwhang at x=1800; its reverse.
	road := []Waypoint{wp(0, TownJangan), wp(600, ""), wp(1200, ""), wp(1800, TownDonwhang)}
	back := []Waypoint{road[3], road[2], road[1], road[0]}
	rt.cfg.Highway = map[[2]TownID][]Waypoint{{TownJangan, TownDonwhang}: road, {TownDonwhang, TownJangan}: back}
	rt.cfg.Terminals = map[TownID]Waypoint{}
	rt.mu.Lock()
	jin := rt.findAgentLocked("division", "[AI]Trader_Jin")
	rt.mu.Unlock()
	rt.HireAgent("division", "Boss", domain.JobTrader, "[AI]Trader_Jin", 1000)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	jin.place(wp(1300, ""), 1000)

	// Hired in Jangan, out only a moment: Donwhang is the goal, joined ahead.
	jin.hireLeftTown, jin.leftTownAtMs = true, 900
	rt.planHiredRunLocked(jin, 1000)
	if jin.DestTown != TownDonwhang || jin.WaypointIndex != 3 {
		t.Fatalf("lead plans %s from waypoint %d, want Donwhang from 3", jin.DestTown, jin.WaypointIndex)
	}
	// Long enough out that Jangan counts, and standing near it: Jangan wins.
	jin.place(wp(400, ""), 1000)
	rt.planHiredRunLocked(jin, 900+HireRunMinOutMs)
	if jin.DestTown != TownJangan {
		t.Fatalf("lead plans %s, want the nearer Jangan", jin.DestTown)
	}

	// Taking the lead back drops the old route.
	jin.EscortingTrader, jin.Route = "Boss", road
	rt.refreshHireLeaderLocked(jin)
	if jin.EscortingTrader != "" || jin.Route != nil {
		t.Fatalf("the new lead kept its old route (%d points) or leader %q", len(jin.Route), jin.EscortingTrader)
	}

	// Another hired Trader 60m behind holds the lead; 40m does not.
	bao := rt.findAgentLocked("division", "[AI]Trader_Bao")
	bindHire(bao, "Boss", TownJangan, 0, 1000)
	bao.place(Waypoint{RegionID: jin.RegionID, X: jin.X + 600, Z: jin.Z}, 1000)
	if !rt.hiredTraderBehindLocked(jin) {
		t.Fatal("the lead does not wait for a Trader 60m behind")
	}
	bao.place(Waypoint{RegionID: jin.RegionID, X: jin.X + 400, Z: jin.Z}, 1000)
	if rt.hiredTraderBehindLocked(jin) {
		t.Fatal("the lead waits for a Trader 40m away")
	}
}
