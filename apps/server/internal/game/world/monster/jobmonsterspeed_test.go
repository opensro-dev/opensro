/*
===========================================================================

jobmonsterspeed_test.go - tests for jobmonsterspeed.go

===========================================================================
*/
package monster

import "testing"

/*
================
TestJobMonsterSpeedScalesOnlyJobMonsters

Thief and hunter references run 20% faster under the rule; an ordinary
monster and the receiver's own references keep their speeds.
================
*/
func TestJobMonsterSpeedScalesOnlyJobMonsters(t *testing.T) {
	ref := func(typeID4 uint8) MonsterRef {
		return MonsterRef{TidWord: 0x00c6, TypeID4: typeID4, WalkSpeed: 50, RunSpeed: 100}
	}
	base := Template{Refs: map[uint32]MonsterRef{1: ref(2), 2: ref(3), 3: ref(1)}}
	fast, scaled := base.WithJobMonsterSpeed()
	if scaled != 2 {
		t.Fatalf("scaled %d references, want the thief and the hunter", scaled)
	}
	for id, want := range map[uint32][2]float64{1: {60, 120}, 2: {60, 120}, 3: {50, 100}} {
		if got := fast.Refs[id]; got.WalkSpeed != want[0] || got.RunSpeed != want[1] {
			t.Fatalf("ref %d: walk %v run %v, want %v", id, got.WalkSpeed, got.RunSpeed, want)
		}
	}
	if base.Refs[1].RunSpeed != 100 {
		t.Fatal("the receiver's references were scaled")
	}
}

/*
================
TestJobMonsterSpeedFromEnv
================
*/
func TestJobMonsterSpeedFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "on": true, "TRUE": true, "1": true} {
		t.Setenv(EnvJobMonsterSpeed, value)
		if got := JobMonsterSpeedFromEnv(); got != want {
			t.Fatalf("%q: %v, want %v", value, got, want)
		}
	}
}
