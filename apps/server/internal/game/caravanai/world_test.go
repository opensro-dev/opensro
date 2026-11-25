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
	rt.manualRuns = true
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
	var arrival Waypoint
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{FollowFerry: func(_, _ string, leader Waypoint) (Waypoint, bool) {
		crossed = leader.FerrySourceRef == 1 && leader.FerryTargetID == 2
		return arrival, true
	}})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true
	pairEscort(rt, "[AI]Trader_Mei", "[AI]Hunter_Wei")
	rt.mu.Lock()
	hunter := rt.findAgentLocked("division", "[AI]Hunter_Wei")
	leader := rt.findAgentLocked("division", hunter.EscortingTrader)
	if WaypointDistance(Waypoint{RegionID: hunter.RegionID, X: hunter.X, Z: hunter.Z}, Waypoint{RegionID: leader.RegionID, X: leader.X, Z: leader.Z}) > 12 {
		t.Fatal("convoy started in different towns")
	}
	// A long crossing still uses the leader's ferry, before catch-up can teleport.
	arrival = offsetPoint(position(hunter), 2000, 0)
	arrival.FerrySourceRef, arrival.FerryTargetID = 1, 2
	leader.Route = []Waypoint{arrival, offsetPoint(arrival, 100, 0)}
	leader.WaypointIndex = 1
	leader.RegionID, leader.X, leader.Z = arrival.RegionID, arrival.X, arrival.Z
	hunter.State = StateTraveling
	rt.mu.Unlock()
	_, unit, _ := rt.StepAgent(context.Background(), "division", "[AI]Hunter_Wei", nil, 1000)
	if !crossed || unit.RegionID != arrival.RegionID || unit.X != arrival.X {
		t.Fatal("escort did not follow the ferry")
	}

	crossed = false
	leader.place(offsetPoint(arrival, 800, 0), 1100)
	_, unit, _ = rt.StepAgent(context.Background(), "division", hunter.Name, nil, 1100)
	if crossed || WaypointDistance(position(unit), position(leader)) >= 800 {
		t.Fatal("escort retried the ferry after the convoy had left the landing")
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
	rt.manualRuns = true
	rt.ObserveVitals("division", "[AI]Hunter_Wei", 1000, 0, false, 1000)
	decision, _, _ := rt.StepAgent(context.Background(), "division", "[AI]Hunter_Wei", nil, 2000)
	if decision.Action != ActionUsePotion || !healed {
		t.Fatal("exhausted actor kept trying to cast")
	}
}

