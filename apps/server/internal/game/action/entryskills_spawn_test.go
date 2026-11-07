/*
===========================================================================

entryskills_spawn_test.go - the tick's shared spawn projection

SpawnEntrySkills (#2030) hands the peer-appearance tick the same
rows EntrySkills builds without rebuilding them per tick: the
projection is reused while the registry view and every row's
liveness stand, its remaining times refresh in place, and any
registry move or expiry rebuilds it.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
)

/*
================
TestSpawnEntrySkillsReusesProjectionUntilLivenessFlips
================
*/
func TestSpawnEntrySkillsReusesProjectionUntilLivenessFlips(t *testing.T) {
	rt, c := newActiveEffectTestRuntime(t, staticSkillSource{
		100: {ID: 100, Group: 9, SpawnToken: true, SpawnStatus: true, EffectDurationMs: 5000},
	})
	if !rt.ApplyCharacterEffectPresentation(testDivision, c.Name, 100, 99, statuseffect.StateActive, false, EffectPresentation{Phase: 2}, 10000) {
		t.Fatal("apply")
	}

	first := rt.spawnEntrySkillsAt(testDivision, c.Name, 11000)
	if len(first) != 1 || *first[0].Token != 99 || *first[0].Remaining != 4000 {
		t.Fatalf("first projection: %+v", first)
	}
	second := rt.spawnEntrySkillsAt(testDivision, c.Name, 12000)
	if len(second) != 1 || &second[0] != &first[0] {
		t.Fatalf("unchanged projection rebuilt: %+v", second)
	}
	if *second[0].Remaining != 3000 {
		t.Fatalf("remaining time not refreshed in place: %d", *second[0].Remaining)
	}

	// The bootstrap still gets its own rows: it retains them while it
	// encodes, so EntrySkills never hands out the shared projection.
	fresh := rt.entrySkillsAt(testDivision, c.Name, 12000)
	if len(fresh) != 1 || &fresh[0] == &first[0] || *fresh[0].Remaining != 3000 {
		t.Fatalf("bootstrap projection shared or stale: %+v", fresh)
	}

	// Expiry is a liveness flip on time alone: the row must go, not be
	// served from the cache one tick past its deadline.
	if got := rt.spawnEntrySkillsAt(testDivision, c.Name, 15001); len(got) != 0 {
		t.Fatalf("expired row kept by the cache: %+v", got)
	}
}

/*
================
TestSpawnEntrySkillsRebuildsWhenTheRegistryMoves
================
*/
func TestSpawnEntrySkillsRebuildsWhenTheRegistryMoves(t *testing.T) {
	rt, c := newActiveEffectTestRuntime(t, staticSkillSource{
		100: {ID: 100, Group: 9, SpawnToken: true, SpawnStatus: true, EffectDurationMs: 60000},
		101: {ID: 101, Group: 10, SpawnToken: true, SpawnStatus: true, EffectDurationMs: 60000},
	})
	if !rt.ApplyCharacterEffectPresentation(testDivision, c.Name, 100, 99, statuseffect.StateActive, false, EffectPresentation{Phase: 2}, 10000) {
		t.Fatal("apply 100")
	}
	first := rt.spawnEntrySkillsAt(testDivision, c.Name, 11000)
	if len(first) != 1 {
		t.Fatalf("first projection: %+v", first)
	}
	if !rt.ApplyCharacterEffectPresentation(testDivision, c.Name, 101, 7, statuseffect.StateActive, false, EffectPresentation{Phase: 2}, 11000) {
		t.Fatal("apply 101")
	}
	second := rt.spawnEntrySkillsAt(testDivision, c.Name, 11000)
	if len(second) != 2 || &second[0] == &first[0] {
		t.Fatalf("registry move did not rebuild: %+v", second)
	}
	rt.effects.Forget(testDivision, c.Name)
	if got := rt.spawnEntrySkillsAt(testDivision, c.Name, 11000); len(got) != 0 {
		t.Fatalf("forgotten owner's rows kept by the cache: %+v", got)
	}
}

/*
================
BenchmarkSpawnEntrySkillsProjection

The #2030 measure: the fresh projection (entrySkillsAt, the
pre-change per-tick cost of the peer tick) against the shared one
(spawnEntrySkillsAt) over two token rows that never change.
================
*/
func BenchmarkSpawnEntrySkillsProjection(b *testing.B) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	rt.deps.(*enterworld.Deps).Skills = staticSkillSource{
		100: {ID: 100, Group: 9, SpawnToken: true, SpawnStatus: true, EffectDurationMs: 3600000},
		101: {ID: 101, Group: 10, SpawnToken: true, SpawnStatus: true, EffectDurationMs: 3600000},
	}
	if !rt.ApplyCharacterEffectPresentation(testDivision, c.Name, 100, 99, statuseffect.StateActive, false, EffectPresentation{Phase: 2}, 10000) {
		b.Fatal("apply 100")
	}
	if !rt.ApplyCharacterEffectPresentation(testDivision, c.Name, 101, 7, statuseffect.StateActive, false, EffectPresentation{Phase: 2}, 10000) {
		b.Fatal("apply 101")
	}
	b.Run("fresh", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = rt.entrySkillsAt(testDivision, c.Name, 11000+int64(i))
		}
	})
	b.Run("shared", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = rt.spawnEntrySkillsAt(testDivision, c.Name, 11000+int64(i))
		}
	})
}
