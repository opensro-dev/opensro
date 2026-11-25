/*
===========================================================================

runtime_strike_test.go - agent strikes on players (port-only, #3704 slice 3e)

When the brain's attack decision names a player inside strike range,
the runtime fires its strike hook at its own pace - never faster -
and records the kill when the consumer reports the player fell. Out
of range, the agent postures in combat but lands nothing.

===========================================================================
*/
package caravanai

import (
	"context"
	"testing"

	"opensro.online/server/internal/domain"
)

// strikeCall records one OnAgentStrike firing.
type strikeCall struct {
	agent  string
	target string
}

/*
==================
TestAgentStrikePacingAndKillMemory

Viper meets a trader inside strike range: the first step strikes,
the next inside the interval does not, the step at the interval does,
and a reported kill lands in the pair's memory.
==================
*/
func TestAgentStrikePacingAndKillMemory(t *testing.T) {
	var calls []strikeCall
	rt := NewRuntime(
		Config{Enabled: true, BrainMode: BrainHardcoded},
		nil, nil,
		WorldHooks{OnAgentStrike: func(_, agentName, targetPlayerName string, _ int64) bool {
			calls = append(calls, strikeCall{agent: agentName, target: targetPlayerName})
			return len(calls) >= 2 // the second strike kills
		}},
	)
	const div = "strike-test"
	rt.EnsureDivisionRoster(div)
	nearby := []ObservedPlayer{{Name: "TraderJo", JobType: domain.JobTrader, HPPercent: 100, Distance: 5}}

	dec, unit, ok := rt.StepAgent(context.Background(), div, "[AI]Thief_Viper", nearby, 10_000)
	if !ok || dec.Action != ActionAttackTarget {
		t.Fatalf("step 1 decision = %+v ok=%v, want an attack on the trader", dec, ok)
	}
	if len(calls) != 1 || calls[0].target != "TraderJo" {
		t.Fatalf("strike calls = %+v, want one strike on TraderJo", calls)
	}
	if unit.CombatTarget != "TraderJo" {
		t.Fatalf("CombatTarget = %q, want TraderJo (the fight-back admission reads it)", unit.CombatTarget)
	}

	if _, _, _ = rt.StepAgent(context.Background(), div, "[AI]Thief_Viper", nearby, 10_100); len(calls) != 1 {
		t.Fatalf("a strike landed inside the pace interval: %+v", calls)
	}
	if _, _, _ = rt.StepAgent(context.Background(), div, "[AI]Thief_Viper", nearby, 10_000+agentStrikeIntervalMs); len(calls) != 2 {
		t.Fatalf("no strike at the pace interval: %+v", calls)
	}
	mem, exists := rt.Memories().Relationship(div, "[AI]Thief_Viper", "TraderJo")
	if !exists || mem.Kills != 1 {
		t.Fatalf("kill memory = %+v (exists %v), want one recorded kill", mem, exists)
	}
}

/*
==================
TestAgentStrikeOutOfRange

The same meeting beyond strike range: the brain still chooses the
fight, but no strike fires.
==================
*/
func TestAgentStrikeOutOfRange(t *testing.T) {
	struck := false
	rt := NewRuntime(
		Config{Enabled: true, BrainMode: BrainHardcoded},
		nil, nil,
		WorldHooks{OnAgentStrike: func(_, _, _ string, _ int64) bool {
			struck = true
			return false
		}},
	)
	const div = "strike-range-test"
	rt.EnsureDivisionRoster(div)
	far := []ObservedPlayer{{Name: "TraderJo", JobType: domain.JobTrader, HPPercent: 100, Distance: agentStrikeRangeUnits + 1}}

	dec, _, ok := rt.StepAgent(context.Background(), div, "[AI]Thief_Viper", far, 10_000)
	if !ok {
		t.Fatal("the step failed")
	}
	if dec.Action == ActionAttackTarget && struck {
		t.Fatal("a strike landed beyond strike range")
	}
	if struck {
		t.Fatal("a strike fired with nobody in range")
	}
}
