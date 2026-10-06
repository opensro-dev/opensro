package simulation

import (
	"opensro.online/server/internal/game/world/monster"
	"testing"
	"time"
)

func TestObservatoryDoesNotDriveWorldLifecycle(t *testing.T) {
	s := NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{1: {TidWord: 0x00C6, RefObjID: 1, Name: "Unique", MaxHP: 100, MonsterType: 3}}, []monster.NestRow{{SpawnPoint: monster.SpawnPoint{RefObjID: 1, RegionID: 257, X: 12, Z: 34}, MaxCount: 1, PolicyPinned: true, Respawn: true, RespawnDelayMinSec: 1, RespawnDelayMaxSec: 1}}))
	now := time.Unix(100, 0)
	s.SetTimeSource(func() time.Time { return now })
	if snap := s.Observatory("a", nil); snap.Resident != 0 || len(s.divs) != 0 {
		t.Fatal("observation created population")
	}
	s.StartDivision("a")
	s.AdvancePopulation(s.CurrentTimeMillis())
	first := s.InstancesInRegions("a", []uint16{257})[0]
	snap := s.Observatory("a", nil)
	if snap.Resident != 1 || len(snap.Monsters) != 1 || snap.Monsters[0].GID != first.Gid || snap.Monsters[0].Rarity != 3 {
		t.Fatalf("wrong snapshot: %+v", snap)
	}
	snap.Monsters[0].HP = 0
	if s.Observatory("a", nil).Monsters[0].HP == 0 {
		t.Fatal("snapshot aliases authority")
	}
	if len(s.DrainUniqueNotices("a")) != 1 {
		t.Fatal("capture consumed notice")
	}
	s.Defeat("a", first.Gid, now)
	now = now.Add(time.Minute)
	if s.Observatory("a", nil).Resident != 0 {
		t.Fatal("capture advanced respawn")
	}
}

/*
================
TestObservatoryKeepsTheFocusAheadOfTheCap

Monsters near a focus region (an online player) are listed before the cap
drops anything, and FocusComplete says whether all of them fit.
================
*/
func TestObservatoryKeepsTheFocusAheadOfTheCap(t *testing.T) {
	row := func(gid uint32, region uint16) ObservatoryMonster {
		return ObservatoryMonster{GID: gid, Region: region}
	}
	focus := []ObservatoryMonster{row(9, 0x6e4b), row(8, 0x6f4c)}
	others := []ObservatoryMonster{row(1, 0x1010), row(2, 0x1011), row(3, 0x1012)}
	rows, truncated, complete := capObservatory(focus, others, false, 3)
	if len(rows) != 3 || rows[0].GID != 9 || rows[1].GID != 8 || !truncated || !complete {
		t.Fatalf("cap kept %+v truncated=%v complete=%v", rows, truncated, complete)
	}
	if _, truncated, complete := capObservatory(focus, nil, false, 1); !truncated || complete {
		t.Fatal("a focus larger than the cap was reported complete")
	}
	near := focusNeighbourhood([]uint16{0x6e4b})
	if len(near) != 9 || !near[0x6d4a] || !near[0x6f4c] || near[0x6e4d] {
		t.Fatalf("neighbourhood = %v", near)
	}
	if edge := focusNeighbourhood([]uint16{0x0000}); len(edge) != 4 {
		t.Fatalf("a corner region has %d in-range neighbours, want 4", len(edge))
	}

	s := NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{1: {TidWord: 0x00C6, RefObjID: 1, Name: "Near", MaxHP: 100}},
		[]monster.NestRow{
			{SpawnPoint: monster.SpawnPoint{RefObjID: 1, RegionID: 257, X: 12, Z: 34}, MaxCount: 1, PolicyPinned: true},
			{SpawnPoint: monster.SpawnPoint{RefObjID: 1, RegionID: 0x2020, X: 12, Z: 34}, MaxCount: 1, PolicyPinned: true},
		}))
	s.StartDivision("a")
	s.AdvancePopulation(s.CurrentTimeMillis())
	snap := s.Observatory("a", []uint16{257})
	if len(snap.Monsters) != 2 || snap.Truncated || !snap.FocusComplete {
		t.Fatalf("focused capture = %+v", snap)
	}
}
