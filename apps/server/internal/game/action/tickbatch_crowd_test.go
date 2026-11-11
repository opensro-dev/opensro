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
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/paramkeeper"
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
		Skills:           staticSkillSource{},
		MagicOptions:     &emptyMagicOptionSource{},
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
TestRecoveringCrowdReusesMemoizedStats

The recovery pulse reads each member's stats through
PlayerStatsWithModifiers, whose content-keyed memo (combat/statscache.go)
engages only when every reference source carries an identity. The crowd
fixture wires its sources the way production does (maps and pointers),
so this test and the bench below measure the rebuild production
actually pays - with a source missing, the memo refuses every call and
the pulse clones the keeper graph per member per pulse (#2030).
================
*/
func TestRecoveringCrowdReusesMemoizedStats(t *testing.T) {
	rt, _, clock, crowd := crowdStoreRuntime(t, true)
	now := clock.NowMs()
	for pulse := int64(1); pulse <= 2; pulse++ {
		for _, c := range crowd {
			hp := int64(1)
			c.CurrentHP = &hp
		}
		rt.advanceNaturalRecovery(now + pulse*4000)
	}
	c := crowd[0]
	writes := rt.statModifierWrites(testDivision, c)
	writes = append(writes,
		paramkeeper.Write{Parameter: itemParamHPRecovery, Source: recoveryPostureSource, Value: standingRecoveryRate},
		paramkeeper.Write{Parameter: itemParamMPRecovery, Source: recoveryPostureSource, Value: standingRecoveryRate},
	)
	if _, _, _, ok := combat.LookupMemoizedPlayerStats(c, rt.statCatalogs(), writes, rt.playerAbnormal(testDivision, c.Name)); !ok {
		t.Fatal("the recovery pulse rebuilt the stats projection instead of reusing the memoised one")
	}
}

/*
================
BenchmarkRecoveringCrowdTick

One recovery pulse of the whole crowd: immediate doors against the batch.
================
*/
/*
================
BenchmarkRecoveringCrowdTickPopulatedStores

The recovering crowd while someone outside it holds a buff and a
status: both stores populated, no crowd member in either. The pulse's
reads must miss by the session's held keys, never by folding fresh
ones (#2033); with empty stores this bench degenerates to
BenchmarkRecoveringCrowdTick.
================
*/
func BenchmarkRecoveringCrowdTickPopulatedStores(b *testing.B) {
	for _, batched := range []bool{false, true} {
		name := "immediate"
		if batched {
			name = "batched"
		}
		b.Run(name, func(b *testing.B) {
			rt, st, clock, crowd := crowdStoreRuntime(b, batched)
			modifiers, err := statuseffect.NewModifiers([]paramkeeper.Write{{Parameter: 5, Value: 20}})
			if err != nil {
				b.Fatal(err)
			}
			if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: "benchkeeper",
				SkillID: 901, SkillGroup: 901, InstanceToken: 901, State: statuseffect.StateActive, Modifiers: modifiers}) {
				b.Fatal("install unrelated effect")
			}
			rt.storePlayerAbnormal(testDivision, "benchkeeper", &abnormal.Block{Mask: 1})
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
