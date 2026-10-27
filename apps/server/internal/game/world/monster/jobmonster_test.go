/*
===========================================================================

jobmonster_test.go - tests for jobmonster.go

===========================================================================
*/
package monster

import "testing"

/*
================
TestJobMonsterRefusesItsOwnSide

The native validation: a thief monster refuses job states 2 and 4, a
hunter monster 1, 3 and 4; 4 is a player without a suit. An ordinary
monster refuses nobody.
================
*/
func TestJobMonsterRefusesItsOwnSide(t *testing.T) {
	monsterOf := func(typeID4 uint8) Instance {
		var i Instance
		i.Ref.TidWord, i.Ref.TypeID4 = 0x00c6, typeID4
		return i
	}
	thief, hunter, ordinary := monsterOf(2), monsterOf(3), monsterOf(1)
	if !thief.ThiefMonster() || !hunter.HunterMonster() || ordinary.ThiefMonster() || ordinary.HunterMonster() {
		t.Fatal("fixture monsters are not a thief, a hunter and an ordinary monster")
	}
	for job := uint8(0); job <= 4; job++ {
		wantThief := job == jobStateThief || job == jobStateSpecial
		wantHunter := job == jobStateTrader || job == jobStateHunter || job == jobStateSpecial
		if got := thief.JobTargetRefused(job); got != wantThief {
			t.Errorf("thief monster, job %d: refused %v, want %v", job, got, wantThief)
		}
		if got := hunter.JobTargetRefused(job); got != wantHunter {
			t.Errorf("hunter monster, job %d: refused %v, want %v", job, got, wantHunter)
		}
		if ordinary.JobTargetRefused(job) {
			t.Errorf("ordinary monster refused job %d", job)
		}
	}
}
