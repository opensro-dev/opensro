/*
===========================================================================

monster_storage_sparse_test.go - read equivalence for the sparse mask and
the fused population search

The sparse mask lets a read skip transient maps that cannot hold an actor,
and locateObject keeps the row its population search probed. Both are pure
speedups: these tests replay random writes against a plain reference model
and require every read to return what the unmasked join and the two-step
search return.

===========================================================================
*/

package simulation

import (
	"math/rand"
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

const (
	sparseTestGIDs  = 64
	sparseTestSteps = 20000
)

/*
================
sparseTestActor

A random actor carrying any subset of the four transient projections.
================
*/
func sparseTestActor(gid uint32, r *rand.Rand) monster.Instance {
	row := monster.Instance{Gid: gid, CurrentHP: uint32(r.Intn(3)), Ref: monster.MonsterRef{MaxHP: 100, RefObjID: uint32(r.Intn(4))}}
	mask := r.Intn(16)
	if mask&sparseLinked != 0 {
		row.LinkedEffects = &monster.EffectSnapshot{}
	}
	if mask&sparseAbnormal != 0 {
		row.Abnormal = &abnormal.Block{}
	}
	if mask&sparseSelf != 0 {
		row.SelfEffects[r.Intn(len(row.SelfEffects))].Token = uint32(r.Intn(1000) + 1)
	}
	if mask&sparseTarget != 0 {
		row.TargetEffects[r.Intn(len(row.TargetEffects))].Token = uint32(r.Intn(1000) + 1)
	}
	return row
}

/*
================
unmaskedLookup

The join before the mask: probe every sparse map for a resident actor.
================
*/
func unmaskedLookup(s *monsterStorage, gid uint32) (monster.Instance, bool) {
	if r, ok := s.hotRow(gid); ok {
		row := r.value(gid)
		row.LinkedEffects = s.linkedEffects[gid]
		row.Abnormal = s.abnormal[gid]
		row.SelfEffects = s.selfEffects[gid]
		row.TargetEffects = s.targetEffects[gid]
		return row, true
	}
	if r, ok := s.cold[gid]; ok {
		return s.archive.get(gid, r), true
	}
	return monster.Instance{}, false
}

/*
================
TestSparseMaskMatchesUnmaskedJoin

Random set/remove/freeze/wake traffic; after every step each gid reads the
same through the masked lookup, the unmasked join and the reference model.
================
*/
func TestSparseMaskMatchesUnmaskedJoin(t *testing.T) {
	r := rand.New(rand.NewSource(1820))
	s := newMonsterStorage(nil)
	s.archive = &monsterArchive{}
	model := map[uint32]monster.Instance{}
	for step := 0; step < sparseTestSteps; step++ {
		gid := uint32(r.Intn(sparseTestGIDs) + 1)
		switch r.Intn(4) {
		case 0, 1:
			row := sparseTestActor(gid, r)
			s.set(gid, row)
			model[gid] = row
		case 2:
			s.remove(gid)
			delete(model, gid)
		case 3:
			if r.Intn(2) == 0 {
				s.freeze(gid)
			} else {
				s.wake(gid)
			}
		}
		for g := uint32(1); g <= sparseTestGIDs; g++ {
			got, ok := s.lookup(g)
			want, wantOK := unmaskedLookup(&s, g)
			ref, refOK := model[g]
			if ok != wantOK || got != want || ok != refOK || got != ref {
				t.Fatalf("step %d gid %d: masked (%v,%+v) unmasked (%v,%+v) model (%v,%+v)", step, g, ok, got, wantOK, want, refOK, ref)
			}
		}
	}
}

/*
================
TestLocateObjectMatchesTwoStepSearch

Get, GetInWorld and locateObject answer exactly what populationForObject
followed by lookup answers, across the field population, world populations,
another division and unknown gids.
================
*/
func TestLocateObjectMatchesTwoStepSearch(t *testing.T) {
	s := lookupBenchmarkState()
	other := &divisionMonsterState{lease: instance.Lease{ID: 9, Generation: 1}, instances: newMonsterStorage(nil)}
	other.instances.set(7, lookupBenchmarkActor(7))
	s.worldPopulations[populationKey{"other", other.lease}] = other
	gids := []uint32{0, 1, 2, 3, 10, 21, benchFieldActors, benchFieldActors + 1, 7}
	for w := 0; w < benchWorldCount; w++ {
		base := uint32(benchWorldGIDOffset + w*benchWorldActors)
		gids = append(gids, base, base+benchWorldActors-1)
	}
	for _, division := range []string{benchDivision, "other", "missing"} {
		for _, gid := range gids {
			state := s.populationForObject(division, gid)
			want, wantOK := state.instances.lookup(gid)
			found, slot := s.locateObject(division, gid)
			ok := found != nil
			var got monster.Instance
			if ok {
				got = found.instances.slotValue(gid, &slot)
			}
			if ok != wantOK || got != want || (ok && found != state) {
				t.Fatalf("%s/%d: locateObject found %v same population %v, two-step found %v", division, gid, ok, found == state, wantOK)
			}
			if pub, pubOK := s.Get(division, gid); pubOK != wantOK || pub != finishSummonAction(want, s.nowMillis()) {
				t.Fatalf("%s/%d: Get diverged", division, gid)
			}
			for _, world := range []instance.ID{0, 2, 3, 9} {
				pub, pubOK := s.GetInWorld(division, world, gid)
				inWorld := wantOK && state.lease.ID == world
				if pubOK != inWorld || (inWorld && pub != want) || (!inWorld && pub != (monster.Instance{})) {
					t.Fatalf("%s/%d world %d: GetInWorld (%v) want (%v)", division, gid, world, pubOK, inWorld)
				}
			}
		}
	}
}

/*
================
TestPopulationForObjectHintNeverStale

The popHint populationForObject remembers (#2029) must only ever repeat
the scan's own answer: repeated reads agree, an actor moved to another
world population resolves there (not to the hinted population), and a
retired actor stops resolving to it.
================
*/
func TestPopulationForObjectHintNeverStale(t *testing.T) {
	s := lookupBenchmarkState()
	gid := uint32(benchWorldGIDOffset + (benchWorldCount-1)*benchWorldActors)
	last := s.populationForObject(benchDivision, gid)
	if last == nil || last.lease.ID != instance.ID(benchWorldCount+1) {
		t.Fatalf("actor resolved to lease %v, want the last world population", last)
	}
	for i := 0; i < 3; i++ {
		if got := s.populationForObject(benchDivision, gid); got != last {
			t.Fatal("repeated read changed its answer")
		}
	}
	first := s.worldPopulations[populationKey{benchDivision, instance.Lease{ID: 2, Generation: 1}}]
	actor, _ := last.instances.lookup(gid)
	last.instances.remove(gid)
	first.instances.set(gid, actor)
	if got := s.populationForObject(benchDivision, gid); got != first {
		t.Fatal("moved actor still resolves to the hinted population")
	}
	first.instances.remove(gid)
	if got := s.populationForObject(benchDivision, gid); got == first || got == last {
		t.Fatal("retired actor still resolves to a world population")
	}
	// A population struck from the registry while its storage still
	// holds the actor (ReleasePopulation's shape) must not resolve
	// either: the scan could never return it again.
	last.instances.set(gid, actor)
	if got := s.populationForObject(benchDivision, gid); got != last {
		t.Fatal("actor did not resolve back to the last world population")
	}
	delete(s.worldPopulations, populationKey{benchDivision, last.lease})
	if got := s.populationForObject(benchDivision, gid); got == last {
		t.Fatal("unregistered population still resolves through the hint")
	}
}

/*
================
TestGetInWorldFinishesExpiredSummonActionOnce

The cheap expiry test keeps the write-back: an expired action is cleared in
storage, a live one is left alone, and the clock is read once per found actor.
================
*/
func TestGetInWorldFinishesExpiredSummonActionOnce(t *testing.T) {
	s := lookupBenchmarkState()
	reads := 0
	nowMs := int64(5000)
	s.clock = func() time.Time { reads++; return time.UnixMilli(nowMs) }
	field := s.divs[benchDivision]
	expired := lookupBenchmarkActor(1)
	expired.SummonActionUntilMs = 4000
	field.instances.set(1, expired)
	live := lookupBenchmarkActor(2)
	live.SummonActionUntilMs = 6000
	field.instances.set(2, live)

	got, ok := s.GetInWorld(benchDivision, 0, 1)
	if !ok || got.SummonActionUntilMs != 0 || field.instances.get(1).SummonActionUntilMs != 0 || reads != 1 {
		t.Fatalf("expired action: ok %v until %d stored %d reads %d", ok, got.SummonActionUntilMs, field.instances.get(1).SummonActionUntilMs, reads)
	}
	got, ok = s.GetInPopulation(benchDivision, field.lease, 2)
	if !ok || got != live || field.instances.get(2) != live || reads != 2 {
		t.Fatalf("live action changed: ok %v reads %d", ok, reads)
	}
	if _, ok := s.GetInWorld(benchDivision, 2, 2); ok || reads != 2 {
		t.Fatalf("foreign world read the clock or answered: ok %v reads %d", ok, reads)
	}
}