/*
================
TestTraderKeepsMovingWithCloseEscort
================
*/
func TestTraderKeepsMovingWithCloseEscort(t *testing.T) {
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
	rt.manualRuns = true
	pairEscort(rt, "[AI]Trader_Mei", "[AI]Hunter_Wei")
	leader := rt.findAgentLocked("division", "[AI]Trader_Mei")
	guard := rt.findAgentLocked("division", "[AI]Hunter_Wei")
	leader.RegionID, leader.X, leader.Z = 0x62aa, 100, 100
	leader.State = StateTraveling
	leader.Route = []Waypoint{{RegionID: 0x62aa, X: 200, Z: 100}, {RegionID: 0x62aa, X: 800, Z: 100, FerrySourceRef: 1, FerryTargetID: 2}}
	leader.WaypointIndex = 0
	guard.RegionID, guard.X, guard.Z = leader.RegionID, 110, 100
	guard.State = StateInCombat
	rt.StepAgent(context.Background(), "division", leader.Name, nil, 1000)
	if moves != 1 {
		t.Fatal("a nearby fighting escort stalled the trader")
	}
	guard.State = StateTraveling
	guard.X = 1600 // 150m behind: catching up, not yet teleported
	rt.StepAgent(context.Background(), "division", leader.Name, nil, 1100)
	if moves != 1 {
		t.Fatal("trader increased the gap to its distant escort")
	}
	guard.X = 110
	rt.StepAgent(context.Background(), "division", leader.Name, nil, 1200)
	if moves != 2 {
		t.Fatal("trader did not resume once its escort caught up")
	}
	leader.WaypointIndex = 1
	guard.X = 1600
	rt.StepAgent(context.Background(), "division", leader.Name, nil, 1300)
	if crossings != 0 {
		t.Fatal("trader crossed the ferry without its escort")
	}
	guard.X = leader.X
	rt.StepAgent(context.Background(), "division", leader.Name, nil, 1400)
	if crossings != 1 {
		t.Fatal("convoy could not resume its ferry crossing")
	}

	leader.WaypointIndex = 0
	other := rt.findAgentLocked("division", "[AI]Hunter_Ming")
	other.EscortingTrader = leader.Name
	other.State = StateTraveling
	other.place(position(leader), 1500)
	guard.place(offsetPoint(position(leader), 1200, 0), 1500)
	rt.StepAgent(context.Background(), "division", leader.Name, nil, 1500)
	if moves != 3 {
		t.Fatal("a convoy with close cover waited for one lagging escort")
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
	rt.manualRuns = true
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

/*
================
TestThiefPotionsAreBoundedAndNeverStalemateIndefinitely
================
*/
func TestThiefPotionsAreBoundedAndNeverStalemateIndefinitely(t *testing.T) {
	heals := 0
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{Heal: func(_, _ string, _ int64) int64 { heals++; return 5000 }})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true
	rt.mu.Lock()
	rt.findAgentLocked("division", "[AI]Thief_Kael").PotionCharges = 3
	rt.mu.Unlock()
	rt.ObserveVitals("division", "[AI]Thief_Kael", 4000, 1000, false, 1000)
	for i := 0; i < 6; i++ {
		now := int64(2000 + i*2000)
		rt.ObserveVitals("division", "[AI]Thief_Kael", 4000, 1000, false, now-100)
		dec, unit, _ := rt.StepAgent(context.Background(), "division", "[AI]Thief_Kael", []ObservedPlayer{
			{Name: "Guard", JobType: domain.JobHunter, HPPercent: 100, Distance: 100},
		}, now)
		if i < 3 && dec.Action != ActionUsePotion {
			t.Fatalf("step %d: expected potion use while charges remain (%d), got %+v", i, unit.PotionCharges, dec)
		}
		if i >= 3 && dec.Action == ActionUsePotion {
			t.Fatalf("step %d: depleted thief kept healing endlessly (%+v)", i, unit)
		}
	}
	if heals != 3 {
		t.Fatalf("expected thief potion heals capped at 3, got %d", heals)
	}
}

/*
================
TestEscortDisengagesDistantStalemateToRejoinTrader
================
*/
func TestEscortDisengagesDistantStalemateToRejoinTrader(t *testing.T) {
	movedTo := Waypoint{}
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{
		Move: func(_ string, from, to Waypoint, _ float32) (Waypoint, bool) {
			movedTo = to
			return from, false
		},
	})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true
	pairEscort(rt, "[AI]Trader_Mei", "[AI]Hunter_Wei")
	rt.mu.Lock()
	mei := rt.findAgentLocked("division", "[AI]Trader_Mei")
	wei := rt.findAgentLocked("division", "[AI]Hunter_Wei")
	mei.RegionID, mei.X, mei.Z = TownWaypoint(TownDonwhang).RegionID, 400, 1500
	mei.State = StateTraveling
	wei.RegionID, wei.Z = mei.RegionID, mei.Z
	rt.mu.Unlock()

	for _, tc := range []struct {
		gap    float32
		action ActionKind
	}{
		{1000, ActionAttackTarget},
		{1100, ActionContinueRoute},
		{650, ActionContinueRoute},
		{300, ActionAttackTarget},
		{850, ActionAttackTarget},
	} {
		rt.mu.Lock()
		wei.X = mei.X + tc.gap
		rt.mu.Unlock()
		dec, _, _ := rt.StepAgentWithWorldThreat(context.Background(), "division", wei.Name, nil, true, 2000)
		if dec.Action != tc.action {
			t.Fatalf("escort gap %.0fm: action=%s, want %s", tc.gap/UnitsPerMeter, dec.Action, tc.action)
		}
		if tc.action == ActionContinueRoute && (movedTo.RegionID != mei.RegionID || movedTo.X != mei.X) {
			t.Fatalf("escort gap %.0fm: expected movement toward Mei, got %+v", tc.gap/UnitsPerMeter, movedTo)
		}
	}
}

/*
================
TestDedicatedEscortRespawnsWithLeaderAndReattachesAtDeparture
================
*/
func TestDedicatedEscortRespawnsWithLeaderAndReattachesAtDeparture(t *testing.T) {
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true
	pairEscort(rt, "[AI]Trader_Mei", "[AI]Hunter_Wei")

	_, defeated, _ := rt.RecordPlayerAttack("division", "[AI]Hunter_Wei", "KaelPlayer", 50000, 1000)
	if !defeated {
		t.Fatal("expected Wei to be defeated")
	}
	rt.mu.Lock()
	wei := rt.findAgentLocked("division", "[AI]Hunter_Wei")
	mei := rt.findAgentLocked("division", "[AI]Trader_Mei")
	if wei.EscortingTrader != mei.Name || wei.OriginTown != mei.OriginTown {
		t.Fatalf("expected dedicated escort to stay paired at Mei's origin %s, got escort=%q town=%s", mei.OriginTown, wei.EscortingTrader, wei.OriginTown)
	}
	mei.State = StateIdleInTown
	wei.State = StateIdleInTown
	rt.dispatchTraderLocked(mei, RunEscorted, nil, 2000)
	if wei.State != StateTraveling || wei.EscortingTrader != mei.Name || wei.PotionCharges <= 0 {
		t.Fatalf("expected Wei to dispatch with Mei, got state=%s escort=%q charges=%d", wei.State, wei.EscortingTrader, wei.PotionCharges)
	}
	rt.mu.Unlock()
}

