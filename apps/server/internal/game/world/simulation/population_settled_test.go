/*
===========================================================================

population_settled_test.go - when the boot fill counts as settled

The rule is per callback unit (a nest outside a hive, or a hive) and
sticky: a unit settles at its first visit that places nothing. These tests
run several nests that need several visits each, with monsters dying and
respawning throughout, which is what the whole-world rule it replaces
could never get past.

===========================================================================
*/

package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

const (
	settledTestNests    = 12
	settledTestPerNest  = 3
	settledTestStepMs   = 100
	settledTestLimitMs  = 30000
	settledTestRegionID = 257
)

/*
================
settledTestState

settledTestNests respawning nests of settledTestPerNest monsters in one
region; each visit places at most one monster per nest, so the fill takes
several nest ticks.
================
*/
func settledTestState(t *testing.T) (*MonsterState, *time.Time) {
	t.Helper()
	nests := make([]monster.NestRow, 0, settledTestNests)
	for i := 0; i < settledTestNests; i++ {
		nests = append(nests, monster.NestRow{
			SpawnPoint: monster.SpawnPoint{RefObjID: 1, RegionID: settledTestRegionID, X: float64(10 + i*40), Z: 34},
			MaxCount:   settledTestPerNest, PolicyPinned: true, Respawn: true,
		})
	}
	s := NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{1: {TidWord: 0x00C6, RefObjID: 1, Name: "Nest", MaxHP: 100}},
		nests,
	))
	now := time.Unix(100, 0)
	s.SetTimeSource(func() time.Time { return now })
	s.StartDivision("a")
	return s, &now
}

/*
================
TestPopulationSettlesPerUnitDespiteRespawnChurn

Not settled before a pass or while nests are still placing; settled once
every nest has had a visit that placed nothing, even though a monster is
killed (and respawns) every half second; and it stays settled under churn.
================
*/
func TestPopulationSettlesPerUnitDespiteRespawnChurn(t *testing.T) {
	s, now := settledTestState(t)
	if s.PopulationSettled("a") {
		t.Fatal("settled before any population pass")
	}
	settledAt, kills := int64(-1), 0
	for elapsed := int64(0); elapsed <= settledTestLimitMs; elapsed += settledTestStepMs {
		s.AdvancePopulation(s.CurrentTimeMillis())
		live := s.InstancesInRegions("a", []uint16{settledTestRegionID})
		// Churn from the first second: one death every half second.
		if elapsed%500 == 0 && len(live) > 0 {
			if s.Defeat("a", live[0].Gid, *now) {
				kills++
			}
		}
		if settledAt < 0 && s.PopulationSettled("a") {
			settledAt = elapsed
		}
		*now = now.Add(settledTestStepMs * time.Millisecond)
	}
	if settledAt < 0 {
		t.Fatalf("never settled in %d ms with %d kills of churn", settledTestLimitMs, kills)
	}
	// Each nest places one monster per nest tick, so the fill cannot settle
	// before its nests have had settledTestPerNest visits.
	if settledAt < (settledTestPerNest-1)*monster.NestHiveTickMs {
		t.Fatalf("settled at %d ms, before the nests could have filled", settledAt)
	}
	if kills == 0 || !s.PopulationSettled("a") {
		t.Fatalf("churn %d kills; settled at the end %v", kills, s.PopulationSettled("a"))
	}
	if !s.PopulationSettled("absent") {
		t.Fatal("a division without population is not settled")
	}
}
