package simulation

import (
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

func TestMonsterArchiveExactRoundTripInternsSharedRows(t *testing.T) {
	owner := NewMonsterState(monster.Template{})
	if err := owner.EnableDormantStorage(); err != nil {
		t.Fatal(err)
	}
	s := newMonsterStorage(nil)
	s.archive = owner.archive
	actor := monster.Instance{Gid: 42, Ref: monster.MonsterRef{RefObjID: 123, Name: "preserved monster", MaxHP: 99, BodyRadius: 8}, Nest: lifecycleNest(151.123456789), Spawn: monster.SpawnPoint{RegionID: lifecycleRegion, X: 153.987654321, Y: -1.25, Z: 615.456789123}, CurrentHP: 99, SpawnHeading: 61234}
	for i := 0; i < 100; i++ {
		s.set(actor.Gid, actor)
		s.freeze(actor.Gid)
		if len(s.hot) != 0 || len(s.cold) != 1 || s.len() != 1 {
			t.Fatal("archive retained live actor data")
		}
		if got, ok := s.lookup(actor.Gid); !ok || got != actor {
			t.Fatal("archive changed actor identity, values or position")
		}
		if len(s.hot) != 0 {
			t.Fatal("read-only query woke actor")
		}
		s.wake(actor.Gid)
		if s.get(actor.Gid) != actor || len(s.cold) != 0 {
			t.Fatal("wake changed actor")
		}
	}
	// Two sleepers of one kind and nest share the large rows.
	twin := actor
	twin.Gid = 43
	s.set(twin.Gid, twin)
	s.freeze(actor.Gid)
	s.freeze(twin.Gid)
	if a, b := s.cold[actor.Gid], s.cold[twin.Gid]; a.ref != b.ref || a.nest != b.nest {
		t.Fatal("sleepers of one nest hold their own reference or nest rows")
	}
	s.wake(twin.Gid)
	s.remove(twin.Gid)
	s.freeze(actor.Gid)
	s.remove(actor.Gid)
	if s.contains(actor.Gid) || s.len() != 0 {
		t.Fatal("removed actor survived in backing index")
	}
	unique := actor
	unique.Ref.MonsterType = 3
	s.set(unique.Gid, unique)
	s.freeze(unique.Gid)
	if len(s.cold) != 0 {
		t.Fatal("unique was archived")
	}
}
