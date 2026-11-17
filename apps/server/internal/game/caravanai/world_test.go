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
	if !rt.EnsureDivisionRoster("division")[0].RespawnPending {
		t.Fatal("defeat did not request a live actor/transport respawn")
	}
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

/*
================
TestCaravanRecoversExhaustedManaForItsSkillRotation
================
*/
func TestCaravanRecoversExhaustedManaForItsSkillRotation(t *testing.T) {
	healed := false
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{Heal: func(_, _ string, amount int64) int64 { healed = true; return 1000 }})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.ObserveVitals("division", "[AI]Hunter_Wei", 1000, 0, false, 1000)
	decision, _, _ := rt.StepAgent(context.Background(), "division", "[AI]Hunter_Wei", nil, 2000)
	if decision.Action != ActionUsePotion || !healed {
		t.Fatal("exhausted actor kept trying to cast")
	}
}

/*
================
TestTraderWaitsForItsFightingOrDistantEscort
================
*/
func TestTraderWaitsForItsFightingOrDistantEscort(t *testing.T) {
	moves, crossings := 0, 0
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{
		Move: func(_ string, from, _ Waypoint, _ float32) (Waypoint, bool) {
			moves++
			from.X += 10
			return from, false
		},
		Ferry: func(_, _ string, _, _ uint32) (Waypoint, bool) {
			crossings++
			return Waypoint{RegionID: 0x62aa, X: 800, Z: 100}, true
		},
	})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	leader := rt.findAgentLocked("division", "[AI]Trader_Mei")
	guard := rt.findAgentLocked("division", "[AI]Hunter_Wei")
	leader.RegionID, leader.X, leader.Z = 0x62aa, 100, 100
	leader.State = StateTraveling
	leader.Route = []Waypoint{{RegionID: 0x62aa, X: 200, Z: 100}, {RegionID: 0x62aa, X: 800, Z: 100, FerrySourceRef: 1, FerryTargetID: 2}}
	leader.WaypointIndex = 0
	guard.RegionID, guard.X, guard.Z = leader.RegionID, 110, 100
	guard.State = StateInCombat
	rt.StepAgent(context.Background(), "division", leader.Name, nil, 1000)
	if moves != 0 {
		t.Fatal("trader abandoned its fighting escort")
	}
	guard.State = StateTraveling
	guard.X = 500
	rt.StepAgent(context.Background(), "division", leader.Name, nil, 1100)
	if moves != 0 {
		t.Fatal("trader increased the gap to its distant escort")
	}
	guard.X = 110
	rt.StepAgent(context.Background(), "division", leader.Name, nil, 1200)
	if moves != 1 {
		t.Fatal("trader did not resume once its escort caught up")
	}
	leader.WaypointIndex = 1
	guard.X = 500
	rt.StepAgent(context.Background(), "division", leader.Name, nil, 1300)
	if crossings != 0 {
		t.Fatal("trader crossed the ferry without its escort")
	}
	guard.X = leader.X
	rt.StepAgent(context.Background(), "division", leader.Name, nil, 1400)
	if crossings != 1 {
		t.Fatal("convoy could not resume its ferry crossing")
	}
}

/*
================
TestNativeManaRefusalRequestsRecoveryAboveThePercentageThreshold
================
*/
func TestNativeManaRefusalRequestsRecoveryAboveThePercentageThreshold(t *testing.T) {
	healed := 0
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{Heal: func(_, _ string, _ int64) int64 { healed++; return 14500 }})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	guard := rt.findAgentLocked("division", "[AI]Hunter_Wei")
	rt.RequestManaRecovery("division", guard.Name)
	rt.ObserveVitals("division", guard.Name, int64(guard.MaxHP), int64(guard.MaxMP/4), false, 1000)
	decision, _, _ := rt.StepAgent(context.Background(), "division", guard.Name, nil, 2000)
	if decision.Action != ActionUsePotion || healed != 1 {
		t.Fatal("native mana refusal did not recover above the 20% threshold")
	}
	rt.ObserveVitals("division", guard.Name, int64(guard.MaxHP), int64(guard.MaxMP), false, 2100)
	rt.StepAgent(context.Background(), "division", guard.Name, nil, 4000)
	if healed != 1 {
		t.Fatal("mana recovery request did not clear after healing")
	}
}
