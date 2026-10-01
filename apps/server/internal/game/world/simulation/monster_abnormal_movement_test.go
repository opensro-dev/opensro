/*
===========================================================================

monster_abnormal_movement_test.go - immobilizers settle an admitted mover

Check the damage transaction, retained pose and outward correction together.
The next-leg admission gate alone cannot prevent an old segment drifting.

===========================================================================
*/

package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestImmobilizingStatusSettlesMonsterMovement
================
*/
func TestImmobilizingStatusSettlesMonsterMovement(t *testing.T) {
	const now int64 = 10000
	for _, status := range []abnormal.Status{abnormal.Freeze, abnormal.Sleep, abnormal.Stun, abnormal.Root} {
		ops, instance := monsterLegFixture(t, aggressiveTactics())
		state := ops.Monsters
		state.clock = func() time.Time { return time.UnixMilli(now) }
		state.SetAbnormalContext(testAbnormalContext{})
		mover, _ := state.Mover(monsterTestDivision, instance.Gid)
		if err := mover.Transition(monster.MoverEventStartWander, 0); err != nil {
			t.Fatal(err)
		}
		mover.From = monster.Pose{RegionID: 25000, X: 1000, Y: 20, Z: 1000}
		mover.To = monster.Pose{RegionID: 25000, X: 1100, Y: 20, Z: 1000}
		mover.Pose = mover.From
		mover.DepartMs, mover.ArriveMs = now-500, now+500
		state.CommitMover(monsterTestDivision, instance.Gid, mover)
		record := abnormal.Record{Status: status, Level: 1, Grade: 1, DurationMs: 1000, SourceGID: 9}
		result := applyRecords(t, state, monsterTestDivision, instance.Gid, 9, 0, record)
		after, _ := state.Mover(monsterTestDivision, instance.Gid)
		if after.InFlight(now) || after.Pose.X != 1050 || result.Abnormal.Halted == nil || result.Abnormal.Halted.X != 1050 {
			t.Errorf("status %d: mover %+v, correction %+v", status, after, result.Abnormal.Halted)
		}
		if !result.Instance.MovementBlocked() {
			t.Errorf("status %d did not block the next movement leg", status)
		}
	}
}

/*
================
TestMonsterBombPreservesAuthoredOverkillDamage
================
*/
func TestMonsterBombPreservesAuthoredOverkillDamage(t *testing.T) {
	instance := monster.Instance{CurrentHP: 7}
	owner := monsterAbnormalOwner{instance: &instance, names: map[uint32]string{9: "caster"}}
	owner.Detonate(abnormal.Slot{Record: abnormal.Record{Status: abnormal.TimeBomb, Damage1C: 300, SourceGID: 9}})
	if instance.CurrentHP != 0 || len(owner.fx.Detonations) != 1 || owner.fx.Detonations[0].Damage1C != 300 {
		t.Fatalf("bomb result %+v", owner.fx)
	}
	if len(owner.fx.Hits) != 1 || owner.fx.Hits[0].Damage != 7 {
		t.Fatal("fatal debit did not use remaining HP")
	}
}

/*
================
TestMonsterSlowRetimesOwnedPathWithoutChangingSurface
================
*/
func TestMonsterSlowRetimesOwnedPathWithoutChangingSurface(t *testing.T) {
	const now int64 = 10000
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	state := ops.Monsters
	state.clock = func() time.Time { return time.UnixMilli(now) }
	state.SetAbnormalContext(testAbnormalContext{})
	mover, _ := state.Mover(monsterTestDivision, instance.Gid)
	if err := mover.Transition(monster.MoverEventStartWander, 0); err != nil {
		t.Fatal(err)
	}
	mover.From = monster.Pose{RegionID: 25000, X: 1000, Y: 20, Z: 1000}
	mover.To = monster.Pose{RegionID: 25000, X: 1008, Y: 20, Z: 1000}
	mover.Pose = mover.From
	mover.DepartMs, mover.ArriveMs = now-500, now+500
	mover.Channel = 2
	mover.SetNavigationMotion(instance.Ref.WalkSpeed, 2)
	mover.AdoptNavigation(monster.NewNavigationPath(mover.From, mover.To, mover.To, 0, func(fraction float64, _ monster.Pose) (float64, bool) { return 20 + fraction*8, true }))
	state.CommitMover(monsterTestDivision, instance.Gid, mover)
	before := mover.LivePoseAt(now, nil)
	record := abnormal.Record{Status: abnormal.Frostbite, Level: 1, DurationMs: 10000, SourceGID: 9}
	result := applyRecords(t, state, monsterTestDivision, instance.Gid, 9, 0, record)
	after, _ := state.Mover(monsterTestDivision, instance.Gid)
	speed, channel := after.NavigationMotion()
	if !result.Abnormal.SpeedChanged || after.LivePoseAt(now, nil) != before || after.ArriveMs != now+1000 || speed != instance.Ref.WalkSpeed/2 || channel != 2 {
		t.Fatalf("slow did not retain path and phase: %+v", after)
	}
	pose := after.LivePoseAt(now+500, nil)
	if pose.X != 1006 || pose.Y != 26 {
		t.Fatalf("slowed surface pose %+v", pose)
	}
}
