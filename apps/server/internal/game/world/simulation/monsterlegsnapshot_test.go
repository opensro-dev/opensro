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

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/world/monster"
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
		defer ops.Monsters.releaseLegSnapshot(view)
		for i, ref := range batch.actors {
			want := ops.Monsters.behaviorActorMover(batch.key, ref)
			got, ok := view.actorAt(i)
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
		defer ops.Monsters.releaseLegSnapshot(views[0])
		defer ops.Monsters.releaseLegSnapshot(views[1])
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

/*
================
TestLegSnapshotBuildAllocations

The retained slice view builds with no steady-state allocation: the
map it replaces charged every batch a fresh view and a fresh set of
buckets - 3.6 GB a minute of copies on the live shard (#2030, two
heap pulls 60 s apart on 2026-11-03). The window runs warm first;
the state's retained stack is only empty once.
================
*/
func TestLegSnapshotBuildAllocations(t *testing.T) {
	ops, _, now := seededLegWorld(t, 24)
	batches := ops.Monsters.behaviorBatches(*now)
	if len(batches) == 0 {
		t.Fatal("seeded leg produced no behavior batches")
	}
	batch := batches[0]
	build := func() {
		view := ops.Monsters.legSnapshot(batch.key, batch.actors)
		ops.Monsters.releaseLegSnapshot(view)
	}
	build()
	fewest := testing.AllocsPerRun(50, build)
	for range 2 {
		if got := testing.AllocsPerRun(50, build); got < fewest {
			fewest = got
		}
	}
	if fewest > 0.5 {
		t.Fatalf("leg snapshot build allocates %.2f per run in steady state, want at most 0.5", fewest)
	}
}

/*
================
TestLegSnapshotViewReturnsToTheState

A released view is the next build's view, backing included: the
state's retained stack, not the collector's pool, carries it across
batches, so a collection between legs cannot charge the next leg a
fresh multi-megabyte backing. The retained view comes back empty -
nothing of the dispatched batch survives in it.
================
*/
func TestLegSnapshotViewReturnsToTheState(t *testing.T) {
	ops, _, now := seededLegWorld(t, 24)
	batches := ops.Monsters.behaviorBatches(*now)
	if len(batches) == 0 {
		t.Fatal("seeded leg produced no behavior batches")
	}
	batch := batches[0]
	view := ops.Monsters.legSnapshot(batch.key, batch.actors)
	if len(batch.actors) == 0 || len(view.actors) != len(batch.actors) {
		t.Fatalf("view holds %d actors for a batch of %d", len(view.actors), len(batch.actors))
	}
	backing := &view.actors[0]
	ops.Monsters.releaseLegSnapshot(view)
	next := ops.Monsters.legSnapshot(batch.key, batch.actors)
	defer ops.Monsters.releaseLegSnapshot(next)
	if next != view {
		t.Fatal("the next build did not reuse the released view")
	}
	if len(next.actors) == 0 || &next.actors[0] != backing {
		t.Fatal("the reused view lost its backing array")
	}
}

/*
================
TestLegSnapshotReusedSlotOfAGoneActor

The view is built in place (#2982): each actor is expanded straight
into its retained slot, so a slot whose actor has since gone must
read as gone and hold nothing of the actor it held in the last
batch. The test builds, releases, removes the first actor and
rebuilds the same refs into the same backing.
================
*/
func TestLegSnapshotReusedSlotOfAGoneActor(t *testing.T) {
	ops, _, now := seededLegWorld(t, 24)
	batches := ops.Monsters.behaviorBatches(*now)
	if len(batches) == 0 || len(batches[0].actors) < 2 {
		t.Fatal("seeded leg produced no batch of two actors")
	}
	batch := batches[0]
	view := ops.Monsters.legSnapshot(batch.key, batch.actors)
	if first, ok := view.actorAt(0); !ok || first.instance.Gid != batch.actors[0].gid {
		t.Fatalf("first build did not read actor %d", batch.actors[0].gid)
	}
	ops.Monsters.releaseLegSnapshot(view)
	ops.Monsters.mu.Lock()
	ops.Monsters.populationForLease(batch.key.division, batch.key.lease).instances.remove(batch.actors[0].gid)
	ops.Monsters.mu.Unlock()
	next := ops.Monsters.legSnapshot(batch.key, batch.actors)
	defer ops.Monsters.releaseLegSnapshot(next)
	if next != view {
		t.Fatal("the rebuild did not reuse the released view")
	}
	gone, ok := next.actorAt(0)
	if ok || *gone != (behaviorActorSnapshot{}) {
		t.Fatalf("a gone actor's reused slot reads ok=%v gid %d, want the zero snapshot", ok, gone.instance.Gid)
	}
	if kept, ok := next.actorAt(1); !ok || kept.instance.Gid != batch.actors[1].gid {
		t.Fatalf("the surviving actor %d did not read through its slot", batch.actors[1].gid)
	}
}

/*
================
TestLegSnapshotRecycledSlotCarriesNoPriorActor

A build writes its actor into the slot field by field
(residentMonster.fill plus the sparse join in hotFill); nothing
re-zeroes the slot wholesale first, so a fully populated actor
built into a slot must leave nothing behind for the actor built
into the same slot next: the second build's instance must equal
its own storage lookup, field for field. A future Instance field
that fill forgets fails here, not in production.
================
*/
func TestLegSnapshotRecycledSlotCarriesNoPriorActor(t *testing.T) {
	ops, _, now := seededLegWorld(t, 24)
	batches := ops.Monsters.behaviorBatches(*now)
	if len(batches) == 0 || len(batches[0].actors) < 2 {
		t.Fatal("seeded leg produced no batch of two actors")
	}
	batch := batches[0]
	first, second := batch.actors[0], batch.actors[1]

	// Fully populate the first actor: every sparse family set.
	ops.Monsters.mu.Lock()
	state := ops.Monsters.populationForLease(batch.key.division, batch.key.lease)
	row, ok := state.instances.lookup(first.gid)
	if ok {
		row.LinkedEffects = &monster.EffectSnapshot{}
		row.Abnormal = &abnormal.Block{}
		row.SelfEffects[0] = monster.SelfEffect{SkillID: 1, Token: 1, UntilMs: 1 << 40}
		row.TargetEffects[0] = monster.SelfEffect{SkillID: 2, Token: 2, UntilMs: 1 << 40}
		state.instances.set(first.gid, row)
	}
	ops.Monsters.mu.Unlock()
	if !ok {
		t.Fatalf("actor %d not resident", first.gid)
	}

	view := ops.Monsters.legSnapshot(batch.key, []behaviorActorRef{first})
	full, found := view.actorAt(0)
	if !found || full.instance.LinkedEffects == nil || full.instance.Abnormal == nil ||
		full.instance.SelfEffects[0].Token != 1 || full.instance.TargetEffects[0].Token != 2 {
		t.Fatalf("first build did not carry every sparse family: linked %v abnormal %v self %+v target %+v",
			full.instance.LinkedEffects != nil, full.instance.Abnormal != nil,
			full.instance.SelfEffects[0], full.instance.TargetEffects[0])
	}
	ops.Monsters.releaseLegSnapshot(view)

	next := ops.Monsters.legSnapshot(batch.key, []behaviorActorRef{second})
	defer ops.Monsters.releaseLegSnapshot(next)
	if next != view {
		t.Fatal("the rebuild did not reuse the released view")
	}
	clean, found := next.actorAt(0)
	if !found || clean.instance.Gid != second.gid {
		t.Fatalf("second build did not read actor %d through the recycled slot", second.gid)
	}
	ops.Monsters.mu.Lock()
	want, wantOk := state.instances.lookup(second.gid)
	wantMover, wantMoverOk := state.movers.lookup(second.gid)
	ops.Monsters.mu.Unlock()
	if !wantOk || !wantMoverOk {
		t.Fatalf("actor %d not resident", second.gid)
	}
	if !reflect.DeepEqual(clean.instance, want) {
		t.Fatalf("recycled slot carried the prior actor over in Instance.%s", instanceDifference(clean.instance, want))
	}
	if !reflect.DeepEqual(clean.mover, wantMover) {
		t.Fatalf("recycled slot carried the prior mover over:\n got %+v\nwant %+v", clean.mover, wantMover)
	}
}

/*
================
instanceDifference

The first Instance field two values disagree in, for the recycled
slot test's failure line (the whole value is 2 KB; the field name
is the actionable part).
================
*/
func instanceDifference(got, want monster.Instance) string {
	gotValue, wantValue := reflect.ValueOf(got), reflect.ValueOf(want)
	fields := gotValue.Type()
	for i := 0; i < fields.NumField(); i++ {
		if !reflect.DeepEqual(gotValue.Field(i).Interface(), wantValue.Field(i).Interface()) {
			return fields.Field(i).Name
		}
	}
	return ""
}