/*
================
TestEscortedTraderStaysMountedAndHealsTransportAtLowHP
================
*/
func TestEscortedTraderStaysMountedAndHealsTransportAtLowHP(t *testing.T) {
	heals := 0
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{Heal: func(_, _ string, _ int64) int64 { heals++; return 21908 }})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true
	pairEscort(rt, "[AI]Trader_Mei", "[AI]Hunter_Wei")
	rt.mu.Lock()
	mei := rt.findAgentLocked("division", "[AI]Trader_Mei")
	wei := rt.findAgentLocked("division", "[AI]Hunter_Wei")
	mei.RegionID, mei.X, mei.Z = 25239, 1000, 500
	mei.State = StateTraveling
	wei.RegionID, wei.X, wei.Z = 25239, 1005, 500
	wei.State = StateTraveling
	rt.mu.Unlock()

	// Escorted greedy trader keeps riding along route when a hostile is within 320u.
	dec, unit, _ := rt.StepAgent(context.Background(), "division", "[AI]Trader_Mei", []ObservedPlayer{
		{Name: "Bandit", JobType: domain.JobThief, HPPercent: 100, Distance: 80},
	}, 2000)
	if dec.Action != ActionContinueRoute || unit.State == StateInCombat {
		t.Fatalf("expected escorted trader to remain mounted on route, got dec=%+v state=%s", dec, unit.State)
	}

	// Transport drops to 30% HP while rider stays 100% HP -> triggers potion heal.
	rt.ObserveTransportVitals("division", "[AI]Trader_Mei", 10000, 35500)
	dec, _, _ = rt.StepAgent(context.Background(), "division", "[AI]Trader_Mei", nil, 3000)
	if dec.Action != ActionUsePotion || heals != 1 {
		t.Fatalf("expected trader to heal damaged transport, got dec=%+v heals=%d", dec, heals)
	}
}

/*
================
TestBowAgentStopsAtRangedDistanceAndHoldsWhileCasting
================
*/
func TestBowAgentStopsAtRangedDistanceAndHoldsWhileCasting(t *testing.T) {
	moves := 0
	casting := false
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{
		Move:    func(_ string, from, _ Waypoint, _ float32) (Waypoint, bool) { moves++; return from, false },
		Casting: func(_, _ string) bool { return casting },
	})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true
	rt.mu.Lock()
	kael := rt.findAgentLocked("division", "[AI]Thief_Kael")
	kael.RegionID, kael.X, kael.Z = 25239, 1000, 500
	kael.State = StateInCombat
	rt.mu.Unlock()

	// Within bow engagement stop range (100 <= 140) -> no approach toward melee.
	rt.ApproachWorldThreat("division", "[AI]Thief_Kael", Waypoint{RegionID: 25239, X: 1100, Z: 500})
	if moves != 0 {
		t.Fatalf("bow thief walked into melee despite being inside bow range (moves=%d)", moves)
	}

	// Out of bow stop range (220 > 140), but mid-cast -> must stay planted.
	casting = true
	rt.ApproachWorldThreat("division", "[AI]Thief_Kael", Waypoint{RegionID: 25239, X: 1220, Z: 500})
	if moves != 0 {
		t.Fatalf("bow thief moved mid-cast (moves=%d)", moves)
	}
	_, updated, _ := rt.StepAgent(context.Background(), "division", kael.Name, nil, 1000)
	if updated.X != 1000 || updated.Z != 500 || moves != 0 {
		t.Fatal("the route step moved a thief while its cast was open")
	}

	// Not casting and outside 140 -> approaches cleanly.
	casting = false
	rt.ApproachWorldThreat("division", "[AI]Thief_Kael", Waypoint{RegionID: 25239, X: 1220, Z: 500})
	if moves != 1 {
		t.Fatalf("expected approach when out of bow range and idle, got moves=%d", moves)
	}
}

/*
================
TestWorldThreatSuppressesCompetingPlayerChase
================
*/
func TestWorldThreatSuppressesCompetingPlayerChase(t *testing.T) {
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{
		Player: func(_, _ string) (PlayerPosition, bool) {
			return PlayerPosition{
				ObservedPlayer: ObservedPlayer{Name: "[AI]Thief_Kael", JobType: domain.JobThief, HPPercent: 100, Distance: 80},
				At:             Waypoint{RegionID: 26008, X: 520, Z: 1100},
			}, true
		},
	})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true
	rt.mu.Lock()
	wei := rt.findAgentLocked("division", "[AI]Hunter_Wei")
	wei.RegionID, wei.X, wei.Z = 26008, 600, 1170
	wei.State = StateInCombat
	wei.RunSpeed = 75
	rt.mu.Unlock()

	dec, unit, ok := rt.StepAgentWithWorldThreat(context.Background(), "division", "[AI]Hunter_Wei", nil, true, 2000)
	if !ok || dec.Action != ActionAttackTarget || unit.X != 600 || unit.Z != 1170 {
		t.Fatalf("route movement competed with the native threat: decision=%+v unit=%+v", dec, unit)
	}
	after := rt.ApproachWorldThreat("division", "[AI]Hunter_Wei", Waypoint{RegionID: 26008, X: 760, Z: 1320})
	if after.X <= 600 || after.Z <= 1170 {
		t.Fatalf("expected Wei to advance northeast toward NPC threat without tug-of-war, got (%.1f, %.1f)", after.X, after.Z)
	}
}

