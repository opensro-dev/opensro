/*
===========================================================================

development_follow_test.go - simulation development follow test ownership

===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestFollowFixtureDoesNotReadUnrelatedDormantArchive
================
*/
func TestFollowFixtureDoesNotReadUnrelatedDormantArchive(t *testing.T) {
	s := NewMonsterState(monster.Template{})
	state := s.division("fixture")
	// No backing reader: touching unrelated cold actors fails immediately.
	state.instances.cold = make(map[uint32]archivedMonster, 51672)
	for gid := uint32(99); gid < 99+51672; gid++ {
		state.instances.cold[gid] = archivedMonster{}
	}
	state.movers = newMoverStorage(nil)
	for _, actor := range []monster.Instance{
		{Gid: 1, Ref: monster.MonsterRef{MonsterType: 3}},
		{Gid: 2, SummonerGID: 1},
		{Gid: 3, SummonerGID: 8},
	} {
		state.instances.set(actor.Gid, actor)
		state.movers.set(actor.Gid, monster.NewSpawnMover(actor, 1000))
	}
	if rows := s.DevelopmentFollowSnapshot("fixture", 1, 1000); len(rows) != 2 {
		t.Fatalf("fixture family = %+v", rows)
	}
	s.DevelopmentRemoveFamily("fixture", 1)
	if len(state.instances.cold) != 51672 {
		t.Fatal("fixture cleanup changed unrelated sleeping population")
	}
	if state.instances.contains(1) || state.instances.contains(2) || !state.instances.contains(3) || !state.instances.contains(99) {
		t.Fatal("fixture cleanup crossed family ownership")
	}
}

// Compare active-family observation with an empty and a populated sleeping
// archive. No archive reader is installed: deserializing any cold row fails.
/*
================
BenchmarkFollowSnapshotSleepingPopulation
================
*/
func BenchmarkFollowSnapshotSleepingPopulation(b *testing.B) {
	for _, count := range []int{0, 51672} {
		name := "empty"
		if count != 0 {
			name = "51672_sleeping"
		}
		b.Run(name, func(b *testing.B) {
			s := NewMonsterState(monster.Template{})
			state := s.division("fixture")
			state.instances.cold = make(map[uint32]archivedMonster, count)
			for i := 0; i < count; i++ {
				state.instances.cold[uint32(i+99)] = archivedMonster{}
			}
			state.movers = newMoverStorage(nil)
			for _, actor := range []monster.Instance{{Gid: 1, Ref: monster.MonsterRef{MonsterType: 3}}, {Gid: 2, SummonerGID: 1}} {
				state.instances.set(actor.Gid, actor)
				state.movers.set(actor.Gid, monster.NewSpawnMover(actor, 1000))
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if rows := s.DevelopmentFollowSnapshot("fixture", 1, 1000); len(rows) != 2 {
					b.Fatalf("fixture family = %+v", rows)
				}
			}
		})
	}
}
