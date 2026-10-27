/*
===========================================================================

targetsnapshot_test.go - admission-filtered snapshots equal plain ones

#1820 part C lets target resolution weigh the life, deletion, world and
companion gates on the live record and detach a copy only for a target
that passes. These tests pin the contract that makes that safe: a
snapshot taken through the admission door is the same record
characterSnapshot detaches the old way, and a rejected target resolves
exactly as it did when the copy was made first and judged second.

===========================================================================
*/
package action

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestAdmittedSnapshotEqualsCharacterSnapshot
================
*/
func TestAdmittedSnapshotEqualsCharacterSnapshot(t *testing.T) {
	rt, _, character, _ := newCombatTestRuntime(t, 100000)

	want := rt.characterSnapshot(testDivision, character)

	admitted := rt.characterSnapshotAdmitted(testDivision, character, func(live *enterworld.Character) bool {
		return true
	})
	if !reflect.DeepEqual(admitted, want) {
		t.Fatalf("admitted snapshot differs from the plain snapshot:\nadmitted %+v\nplain    %+v", admitted, want)
	}

	unfiltered := rt.characterSnapshotAdmitted(testDivision, character, nil)
	if !reflect.DeepEqual(unfiltered, want) {
		t.Fatal("a nil admission filter must detach exactly like characterSnapshot")
	}

	rejected := rt.characterSnapshotAdmitted(testDivision, character, func(live *enterworld.Character) bool {
		return false
	})
	if rejected != nil {
		t.Fatalf("rejected admission detached %+v, want no copy", rejected)
	}

	if got := rt.characterSnapshotAdmitted(testDivision, nil, nil); got != nil {
		t.Fatalf("nil character detached %+v, want nil", got)
	}
}

/*
================
TestResolveCombatTargetSnapshotEqualsCharacterSnapshot

The admitted player target carries the same detached record the
unfiltered resolution carried; a dead target resolves to nothing, as
it did when its copy was judged after the fact.
================
*/
func TestResolveCombatTargetSnapshotEqualsCharacterSnapshot(t *testing.T) {
	rt, clock, attacker, victim := newPvpPair(t)
	now := clock.NowMs()

	target, ok := rt.resolveCombatTarget(testDivision, attacker, enterworld.ObjectIDForCharacter(victim), now)
	if !ok {
		t.Fatal("the living victim does not resolve")
	}
	want := rt.characterSnapshot(testDivision, victim)
	if !reflect.DeepEqual(target.snapshot, want) {
		t.Fatal("the admitted target's snapshot differs from the plain snapshot")
	}

	*victim.CurrentHP = 0
	if _, ok := rt.resolveCombatTarget(testDivision, attacker, enterworld.ObjectIDForCharacter(victim), now); ok {
		t.Fatal("the dead victim resolves")
	}
}

/*
================
TestResolveCOSTargetSnapshotEqualsCharacterSnapshot
================
*/
func TestResolveCOSTargetSnapshotEqualsCharacterSnapshot(t *testing.T) {
	rt, clock, attacker, owner := newPvpPair(t)
	equipStrikeTestCOS(t, rt, owner)
	cos := owner.ActiveCOS
	now := clock.NowMs()

	target, ok := rt.resolveCOSTarget(testDivision, attacker, cos.GID, now)
	if !ok {
		t.Fatal("the summoned COS does not resolve")
	}
	want := rt.characterSnapshot(testDivision, owner)
	if !reflect.DeepEqual(target.snapshot, want) {
		t.Fatal("the admitted COS owner's snapshot differs from the plain snapshot")
	}

	cos.CurrentHP = 0
	if _, ok := rt.resolveCOSTarget(testDivision, attacker, cos.GID, now); ok {
		t.Fatal("the dead COS resolves")
	}
}

/*
================
TestAreaCandidatesRejectedPlayersStayOut

Dead and deletion-pending players were dropped after their copy was
judged; they are still dropped, and their companions with them.
================
*/
func TestAreaCandidatesRejectedPlayersStayOut(t *testing.T) {
	rt, clock, attacker, victim := newPvpPair(t)
	rt.RewardActorPresent = func(string, string) bool { return true }
	equipStrikeTestCOS(t, rt, victim)
	now := clock.NowMs()
	skill := enterworld.SkillRow{Attack: enterworld.SkillAttack{Present: true}}
	q := areaQuery{selects: areaSelectHostile, division: testDivision, caster: attacker, skill: skill,
		center: rt.liveSpawn(simulation.WorldKey(testDivision, attacker.Name), attacker, now), reach: 50, now: now}

	*victim.CurrentHP = 0
	for _, candidate := range rt.areaPlayerCandidates(q) {
		if candidate.target.player == victim {
			t.Fatal("the dead player or its companion is an area candidate")
		}
	}

	*victim.CurrentHP = 100
	victim.DeletePending = true
	for _, candidate := range rt.areaPlayerCandidates(q) {
		if candidate.target.player == victim {
			t.Fatal("the deletion-pending player or its companion is an area candidate")
		}
	}
}
