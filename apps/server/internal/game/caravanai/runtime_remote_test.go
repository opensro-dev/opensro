/*
===========================================================================

runtime_remote_test.go - the tick never waits on a remote brain or the store

A remote decision is asked in the background and applies on a later step
only while the agent is still in the state it was asked about; memories
reach the store only through Flush, never from the step itself.

===========================================================================
*/
package caravanai

import (
	"context"
	"errors"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/testsupport/wait"
)

// stepBudget is how long one step may take in these tests: far below any
// remote round trip, so a step that waited on the remote brain fails.
const stepBudget = 200 * time.Millisecond

// gatedBrain answers only once its gate opens, like a slow endpoint.
type gatedBrain struct {
	gate   chan struct{}
	answer Decision
	asked  chan Situation
}

func (g *gatedBrain) Decide(ctx context.Context, sit Situation) Decision {
	g.asked <- sit
	select {
	case <-g.gate:
		return g.answer
	case <-ctx.Done():
		return Decision{Action: ActionContinueRoute, Source: BrainHardcoded}
	}
}

func newRemoteRuntime(t *testing.T, mode BrainMode, brain *gatedBrain) *Runtime {
	t.Helper()
	rt := NewRuntime(Config{Enabled: true, BrainMode: mode}, nil, WorldHooks{})
	rt.remote = brain
	t.Cleanup(func() {
		close(brain.gate)
		if err := rt.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return rt
}

// waitForAnswer waits until the agent's remote answer sits in its channel,
// so the next step is the one that reads it.
func waitForAnswer(t *testing.T, rt *Runtime, agent string) {
	t.Helper()
	wait.Eventually(t, time.Second, "the remote answer", func() bool {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		req, pending := rt.inflight[agentKey("div", agent)]
		return pending && len(req.result) == 1
	})
}

func stepWithin(t *testing.T, rt *Runtime, agent string, players []ObservedPlayer, nowMs int64) Decision {
	t.Helper()
	started := time.Now()
	d, _, ok := rt.StepAgent(context.Background(), "div", agent, players, nowMs)
	if !ok {
		t.Fatalf("step of %s at %d found no agent", agent, nowMs)
	}
	if took := time.Since(started); took > stepBudget {
		t.Fatalf("step took %s: it waited on the remote brain", took)
	}
	return d
}

/*
================
TestRemoteBrainNeverHoldsTheStep

A remote brain that has not answered leaves the step to the local brain;
its answer applies on a later step, and the next request waits out the
pacing interval.
================
*/
func TestRemoteBrainNeverHoldsTheStep(t *testing.T) {
	brain := &gatedBrain{
		gate:   make(chan struct{}),
		answer: Decision{Action: ActionAssistAlly, Dialogue: "remote", Source: BrainLLM},
		asked:  make(chan Situation, 4),
	}
	rt := newRemoteRuntime(t, BrainLLM, brain)

	first := stepWithin(t, rt, "[AI]Hunter_Kwan", nil, 1_000)
	if first.Source != BrainHardcoded {
		t.Fatalf("first step = %+v, want the local decision while the remote one is pending", first)
	}
	select {
	case <-brain.asked:
	case <-time.After(time.Second):
		t.Fatal("the remote brain was never asked")
	}
	// Still pending: the step stays local and asks nothing new.
	if d := stepWithin(t, rt, "[AI]Hunter_Kwan", nil, 1_100); d.Source != BrainHardcoded {
		t.Fatalf("pending step = %+v, want local", d)
	}

	brain.gate <- struct{}{}
	waitForAnswer(t, rt, "[AI]Hunter_Kwan")
	applied := stepWithin(t, rt, "[AI]Hunter_Kwan", nil, 1_200)
	if applied.Source != BrainLLM || applied.Dialogue != "remote" {
		t.Fatalf("answered step = %+v, want the remote decision", applied)
	}
	select {
	case <-brain.asked:
		t.Fatal("a second request went out inside the pacing interval")
	default:
	}
}

/*
================
TestStaleRemoteAnswerIsDropped

An answer asked while the agent travelled is dropped once the agent is in
combat: the local decision stands.
================
*/
func TestStaleRemoteAnswerIsDropped(t *testing.T) {
	brain := &gatedBrain{
		gate:   make(chan struct{}),
		answer: Decision{Action: ActionContinueRoute, Dialogue: "stale", Source: BrainLLM},
		asked:  make(chan Situation, 4),
	}
	rt := newRemoteRuntime(t, BrainLLM, brain)
	thief := []ObservedPlayer{{Name: "Raider", JobType: domain.JobThief, HPPercent: 100}}

	stepWithin(t, rt, "[AI]Hunter_Kwan", nil, 1_000)
	<-brain.asked
	// The hunter meets a thief on its route: it is now in combat.
	rt.mu.Lock()
	unit := rt.findAgentLocked("div", "[AI]Hunter_Kwan")
	unit.RegionID = RegionJanganFerryEast
	rt.mu.Unlock()
	if d := stepWithin(t, rt, "[AI]Hunter_Kwan", thief, 1_100); d.Action != ActionAttackTarget {
		t.Fatalf("combat step = %+v, want the local attack", d)
	}

	brain.gate <- struct{}{}
	waitForAnswer(t, rt, "[AI]Hunter_Kwan")
	if d := stepWithin(t, rt, "[AI]Hunter_Kwan", thief, 1_200); d.Source == BrainLLM {
		t.Fatalf("stale answer applied: %+v", d)
	}
}

// recordingStore counts store writes and can fail them.
type recordingStore struct {
	memories, heat int
	fail           bool
}

func (s *recordingStore) LoadCaravanAgentMemories(string) ([]domain.CaravanAgentMemoryRow, error) {
	return nil, nil
}

func (s *recordingStore) SaveCaravanAgentMemory(domain.CaravanAgentMemoryRow) error {
	if s.fail {
		return errors.New("disk full")
	}
	s.memories++
	return nil
}

func (s *recordingStore) LoadCaravanRouteHeat(string) ([]domain.CaravanRouteHeatRow, error) {
	return nil, nil
}

func (s *recordingStore) SaveCaravanRouteHeat(domain.CaravanRouteHeatRow) error {
	if s.fail {
		return errors.New("disk full")
	}
	s.heat++
	return nil
}

/*
================
TestMemoriesReachTheStoreOnlyThroughFlush

A robbery changes the cached memory and route heat at once but writes
nothing; Flush writes the newest row per key, and a failed write is kept
for the next flush.
================
*/
func TestMemoriesReachTheStoreOnlyThroughFlush(t *testing.T) {
	store := &recordingStore{}
	book := NewMemoryBook(store)
	book.RecordRobberyOrKill("div", "[AI]Trader_Jin", "Raider", 1000, RegionJanganFerryEast, "", 1)
	book.RecordRobberyOrKill("div", "[AI]Trader_Jin", "Raider", 1000, RegionJanganFerryEast, "", 2)
	if store.memories != 0 || store.heat != 0 {
		t.Fatalf("a mutation wrote to the store: %d memories, %d heat", store.memories, store.heat)
	}
	if row, ok := book.Relationship("div", "[AI]Trader_Jin", "Raider"); !ok || row.Deaths != 2 {
		t.Fatalf("cached memory = %+v, want two deaths", row)
	}

	store.fail = true
	if err := book.Flush(); err == nil {
		t.Fatal("a failed write reported no error")
	}
	store.fail = false
	if err := book.Flush(); err != nil {
		t.Fatalf("retry flush: %v", err)
	}
	if store.memories != 1 || store.heat != 1 {
		t.Fatalf("flush wrote %d memories and %d heat rows, want one of each", store.memories, store.heat)
	}
	if err := book.Flush(); err != nil || store.memories != 1 {
		t.Fatalf("an empty flush wrote again: %d memories (%v)", store.memories, err)
	}
}

/*
================
TestHPPercentClamps

The observed HP is a whole percentage, clamped, with an unknown maximum
reading as full.
================
*/
func TestHPPercentClamps(t *testing.T) {
	cases := []struct {
		current, maximum int64
		want             int
	}{
		{340, 1000, 34},
		{0, 1000, 0},
		{-5, 1000, 0},
		{1200, 1000, 100},
		{10, 0, FullHPPercent},
	}
	for _, c := range cases {
		if got := HPPercent(c.current, c.maximum); got != c.want {
			t.Errorf("HPPercent(%d, %d) = %d, want %d", c.current, c.maximum, got, c.want)
		}
	}
}
