/*
===========================================================================

world_test.go - caravan trust boundaries and escort eligibility

===========================================================================
*/
package caravanai

import (
	"context"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/testsupport/wait"
	"testing"
	"time"
)

/*
================
TestEscortCannotBeStolenOrCompletedRemotely
================
*/
func TestEscortCannotBeStolenOrCompletedRemotely(t *testing.T) {
	for _, eligible := range []bool{true, false} {
		book := NewContractBook()
		contract := book.BindAITraderEscort("division", "trader", "hunter", TownJangan, TownDonwhang, 4, 1000)
		if !contract.Active {
			t.Fatal("first contract refused")
		}
		if book.BindAITraderEscort("division", "trader", "intruder", TownJangan, TownDonwhang, 4, 2000).Active {
			t.Fatal("contract stolen")
		}
		for now := int64(2000); now <= 41000; now += 1000 {
			book.SampleEscort("division", "trader", eligible, now)
		}
		_, paid := book.CompleteAITraderRun("division", "trader", RegionDonwhangTown, 41000, nil)
		if paid != eligible {
			t.Fatalf("proximity eligibility %v, paid %v", eligible, paid)
		}
		if _, again := book.CompleteAITraderRun("division", "trader", RegionDonwhangTown, 42000, nil); again {
			t.Fatal("duplicate payout")
		}
	}
}

/*
================
TestWorldCallbackCanReenterTheRoster
================
*/
func TestWorldCallbackCanReenterTheRoster(t *testing.T) {
	done := make(chan struct{})
	var rt *Runtime
	rt = NewRuntime(Config{Enabled: true}, nil, WorldHooks{OnParleyCargoDrop: func(division, _ string, _ uint16, _, _, _ float32, _ int, _ int64) {
		rt.EnsureDivisionRoster(division)
		close(done)
	}})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.RecordPlayerAttack("division", "[AI]Trader_Jin", "thief", 9000, 1000)
	go func() {
		rt.StepAgent(context.Background(), "division", "[AI]Trader_Jin", []ObservedPlayer{{Name: "thief", JobType: domain.JobThief, HPPercent: 100, Distance: 10}}, 1100)
	}()
	wait.Eventually(t, time.Second, "world callback to run without the AI lock", func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
}

/*
================
TestDeathCancelsEscortAndChatCannotDealDamage
================
*/
func TestDeathCancelsEscortAndChatCannotDealDamage(t *testing.T) {
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{})
	t.Cleanup(func() { _ = rt.Close() })
	roster := rt.EnsureDivisionRoster("division")
	trader := roster[0]
	rt.contracts.BindAITraderEscort("division", trader.Name, "hunter", TownJangan, TownDonwhang, 4, 1000)
	rt.HandlePlayerCommand("division", "attacker", domain.JobThief, "!attack "+trader.Name+" 9999999", 1000)
	if hp := rt.EnsureDivisionRoster("division")[0].HP; hp != trader.HP {
		t.Fatal("chat dealt damage")
	}
	rt.RecordPlayerAttack("division", trader.Name, "attacker", trader.HP+1, 2000)
	if _, ok := rt.contracts.ActiveContractFor("division", trader.Name); ok {
		t.Fatal("dead trader retained escort")
	}
}

/*
================
TestMoveUsesWorldValidationAndShippedTerminal
================
*/
func TestMoveUsesWorldValidationAndShippedTerminal(t *testing.T) {
	terminal := TownWaypoint(TownJangan)
	terminal.X = 500
	calls := 0
	rt := NewRuntime(Config{Enabled: true, Terminals: map[TownID]Waypoint{TownJangan: terminal}}, nil, WorldHooks{Move: func(_ string, from, to Waypoint, distance float32) (Waypoint, bool) { calls++; return from, false }})
	t.Cleanup(func() { _ = rt.Close() })
	roster := rt.EnsureDivisionRoster("division")
	_, unit, _ := rt.StepAgent(context.Background(), "division", roster[0].Name, nil, 1000)
	if calls != 1 || unit.X != 500 || unit.Route[0].X != 500 {
		t.Fatal("movement bypassed the world or ignored the terminal")
	}
}

/*
================
TestSafeDialogue
================
*/
func TestSafeDialogue(t *testing.T) {
	for _, raw := range []string{"<script>bad</script>", "[item:123]", "visit https://example.com"} {
		if SafeDialogue(raw) != "" {
			t.Fatalf("unsafe dialogue admitted: %q", raw)
		}
	}
	if SafeDialogue(" hello\x00\u202eworld ") != "helloworld" && SafeDialogue(" hello\x00\u202eworld ") != "hello world" {
		t.Fatal("control characters were not stripped")
	}
}

/*
================
TestConvoyStartsTogetherAndFollowsAdmittedFerry
================
*/
func TestConvoyStartsTogetherAndFollowsAdmittedFerry(t *testing.T) {
	crossed := false
	arrival := Waypoint{RegionID: TownWaypoint(TownJangan).RegionID, X: 900, Z: 900}
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{FollowFerry: func(_, _ string, leader Waypoint) (Waypoint, bool) { crossed = true; return arrival, true }})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.mu.Lock()
	hunter := rt.findAgentLocked("division", "[AI]Hunter_Wei")
	leader := rt.findAgentLocked("division", hunter.EscortingTrader)
	if WaypointDistance(Waypoint{RegionID: hunter.RegionID, X: hunter.X, Z: hunter.Z}, Waypoint{RegionID: leader.RegionID, X: leader.X, Z: leader.Z}) > 12 {
		t.Fatal("convoy started in different towns")
	}
	leader.RegionID, leader.X, leader.Z = arrival.RegionID, arrival.X, arrival.Z
	hunter.State = StateTraveling
	rt.mu.Unlock()
	_, unit, _ := rt.StepAgent(context.Background(), "division", "[AI]Hunter_Wei", nil, 1000)
	if !crossed || unit.RegionID != arrival.RegionID || unit.X != arrival.X {
		t.Fatal("escort did not follow the ferry")
	}
}
