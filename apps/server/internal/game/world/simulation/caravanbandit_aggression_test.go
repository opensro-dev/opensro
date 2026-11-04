/*
===========================================================================

caravanbandit_aggression_test.go - a bandit attacks on spawn under the rule

Port-only, not native (#2474): with SRO_JOB_MONSTER_AGGRESSION on, a
caravan bandit spawns with no hold, so its first hit on the caravan it
ambushed lands a spawn hold earlier than the same ambush without the
rule; off, it holds like every monster and lands nothing in the hold.

===========================================================================
*/
package simulation

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
caravanBanditFirstHit

Runs one ambush (the caravanBanditWorld fixture) until the bandit's
first accepted hit; returns the hit's offset from the spawn instant,
or -1 if none lands in 30 s.
================
*/
func caravanBanditFirstHit(t *testing.T, t0 int64) int64 {
	t.Helper()
	_, _, ops, attacks, lastHit := caravanBanditWorld(t, t0)
	push := &fakePusher{}
	for step := int64(0); step <= 300; step++ {
		now := t0 + step*100
		ops.RunMonsterLeg(now, []SessionSnapshot{caravanTraderAt(20, float64(step)/10)}, push)
		if *attacks > 0 {
			return *lastHit - t0
		}
	}
	return -1
}

/*
================
TestCaravanBanditAttacksOnSpawnUnderTheRule
================
*/
func TestCaravanBanditAttacksOnSpawnUnderTheRule(t *testing.T) {
	const t0 = int64(1_784_000_000_000)

	t.Setenv(monster.EnvJobMonsterAggression, "")
	state, bandit, _, _, _ := caravanBanditWorld(t, t0)
	mover, ok := state.Mover(monsterTestDivision, bandit.Gid)
	if !ok || mover.BehaviorDeadlineMs != t0+monster.RetailSpawnHoldMs {
		t.Fatalf("rule off: spawn deadline = %d, want the native hold to %d", mover.BehaviorDeadlineMs, t0+monster.RetailSpawnHoldMs)
	}
	nativeHit := caravanBanditFirstHit(t, t0)
	if nativeHit < monster.RetailSpawnHoldMs {
		t.Fatalf("rule off: first hit at %d ms, inside the native spawn hold", nativeHit)
	}

	t.Setenv(monster.EnvJobMonsterAggression, "on")
	state, bandit, _, _, _ = caravanBanditWorld(t, t0)
	mover, ok = state.Mover(monsterTestDivision, bandit.Gid)
	if !ok || mover.BehaviorDeadlineMs != t0 {
		t.Fatalf("rule on: spawn deadline = %d, want the spawn instant %d (no hold)", mover.BehaviorDeadlineMs, t0)
	}
	ruleHit := caravanBanditFirstHit(t, t0)
	if ruleHit < 0 {
		t.Fatal("rule on: bandit never landed a hit")
	}
	if ruleHit > nativeHit-monster.RetailSpawnHoldMs+500 {
		t.Fatalf("first hit at %d ms under the rule vs %d ms native, want about a spawn hold earlier", ruleHit, nativeHit)
	}
}
