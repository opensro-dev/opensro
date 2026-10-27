/*
===========================================================================

monster_currenthp_test.go - the HP-only read agrees with the full snapshot

CurrentHP stands in for Get on the per-tick critical-history sweep (#1820).
These tests pin that the two report the same HP and existence for every
residency tier and for missing, damaged and dead actors.

===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

const currentHPDivision = "division"

/*
================
TestMonsterStorageCurrentHPMatchesLookup
================
*/
func TestMonsterStorageCurrentHPMatchesLookup(t *testing.T) {
	owner := NewMonsterState(monster.Template{})
	if err := owner.EnableDormantStorage(); err != nil {
		t.Fatal(err)
	}
	s := newMonsterStorage(nil)
	s.archive = owner.archive
	ref := monster.MonsterRef{RefObjID: 123, MaxHP: 99, BodyRadius: 8}
	hot := monster.Instance{Gid: 1, Ref: ref, Nest: lifecycleNest(150), CurrentHP: 40, SummonActionUntilMs: 5}
	dead := monster.Instance{Gid: 2, Ref: ref, Nest: lifecycleNest(151), CurrentHP: 0}
	cold := monster.Instance{Gid: 3, Ref: ref, Nest: lifecycleNest(152), CurrentHP: 77}
	coldDead := monster.Instance{Gid: 4, Ref: ref, Nest: lifecycleNest(153), CurrentHP: 0}
	for _, row := range []monster.Instance{hot, dead, cold, coldDead} {
		s.set(row.Gid, row)
	}
	s.freeze(cold.Gid)
	s.freeze(coldDead.Gid)
	if _, ok := s.cold[cold.Gid]; !ok {
		t.Fatal("fixture did not archive the cold actor")
	}
	if _, ok := s.cold[coldDead.Gid]; !ok {
		t.Fatal("fixture did not archive the dead cold actor")
	}
	for _, gid := range []uint32{1, 2, 3, 4, 5} {
		want, wantOK := s.lookup(gid)
		hp, ok := s.currentHP(gid)
		if ok != wantOK || hp != want.CurrentHP {
			t.Fatalf("gid %d: currentHP = %d,%v; lookup = %d,%v", gid, hp, ok, want.CurrentHP, wantOK)
		}
	}
	if len(s.cold) != 2 {
		t.Fatal("HP read woke a sleeping actor")
	}
}

/*
================
TestMonsterStateCurrentHPMatchesGet
================
*/
func TestMonsterStateCurrentHPMatchesGet(t *testing.T) {
	ref := lifecycleRef(0)
	w := newLifecycleWorld(t, ref, lifecycleNest(100), lifecycleNest(120))
	w.s.StartDivision(currentHPDivision)
	w.s.AdvancePopulation(w.s.CurrentTimeMillis())
	instances := w.s.InstancesInRegions(currentHPDivision, []uint16{lifecycleRegion})
	if len(instances) != 2 {
		t.Fatalf("fixture materialized %d monsters, want 2", len(instances))
	}
	w.s.ApplyDamage(currentHPDivision, instances[0].Gid, 10)
	w.s.ApplyDamage(currentHPDivision, instances[1].Gid, instances[1].CurrentHP)
	if hp, ok := w.s.CurrentHP(currentHPDivision, instances[1].Gid); !ok || hp != 0 {
		t.Fatalf("dead actor: CurrentHP = %d,%v; want 0,true", hp, ok)
	}
	probes := []struct {
		division string
		gid      uint32
	}{
		{currentHPDivision, instances[0].Gid},
		{currentHPDivision, instances[1].Gid},
		{currentHPDivision, instances[1].Gid + 1000},
		{"other-division", instances[0].Gid},
	}
	for _, probe := range probes {
		want, wantOK := w.s.Get(probe.division, probe.gid)
		hp, ok := w.s.CurrentHP(probe.division, probe.gid)
		if ok != wantOK || hp != want.CurrentHP {
			t.Fatalf("%s/%d: CurrentHP = %d,%v; Get = %d,%v", probe.division, probe.gid, hp, ok, want.CurrentHP, wantOK)
		}
	}
}
