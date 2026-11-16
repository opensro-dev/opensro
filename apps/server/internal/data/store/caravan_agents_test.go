/*
===========================================================================

caravan_agents_test.go - bounded persistence tests for AI caravan memories

===========================================================================
*/
package store

import (
	"fmt"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestCaravanAgentMemoriesPersistenceAndPruning

Verifies that caravan agent memories and route heat persist across store
re-opens, clamp trust/note sizes, and automatically prune the oldest
updated_at_ms rows when exceeding MaxCaravanMemoriesPerAgent.
================
*/
func TestCaravanAgentMemoriesPersistenceAndPruning(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, Options{})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	longNote := strings.Repeat("A", domain.MaxCaravanMemoryNoteBytes+40)
	for i := 0; i < domain.MaxCaravanMemoriesPerAgent+10; i++ {
		row := domain.CaravanAgentMemoryRow{
			Division:    "global-official",
			AgentName:   "Trader_Jin",
			PlayerName:  fmt.Sprintf("Bandit_%03d", i),
			TrustScore:  -200,
			Kills:       1,
			Deaths:      2,
			GoldStolen:  15000,
			LastRegion:  24744,
			LastNote:    longNote,
			UpdatedAtMs: int64(1000 + i),
		}
		if err := s.SaveCaravanAgentMemory(row); err != nil {
			t.Fatalf("save memory %d: %v", i, err)
		}
	}

	if err := s.SaveCaravanRouteHeat(domain.CaravanRouteHeatRow{
		Division:    "global-official",
		RegionID:    24744,
		DangerScore: 85,
		Ambushes:    3,
		UpdatedAtMs: 5000,
	}); err != nil {
		t.Fatalf("save route heat: %v", err)
	}

	memories, err := s.LoadCaravanAgentMemories("global-official")
	if err != nil {
		t.Fatalf("load memories: %v", err)
	}
	if len(memories) != domain.MaxCaravanMemoriesPerAgent {
		t.Fatalf("expected %d pruned memories, got %d", domain.MaxCaravanMemoriesPerAgent, len(memories))
	}
	// Oldest 10 rows (Bandit_000 .. Bandit_009) must have been pruned.
	for _, m := range memories {
		if m.UpdatedAtMs < 1010 {
			t.Fatalf("expected oldest row to be pruned, found %s with UpdatedAtMs=%d", m.PlayerName, m.UpdatedAtMs)
		}
		if m.TrustScore != domain.CaravanTrustMin {
			t.Fatalf("expected clamped trust %d, got %d", domain.CaravanTrustMin, m.TrustScore)
		}
		if len(m.LastNote) != domain.MaxCaravanMemoryNoteBytes {
			t.Fatalf("expected note length %d, got %d", domain.MaxCaravanMemoryNoteBytes, len(m.LastNote))
		}
	}

	heat, err := s.LoadCaravanRouteHeat("global-official")
	if err != nil {
		t.Fatalf("load route heat: %v", err)
	}
	if len(heat) != 1 || heat[0].RegionID != 24744 || heat[0].DangerScore != 85 || heat[0].Ambushes != 3 {
		t.Fatalf("unexpected route heat: %+v", heat)
	}
}
