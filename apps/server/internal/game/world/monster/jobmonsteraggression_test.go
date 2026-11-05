/*
===========================================================================

jobmonsteraggression_test.go - tests for jobmonsteraggression.go

===========================================================================
*/
package monster

import "testing"

/*
================
TestJobMonsterAggressionFromEnv
================
*/
func TestJobMonsterAggressionFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "on": true, "TRUE": true, "1": true} {
		t.Setenv(EnvJobMonsterAggression, value)
		if got := JobMonsterAggressionFromEnv(); got != want {
			t.Fatalf("%q: %v, want %v", value, got, want)
		}
	}
}

/*
================
TestInstanceJobMonster

Only the thief and hunter type words are job monsters; an ordinary
monster and the neighbouring bands are not.
================
*/
func TestInstanceJobMonster(t *testing.T) {
	for _, c := range []struct {
		typeID4 uint8
		want    bool
	}{{2, true}, {3, true}, {1, false}, {4, false}} {
		i := Instance{Ref: MonsterRef{TidWord: 0x00c6, TypeID4: c.typeID4}}
		if got := i.JobMonster(); got != c.want {
			t.Fatalf("TypeID4 %d: JobMonster %v, want %v", c.typeID4, got, c.want)
		}
	}
}