/*
================
TestHunterManaRecoveryPreservesBattleHPPotions
================
*/
func TestHunterManaRecoveryPreservesBattleHPPotions(t *testing.T) {
	heals := 0
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{Heal: func(_, _ string, _ int64) int64 { heals++; return 14500 }})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true
	guard := rt.findAgentLocked("division", "[AI]Hunter_Wei")
	startCharges := guard.PotionCharges

	// Full HP with exhausted MP recovers mana without consuming battle HP charges.
	rt.ObserveVitals("division", guard.Name, int64(guard.MaxHP), 0, false, 1000)
	dec, updated, _ := rt.StepAgent(context.Background(), "division", guard.Name, nil, 2000)
	if dec.Action != ActionUsePotion || heals != 1 {
		t.Fatalf("expected MP recovery potion use, got dec=%+v heals=%d", dec, heals)
	}
	if updated.PotionCharges != startCharges {
		t.Fatalf("mana recovery consumed HP battle potion charge (%d -> %d)", startCharges, updated.PotionCharges)
	}

	// Low HP (<=45%) consumes a battle HP charge.
	rt.ObserveVitals("division", guard.Name, int64(guard.MaxHP/4), int64(guard.MaxMP), false, 3000)
	dec, updated, _ = rt.StepAgent(context.Background(), "division", guard.Name, nil, 4000)
	if dec.Action != ActionUsePotion || updated.PotionCharges != startCharges-1 {
		t.Fatalf("expected low-HP heal to consume 1 potion charge, got dec=%+v charges=%d", dec, updated.PotionCharges)
	}
}

func TestUnreachableWorldThreatDisengagesAfterCombatStall(t *testing.T) {
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{
		Move: func(_ string, from, _ Waypoint, _ float32) (Waypoint, bool) {
			// Simulate a nav-blocked cliff/wall preventing any approach progress.
			return from, false
		},
	})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true
	guard := rt.findAgentLocked("division", "[AI]Hunter_Wei")
	leader := rt.findAgentLocked("division", "[AI]Trader_Mei")
	guard.RegionID, guard.X, guard.Z, guard.State = 25238, 1157, 470, StateTraveling
	leader.RegionID, leader.X, leader.Z, leader.State = 25238, 1160, 460, StateTraveling
	threat := Waypoint{RegionID: 25238, X: 1123, Z: 590}

	snap, stalled := rt.ApproachWorldThreatAt("division", guard.Name, threat, 1000)
	if stalled || snap.State != StateInCombat {
		t.Fatalf("expected initial engagement without stall, got state=%s stalled=%v", snap.State, stalled)
	}
	snap, stalled = rt.ApproachWorldThreatAt("division", guard.Name, threat, 4000)
	if stalled || snap.State != StateInCombat {
		t.Fatalf("expected combat pursuit before timeout, got state=%s stalled=%v", snap.State, stalled)
	}
	snap, stalled = rt.ApproachWorldThreatAt("division", guard.Name, threat, 6100)
	if !stalled || snap.State != StateTraveling {
		t.Fatalf("expected unreachable threat to stall and resume StateTraveling, got state=%s stalled=%v", snap.State, stalled)
	}
	if rt.waitForEscortLocked(leader) {
		t.Fatalf("expected trader no longer held in town/route once escort disengaged stalled threat")
	}
}

/*
================
TestHiredEscortTeleportsBeyond200mAndFiresContractPartyHook
================
*/
func TestHiredEscortTeleportsBeyond200mAndFiresContractPartyHook(t *testing.T) {
	jangan := TownWaypoint(TownJangan)
	playerAt := jangan
	var formed []string
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{
		OnHireFee: func(_, _ string, _ int64) bool { return true },
		OnContractFormed: func(division, playerName, agentName string) {
			formed = append(formed, division+":"+playerName+":"+agentName)
		},
		Player: func(_, name string) (PlayerPosition, bool) {
			if name != "PlayerTrader" {
				return PlayerPosition{}, false
			}
			return PlayerPosition{
				ObservedPlayer: ObservedPlayer{Name: name, JobType: domain.JobTrader, HPPercent: 100},
				At:             playerAt,
			}, true
		},
	})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true
	rt.mu.Lock()
	kwan := rt.findAgentLocked("division", "[AI]Hunter_Kwan")
	kwan.RegionID, kwan.X, kwan.Y, kwan.Z = jangan.RegionID, jangan.X, jangan.Y, jangan.Z
	kwan.RunSpeed = 75
	rt.mu.Unlock()

	reply, handled := rt.HandlePlayerCommand("division", "PlayerTrader", domain.JobTrader, "!hire [AI]Hunter_Kwan", 1000)
	if !handled || reply == "" {
		t.Fatalf("expected !hire to be handled, got handled=%v reply=%q", handled, reply)
	}
	if len(formed) != 1 || formed[0] != "division:PlayerTrader:[AI]Hunter_Kwan" {
		t.Fatalf("expected OnContractFormed callback, got %v", formed)
	}
	if !rt.IsContractPartner("division", "PlayerTrader", "[AI]Hunter_Kwan") {
		t.Fatal("expected hiring trader to be recognized as contract partner")
	}
	if rt.IsContractPartner("division", "OtherTrader", "[AI]Hunter_Kwan") {
		t.Fatal("non-hirer must not be recognized as contract partner")
	}

	// Player outside town 140m ahead -> the hunter stops fighting to rejoin.
	playerAt = Waypoint{RegionID: 25001, X: 1500, Z: 500}
	rt.mu.Lock()
	kwan = rt.findAgentLocked("division", "[AI]Hunter_Kwan")
	kwan.pendingTeleport = nil
	kwan.RegionID, kwan.X, kwan.Z = 25001, 100, 500
	kwan.State = StateInCombat
	rt.mu.Unlock()
	if !rt.EscortMustRejoin("division", "[AI]Hunter_Kwan") {
		t.Fatal("expected hired hunter >120m behind player to disengage combat to rejoin")
	}

	// Player more than 200m ahead -> the hunter teleports beside the hiring player.
	playerAt = Waypoint{RegionID: 25002, X: 600, Z: 500}
	_, updated, ok := rt.StepAgentWithWorldThreat(context.Background(), "division", "[AI]Hunter_Kwan", nil, false, 2000)
	if !ok || updated == nil {
		t.Fatal("StepAgentWithWorldThreat failed")
	}
	if d := WaypointDistance(position(updated), playerAt); d > 31 {
		t.Fatalf("expected hired hunter >200m behind to teleport beside the player, still %.1f away", d)
	}
}

