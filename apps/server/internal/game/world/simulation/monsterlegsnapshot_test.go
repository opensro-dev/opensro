/*
===========================================================================

monsterlegsnapshot_test.go - pins for the leg-start snapshot (#2203)

The snapshot must read exactly what the old in-lock dispatch read,
two builders over the same state must agree, and a commit planned
from a snapshot must drop when a slower lane's mover set has
advanced the row's generation in between - the overwrite the #2047
review removed by timing is now removed by the generation guard.
===========================================================================
*/

package simulation

import (
	"reflect"
	"sync"
	"testing"
)

/*
================
TestLegSnapshotMatchesDispatchRead

Every actor in the view equals the dispatch read
(behaviorActorMover) of the same moment: instance, mover and the
commit generation carried with them.
================
*/
func TestLegSnapshotMatchesDispatchRead(t *testing.T) {
	ops, _, now := seededLegWorld(t, 24)
	batches := ops.Monsters.behaviorBatches(*now)
	if len(batches) == 0 {
		t.Fatal("seeded leg produced no behavior batches")
	}
	for _, batch := range batches {
		view := ops.Monsters.legSnapshot(batch.key, batch.actors)
		for _, ref := range batch.actors {
			want := ops.Monsters.behaviorActorMover(batch.key, ref)
			got, ok := view.actor(ref)
			if ok != want.ok {
				t.Fatalf("actor %d: snapshot ok=%v, dispatch read ok=%v", ref.gid, ok, want.ok)
			}
			if !ok {
				continue
			}
			if got.instance != want.instance || got.mover != want.mover || got.moverGen != want.moverGen {
				t.Fatalf("actor %d: snapshot %+v / %+v gen %d, dispatch read %+v / %+v gen %d",
					ref.gid, got.instance, got.mover, got.moverGen, want.instance, want.mover, want.moverGen)
			}
		}
	}
}

/*
================
TestLegSnapshotConcurrentBuildersAgree

Two builders over the same state produce equal views. The race
detector (CI) owns the data-race half of this proof; the equality
half runs everywhere.
================
*/
func TestLegSnapshotConcurrentBuildersAgree(t *testing.T) {
	ops, _, now := seededLegWorld(t, 24)
	batches := ops.Monsters.behaviorBatches(*now)
	if len(batches) == 0 {
		t.Fatal("seeded leg produced no behavior batches")
	}
	for _, batch := range batches {
		views := make([]*legSnapshot, 2)
		var wg sync.WaitGroup
		for i := range views {
			wg.Add(1)
			go func() {
				defer wg.Done()
				views[i] = ops.Monsters.legSnapshot(batch.key, batch.actors)
			}()
		}
		wg.Wait()
		if !reflect.DeepEqual(views[0].actors, views[1].actors) {
			t.Fatalf("concurrent snapshot builds disagree over %d actors", len(batch.actors))
		}
	}
}

/*
================
TestLegSnapshotStaleCommitDrops

A slower lane's mover set between the snapshot read and the step's
commit advances the row's generation: the pending commit planned
from the snapshot drops instead of overwriting the newer value,
and a commit re-based on the current generation lands.
================
*/
func TestLegSnapshotStaleCommitDrops(t *testing.T) {
	ops, _, _ := seededLegWorld(t, 8)
	registry := ops.Monsters
	instances := registry.InstancesInRegions(monsterTestDivision, []uint16{monsterTestRegion})
	if len(instances) == 0 {
		t.Fatal("seeded leg produced no monsters")
	}
	gid := instances[0].Gid

	_, baseGen, ok := registry.MoverGeneration(monsterTestDivision, gid)
	if !ok {
		t.Fatalf("seeded monster %d has no mover", gid)
	}

	// The slower lane sets the mover after the snapshot was read.
	slower, _, _ := registry.MoverGeneration(monsterTestDivision, gid)
	slower.Pose.Heading = 12345
	if !registry.CommitMover(monsterTestDivision, gid, slower) {
		t.Fatal("slower-lane commit refused")
	}
	_, afterGen, _ := registry.MoverGeneration(monsterTestDivision, gid)
	if afterGen == baseGen {
		t.Fatal("slower-lane set did not advance the commit generation")
	}

	// The pending commit still bases on the snapshot's generation.
	planned := slower
	planned.Pose.Heading = 23456
	if _, ok := registry.CommitMoverGen(monsterTestDivision, gid, planned, baseGen); ok {
		t.Fatal("stale commit landed over the slower lane's set")
	}
	if frames := registry.CommitMoverFramesGen(monsterTestDivision, gid, planned, baseGen, []Frame{{Opcode: 1}}); frames != nil {
		t.Fatal("stale commit released its frames")
	}
	stored, _, _ := registry.MoverGeneration(monsterTestDivision, gid)
	if stored.Pose.Heading != 12345 {
		t.Fatalf("stored heading = %d, want the slower lane's 12345", stored.Pose.Heading)
	}

	// Re-based on the current generation, the commit lands.
	gen, ok := registry.CommitMoverGen(monsterTestDivision, gid, planned, afterGen)
	if !ok {
		t.Fatal("re-based commit refused")
	}
	if gen == afterGen {
		t.Fatal("accepted commit did not advance the generation")
	}
	stored, _, _ = registry.MoverGeneration(monsterTestDivision, gid)
	if stored.Pose.Heading != 23456 {
		t.Fatalf("stored heading = %d, want the re-based commit's 23456", stored.Pose.Heading)
	}
}
