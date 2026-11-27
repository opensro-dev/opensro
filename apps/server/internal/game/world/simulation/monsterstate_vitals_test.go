/*
===========================================================================

monsterstate_vitals_test.go - natural recovery and latch tests

Pins the four-second standing/sitting heal of the reference's recovery
column, the motion and latch gates, and that a dormant monster heals in
its cold row without waking.

===========================================================================
*/

package simulation

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
vitalsTestState
================
*/
func vitalsTestState() *MonsterState {
	template := monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{
			1933: {RefObjID: 1933, MaxHP: 100, RecoveryHP: 10, WalkSpeed: 8, RunSpeed: 22, ScaleDenom: 100},
		},
		[]monster.NestRow{{
			SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: 0x62AA, X: 100, Y: 20, Z: 100},
		}},
	)
	return NewMonsterState(template)
}

/*
================
vitalsHP
================
*/
func vitalsHP(t *testing.T, s *MonsterState, division string, gid uint32) uint32 {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	pop := s.populationForObject(division, gid)
	if pop == nil {
		t.Fatal("no population for the test monster")
	}
	hp, ok := pop.instances.currentHP(gid)
	if !ok {
		t.Fatal("test monster is gone")
	}
	return hp
}

/*
================
TestStandingMonsterRecoversEveryFourSeconds
================
*/
func TestStandingMonsterRecoversEveryFourSeconds(t *testing.T) {
	s := vitalsTestState()
	m := firstDamageTestMonster(t, s, "vitals")
	now := int64(1_000_000)

	if hit, ok := s.ApplyDamage("vitals", m.Gid, 30); !ok || hit.CurrentHP != 70 {
		t.Fatalf("hurt = %+v, %v; want HP 70", hit, ok)
	}
	s.AdvanceMonsterVitals(now) // discovery only; first tick is one period out
	if got := vitalsHP(t, s, "vitals", m.Gid); got != 70 {
		t.Fatalf("HP after discovery = %d, want 70 (no instant heal)", got)
	}
	s.AdvanceMonsterVitals(now + 3999)
	if got := vitalsHP(t, s, "vitals", m.Gid); got != 70 {
		t.Fatalf("HP before the period = %d, want 70", got)
	}
	s.DrainChangedVitals(nil) // the hit's own publication
	s.AdvanceMonsterVitals(now + 4000)
	if got := vitalsHP(t, s, "vitals", m.Gid); got != 80 {
		t.Fatalf("HP after one period = %d, want 80", got)
	}
	// The heal publishes the new HP with the natural-recovery cause.
	changed := s.DrainChangedVitals(nil)
	if len(changed) != 1 || changed[0].Gid != m.Gid {
		t.Fatalf("published %+v after the heal, want one 0x33A6 for the monster", changed)
	}
	if want := MonsterVitalsPayload(monster.Instance{Gid: m.Gid, CurrentHP: 80}, VitalsSourceNaturalRecovery); string(changed[0].Payload) != string(want) {
		t.Fatalf("heal payload % x, want % x", changed[0].Payload, want)
	}
	s.AdvanceMonsterVitals(now + 8000)
	s.AdvanceMonsterVitals(now + 12000)
	if got := vitalsHP(t, s, "vitals", m.Gid); got != 100 {
		t.Fatalf("HP after three periods = %d, want 100 (clamped at max)", got)
	}
	s.DrainChangedVitals(nil)
	s.AdvanceMonsterVitals(now + 16000) // record dropped; stays full
	if got := vitalsHP(t, s, "vitals", m.Gid); got != 100 {
		t.Fatalf("HP after the record dropped = %d, want 100", got)
	}
	if changed := s.DrainChangedVitals(nil); len(changed) != 0 {
		t.Fatalf("a full monster published %+v", changed)
	}
}

/*
================
TestRecoveryWaitsForStandingOrSitting
================
*/
func TestRecoveryWaitsForStandingOrSitting(t *testing.T) {
	s := vitalsTestState()
	m := firstDamageTestMonster(t, s, "vitals-motion")
	now := int64(2_000_000)

	if _, ok := s.ApplyDamage("vitals-motion", m.Gid, 30); !ok {
		t.Fatal("hurt rejected")
	}
	s.mu.Lock()
	pop := s.populationForObject("vitals-motion", m.Gid)
	row, _ := pop.instances.hotRow(m.Gid)
	live := pop.instances.hotValue(m.Gid, row)
	live.Motion = monster.MotionHold{State: 2, UntilMs: math.MaxInt64}
	pop.instances.set(m.Gid, live)
	s.mu.Unlock()

	s.AdvanceMonsterVitals(now)
	s.AdvanceMonsterVitals(now + 4000)
	if got := vitalsHP(t, s, "vitals-motion", m.Gid); got != 70 {
		t.Fatalf("HP while walking = %d, want 70 (motion gate)", got)
	}

	s.mu.Lock()
	live.Motion = monster.MotionHold{State: 4, UntilMs: math.MaxInt64}
	pop.instances.set(m.Gid, live)
	s.mu.Unlock()

	s.AdvanceMonsterVitals(now + 8000)
	if got := vitalsHP(t, s, "vitals-motion", m.Gid); got != 80 {
		t.Fatalf("HP while sitting = %d, want 80 (motion 4 recovers)", got)
	}
}