/*
================
TestRespawnedEscortFollowsHighwayGateOutOfTown
================
*/
func TestRespawnedEscortFollowsHighwayGateOutOfTown(t *testing.T) {
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true
	pairEscort(rt, "[AI]Trader_Mei", "[AI]Hunter_Wei")
	rt.mu.Lock()
	mei := rt.findAgentLocked("division", "[AI]Trader_Mei")
	wei := rt.findAgentLocked("division", "[AI]Hunter_Wei")
	mei.OriginTown, mei.DestTown = TownDonwhang, TownHotan
	mei.Route = rt.route(mei)
	mei.WaypointIndex = 2
	// 120m ahead of the town shop, in another direction than the gate.
	ahead := offsetPoint(TownWaypoint(TownDonwhang), 1200, 0)
	mei.RegionID, mei.X, mei.Z = ahead.RegionID, ahead.X, ahead.Z
	mei.State = StateTraveling

	wei.OriginTown, wei.DestTown = TownDonwhang, TownHotan
	wei.Route = rt.route(wei)
	wei.WaypointIndex = 0
	dh := TownWaypoint(TownDonwhang)
	wei.RegionID, wei.X, wei.Z = dh.RegionID, dh.X, dh.Z
	wei.State = StateTraveling
	rt.mu.Unlock()

	gate := mei.Route[1]
	beforeGate := WaypointDistance(Waypoint{RegionID: wei.RegionID, X: wei.X, Z: wei.Z}, gate)
	beforeLeader := WaypointDistance(Waypoint{RegionID: wei.RegionID, X: wei.X, Z: wei.Z}, Waypoint{RegionID: mei.RegionID, X: mei.X, Z: mei.Z})
	_, updated, ok := rt.StepAgentWithWorldThreat(context.Background(), "division", "[AI]Hunter_Wei", nil, false, 2000)
	if !ok || updated == nil {
		t.Fatal("StepAgentWithWorldThreat failed")
	}
	afterGate := WaypointDistance(Waypoint{RegionID: updated.RegionID, X: updated.X, Z: updated.Z}, gate)
	afterLeader := WaypointDistance(Waypoint{RegionID: updated.RegionID, X: updated.X, Z: updated.Z}, Waypoint{RegionID: mei.RegionID, X: mei.X, Z: mei.Z})
	if afterGate >= beforeGate {
		t.Fatalf("expected Wei in town to move toward highway gate Route[1], before=%.2f after=%.2f", beforeGate, afterGate)
	}
	// Route[1] leads out of town even when Mei is in a different direction than the gate.
	_ = beforeLeader
	_ = afterLeader
	if WaypointDistance(Waypoint{RegionID: dh.RegionID, X: dh.X, Z: dh.Z}, Waypoint{RegionID: updated.RegionID, X: updated.X, Z: updated.Z}) == 0 {
		t.Fatal("expected Wei to advance toward highway gate")
	}
}

