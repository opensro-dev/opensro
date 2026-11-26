package caravanai

import (
	"context"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
)

// hireMemStore keeps hires in memory; memories and heat are not kept.
type hireMemStore struct {
	rows map[string]domain.CaravanHireRow
}

func (s *hireMemStore) LoadCaravanAgentMemories(string) ([]domain.CaravanAgentMemoryRow, error) {
	return nil, nil
}
func (s *hireMemStore) SaveCaravanAgentMemory(domain.CaravanAgentMemoryRow) error { return nil }
func (s *hireMemStore) LoadCaravanRouteHeat(string) ([]domain.CaravanRouteHeatRow, error) {
	return nil, nil
}
func (s *hireMemStore) SaveCaravanRouteHeat(domain.CaravanRouteHeatRow) error { return nil }
func (s *hireMemStore) LoadCaravanHires(division string) ([]domain.CaravanHireRow, error) {
	var out []domain.CaravanHireRow
	for _, row := range s.rows {
		if row.Division == division {
			out = append(out, row)
		}
	}
	return out, nil
}
func (s *hireMemStore) SaveCaravanHire(row domain.CaravanHireRow) error {
	s.rows[row.AgentName] = row
	return nil
}
func (s *hireMemStore) DeleteCaravanHire(_, agent string) error {
	delete(s.rows, agent)
	return nil
}

/*
================
TestHiresSurviveARestart

A restart resumes each hire: the agent waits for its hirer, rejoins the
party when they are back, and an ended hire leaves the store. A stored hire
whose agent is gone pays the goods back.
================
*/
func TestHiresSurviveARestart(t *testing.T) {
	store := &hireMemStore{rows: map[string]domain.CaravanHireRow{}}
	players := map[string]*hirePlayer{"Boss": {at: TownWaypoint(TownJangan), job: domain.JobTrader, online: true}}
	paid := map[string]int64{}
	now := time.Now().UnixMilli()

	before := hireRuntimeWith(t, store, players, &paid)
	for _, name := range []string{"[AI]Trader_Jin", "[AI]Hunter_Kwan"} {
		if reply := before.HireAgent("division", "Boss", domain.JobTrader, name, now); !strings.Contains(reply, "joins you") {
			t.Fatalf("hire of %s refused: %s", name, reply)
		}
	}
	if err := before.flushHires(); err != nil || len(store.rows) != 2 {
		t.Fatalf("hires not stored: %v %+v", err, store.rows)
	}
	goods := store.rows["[AI]Trader_Jin"].GoodsCostGold
	if goods <= 0 || store.rows["[AI]Trader_Jin"].CargoStacks != 10 {
		t.Fatalf("trader row: %+v", store.rows["[AI]Trader_Jin"])
	}
	// A hire whose agent no longer exists is paid back at boot.
	store.rows["[AI]Gone"] = domain.CaravanHireRow{Division: "division", AgentName: "[AI]Gone", PlayerName: "Boss", HiredAtMs: now, HireTown: string(TownJangan), GoodsCostGold: 100_000, CargoStacks: 5}

	players["Boss"].online = false
	paid = map[string]int64{}
	var rejoined []string
	after := hireRuntimeWith(t, store, players, &paid)
	after.hooks.OnContractFormed = func(_, _, agent string) { rejoined = append(rejoined, agent) }
	after.mu.Lock()
	jin := after.findAgentLocked("division", "[AI]Trader_Jin")
	if jin.HiredBy != "Boss" || jin.RunKind != RunHired || jin.CargoStacks != 10 || jin.HiredAtMs != now {
		after.mu.Unlock()
		t.Fatalf("hire not restored: %+v", jin)
	}
	after.mu.Unlock()

	// The hirer is still offline past the usual 2 minutes: the hire waits.
	after.StepAgent(context.Background(), "division", "[AI]Trader_Jin", nil, now+3*60_000)
	after.StepAgent(context.Background(), "division", "[AI]Trader_Jin", nil, now+4*60_000)
	if paid["Boss"] != 50_000 {
		t.Fatalf("the vanished agent's goods were not paid back once: %d", paid["Boss"])
	}
	players["Boss"].online = true
	after.StepAgent(context.Background(), "division", "[AI]Trader_Jin", nil, now+5*60_000)
	after.StepAgent(context.Background(), "division", "[AI]Hunter_Kwan", nil, now+5*60_000)
	if len(rejoined) != 2 {
		t.Fatalf("restored hires did not rejoin the party: %v", rejoined)
	}

	if err := after.flushHires(); err != nil || len(store.rows) != 2 {
		t.Fatalf("the vanished hire stays stored: %v %+v", err, store.rows)
	}
	after.HandlePlayerCommand("division", "Boss", domain.JobTrader, "!dismiss", now+6*60_000)
	if paid["Boss"] != 50_000+goods {
		t.Fatalf("dismiss after restore paid %d, want the goods %d back", paid["Boss"]-50_000, goods)
	}
	if err := after.flushHires(); err != nil || len(store.rows) != 0 {
		t.Fatalf("ended hires stay stored: %v %+v", err, store.rows)
	}
}

/*
================
TestHireRosterMarksWhoIsAvailableHere

The hire window lists the city's kinds only, available ones first; a
Trader waiting in another town or one hired already shows why not, and a
star choice prices the goods.
================
*/
func TestHireRosterMarksWhoIsAvailableHere(t *testing.T) {
	players := map[string]*hirePlayer{"Boss": {at: TownWaypoint(TownJangan), job: domain.JobTrader, online: true}}
	paid := map[string]int64{}
	rt := hireRuntime(t, players, &paid)
	if reply := rt.HireAgentStars("division", "Boss", domain.JobTrader, "[AI]Trader_Jin", 5, 1000); !strings.Contains(reply, "joins you") {
		t.Fatalf("hire refused: %s", reply)
	}
	if want := -(rt.HireFee() + GoodsForStars(5)); paid["Boss"] != want {
		t.Fatalf("5-star hire charged %d, want %d", paid["Boss"], want)
	}
	rows := rt.HireRoster("division", "Boss", domain.JobTrader, TownJangan, 1000)
	seenUnavailable := false
	status := map[string]string{}
	for _, row := range rows {
		if row.Role == RoleThief {
			t.Fatalf("a city lists Thief %s", row.Name)
		}
		if row.Available && seenUnavailable {
			t.Fatal("an available agent is listed after an unavailable one")
		}
		seenUnavailable = seenUnavailable || !row.Available
		status[row.Name] = row.Status
	}
	if status["[AI]Trader_Jin"] != "Working for you" || !strings.HasPrefix(status["[AI]Trader_Mei"], "Waiting in") || status["[AI]Hunter_Kwan"] != "Available" {
		t.Fatalf("statuses: Jin %q, Mei %q, Kwan %q", status["[AI]Trader_Jin"], status["[AI]Trader_Mei"], status["[AI]Hunter_Kwan"])
	}
	for _, row := range rt.HireRoster("division", "Boss", domain.JobTrader, TownThiefDen, 1000) {
		if row.Role != RoleThief || row.Available {
			t.Fatalf("the Den offers %s (%v) to a Trader", row.Name, row.Available)
		}
	}
}
