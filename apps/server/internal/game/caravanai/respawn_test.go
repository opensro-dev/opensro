/*
===========================================================================

respawn_test.go - dead agents, the grouped respawn wait and resurrection

===========================================================================
*/
package caravanai

import (
	"context"
	"testing"

	"opensro.online/server/internal/domain"
)

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
	rt := NewRuntime(Config{Enabled: true}, nil, nil, WorldHooks{})
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
	rt := NewRuntime(Config{Enabled: true}, nil, nil, WorldHooks{
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
	rt := NewRuntime(Config{Enabled: true}, nil, nil, WorldHooks{})
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
	rt := NewRuntime(Config{Enabled: true}, nil, nil, WorldHooks{})
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
	rt := NewRuntime(Config{Enabled: true}, nil, nil, WorldHooks{})
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
