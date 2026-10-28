/*
===========================================================================

monsterleg_golden_test.go - the seeded monster leg: golden output and bench

A deterministic population (seeded nest grid, seeded Rand, fixed clock)
driven through RunMonsterLeg. The golden test pins every pushed frame and
the final mover values, so a movement-performance change must prove its
output is bit-identical; the benchmark measures the same leg for the
before/after numbers the profile asked for (#1943).

===========================================================================
*/

package simulation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

// seededLegBaseMs is the fixed clock the seeded leg starts at.
const seededLegBaseMs = int64(1_000_000)

/*
================
seededLegWorld

seededLegWorld builds count mobile monsters on a nest grid around the
fixture viewer, each already past its spawn hold with an activity cadence
set, and returns the ops, the viewer sessions and the shared leg clock.
The Rand source is seeded, so the whole run is reproducible.
================
*/
func seededLegWorld(tb testing.TB, count int) (*MonsterMoverOps, []SessionSnapshot, *int64) {
	tb.Helper()
	clock := seededLegBaseMs
	now := &clock
	nests := make([]monster.NestRow, 0, count)
	cols := 40
	rows := (count + cols - 1) / cols
	for i := 0; i < count; i++ {
		x := 1000 + float64(i%cols-cols/2)*35
		z := 1000 + float64(i/cols-rows/2)*35
		nests = append(nests, monster.NestRow{SpawnPoint: monster.SpawnPoint{
			RefObjID: 1933, RegionID: monsterTestRegion, X: x, Y: 20, Z: z,
		}})
	}
	registry := NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{
			1933: {RefObjID: 1933, TidWord: 0x00C6, Codename: "MOB_CH_MANGNYANG", WalkSpeed: 8, RunSpeed: 22, ScaleDenom: 100, BodyRadius: 6, MaxHP: 54},
		}, nests))
	registry.SetTimeSource(func() time.Time { return time.UnixMilli(*now) })
	registry.StartDivision(monsterTestDivision)
	registry.AdvancePopulation(*now)
	instances := registry.InstancesInRegions(monsterTestDivision, []uint16{monsterTestRegion})
	if len(instances) != count {
		tb.Fatalf("seeded %d monsters, want %d", len(instances), count)
	}
	// Seed in gid order: commit order is behaviour-queue insertion order,
	// and the leg must be reproducible run over run.
	sort.Slice(instances, func(i, j int) bool { return instances[i].Gid < instances[j].Gid })
	gids := make([]uint32, 0, len(instances))
	for _, instance := range instances {
		mover, ok := registry.Mover(monsterTestDivision, instance.Gid)
		if !ok {
			tb.Fatalf("seeded monster %d has no mover", instance.Gid)
		}
		if err := mover.Transition(monster.MoverEventSpawnHoldElapsed, 0); err != nil {
			tb.Fatalf("seed idle mover %d: %v", instance.Gid, err)
		}
		mover.BehaviorDeadlineMs = 0
		mover.Pose.Heading = 49151 // fixed facing; population spawn headings are randomized
		mover.Activity = monster.ActivityCadence{Interval: uint16(1000 + instance.Gid%997), LastCheck: uint32(*now)}
		registry.CommitMover(monsterTestDivision, instance.Gid, mover)
		gids = append(gids, instance.Gid)
	}
	// The viewer reached the tick after its bootstrap object list, as in
	// production: record that list so the first leg seeds its shown set.
	registry.RecordObjectList(monsterTestDivision, PlayerObjectID(1), gids)
	// The session carries its population's own lease, as a live session
	// does; interest and activity resolve through it.
	lease, ok := registry.ObjectPopulation(monsterTestDivision, gids[0])
	if !ok {
		tb.Fatal("seeded population has no lease")
	}
	viewer := playerSessionAt(1, 1000, 1000)
	viewer.Population = lease
	viewer.WorldInstance = uint32(lease.ID)
	rng := rand.New(rand.NewSource(1943))
	ops := &MonsterMoverOps{
		Monsters:   registry,
		TacticsFor: fixedTactics(aggressiveTactics()),
		Rand:       rng.Float64,
	}
	ops.AttackPlan = func(monster.Instance, uint32, AttackPick) (MonsterAttackPlan, bool) {
		return MonsterAttackPlan{SkillID: 0x1234, Reach: ActionReach(6), CooldownMs: 1000, ActionLifecycleMs: 600}, true
	}
	return ops, []SessionSnapshot{viewer}, now
}

