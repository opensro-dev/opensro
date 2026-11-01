/*
===========================================================================

monsterlegstep_test.go - the step-level generation pins

The leg snapshot arms each actor step with the mover row's commit
generation (advanceAndPublish). A slower lane's set after the snapshot
advances the row, and the step's whole-value commits must drop rather
than overwrite the newer value (#2047, carried by the generation
since #2137 slice 1). TestMonsterLegStepKeepsTheInterleavedMover
covers the commits that sit behind value guards; the two pins here
cover the stepped commits that have no value guard of their own, so
disarming the step (commitSteppedMover / commitSteppedMoverFrames
falling back to the plain commits) goes red.

===========================================================================
*/

package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

// The slower lane's set is recognised by this stamp: no step plan
// writes it, so it survives only while every commit planned from the
// pre-set snapshot dropped.
const legStepSlowerLaneMark uint32 = 424242

/*
================
TestMonsterLegStepDropsAttackCommitAfterSlowerLaneSet

The attack step commits twice around its damage callback: the
attack-start value before it, a whole-value post-attack value after
it. The RunAction double's attack operation stands in for the slower
lane and lands its set from inside the callback - after the
attack-start commit, after the activity gate's re-base, before the
post-attack commit planned from the same snapshot. Only the step's
commit generation can drop that last commit: the retaliation
revision is untouched and no before-value guard sits on the path.
Disarming commitSteppedMover (the plain CommitMover fallback) lands
the stale plan and erases the mark, so this pin goes red.
================
*/
func TestMonsterLegStepDropsAttackCommitAfterSlowerLaneSet(t *testing.T) {
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	ops.AttackPlan = func(monster.Instance, uint32, AttackPick) (MonsterAttackPlan, bool) {
		return MonsterAttackPlan{SkillID: 0x1234, Reach: ActionReach(50), CooldownMs: 1000, ActionLifecycleMs: 600}, true
	}
	attacks := 0
	interleaved := false
	ops.RunAction = func(_ string, run func(MonsterAttackOperation)) {
		run(func(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
			attacks++
			// The slower lane's set lands mid-step: the step's
			// attack-start commit is already in, its post-attack
			// commit is still planned from the leg's snapshot.
			stored, ok := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
			if !ok {
				t.Fatal("attack callback lost the actor")
			}
			stored.LastBattleActivityMs = legStepSlowerLaneMark
			if !ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, stored) {
				t.Fatal("slower lane's set was rejected")
			}
			interleaved = true
			return MonsterAttackResult{Accepted: true, TargetAlive: true}
		})
	}
	if !ops.Monsters.ArmRetaliation(monsterTestDivision, instance.Gid, PlayerObjectID(1)) {
		t.Fatal("retaliation refused")
	}
	legAt := time.Now().UnixMilli() + 2000
	player := playerSessionAt(1, 1000, 1000)
	ops.RunMonsterLeg(legAt, []SessionSnapshot{player}, &fakePusher{})
	if attacks != 1 || !interleaved {
		t.Fatalf("step did not reach its damage callback: attacks %d, interleaved %v", attacks, interleaved)
	}
	stored, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if stored.LastBattleActivityMs != legStepSlowerLaneMark {
		t.Fatalf("step overwrote the slower lane's set: LastBattleActivityMs %d, want the mark %d - its post-attack whole-value commit should have dropped on the advanced generation",
			stored.LastBattleActivityMs, legStepSlowerLaneMark)
	}
}

/*
================
TestMonsterLegStepDropsPendingSettleAfterSlowerLaneSet

The PENDING gate settle is the one stepped commit no re-base
precedes: a suspended actor whose in-flight segment matures settles
straight from the armed snapshot (monsteractivity.go). The fixture
holds the activity cadence not yet due at arrival, so the gate
falls through to the settle and the settle's frames commit is the
step's only commit - with the cadence due, commitActivity's
before-value guard would end the gate earlier and mask it. The
RunAction double lands the slower lane's set after the settle leg's
snapshot, before the step runs; the settle commit must drop,
leaving the marked mover - segment still in flight - for the next
due. Disarming commitSteppedMoverFrames (the plain
CommitMoverFrames fallback) lands the stale settle, applies the
arrival and erases the mark, so this pin goes red.
================
*/
func TestMonsterLegStepDropsPendingSettleAfterSlowerLaneSet(t *testing.T) {
	ops, actor, now := activityFixture(t)
	original, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	ops.RunMonsterLeg(now+1001, nil, &fakePusher{})
	pending, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if pending.Mode() != monster.MoverPending || pending.ArriveMs != original.ArriveMs {
		t.Fatalf("fixture did not suspend with its segment in flight: %+v", pending)
	}
	// Hold the cadence quiet at arrival (see the banner): the next
	// check falls a full interval past the settle leg.
	pending.Activity.LastCheck = uint32(original.ArriveMs)
	if !ops.Monsters.CommitMover(monsterTestDivision, actor.Gid, pending) {
		t.Fatal("cadence retime was rejected")
	}
	interleaved := false
	ops.RunAction = func(_ string, run func(MonsterAttackOperation)) {
		// The slower lane lands once, after this leg's snapshot and
		// before the steps run. The wrapper fires per dispatched
		// actor; landing the set again after a later dispatch would
		// rewrite it over a settle that already committed.
		if !interleaved {
			stored, ok := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
			if !ok {
				t.Fatal("interleave lost the actor")
			}
			stored.LastBattleActivityMs = legStepSlowerLaneMark
			if !ops.Monsters.CommitMover(monsterTestDivision, actor.Gid, stored) {
				t.Fatal("slower lane's set was rejected")
			}
			interleaved = true
		}
		run(nil)
	}
	ops.RunMonsterLeg(original.ArriveMs, nil, &fakePusher{})
	if !interleaved {
		t.Fatal("the settle leg never reached the action lane")
	}
	stored, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if stored.LastBattleActivityMs != legStepSlowerLaneMark {
		t.Fatalf("settle overwrote the slower lane's set: LastBattleActivityMs %d, want the mark %d - its frames commit should have dropped on the advanced generation",
			stored.LastBattleActivityMs, legStepSlowerLaneMark)
	}
	if stored.ArriveMs != pending.ArriveMs || stored.Pose != pending.Pose {
		t.Fatalf("settle landed on the stale snapshot: ArriveMs %d Pose %+v, want the in-flight segment (ArriveMs %d) kept",
			stored.ArriveMs, stored.Pose, pending.ArriveMs)
	}
}