/*
================
TestDeadCharactersAreIgnoredAsIfAbsent
================
*/
func TestDeadCharactersAreIgnoredAsIfAbsent(t *testing.T) {
	brain := NewHardcodedBrain()
	ctx := context.Background()
	deadThief := ObservedPlayer{Name: "DeadThief", JobType: domain.JobThief, HPPercent: 0, Distance: 10, TrustScore: domain.CaravanVendettaScore}
	deadTrader := ObservedPlayer{Name: "DeadTrader", JobType: domain.JobTrader, HPPercent: 0, Distance: 10, TrustScore: domain.CaravanVendettaScore}
	deadHunter := ObservedPlayer{Name: "DeadHunter", JobType: domain.JobHunter, HPPercent: 0, Distance: 10, TrustScore: domain.CaravanAllyScore}

	// Hunter must not target or camp a dead Thief corpse.
	dec := brain.Decide(ctx, Situation{
		AgentName: "[AI]Hunter_Wei", Role: RoleHunter, RegionID: RegionKarakoramPass, HPPercent: 100,
		Players: []ObservedPlayer{deadThief},
	})
	if dec.Action != ActionContinueRoute {
		t.Fatalf("hunter engaged dead thief corpse: %+v", dec)
	}

	// Thief must not target a dead Trader or dead Hunter corpse.
	dec = brain.Decide(ctx, Situation{
		AgentName: "[AI]Thief_Kael", Role: RoleThief, RegionID: RegionKarakoramPass, HPPercent: 100,
		Players: []ObservedPlayer{deadTrader, deadHunter},
	})
	if dec.Action != ActionContinueRoute {
		t.Fatalf("thief engaged dead trader/hunter corpse: %+v", dec)
	}

	// Trader must not count a dead Hunter as an ally or greet a dead trusted ally.
	if countFriendlyHunters([]ObservedPlayer{deadHunter}) != 0 {
		t.Fatal("dead hunter was counted as a friendly hunter")
	}
	dec = brain.Decide(ctx, Situation{
		AgentName: "[AI]Trader_Jin", Role: RoleTrader, RegionID: RegionKarakoramPass, HPPercent: 100,
		Players: []ObservedPlayer{deadHunter},
	})
	if dec.Action != ActionContinueRoute {
		t.Fatalf("trader reacted to dead hunter corpse: %+v", dec)
	}

	// Runtime StepAgent must ignore a dead Thief player on the road and keep moving.
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true
	rt.mu.Lock()
	wei := rt.findAgentLocked("division", "[AI]Hunter_Wei")
	mei := rt.findAgentLocked("division", "[AI]Trader_Mei")
	mei.RegionID, mei.X, mei.Z, mei.State = RegionKarakoramPass, 500, 500, StateTraveling
	wei.RegionID, wei.X, wei.Z, wei.State = RegionKarakoramPass, 510, 500, StateInCombat
	wei.EscortingTrader = mei.Name
	rt.mu.Unlock()

	dec, updated, ok := rt.StepAgent(ctx, "division", "[AI]Hunter_Wei", []ObservedPlayer{deadThief}, 2000)
	if !ok || dec.Action != ActionContinueRoute || updated.State != StateTraveling {
		t.Fatalf("expected hunter to ignore dead thief corpse and resume escorting, got dec=%+v state=%s", dec, updated.State)
	}
}

/*
================
TestGroupedRespawnDelayResurrectionAndSoloInstantRespawn
================
*/
func TestGroupedRespawnDelayResurrectionAndSoloInstantRespawn(t *testing.T) {
	ctx := context.Background()
	playerAt := Waypoint{RegionID: 25239, X: 1400, Z: 1000}
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{
		Player: func(_, name string) (PlayerPosition, bool) {
			if name != "PlayerTrader" {
				return PlayerPosition{}, false
			}
			return PlayerPosition{
				ObservedPlayer: ObservedPlayer{Name: name, JobType: domain.JobTrader, HPPercent: 100},
				At:             playerAt,
			}, true
		},
	})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true

	// 1. Solo Thief respawns immediately in town.
	rt.mu.Lock()
	solo := rt.findAgentLocked("division", "[AI]Thief_Kael")
	solo.RegionID, solo.X, solo.Z = 25239, 900, 900
	solo.HiredBy, solo.EscortingTrader, solo.DedicatedEscortFor, solo.RunKind = "", "", "", RunNone
	rt.mu.Unlock()
	_, defeated, _ := rt.RecordPlayerAttack("division", "[AI]Thief_Kael", "Guard", 999999, 1000)
	if !defeated {
		t.Fatal("expected solo thief defeat")
	}
	rt.mu.Lock()
	solo = rt.findAgentLocked("division", "[AI]Thief_Kael")
	if solo.DeadUntilMs != 0 || !solo.RespawnPending || solo.HP <= 0 {
		rt.mu.Unlock()
		t.Fatalf("expected solo thief to respawn immediately, got DeadUntilMs=%d RespawnPending=%v HP=%d", solo.DeadUntilMs, solo.RespawnPending, solo.HP)
	}
	rt.mu.Unlock()

	// 2. Hired Hunter dies on the highway -> waits 4 minutes on the ground (does NOT teleport back).
	deathAt := Waypoint{RegionID: 25239, X: 900, Z: 1000}
	rt.mu.Lock()
	kwan := rt.findAgentLocked("division", "[AI]Hunter_Kwan")
	kwan.HiredBy = "PlayerTrader"
	kwan.EscortingTrader = "PlayerTrader"
	kwan.State = StateTraveling
	kwan.RegionID, kwan.X, kwan.Z = deathAt.RegionID, deathAt.X, deathAt.Z
	rt.mu.Unlock()

	_, defeated, _ = rt.RecordPlayerAttack("division", "[AI]Hunter_Kwan", "Bandit", 999999, 2000)
	if !defeated {
		t.Fatal("expected hired hunter defeat")
	}
	// Step 1 minute later while player is >200m away: must stay dead at deathAt, no teleport.
	_, snap, ok := rt.StepAgent(ctx, "division", "[AI]Hunter_Kwan", nil, 2000+60_000)
	if !ok || snap.DeadUntilMs != 2000+GroupedRespawnMs || snap.HP != 0 || snap.X != deathAt.X || snap.RespawnPending {
		t.Fatalf("expected hired hunter to stay dead on ground for 4min, got %+v", snap)
	}
	if rt.EscortMustRejoin("division", "[AI]Hunter_Kwan") {
		t.Fatal("dead hunter must not trigger EscortMustRejoin")
	}

	// 3. Player resurrects Kwan during the 4-minute window -> revives in place immediately.
	rt.ObserveVitals("division", "[AI]Hunter_Kwan", 5000, 1000, false, 2000+90_000)
	rt.mu.Lock()
	kwan = rt.findAgentLocked("division", "[AI]Hunter_Kwan")
	if kwan.DeadUntilMs != 0 || kwan.HP != 5000 || kwan.X != deathAt.X || kwan.State != StateTraveling {
		rt.mu.Unlock()
		t.Fatalf("expected resurrected hunter to revive in place, got DeadUntilMs=%d HP=%d X=%.1f State=%s", kwan.DeadUntilMs, kwan.HP, kwan.X, kwan.State)
	}
	rt.mu.Unlock()

	// 4. Die again and let the full 4-minute timer expire -> respawns in town.
	_, defeated, _ = rt.RecordPlayerAttack("division", "[AI]Hunter_Kwan", "Bandit", 999999, 100_000)
	if !defeated {
		t.Fatal("expected second defeat")
	}
	_, snap, ok = rt.StepAgent(ctx, "division", "[AI]Hunter_Kwan", nil, 100_000+GroupedRespawnMs)
	if !ok || snap.DeadUntilMs != 0 || !snap.RespawnPending || snap.HP != snap.MaxHP || !isTownRegion(snap.RegionID) {
		t.Fatalf("expected hunter to respawn in town after 4min, got DeadUntilMs=%d RespawnPending=%v HP=%d Region=%d", snap.DeadUntilMs, snap.RespawnPending, snap.HP, snap.RegionID)
	}
}

