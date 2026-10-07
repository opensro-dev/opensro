/*
===========================================================================

observatory_test.go - read-only population capture and focused truncation

Verifies that inspection cannot drive simulation lifecycle and that reply
limits retain the requested focus or explicitly report its incompleteness.

===========================================================================
*/

package simulation

import (
	"opensro.online/server/internal/game/world/monster"
	"testing"
	"time"
)

/*
================
TestObservatoryDoesNotDriveWorldLifecycle
================
*/
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
	if len(rows) != 3 || rows[0].GID != 8 || rows[1].GID != 9 || !truncated || !complete {
		t.Fatalf("cap kept %+v truncated=%v complete=%v", rows, truncated, complete)
	}
	if _, truncated, complete := capObservatory(focus, nil, false, 1); !truncated || complete {
		t.Fatal("a focus larger than the cap was reported complete")
	}
	near := focusNeighbourhood([]uint16{0x6e4b})
	if len(near) != 9 || !near[0x6d4a] || !near[0x6f4c] || near[0x6e4d] {
		t.Fatalf("neighbourhood = %v", near)
	}
	if dungeon := focusNeighbourhood([]uint16{0x8001}); len(dungeon) != 1 || !dungeon[0x8001] {
		t.Fatalf("a dungeon focus covers %v, want only itself", dungeon)
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
	// Overflow through the real capture: with room for one row, the monster
	// near the focus is the one kept, whichever the map yields first.
	for _, focus := range [][]uint16{{257}, {0x2020}} {
		capped := s.observatory("a", focus, 1)
		if len(capped.Monsters) != 1 || !capped.Truncated || !capped.FocusComplete ||
			capped.Monsters[0].Region != focus[0] {
			t.Fatalf("focus %#x capture = %+v", focus[0], capped)
		}
	}
}

/*
================
TestObservatoryRetainsRespawnedUniquesBeyondTheCensusCap

Later unique spawns have larger GIDs than the original ordinary population.
Both ordinary and party uniques must survive the census cap; corpses must
not inflate the authoritative count or displace living encounters.
================
*/
func TestObservatoryRetainsRespawnedUniquesBeyondTheCensusCap(t *testing.T) {
	s := NewMonsterState(monster.TemplateFromParts(nil, nil))
	s.StartDivision("audit")
	state := s.divs["audit"]
	for _, row := range []struct {
		gid    uint32
		region uint16
		rarity uint8
		hp     uint32
	}{
		{1, 0x1010, 0, 100},
		{2, 0x2020, 0, 100},
		{3, 0x2020, 0, 100},
		{4, 0x1010, 3, 0},
		{100, 0x1010, 3, 100},
		{101, 0x1010, 0x13, 100},
	} {
		state.instances.set(row.gid, monster.Instance{
			Gid:       row.gid,
			Ref:       monster.MonsterRef{RefObjID: row.gid, MonsterType: row.rarity & 15, MaxHP: 100},
			Nest:      monster.NestRow{HasRarityOverride: true, RarityOverride: row.rarity},
			Spawn:     monster.SpawnPoint{RegionID: row.region},
			CurrentHP: row.hp,
		})
	}
	for _, limit := range []int{4, 2, 1} {
		snapshot := s.observatory("audit", []uint16{0x2020}, limit)
		if snapshot.Resident != 6 || snapshot.UniqueAlive != 2 || !snapshot.Truncated || len(snapshot.Monsters) != limit {
			t.Fatalf("limit %d lost the full population count: %+v", limit, snapshot)
		}
		if snapshot.FocusComplete != (limit == 4) {
			t.Fatalf("limit %d misreported focus completeness: %+v", limit, snapshot)
		}
		alive := 0
		for _, row := range snapshot.Monsters {
			if row.Rarity&15 == 3 && row.HP > 0 {
				alive++
			}
		}
		if want := min(limit, 2); alive != want {
			t.Fatalf("limit %d kept %d living uniques, want %d", limit, alive, want)
		}
	}
}
