/*
===========================================================================
wolf_test.go - the first Execute book retains its existing threshold curve.
===========================================================================
*/
package companion

import "testing"

/*
================
TestWolfBookScalingNeverDropsAtPromotion
================
*/
func TestWolfBookScalingNeverDropsAtPromotion(t *testing.T) {
	for _, family := range []string{"Hunting Howl", "Bloodscent"} {
		previous := 0.0
		for _, skill := range D.TreeSkills {
			if skill.Family != family {
				continue
			}
			if skill.ValueAt(1) < previous {
				t.Fatalf("%s promotion loses value", skill.Name)
			}
			previous = skill.ValueAt(10)
			if family == "Hunting Howl" && skill.ValueAt(255) != 500 {
				t.Fatal("equipment skill bonuses bypassed the Howl cap")
			}
		}
		if family == "Hunting Howl" && previous != 500 {
			t.Fatalf("Howl maximum %v, want 500", previous)
		}
	}
}

/*
================
TestExecutionThreshold
================
*/
func TestExecutionThreshold(t *testing.T) {
	for _, s := range D.TreeSkills {
		if s.Family != "Execute" {
			continue
		}
		for _, rank := range []uint8{1, 5, 10, 12} {
			if ExecutionThreshold(s, rank, false) != s.ValueAt(rank) {
				t.Fatal("base threshold changed")
			}
			bonus := 0.0
			if rank >= D.Rules.Rank2Level || s.Option > 1 {
				bonus = 5
			}
			if ExecutionThreshold(s, rank, true) != s.ValueAt(rank)+bonus {
				t.Fatal("bleeding threshold changed")
			}
		}
	}
}
