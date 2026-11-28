/*
===========================================================================

unique_states_test.go - the unique tracker follows each living copy

Regression coverage for the Ch'in Tomb counter: a Sarin general stands on
its Floor 3 nest and in the Floor 6 South stage at once. One counter and
one spawn time per reference showed a killed copy's neighbour as standing
where the dead copy stood. Two nests of one unique reference stand in for
the two copies here.

===========================================================================
*/

package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

const (
	twinUniqueRef     = 1
	twinUniqueRegionA = 0x62aa
	twinUniqueRegionB = 0x62ab
	twinRespawnSec    = 10
)

/*
================
twinUniqueState

A division whose one unique reference has two nests, both standing.
================
*/
func twinUniqueState(t *testing.T, now *time.Time) *MonsterState {
	t.Helper()
	refs := map[uint32]monster.MonsterRef{twinUniqueRef: {TidWord: 0x00C6, RefObjID: twinUniqueRef,
		Codename: "MOB_TEST_TWIN", MaxHP: 100, MonsterType: 3, ScaleDenom: 100}}
	var nests []monster.NestRow
	for _, region := range []uint16{twinUniqueRegionA, twinUniqueRegionB} {
		nests = append(nests, monster.NestRow{SpawnPoint: monster.SpawnPoint{RefObjID: twinUniqueRef, RegionID: region},
			PolicyPinned: true, MaxCount: 1, Respawn: true,
			RespawnDelayMinSec: twinRespawnSec, RespawnDelayMaxSec: twinRespawnSec})
	}
	s := NewMonsterState(monster.TemplateFromParts(refs, nests))
	s.SetTimeSource(func() time.Time { return *now })
	s.StartDivision("d")
	s.AdvancePopulation(s.CurrentTimeMillis())
	return s
}

/*
================
twinState

The tracker's row for the twin reference.
================
*/
func twinState(t *testing.T, s *MonsterState) UniqueState {
	t.Helper()
	for _, entry := range s.UniqueStates("d") {
		if entry.RefObjID == twinUniqueRef {
			return entry
		}
	}
	t.Fatal("the tracker does not list the unique")
	return UniqueState{}
}

/*
================
twinCopies

The two living copies, oldest gid first.
================
*/
func twinCopies(t *testing.T, s *MonsterState) (monster.Instance, monster.Instance) {
	t.Helper()
	all := s.InstancesInRegions("d", []uint16{twinUniqueRegionA, twinUniqueRegionB})
	if len(all) != 2 {
		t.Fatalf("%d copies stand, want 2", len(all))
	}
	if all[0].Gid > all[1].Gid {
		all[0], all[1] = all[1], all[0]
	}
	return all[0], all[1]
}

/*
================
TestUniqueTrackerKeepsTheSurvivingCopy

Killing the newer copy leaves the older one listed where it stands, with
its own spawn time; killing both reads dead; a respawn reads alive again
at the respawn time.
================
*/
func TestUniqueTrackerKeepsTheSurvivingCopy(t *testing.T) {
	now := time.Unix(100, 0)
	s := twinUniqueState(t, &now)
	older, newer := twinCopies(t, s)
	entry := twinState(t, s)
	if !entry.Alive || len(entry.Copies) != 2 {
		t.Fatalf("both copies stand: alive %v, %d copies", entry.Alive, len(entry.Copies))
	}

	now = now.Add(time.Second)
	if !s.Defeat("d", newer.Gid, now) {
		t.Fatal("the newer copy's defeat was refused")
	}
	entry = twinState(t, s)
	if !entry.Alive || len(entry.Copies) != 1 {
		t.Fatalf("one copy stands: alive %v, %d copies", entry.Alive, len(entry.Copies))
	}
	if entry.RegionID != older.Spawn.RegionID || entry.Copies[0].RegionID != older.Spawn.RegionID {
		t.Fatalf("the row stands in region %#x, the survivor in %#x", entry.RegionID, older.Spawn.RegionID)
	}

	if !s.Defeat("d", older.Gid, now) {
		t.Fatal("the older copy's defeat was refused")
	}
	if entry = twinState(t, s); entry.Alive || len(entry.Copies) != 0 {
		t.Fatalf("no copy stands: alive %v, %d copies", entry.Alive, len(entry.Copies))
	}

	now = now.Add((twinRespawnSec + 1) * time.Second)
	s.AdvancePopulation(s.CurrentTimeMillis())
	entry = twinState(t, s)
	if !entry.Alive || entry.SpawnedAtMs != now.UnixMilli() {
		t.Fatalf("after the respawn: alive %v, spawned %d, want %d", entry.Alive, entry.SpawnedAtMs, now.UnixMilli())
	}
}

/*
================
TestUniqueTrackerIgnoresAnUnmarkedDeath

A death the tracker never saw spawn (a quest unique raised without its
announcement) leaves the living copies alone.
================
*/
func TestUniqueTrackerIgnoresAnUnmarkedDeath(t *testing.T) {
	now := time.Unix(100, 0)
	s := twinUniqueState(t, &now)
	s.mu.Lock()
	s.divs["d"].markUniqueDeath(twinUniqueRef, 0xFFFFFF)
	s.mu.Unlock()
	if entry := twinState(t, s); len(entry.Copies) != 2 {
		t.Fatalf("%d copies after an unmarked death, want 2", len(entry.Copies))
	}
}