/*
================
TestDisbandedEscortsAndReinforcementsTeleportToTownImmediately
================
*/
func TestDisbandedEscortsAndReinforcementsTeleportToTownImmediately(t *testing.T) {
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true

	desert := Waypoint{RegionID: RegionKarakoramPass, X: 900, Z: 900}
	rt.mu.Lock()
	jin := rt.findAgentLocked("division", "[AI]Trader_Jin")
	feng := rt.findAgentLocked("division", "[AI]Hunter_Feng")
	chen := rt.findAgentLocked("division", "[AI]Hunter_Chen")
	jin.place(desert, 1000)
	jin.State = StateTraveling
	feng.place(offsetPoint(desert, 20, 0), 1000)
	feng.State, feng.EscortingTrader, feng.AvailableForHire = StateTraveling, jin.Name, false
	chen.place(offsetPoint(desert, -20, 0), 1000)
	chen.State, chen.ReinforcingTrader, chen.AvailableForHire = StateReinforcing, jin.Name, false
	rt.mu.Unlock()

	_, defeated, _ := rt.RecordPlayerAttack("division", jin.Name, "Warlock", 999999, 2000)
	if !defeated {
		t.Fatal("expected Jin defeat")
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for _, h := range []*AgentUnit{feng, chen} {
		if !isTownRegion(h.RegionID) || h.State != StateIdleInTown || !h.AvailableForHire {
			t.Fatalf("expected %s teleported back to town after Jin died, got region=%d state=%s hireable=%v", h.Name, h.RegionID, h.State, h.AvailableForHire)
		}
	}
}

/*
================
TestHunterWithRessKeepsDeadTraderAndRevivesInPlace
================
*/
func TestHunterWithRessKeepsDeadTraderAndRevivesInPlace(t *testing.T) {
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true
	pairEscort(rt, "[AI]Trader_Jin", "[AI]Hunter_Chen")
	rt.SetCanResurrect("division", "[AI]Hunter_Chen", true)

	field := Waypoint{RegionID: 25254, X: 960, Z: 960}
	rt.mu.Lock()
	jin := rt.findAgentLocked("division", "[AI]Trader_Jin")
	chen := rt.findAgentLocked("division", "[AI]Hunter_Chen")
	jin.place(field, 1000)
	jin.State = StateTraveling
	jin.CargoStacks = 10
	chen.place(offsetPoint(field, 250, 0), 1000)
	chen.State = StateTraveling
	rt.mu.Unlock()

	// Jin dies while Chen (who has Resurrection) is alive and escorting him.
	rt.ObserveVitals("division", "[AI]Trader_Jin", 0, 100, false, 2000)

	rt.mu.Lock()
	if jin.DeadUntilMs != 2000+GroupedRespawnMs {
		t.Fatalf("expected dead trader with ress hunter to stay on ground for 4 min, DeadUntilMs=%d", jin.DeadUntilMs)
	}
	if chen.EscortingTrader != "[AI]Trader_Jin" || isTownRegion(chen.RegionID) {
		t.Fatalf("expected ress hunter to stay with fallen trader, escort=%q region=%d", chen.EscortingTrader, chen.RegionID)
	}
	rt.mu.Unlock()

	targetName, targetAt, ok := rt.ResurrectTarget("division", "[AI]Hunter_Chen", 3000)
	if !ok || targetName != "[AI]Trader_Jin" || targetAt.RegionID != field.RegionID {
		t.Fatalf("expected ResurrectTarget to point at fallen Jin, got ok=%v name=%q at=%+v", ok, targetName, targetAt)
	}

	// Stepping Chen moves him closer to Jin rather than releasing him to town.
	before := WaypointDistance(position(chen), field)
	_, updatedChen, _ := rt.StepAgent(context.Background(), "division", "[AI]Hunter_Chen", nil, 3000)
	after := WaypointDistance(position(updatedChen), field)
	if after >= before || isTownRegion(updatedChen.RegionID) {
		t.Fatalf("expected ress hunter to approach fallen trader, before=%.1f after=%.1f region=%d", before, after, updatedChen.RegionID)
	}

	// Once Jin is revived in place, both resume their run on the road.
	rt.ObserveVitals("division", "[AI]Trader_Jin", 5000, 1000, false, 4000)
	rt.mu.Lock()
	if jin.DeadUntilMs != 0 || jin.HP != 5000 || jin.RegionID != field.RegionID || jin.CargoStacks != 10 {
		t.Fatalf("expected Jin revived in place, deadUntil=%d hp=%d region=%d", jin.DeadUntilMs, jin.HP, jin.RegionID)
	}
	rt.mu.Unlock()
}

/*
================
TestTransportLossWithRessHunterEndsRunCleanlyInsteadOfLoopingOnFoot
================
*/
func TestTransportLossWithRessHunterEndsRunCleanlyInsteadOfLoopingOnFoot(t *testing.T) {
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true
	pairEscort(rt, "[AI]Trader_Jin", "[AI]Hunter_Chen")
	rt.SetCanResurrect("division", "[AI]Hunter_Chen", true)

	field := Waypoint{RegionID: 25254, X: 960, Z: 960}
	rt.mu.Lock()
	jin := rt.findAgentLocked("division", "[AI]Trader_Jin")
	chen := rt.findAgentLocked("division", "[AI]Hunter_Chen")
	jin.place(field, 1000)
	jin.State = StateTraveling
	chen.place(offsetPoint(field, 100, 0), 1000)
	chen.State = StateTraveling
	rt.mu.Unlock()

	// Jin's transport dies while Jin is still alive (HP > 0). A dead horse
	// cannot be resurrected, so Jin must cleanly respawn in town (restoring
	// its horse) instead of entering DeadUntilMs and looping on foot.
	rt.ObserveVitals("division", "[AI]Trader_Jin", 21000, 5000, true, 2000)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if jin.DeadUntilMs != 0 || !jin.RespawnPending || !isTownRegion(jin.RegionID) {
		t.Fatalf("expected transport loss to cleanly respawn trader in town, deadUntil=%d respawn=%v region=%d", jin.DeadUntilMs, jin.RespawnPending, jin.RegionID)
	}
	if !isTownRegion(chen.RegionID) || chen.State != StateIdleInTown {
		t.Fatalf("expected escort to disband back to town on transport loss, region=%d state=%s", chen.RegionID, chen.State)
	}
}

/*
================
TestGrandCaravanBothTraderGroupsFollowTheFerry
================
*/
func TestGrandCaravanBothTraderGroupsFollowTheFerry(t *testing.T) {
	crossings := map[string]int{}
	rt := NewRuntime(Config{Enabled: true}, nil, WorldHooks{
		FollowFerry: func(_, name string, at Waypoint) (Waypoint, bool) {
			if at.FerrySourceRef == 10 && at.FerryTargetID == 20 {
				crossings[name]++
			}
			return at, true
		},
	})
	t.Cleanup(func() { _ = rt.Close() })
	rt.EnsureDivisionRoster("division")
	rt.manualRuns = true
	lead := rt.findAgentLocked("division", "[AI]Trader_Mei")
	second := rt.findAgentLocked("division", "[AI]Trader_Lin")
	lead.RunKind, lead.State = RunGrand, StateTraveling
	source := Waypoint{RegionID: 25239, X: 600, Z: 1000}
	arrival := offsetPoint(source, 1000, 0)
	arrival.FerrySourceRef, arrival.FerryTargetID = 10, 20
	lead.place(source, 1000)
	second.place(source, 1000)
	rt.assignEscortLocked(second, lead, RunGrand)
	lead.Route, lead.WaypointIndex = []Waypoint{arrival, offsetPoint(arrival, 600, 0)}, 0
	guards := []string{"[AI]Hunter_Wei", "[AI]Hunter_Kwan", "[AI]Hunter_Lei", "[AI]Hunter_Ming", "[AI]Hunter_Feng", "[AI]Hunter_Rui"}
	for i, name := range guards {
		guard := rt.findAgentLocked("division", name)
		guard.place(source, 1000)
		trader := lead
		if i >= 3 {
			trader = second
		}
		rt.assignEscortLocked(guard, trader, RunGrand)
	}
	tailGuard := rt.findAgentLocked("division", guards[5])
	tailGuard.place(offsetPoint(source, -250, 0), 1000)
	if !rt.waitForEscortLocked(lead) {
		t.Fatal("lead trader did not wait for the second trader's hunter at the ferry")
	}
	tailGuard.place(source, 1000)
	lead.place(arrival, 1000)
	lead.WaypointIndex = 1
	for _, name := range append([]string{second.Name}, guards...) {
		_, unit, _ := rt.StepAgent(context.Background(), "division", name, nil, 2000)
		if crossings[name] != 1 || WaypointDistance(position(unit), arrival) > 1 {
			t.Fatalf("%s did not use the shared crossing: %d requests, at %+v", name, crossings[name], position(unit))
		}
	}
}
