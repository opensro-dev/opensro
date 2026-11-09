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
TestHotFillClearsSparseFieldsOnAReusedSlot

The leg snapshot fills straight into retained view slots (#2150), so
the destination of a fill cannot be trusted to arrive zero: a slot
that held an actor with every sparse projection, refilled after those
projections are gone, must not keep the old ones standing. This pins
hotFill's clearing assignments; the masked-join test above reads onto
a fresh value each time and cannot see a stale field survive.
================
*/
func TestHotFillClearsSparseFieldsOnAReusedSlot(t *testing.T) {
	s := newMonsterStorage(nil)
	withSparse := monster.Instance{Gid: 7, CurrentHP: 3, Ref: monster.MonsterRef{MaxHP: 100, RefObjID: 1},
		LinkedEffects: &monster.EffectSnapshot{}, Abnormal: &abnormal.Block{}}
	withSparse.SelfEffects[0].Token = 5
	withSparse.TargetEffects[0].Token = 6
	s.set(7, withSparse)
	r, ok := s.hotRow(7)
	if !ok {
		t.Fatal("the sparse actor is not hot")
	}
	var dst monster.Instance
	s.hotFill(&dst, 7, r)
	if dst.LinkedEffects == nil || dst.Abnormal == nil || dst.SelfEffects[0].Token != 5 || dst.TargetEffects[0].Token != 6 {
		t.Fatalf("the first fill did not carry the sparse fields: %+v", dst)
	}
	s.set(7, monster.Instance{Gid: 7, CurrentHP: 2, Ref: monster.MonsterRef{MaxHP: 100, RefObjID: 1}})
	r, ok = s.hotRow(7)
	if !ok {
		t.Fatal("the cleared actor is not hot")
	}
	s.hotFill(&dst, 7, r)
	if dst.LinkedEffects != nil || dst.Abnormal != nil ||
		dst.SelfEffects != (monster.SelfEffects{}) || dst.TargetEffects != (monster.TargetEffects{}) {
		t.Fatalf("the reused slot kept the previous sparse fields: %+v", dst)
	}
	if dst.CurrentHP != 2 {
		t.Fatalf("the reused slot kept the previous scalar state: %+v", dst)
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
TestPopulationForObjectAtAnswersAsPopulationForObject

populationForObjectAt answers exactly what populationForObject
answers - the same population pointer, popHint side effects
included - under the real slot, the no-slot sentinel, an
out-of-range slot and a slot naming another gid (#2133).
================
*/
func TestPopulationForObjectAtAnswersAsPopulationForObject(t *testing.T) {
	s := lookupBenchmarkState()
	same := func(a, b *divisionMonsterState) bool {
		return a == b ||
			a.instances.len() == 0 && b.instances.len() == 0 && a.lease == b.lease
	}
	gids := []uint32{1, 2, benchWorldGIDOffset, benchWorldGIDOffset + benchWorldActors - 1, 999999}
	for _, division := range []string{benchDivision, "missing"} {
		for _, gid := range gids {
			want := s.populationForObject(division, gid)
			realSlot := behaviorNoMonsterSlot
			if slot, hot := want.instances.hotSlotOf(gid); hot {
				realSlot = slot
			}
			otherSlot := behaviorNoMonsterSlot
			if slot, hot := want.instances.hotSlotOf(1); hot && gid != 1 {
				otherSlot = slot
			}
			hints := []uint32{realSlot, behaviorNoMonsterSlot, 1 << 20, otherSlot}
			for _, hint := range hints {
				if got := s.populationForObjectAt(division, gid, hint); !same(got, want) {
					t.Fatalf("%s/%d hint %d: populationForObjectAt answered a different population", division, gid, hint)
				}
				// The hint's side effects must not move the plain door.
				if got := s.populationForObject(division, gid); !same(got, want) {
					t.Fatalf("%s/%d hint %d: plain door moved after the carried read", division, gid, hint)
				}
			}
		}
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

/*
================
TestHotRowPtrReadsInPlace

The scheduler's pointer read answers the row where it sits: the
fields are the stored row's, a re-set is visible to the next read,
a slot that no longer names the gid misses, and removeHot's swap
leaves the moved row readable at its new slot while the removed
gid misses (#3027).
================
*/
func TestHotRowPtrReadsInPlace(t *testing.T) {
	s := newMonsterStorage(nil)
	s.set(1, monster.Instance{Gid: 1, CurrentHP: 30, Ref: monster.MonsterRef{MaxHP: 100, RefObjID: 1}})
	s.set(3, monster.Instance{Gid: 3, CurrentHP: 7, Ref: monster.MonsterRef{MaxHP: 300, RefObjID: 3}})

	row, ok := s.hotRowPtrAt(0, 1)
	if !ok || row == nil || row.currentHP != 30 {
		t.Fatalf("slot read = %+v (ok %v)", row, ok)
	}
	if _, ok := s.hotRowPtrAt(0, 3); ok {
		t.Fatal("slot read answered for the wrong gid")
	}
	s.set(1, monster.Instance{Gid: 1, CurrentHP: 31, Ref: monster.MonsterRef{MaxHP: 100, RefObjID: 1}})
	row, ok = s.hotRowPtr(1)
	if !ok || row.currentHP != 31 {
		t.Fatalf("re-set read = %+v (ok %v)", row, ok)
	}
	s.remove(1)
	row, ok = s.hotRowPtr(3)
	if !ok || row == nil || row.currentHP != 7 {
		t.Fatalf("swapped read = %+v (ok %v)", row, ok)
	}
	if _, ok := s.hotRowPtr(1); ok {
		t.Fatal("removed gid still reads hot")
	}
}
