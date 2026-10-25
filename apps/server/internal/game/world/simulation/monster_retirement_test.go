/*
===========================================================================

monster_retirement_test.go - evacuation begins before final release is queued

The public population read must distinguish an occupied retiring layer from
its later empty release request, and must not read a replacement lifetime.

===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestPopulationRetiringTracksEvacuationAndExactLifetime
================
*/
func TestPopulationRetiringTracksEvacuationAndExactLifetime(t *testing.T) {
	s := NewMonsterState(monster.TemplateFromParts(nil, nil))
	lease, status := s.AllocatePopulation("a", instance.Pack(10, 1))
	if status != instance.Success {
		t.Fatal(status)
	}
	if retiring, exists := s.PopulationRetiring("a", lease); retiring || !exists {
		t.Fatalf("new lifetime retiring=%v exists=%v", retiring, exists)
	}
	if s.AdmitPopulationPC("a", lease, 42, false) != instance.Success {
		t.Fatal("admission")
	}
	if roster, ok := s.BeginPopulationRetirement("a", lease); !ok || len(roster) != 1 {
		t.Fatal("retirement did not capture the resident")
	}
	if len(s.PendingPopulationRetirements("a")) != 0 {
		t.Fatal("occupied evacuation unexpectedly requested final release")
	}
	if retiring, exists := s.PopulationRetiring("a", lease); !retiring || !exists {
		t.Fatalf("occupied evacuation retiring=%v exists=%v", retiring, exists)
	}
	if status, requested := s.LeavePopulationPC("a", lease, 42); status != instance.Success || !requested {
		t.Fatal("last resident did not request release", status, requested)
	}
	if retiring, exists := s.PopulationRetiring("a", lease); !retiring || !exists {
		t.Fatal("empty lifetime stopped retiring before release")
	}
	if !s.ReleasePopulation("a", lease) {
		t.Fatal("release")
	}
	replacement, status := s.AllocatePopulation("a", lease.ID)
	if status != instance.Success || replacement == lease {
		t.Fatal("replacement", replacement, status)
	}
	if retiring, exists := s.PopulationRetiring("a", lease); retiring || exists {
		t.Fatal("stale lease observed its replacement")
	}
	if retiring, exists := s.PopulationRetiring("a", replacement); retiring || !exists {
		t.Fatal("replacement inherited the old evacuation state")
	}
	if retiring, exists := s.PopulationRetiring("b", replacement); retiring || exists {
		t.Fatal("foreign division observed another population")
	}
}
