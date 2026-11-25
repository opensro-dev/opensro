/*
===========================================================================

uniquekiller_test.go - a slain unique's kill owner, kept until Defeat

The EventBot's Random Unique (#3537) pays the name RecordUniqueKiller
retained, read after the fact through UniqueKiller. That name is the
kill owner the reward path passes (the top-damage reward group's
representative, reward_group.go), the same name the unique-death
notice announces. These tests pin what the host relies on: no owner
while the unique stands, the first record kept against later ones,
the record cleared with the death record at Defeat, and no owner for
a monster that is not a unique.

===========================================================================
*/
package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
killableMonster

One division with one spawned monster of the given rarity, on a
frozen clock.
================
*/
func killableMonster(t *testing.T, rarity uint8) (*MonsterState, monster.Instance, time.Time) {
	t.Helper()
	s := NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{1: {TidWord: 0x00C6, RefObjID: 1, MaxHP: 100, MonsterType: rarity}},
		[]monster.NestRow{{SpawnPoint: monster.SpawnPoint{RefObjID: 1, RegionID: 0x62aa}, MaxCount: 1}}))
	now := time.Unix(100, 0)
	s.SetTimeSource(func() time.Time { return now })
	s.StartDivision("a")
	s.AdvancePopulation(s.CurrentTimeMillis())
	found := s.InstancesInRegions("a", []uint16{0x62aa})
	if len(found) != 1 {
		t.Fatalf("spawned %d monsters, want 1", len(found))
	}
	return s, found[0], now
}

/*
================
TestUniqueKillerIsKeptUntilDefeat
================
*/
func TestUniqueKillerIsKeptUntilDefeat(t *testing.T) {
	s, unique, now := killableMonster(t, 3)
	s.RecordUniqueKiller("a", unique.Gid, "early")
	if name, ok := s.UniqueKiller("a", unique.Gid); ok {
		t.Fatalf("a standing unique has owner %q", name)
	}
	if hit, ok := s.ApplyDamage("a", unique.Gid, unique.CurrentHP); !ok || !hit.Fatal {
		t.Fatal("fixture failed to kill the unique")
	}
	s.RecordUniqueKiller("a", unique.Gid, "owner")
	s.RecordUniqueKiller("a", unique.Gid, "late")
	if name, ok := s.UniqueKiller("a", unique.Gid); !ok || name != "owner" {
		t.Fatalf("owner = %q, %v; want the first record, owner", name, ok)
	}
	if _, ok := s.UniqueKiller("b", unique.Gid); ok {
		t.Fatal("another division read the owner")
	}
	s.Defeat("a", unique.Gid, now)
	if name, ok := s.UniqueKiller("a", unique.Gid); ok {
		t.Fatalf("owner %q outlived Defeat", name)
	}
}

/*
================
TestNonUniqueKillNamesNoOwner

A row's unique= codename that is not a unique records no owner, so
its round can never pay (wiring_eventbot.go warns at load).
================
*/
func TestNonUniqueKillNamesNoOwner(t *testing.T) {
	s, ordinary, _ := killableMonster(t, 1)
	if hit, ok := s.ApplyDamage("a", ordinary.Gid, ordinary.CurrentHP); !ok || !hit.Fatal {
		t.Fatal("fixture failed to kill the monster")
	}
	s.RecordUniqueKiller("a", ordinary.Gid, "owner")
	if name, ok := s.UniqueKiller("a", ordinary.Gid); ok {
		t.Fatalf("an ordinary monster has owner %q", name)
	}
}
