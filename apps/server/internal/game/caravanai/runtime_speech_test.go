/*
===========================================================================

runtime_speech_test.go - slice 4 (#3704): what agents say out loud, and
how often players may ask

OnAgentChat is the world's ear: StepAgent paces it so one standing
situation speaks once per line, and HandlePlayerCommand answers one
command per player per cooldown. These tests pin both pacings.

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
==================
TestAgentSpeechReportsEachLineOnce

A trusted friend standing beside the caravan draws the greeting once,
not once per step; a different friend draws a different line at once.
==================
*/
func TestAgentSpeechReportsEachLineOnce(t *testing.T) {
	var spoken []string
	rt := NewRuntime(
		Config{Enabled: true, BrainMode: BrainHardcoded},
		nil, nil,
		WorldHooks{
			OnAgentChat: func(_, _ string, _ uint16, message string) {
				spoken = append(spoken, message)
			},
		},
	)
	const div = "Shard_Silkroad"
	rt.EnsureDivisionRoster(div)
	// Two escort successes make each friend a trusted ally (+50).
	for _, friend := range []string{"OldFriend", "SecondFriend"} {
		rt.Memories().RecordEscortSuccess(div, "[AI]Trader_Jin", friend, RegionJanganTown, 100)
		rt.Memories().RecordEscortSuccess(div, "[AI]Trader_Jin", friend, RegionJanganTown, 200)
	}
	near := func(name string) []ObservedPlayer {
		return []ObservedPlayer{{Name: name, JobType: domain.JobHunter, HPPercent: 100, Distance: 12}}
	}

	if _, _, ok := rt.StepAgent(context.Background(), div, "[AI]Trader_Jin", near("OldFriend"), 1_000); !ok {
		t.Fatal("expected Jin to step")
	}
	if len(spoken) != 1 || !strings.Contains(spoken[0], "OldFriend") {
		t.Fatalf("expected one greeting for OldFriend, got %v", spoken)
	}
	if _, _, ok := rt.StepAgent(context.Background(), div, "[AI]Trader_Jin", near("OldFriend"), 2_000); !ok {
		t.Fatal("expected Jin to step")
	}
	if len(spoken) != 1 {
		t.Fatalf("the same line must not repeat on the next step, got %v", spoken)
	}
	if _, _, ok := rt.StepAgent(context.Background(), div, "[AI]Trader_Jin", near("SecondFriend"), 3_000); !ok {
		t.Fatal("expected Jin to step")
	}
	if len(spoken) != 2 || !strings.Contains(spoken[1], "SecondFriend") {
		t.Fatalf("a different line speaks at once, got %v", spoken)
	}
}

/*
==================
TestPlayerCommandsAreRateLimited

One answered command per cooldown; the excess is consumed silently,
another player is unaffected, and unrecognized text neither consumes
nor burns the slot.
==================
*/
func TestPlayerCommandsAreRateLimited(t *testing.T) {
	rt := NewRuntime(Config{Enabled: true, BrainMode: BrainHardcoded}, nil, nil, WorldHooks{})
	const div = "Shard_Silkroad"
	rt.EnsureDivisionRoster(div)

	if reply, handled := rt.HandlePlayerCommand(div, "Scout", domain.JobHunter, "!dance [AI]Trader_Jin", 10_000); handled || reply != "" {
		t.Fatalf("unrecognized text is not a command, got handled=%v reply=%q", handled, reply)
	}
	reply, handled := rt.HandlePlayerCommand(div, "Scout", domain.JobHunter, "!accept [AI]Trader_Jin", 10_100)
	if !handled || reply == "" {
		t.Fatalf("the first command is answered, got handled=%v reply=%q", handled, reply)
	}
	if reply, handled := rt.HandlePlayerCommand(div, "Scout", domain.JobHunter, "!memory [AI]Trader_Jin", 12_000); !handled || reply != "" {
		t.Fatalf("inside the cooldown the command is consumed silently, got handled=%v reply=%q", handled, reply)
	}
	if reply, handled := rt.HandlePlayerCommand(div, "Other", domain.JobThief, "!memory [AI]Trader_Jin", 12_500); !handled || reply == "" {
		t.Fatalf("the limit is per player, got handled=%v reply=%q", handled, reply)
	}
	if reply, handled := rt.HandlePlayerCommand(div, "Scout", domain.JobHunter, "!memory [AI]Trader_Jin", 16_000); !handled || !strings.Contains(reply, "Trust:") {
		t.Fatalf("after the cooldown the player is answered again, got handled=%v reply=%q", handled, reply)
	}
}

/*
==================
TestAgentNamedResolvesCanonicalName

A command reply is authored by the agent's canonical roster name,
however the player cased it; unknown names resolve to nothing.
==================
*/
func TestAgentNamedResolvesCanonicalName(t *testing.T) {
	rt := NewRuntime(Config{Enabled: true, BrainMode: BrainHardcoded}, nil, nil, WorldHooks{})
	const div = "Shard_Silkroad"
	if name, ok := rt.AgentNamed(div, "[ai]trader_jin"); !ok || name != "[AI]Trader_Jin" {
		t.Fatalf("expected the canonical name, got %q ok=%v", name, ok)
	}
	if _, ok := rt.AgentNamed(div, "Nobody"); ok {
		t.Fatal("an unknown name must not resolve")
	}
}