/*
================
TestRecoveryLatchBlocksUntilItsRestore
================
*/
func TestRecoveryLatchBlocksUntilItsRestore(t *testing.T) {
	s := vitalsTestState()
	m := firstDamageTestMonster(t, s, "vitals-latch")
	now := int64(3_000_000)

	if _, ok := s.ApplyDamage("vitals-latch", m.Gid, 50); !ok {
		t.Fatal("hurt rejected")
	}
	s.AdvanceMonsterVitals(now) // record created
	s.SetMonsterVitalLatches("vitals-latch", m.Gid, false, true, now+60000, 0, now)
	if hp, mp := s.MonsterVitalLatches("vitals-latch", m.Gid); hp || !mp {
		t.Fatalf("latches = %v/%v, want HP down, MP up", hp, mp)
	}
	s.AdvanceMonsterVitals(now + 4000)
	if got := vitalsHP(t, s, "vitals-latch", m.Gid); got != 50 {
		t.Fatalf("HP with the latch down = %d, want 50", got)
	}
	s.AdvanceMonsterVitals(now + 60000) // latch returns; the due tick heals with it
	if hp, _ := s.MonsterVitalLatches("vitals-latch", m.Gid); !hp {
		t.Fatal("HP latch did not return at its restore")
	}
	if got := vitalsHP(t, s, "vitals-latch", m.Gid); got != 60 {
		t.Fatalf("HP at the latch's return = %d, want 60", got)
	}
	s.AdvanceMonsterVitals(now + 64000)
	if got := vitalsHP(t, s, "vitals-latch", m.Gid); got != 70 {
		t.Fatalf("HP one period after the return = %d, want 70", got)
	}
}

/*
================
TestDormantMonsterRecoversWithoutWaking
================
*/
func TestDormantMonsterRecoversWithoutWaking(t *testing.T) {
	s := vitalsTestState()
	m := firstDamageTestMonster(t, s, "vitals-cold")
	now := int64(4_000_000)

	if err := s.EnableDormantStorage(); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	pop := s.populationForObject("vitals-cold", m.Gid)
	pop.instances.freeze(m.Gid)
	cold, isCold := pop.instances.cold[m.Gid]
	if !isCold {
		s.mu.Unlock()
		t.Fatal("test monster did not go dormant")
	}
	// A hurt sleeper: its archived HP is below maximum (production
	// archives one once the damage counter has decayed away).
	cold.hp = 50
	pop.instances.cold[m.Gid] = cold
	s.mu.Unlock()

	s.AdvanceMonsterVitals(now)
	s.AdvanceMonsterVitals(now + 4000)

	s.mu.Lock()
	cold, isCold = pop.instances.cold[m.Gid]
	_, isHot := pop.instances.hotSlotOf(m.Gid)
	s.mu.Unlock()
	if !isCold || isHot {
		t.Fatalf("recovery woke the monster: cold=%v hot=%v", isCold, isHot)
	}
	if cold.hp != 60 {
		t.Fatalf("dormant HP = %d, want 60", cold.hp)
	}
}

/*
================
TestMonsterWithoutRecoveryColumnNeverRecovers
================
*/
func TestMonsterWithoutRecoveryColumnNeverRecovers(t *testing.T) {
	s := damageTestState() // MaxHP 54, no recovery column
	m := firstDamageTestMonster(t, s, "vitals-none")
	now := int64(5_000_000)

	if _, ok := s.ApplyDamage("vitals-none", m.Gid, 20); !ok {
		t.Fatal("hurt rejected")
	}
	s.AdvanceMonsterVitals(now)
	s.AdvanceMonsterVitals(now + 4000)
	if got := vitalsHP(t, s, "vitals-none", m.Gid); got != 34 {
		t.Fatalf("HP = %d, want 34 (no authored recovery)", got)
	}
}

/*
================
TestDeadMonsterDropsItsRecoveryRecord

A hurt monster that dies loses its record, even with its HP latch down:
the record map stays bounded by live hurt or latched monsters, and a
reused gid starts from the spawn's both-up latches.
================
*/
func TestDeadMonsterDropsItsRecoveryRecord(t *testing.T) {
	s := vitalsTestState()
	m := firstDamageTestMonster(t, s, "vitals")
	now := int64(1_000_000)

	if _, ok := s.ApplyDamage("vitals", m.Gid, 30); !ok {
		t.Fatal("hurt refused")
	}
	s.AdvanceMonsterVitals(now)
	s.SetMonsterVitalLatches("vitals", m.Gid, false, true, now+60_000, 0, now)
	s.mu.Lock()
	pop := s.populationForObject("vitals", m.Gid)
	held := pop != nil && pop.vitals[m.Gid] != nil
	s.mu.Unlock()
	if !held {
		t.Fatal("the hurt, latched monster has no record")
	}

	if hit, ok := s.ApplyDamage("vitals", m.Gid, 70); !ok || hit.CurrentHP != 0 {
		t.Fatalf("kill = %+v, %v; want HP 0", hit, ok)
	}
	s.AdvanceMonsterVitals(now + 1)
	s.mu.Lock()
	_, left := pop.vitals[m.Gid]
	s.mu.Unlock()
	if left {
		t.Fatal("the dead monster's record is still held")
	}
	if hp, mp := s.MonsterVitalLatches("vitals", m.Gid); !hp || !mp {
		t.Fatalf("latches after death = %v/%v, want both up", hp, mp)
	}
}
