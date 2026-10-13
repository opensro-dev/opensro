/*
===========================================================================

tickbatch_crowd_test.go - transactions per tick for a recovering crowd

A crowd regenerating after a fight is the tick's commonest writer: every
recovery pulse updates every member (recoverResident). Against a real
authority store, the tick-only doors commit once per tick with the batch
(store tickbatch.go) and once per member without it.

===========================================================================
*/
package action

import (
	"fmt"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

// crowdSize is the recovering crowd.
const crowdSize = 40

/*
================
crowdStoreRuntime

A runtime whose doors are a real store's, as the GameWorld wires them,
holding crowdSize bound characters at 1 HP. batched wires the deferred
doors; without it they fall back to the immediate ones.
================
*/
func crowdStoreRuntime(tb testing.TB, batched bool) (*Runtime, *store.Store, *fakeClock, []*enterworld.Character) {
	tb.Helper()
	st, err := store.Open(tb.TempDir(), store.Options{Now: func() time.Time { return time.UnixMilli(1_000_000) },
		DefaultSkills: func(string, []uint32) ([]uint32, error) { return nil, nil }})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(st.Close)
	crowd := make([]*enterworld.Character, crowdSize)
	for i := range crowd {
		c := testCharacter()
		c.ID, c.Name = int64(100+i), fmt.Sprintf("crowd%02d", i)
		hp := int64(1)
		c.CurrentHP = &hp
		if err := st.CreateCharacter(testDivision, fmt.Sprintf("crowd-account-%02d", i), c); err != nil {
			tb.Fatal(err)
		}
		crowd[i] = c
	}
	deps := &enterworld.Deps{
		Characters:       st.Characters(),
		Items:            testItems(),
		NpcSpawns:        enterworld.NpcSpawnConfig{Roster: simulation.DefaultNpcRoster()},
		MutateCharacter:  st.MutateCharacter,
		MutateCharacters: st.MutateCharacters,
		UpdateCharacter:  st.UpdateCharacter,
		UpdateCharacters: st.UpdateCharacters,
	}
	if batched {
		deps.MutateCharacterDeferred = st.MutateCharacterDeferred
		deps.MutateCharactersDeferred = st.MutateCharactersDeferred
		deps.UpdateCharacterDeferred = st.UpdateCharacterDeferred
		deps.UpdateCharactersDeferred = st.UpdateCharactersDeferred
	}
	rt := NewRuntime(deps, nil)
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}
	rt.Now = clock.Now
	for i, c := range crowd {
		rt.BindRecoverySession(testDivision, c, uint64(i+1))
	}
	return rt, st, clock, crowd
}

/*
================
crowdTick

One coordinated tick as the ticker runs it (tick_commit.go): the batch
opens, the action hook runs, the batch flushes. Returns the transactions
the tick committed.
================
*/
func crowdTick(rt *Runtime, st *store.Store, nowMs int64) uint64 {
	before := st.Health().Commits
	st.BeginTickBatch()
	rt.TickHook()(nowMs)
	st.FlushTickBatch()
	return st.Health().Commits - before
}

/*
================
TestRecoveringCrowdCommitsOncePerTick
================
*/
func TestRecoveringCrowdCommitsOncePerTick(t *testing.T) {
	for _, batched := range []bool{false, true} {
		rt, st, clock, crowd := crowdStoreRuntime(t, batched)
		start := clock.NowMs()
		for pulse := int64(1); pulse <= 3; pulse++ {
			commits := crowdTick(rt, st, start+pulse*4000)
			want := uint64(crowdSize)
			if batched {
				want = 1
			}
			if commits != want {
				t.Fatalf("batched %v, pulse %d: %d transactions, want %d", batched, pulse, commits, want)
			}
		}
		for _, c := range crowd {
			if enterworld.CurrentHP(c) <= 1 {
				t.Fatalf("batched %v: %s never recovered", batched, c.Name)
			}
		}
	}
}

/*
================
BenchmarkRecoveringCrowdTick

One recovery pulse of the whole crowd: immediate doors against the batch.
================
*/
func BenchmarkRecoveringCrowdTick(b *testing.B) {
	for _, batched := range []bool{false, true} {
		name := "immediate"
		if batched {
			name = "batched"
		}
		b.Run(name, func(b *testing.B) {
			rt, st, clock, crowd := crowdStoreRuntime(b, batched)
			nowMs, commits := clock.NowMs(), uint64(0)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				for _, c := range crowd {
					hp := int64(1)
					c.CurrentHP = &hp
				}
				nowMs += 4000
				b.StartTimer()
				commits += crowdTick(rt, st, nowMs)
			}
			b.ReportMetric(float64(commits)/float64(b.N), "commits/tick")
		})
	}
}