/*
================
runSeededLeg

runSeededLeg advances the seeded leg ticks times at 100 ms per tick and
writes every pushed frame, in push order, to out.
================
*/
func runSeededLeg(ops *MonsterMoverOps, sessions []SessionSnapshot, now *int64, ticks int, pusher *fakePusher, record func(string)) {
	for tick := 0; tick < ticks; tick++ {
		*now = seededLegBaseMs + int64(tick)*100
		ops.RunMonsterLeg(*now, sessions, pusher)
		for _, push := range pusher.toSession {
			for _, frame := range push.frames {
				record(fmt.Sprintf("S %s %04x %s %v\n", push.sessionID, frame.Opcode, hex.EncodeToString(frame.Payload), frame.Scope))
			}
		}
		for _, push := range pusher.toDivision {
			for _, frame := range push.frames {
				record(fmt.Sprintf("D %s %04x %s %v\n", push.divisionID, frame.Opcode, hex.EncodeToString(frame.Payload), frame.Scope))
			}
		}
		pusher.toSession = pusher.toSession[:0]
		pusher.toDivision = pusher.toDivision[:0]
	}
}

/*
================
TestSeededMonsterLegGolden

TestSeededMonsterLegGolden runs the seeded leg and compares the sha256
of its whole observable output - every pushed frame plus the final mover
values - against the golden digest. Any behaviour change in the leg,
intended or not, moves this hash; regenerate it only with a behaviour
change that has its own evidence.
================
*/
func TestSeededMonsterLegGolden(t *testing.T) {
	const golden = "1abc853ba2724789362820d66888aead6d52e7f6ac63b0ca66a95e1b134a7b8f"
	ops, sessions, now := seededLegWorld(t, 240)
	hash := sha256.New()
	frameLines := 0
	record := func(line string) {
		frameLines++
		hash.Write([]byte(line))
	}
	runSeededLeg(ops, sessions, now, 300, &fakePusher{}, record)
	if frameLines == 0 {
		t.Fatal("seeded leg published no frames; the golden would pin nothing")
	}
	instances := ops.Monsters.InstancesInRegions(monsterTestDivision, []uint16{monsterTestRegion})
	gids := make([]uint32, 0, len(instances))
	for _, instance := range instances {
		gids = append(gids, instance.Gid)
	}
	sort.Slice(gids, func(i, j int) bool { return gids[i] < gids[j] })
	for _, gid := range gids {
		mover, ok := ops.Monsters.Mover(monsterTestDivision, gid)
		if !ok {
			t.Fatalf("final mover %d missing", gid)
		}
		record(fmt.Sprintf("M %d %d %+v %+v %+v %d %d %d %d %d\n", gid, mover.Mode(), mover.Pose, mover.From, mover.To,
			mover.DepartMs, mover.ArriveMs, mover.BehaviorDeadlineMs, mover.NextAttackMs, mover.Activity))
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if got != golden {
		t.Fatalf("seeded leg output hash = %s, want golden %s", got, golden)
	}
}

/*
================
TestActivityGateResolvesSettledBlockOnce

A settled actor's activity block is a pure function of its pose: the
gate must resolve it once, not once per cadence (on the cave plane each
resolution walks the surface's cells). A moved actor resolves fresh.
================
*/
func TestActivityGateResolvesSettledBlockOnce(t *testing.T) {
	ops, actor := monsterLegFixture(t, passiveTactics())
	const now = int64(10000)
	mover, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	mustMoverTransition(&mover, monster.MoverEventStartWander, 0)
	mover.Activity = monster.ActivityCadence{Interval: 1000, LastCheck: uint32(now)}
	ops.Monsters.CommitMover(monsterTestDivision, actor.Gid, mover)
	calls := 0
	ops.MessageBlockAt = func(p Spawn) (worldgeom.MessageBlock, bool) {
		calls++
		return worldgeom.OutdoorMessageBlock(worldgeom.RegionXZ{RegionID: p.RegionID, X: p.X, Z: p.Z})
	}
	// The viewer stands on the actor's nest, so every due gate finds it
	// active and the actor stays settled in place between gates. The leg
	// normally stamps the batch's world into the snapshot; do it here.
	ops.activity = ops.captureActivity([]SessionSnapshot{playerSessionAt(1, 1000, 1000)}, now)
	ops.activity.world = activityWorld{division: monsterTestDivision, world: 65537, generation: 1}
	settled := calls
	for k := int64(1); k <= 5; k++ {
		current, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
		frames, _, handled := ops.runActivityGate(monsterTestDivision, actor, current, now+k*1000)
		if handled || len(frames) != 0 {
			t.Fatalf("gate %d: active settled actor was suspended or moved", k)
		}
	}
	if calls != settled+1 {
		t.Fatalf("settled block resolved %d times over 5 due gates, want 1", calls-settled)
	}
	moved, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	moved.Pose.X += 50
	ops.Monsters.CommitMover(monsterTestDivision, actor.Gid, moved)
	if _, _, handled := ops.runActivityGate(monsterTestDivision, actor, moved, now+6000); handled {
		t.Fatal("moved actor was suspended beside its viewer")
	}
	if calls != settled+2 {
		t.Fatalf("moved block resolved %d times total, want 2", calls-settled)
	}
}

/*
================
TestMonsterLegActivityGateResolvesSettledBlockOnce

The production leg reaches the gate through the dispatch copy: with
RunAction installed (always, in production), each actor's gate runs on
a value copy of the ops, so a memo map made inside the gate would land
on the discarded copy and every due gate would resolve its block
again. Driven through RunMonsterLeg itself, a settled actor's block
must still resolve at most once across due legs.
================
*/
func TestMonsterLegActivityGateResolvesSettledBlockOnce(t *testing.T) {
	ops, actor, now := activityFixture(t)
	calls := 0
	ops.MessageBlockAt = func(p Spawn) (worldgeom.MessageBlock, bool) {
		calls++
		return worldgeom.OutdoorMessageBlock(worldgeom.RegionXZ{RegionID: p.RegionID, X: p.X, Z: p.Z})
	}
	// The production dispatch shape: the passthrough stands in for the
	// wired action lane, so the gate runs on the leg's per-actor copy.
	ops.RunAction = func(_ string, run func(MonsterAttackOperation)) { run(nil) }
	pusher := &fakePusher{}
	// Suspend the actor unobserved, then let its pending pose settle at
	// the segment's end; from there it stands still.
	ops.RunMonsterLeg(now+1001, nil, pusher)
	mover, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if mover.Mode() != monster.MoverPending {
		t.Fatalf("actor did not suspend unobserved: %v", mover.Mode())
	}
	settleAt, goal := mover.ArriveMs, mover.To
	ops.RunMonsterLeg(settleAt, nil, pusher)
	mover, _ = ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if mover.Mode() != monster.MoverPending || mover.Pose != goal {
		t.Fatalf("pending actor did not settle at its segment goal: %+v", mover)
	}
	settled := calls
	for k := int64(1); k <= 3; k++ {
		at := settleAt + k*1500
		ops.RunMonsterLeg(at, nil, pusher)
		mover, _ = ops.Monsters.Mover(monsterTestDivision, actor.Gid)
		if mover.Activity.LastCheck != uint32(at) {
			t.Fatalf("leg %d ran no due activity gate (LastCheck %d, want %d)", k, mover.Activity.LastCheck, at)
		}
	}
	if calls-settled > 1 {
		t.Fatalf("settled block resolved %d times over 3 due legs through the dispatch, want at most 1", calls-settled)
	}
	if mover.Mode() != monster.MoverPending {
		t.Fatalf("settled actor left PENDING unobserved: %v", mover.Mode())
	}
}

/*
================
BenchmarkMonsterLegSeeded

BenchmarkMonsterLegSeeded runs one 100 ms leg over the seeded 1200-monster
population per iteration, viewer present: the prod shape behind the
RunMonsterLeg profile slice (#1943).
================
*/
func BenchmarkMonsterLegSeeded(b *testing.B) {
	ops, sessions, now := seededLegWorld(b, 1200)
	// Settle the population into its steady state (spawn holds done,
	// activity cadences armed) before measuring, and prove the leg
	// publishes before trusting the measured loop.
	settle := &fakePusher{}
	for tick := 0; tick < 50; tick++ {
		*now = seededLegBaseMs + int64(tick)*100
		ops.RunMonsterLeg(*now, sessions, settle)
	}
	if len(settle.toSession) == 0 && len(settle.toDivision) == 0 {
		b.Fatal("seeded leg published nothing while settling")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		*now += 100
		ops.RunMonsterLeg(*now, sessions, discardPusher{})
	}
}
