/*
===========================================================================

targetexists_test.go - the value-only target verdict agrees with resolution

#2139: the waiting cast's per-tick verdict, the basic-attack chain, the
periodic recheck and the area primary used to detach the target's whole
record (or its owner's pet-owner view) and read only whether it exists.
combatTargetExists judges the same gates on the live record instead.
These tests pin the two halves of that trade: the verdict is
resolveCombatTarget's verdict in every state the gates distinguish,
and the question detaches nothing.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestCombatTargetExistsMatchesResolveVerdict
================
*/
func TestCombatTargetExistsMatchesResolveVerdict(t *testing.T) {
	verdict := func(rt *Runtime, attacker *enterworld.Character, gid uint32, now int64) (bool, bool) {
		_, resolved := rt.resolveCombatTarget(testDivision, attacker, gid, now)
		return rt.combatTargetExists(testDivision, attacker, gid), resolved
	}
	check := func(name string, rt *Runtime, attacker *enterworld.Character, gid uint32, now int64, want bool) {
		t.Helper()
		exists, resolved := verdict(rt, attacker, gid, now)
		if exists != resolved || exists != want {
			t.Fatalf("%s: exists=%v resolved=%v, want %v", name, exists, resolved, want)
		}
	}

	// The monster gate: alive stands, dead does not.
	rt, clock, attacker, target := newCombatTestRuntime(t, 100000)
	check("living monster", rt, attacker, target.Gid, clock.NowMs(), true)
	rt, clock, attacker, target = newCombatTestRuntime(t, 0)
	check("dead monster", rt, attacker, target.Gid, clock.NowMs(), false)

	// The player gate through its states.
	rt, clock, attacker, victim := newPvpPair(t)
	now := clock.NowMs()
	victimGID := enterworld.ObjectIDForCharacter(victim)
	check("living player", rt, attacker, victimGID, now, true)
	check("own gid", rt, attacker, enterworld.ObjectIDForCharacter(attacker), now, false)
	check("zero gid", rt, attacker, 0, now, false)
	check("unknown gid", rt, attacker, 0x7ffffff0, now, false)
	if rt.combatTargetExists(testDivision, nil, victimGID) {
		t.Fatal("a nil attacker has a target")
	}
	*victim.CurrentHP = 0
	check("dead player", rt, attacker, victimGID, now, false)
	*victim.CurrentHP = 100
	victim.DeletePending = true
	check("deletion-pending player", rt, attacker, victimGID, now, false)
	victim.DeletePending = false

	// The COS gate: the owner's summoned companion, judged through it.
	equipStrikeTestCOS(t, rt, victim)
	cos := victim.ActiveCOS
	check("summoned COS", rt, attacker, cos.GID, now, true)
	cos.CurrentHP = 0
	check("dead COS", rt, attacker, cos.GID, now, false)
	cos.CurrentHP = 100
	// The owner arm's own gates, one state at a time: a pet still out
	// does not save a target whose owner is leaving, and the owner's
	// life is not judged at all.
	victim.DeletePending = true
	check("COS of deletion-pending owner", rt, attacker, cos.GID, now, false)
	victim.DeletePending = false
	*victim.CurrentHP = 0
	check("COS of dead owner", rt, attacker, cos.GID, now, true)
	*victim.CurrentHP = 100
	cos.Summoned = false
	check("dismissed COS", rt, attacker, cos.GID, now, false)
}

/*
================
TestCombatTargetExistsDetachesNoRecord

The admitted-player verdict used to cost the victim's whole Snapshot
(20 allocs, ~16 KB on a production-shaped record, per the #2139
probe). The value-only question must stay near the door's own cost,
far under the copy it replaces.
================
*/
func TestCombatTargetExistsDetachesNoRecord(t *testing.T) {
	rt, _, attacker, victim := newPvpPair(t)
	gid := enterworld.ObjectIDForCharacter(victim)

	existsAllocs := testing.AllocsPerRun(100, func() {
		_ = rt.combatTargetExists(testDivision, attacker, gid)
	})
	resolveAllocs := testing.AllocsPerRun(100, func() {
		_, _ = rt.resolveCombatTarget(testDivision, attacker, gid, 0)
	})
	t.Logf("exists %.0f allocs/run, resolve %.0f allocs/run", existsAllocs, resolveAllocs)
	if existsAllocs > 8 {
		t.Fatalf("the existence question costs %.0f allocs/run, want at most 8 (no record detached)", existsAllocs)
	}
	if existsAllocs*3 >= resolveAllocs {
		t.Fatalf("exists %.0f allocs/run is not far under resolve %.0f", existsAllocs, resolveAllocs)
	}
}
