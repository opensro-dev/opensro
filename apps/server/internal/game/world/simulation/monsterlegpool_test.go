/*
===========================================================================

monsterlegpool_test.go - the worker pool's contract (#2205)

The pool must be invisible: a seeded leg produces the identical
observable output (every pushed frame, the final mover values, the
order hits execute in) at any worker count, and a plan whose row
moved underneath it commits nothing. The env knob, the region
partition, and the per-actor rand are pinned at their own seams.

===========================================================================
*/

package simulation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"runtime"
	"sort"
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestMonsterLegWorkersFromEnv
================
*/
func TestMonsterLegWorkersFromEnv(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
	}{
		{"", 0},
		{"0", 0},
		{"1", 1},
		{"2", 2},
		{" 4 ", 4},
		{"-2", 0},
		{"many", 0},
	} {
		t.Setenv(EnvMonsterLegWorkers, tc.raw)
		if got := MonsterLegWorkersFromEnv(); got != tc.want {
			t.Fatalf("%s=%q: workers = %d, want %d", EnvMonsterLegWorkers, tc.raw, got, tc.want)
		}
	}
}

/*
================
TestPartitionLegPlans

A region is never split across bins, every actor is placed exactly
once, bins stay balanced by due count, and the partition of the
same batch is the same every time.
================
*/
func TestPartitionLegPlans(t *testing.T) {
	regions := map[uint16]int{25001: 5, 25002: 3, 25003: 1, 25004: 4}
	var actors []behaviorActorSnapshot
	for region, count := range regions {
		for i := 0; i < count; i++ {
			actor := behaviorActorSnapshot{ok: true}
			actor.instance.Gid = uint32(1000 + len(actors))
			actor.mover.Pose.RegionID = region
			actors = append(actors, actor)
		}
	}
	bins := partitionLegPlans(actors, 2)
	if len(bins) != 2 {
		t.Fatalf("bins = %d, want 2", len(bins))
	}
	placed := make([]int, len(actors))
	binOfRegion := make(map[uint16]int)
	for bin, share := range bins {
		for _, pos := range share.positions {
			placed[pos]++
			region := actors[pos].mover.Pose.RegionID
			if other, seen := binOfRegion[region]; seen && other != bin {
				t.Fatalf("region %d split across bins %d and %d", region, other, bin)
			}
			binOfRegion[region] = bin
		}
	}
	for pos, times := range placed {
		if times != 1 {
			t.Fatalf("actor %d placed %d times, want 1", pos, times)
		}
	}
	// Greedy largest-first packing of 5/4/3/1 over two bins loads 7/6.
	loads := []int{len(bins[0].positions), len(bins[1].positions)}
	if loads[0]+loads[1] != len(actors) || loads[0] > 7 || loads[1] > 7 {
		t.Fatalf("bin loads = %v over %d actors, want a balanced split", loads, len(actors))
	}
	again := partitionLegPlans(actors, 2)
	for bin := range bins {
		if fmt.Sprint(bins[bin].positions) != fmt.Sprint(again[bin].positions) {
			t.Fatalf("partition is not deterministic: %v vs %v", bins[bin].positions, again[bin].positions)
		}
	}
	// More workers than regions still yields one bin per region.
	if bins := partitionLegPlans(actors, 16); len(bins) != len(regions) {
		t.Fatalf("bins with 16 workers = %d, want one per region (%d)", len(bins), len(regions))
	}
}

/*
================
TestLegRandDeterministic

The same (leg time, gid) draws the same stream on any worker; a
different gid draws a different one; every draw is in [0, 1).
================
*/
func TestLegRandDeterministic(t *testing.T) {
	a, b := newLegRand(1_700_000_000_000, 42), newLegRand(1_700_000_000_000, 42)
	for i := 0; i < 64; i++ {
		x, y := a.Float64(), b.Float64()
		if x != y {
			t.Fatalf("draw %d: same seed drew %v and %v", i, x, y)
		}
		if x < 0 || x >= 1 {
			t.Fatalf("draw %d out of range: %v", i, x)
		}
	}
	if newLegRand(1_700_000_000_000, 43).Float64() == newLegRand(1_700_000_000_000, 42).Float64() {
		t.Fatal("neighbouring gids drew the same first value")
	}
}

/*
================
TestLegPlanCommitDropsStalePlan

The stale-plan rule: a plan whose actor's row was set by another
lane after the snapshot commits nothing and publishes nothing -
the actor replans at its next due. The control plan, its row
untouched, lands whole.
================
*/
func TestLegPlanCommitDropsStalePlan(t *testing.T) {
	for _, stale := range []bool{false, true} {
		ops, actor := monsterLegFixture(t, aggressiveTactics())
		mover, gen, ok := ops.Monsters.MoverGeneration(monsterTestDivision, actor.Gid)
		if !ok {
			t.Fatal("fixture actor has no mover row")
		}
		planned := mover
		planned.BehaviorDeadlineMs = mover.BehaviorDeadlineMs + 5000
		// The mover as the step holds it when the hit leaves: legally
		// chased into the attacking state with the attack plan adopted
		// (the mover validator refuses anything else at the
		// continuation's commit).
		attackMover := mover
		for _, step := range []struct {
			event  monster.MoverEvent
			target uint32
		}{
			{monster.MoverEventStartWander, 0},
			{monster.MoverEventSegmentArrived, 0},
			{monster.MoverEventRetaliationArmed, 1},
			{monster.MoverEventChaseStarted, 1},
		} {
			mustMoverTransition(&attackMover, step.event, step.target)
		}
		attackMover.AttackSkillID = 0x1234
		attackMover.AttackReach = 6
		attackMover.AttackCooldownMs = 1000
		attackMover.AttackIntervalMs = 1000
		mustMoverTransition(&attackMover, monster.MoverEventAttackStarted, 1)
		plan := &legPlan{actor: behaviorActorSnapshot{instance: actor, mover: mover, moverGen: gen, ok: true}}
		plan.recordMover(planned)
		plan.attack = &legPlanAttack{
			instance: actor, mover: attackMover, target: playerPose{Gid: 1},
			plan: MonsterAttackPlan{SkillID: 0x1234, Reach: ActionReach(6), CooldownMs: 1000, ActionLifecycleMs: 600},
		}
		attacks := 0
		ops.BasicAttack = func(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
			attacks++
			return MonsterAttackResult{Accepted: true, TargetAlive: true}
		}
		if stale {
			other := mover
			other.BehaviorDeadlineMs = mover.BehaviorDeadlineMs + 9000
			if !ops.Monsters.CommitMover(monsterTestDivision, actor.Gid, other) {
				t.Fatal("the other lane's set was refused")
			}
		}
		pusher := &fakePusher{}
		work := legBatchWork{division: monsterTestDivision, push: pusher, nowMs: 10000}
		ops.commitLegPlan(work, plan, &legStats{})
		row, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
		if len(pusher.toSession) != 0 || len(pusher.toDivision) != 0 {
			t.Fatalf("stale=%v: the commit published frames", stale)
		}
		if stale {
			if attacks != 0 {
				t.Fatalf("a dropped plan executed %d hits, want none", attacks)
			}
			if row.BehaviorDeadlineMs != mover.BehaviorDeadlineMs+9000 || row.NextAttackMs != mover.NextAttackMs {
				t.Fatalf("a dropped plan touched the row: %+v", row)
			}
			continue
		}
		if attacks != 1 {
			t.Fatalf("a landed plan executed %d hits, want 1", attacks)
		}
		// The continuation's own commit chains off the recorded one:
		// it lands only when the recorded commit advanced the row's
		// generation first, and it is the commit that stamps the
		// next attack time (nowMs + the mover's 1000 ms interval).
		if row.NextAttackMs != 11000 {
			t.Fatalf("continuation commit did not land after the recorded one: NextAttackMs = %d, want 11000", row.NextAttackMs)
		}
	}
}

/*
================
TestLegPlanFollowCommitsAtCommitTime

The follow transaction is a commit funnel (#2205 pre-review
finding 1): a planning step records it instead of applying it, so
at plan time neither the mover row nor the entry timer moves, and
a plan whose leader moved before commit drops the follow whole.
================
*/
func TestLegPlanFollowCommitsAtCommitTime(t *testing.T) {
	for _, stale := range []bool{false, true} {
		ops, parent, child, now := followFixture(t)
		mover, gen, ok := ops.Monsters.MoverGeneration("summon", child.Gid)
		if !ok {
			t.Fatal("fixture child has no mover row")
		}
		plan := &legPlan{actor: behaviorActorSnapshot{instance: child, mover: mover, moverGen: gen, ok: true}}
		planOps := *ops
		planOps.plan = plan
		planOps.followSummoner("summon", &child, mover, now)
		live, _ := ops.Monsters.Mover("summon", child.Gid)
		if live.Mode() == monster.MoverFollowing {
			t.Fatalf("stale=%v: the follow applied at plan time", stale)
		}
		if stale {
			leader, _ := ops.Monsters.Mover("summon", parent.Gid)
			leader.Pose.X++
			if !ops.Monsters.CommitMover("summon", parent.Gid, leader) {
				t.Fatal("the leader's move was refused")
			}
		}
		work := legBatchWork{division: "summon", push: &fakePusher{}, nowMs: now}
		ops.commitLegPlan(work, plan, &legStats{})
		live, _ = ops.Monsters.Mover("summon", child.Gid)
		if stale {
			if live.Mode() == monster.MoverFollowing {
				t.Fatal("a dropped plan left the follow applied")
			}
			continue
		}
		if live.Mode() != monster.MoverFollowing || live.FollowLeaderGID() != child.SummonerGID {
			t.Fatalf("a landed plan did not commit the follow: mode %v leader %d", live.Mode(), live.FollowLeaderGID())
		}
		timers := followTimers(ops.Monsters, child.Gid)
		if timer := timers.GetTimer(0); !timer.ArmedImmediate {
			t.Fatalf("a landed plan did not spend the entry timer: %+v", timer)
		}
	}
}

/*
================
TestLegPlanAIEventsConsumedOnlyAtCommit

The tactics-event drain is a consumption (#2205 pre-review
finding 2): a planning step reads the queued events without
draining them, a second take in the same step sees the drain as
the serial step does, and only a landed commit removes them - a
dropped plan leaves them for the actor's next due.
================
*/
func TestLegPlanAIEventsConsumedOnlyAtCommit(t *testing.T) {
	for _, stale := range []bool{false, true} {
		ops, actor := monsterLegFixture(t, aggressiveTactics())
		ops.Monsters.mu.Lock()
		state := ops.Monsters.populationForObject(monsterTestDivision, actor.Gid)
		state.queueAIEvents(actor.Gid, []monsterAIEvent{{Event: aiEventStart, Kind: 9, Source: 7}})
		ops.Monsters.mu.Unlock()
		mover, gen, ok := ops.Monsters.MoverGeneration(monsterTestDivision, actor.Gid)
		if !ok {
			t.Fatal("fixture actor has no mover row")
		}
		plan := &legPlan{actor: behaviorActorSnapshot{instance: actor, mover: mover, moverGen: gen, ok: true}}
		planOps := *ops
		planOps.plan = plan
		events := planOps.takeStepAIEvents(monsterTestDivision, actor.Gid)
		if len(events) != 1 || events[0].Source != 7 {
			t.Fatalf("stale=%v: the planning step read %v, want the queued event", stale, events)
		}
		if again := planOps.takeStepAIEvents(monsterTestDivision, actor.Gid); len(again) != 0 {
			t.Fatalf("stale=%v: a second take in one step read %d events, want the drain", stale, len(again))
		}
		if queued := ops.Monsters.peekAIEvents(monsterTestDivision, actor.Gid); len(queued) != 1 {
			t.Fatalf("stale=%v: planning consumed the queue (%d left)", stale, len(queued))
		}
		planned := mover
		planned.BehaviorDeadlineMs = mover.BehaviorDeadlineMs + 5000
		plan.recordMover(planned)
		if stale {
			other := mover
			other.BehaviorDeadlineMs = mover.BehaviorDeadlineMs + 9000
			if !ops.Monsters.CommitMover(monsterTestDivision, actor.Gid, other) {
				t.Fatal("the other lane's set was refused")
			}
		}
		work := legBatchWork{division: monsterTestDivision, push: &fakePusher{}, nowMs: 10000}
		ops.commitLegPlan(work, plan, &legStats{})
		remaining := ops.Monsters.peekAIEvents(monsterTestDivision, actor.Gid)
		if stale && len(remaining) != 1 {
			t.Fatalf("a dropped plan consumed its events: %d left", len(remaining))
		}
		if !stale && len(remaining) != 0 {
			t.Fatalf("a landed plan left its events queued: %d left", len(remaining))
		}
	}
}

/*
================
TestLegPlanConditionalSpentOnlyAtCommit

The conditional-skill selection is a once-only spend (#2205
pre-review finding 2): a planning step selects without spending,
and only a landed commit burns the entry - a dropped plan keeps
the monster's one shot.
================
*/
func TestLegPlanConditionalSpentOnlyAtCommit(t *testing.T) {
	for _, stale := range []bool{false, true} {
		ops, actor := monsterLegFixture(t, aggressiveTactics())
		ops.Monsters.mu.Lock()
		pop := ops.Monsters.populationForObject(monsterTestDivision, actor.Gid)
		row := pop.instances.get(actor.Gid)
		row.Ref.DefaultSkillIDs = [10]uint32{22}
		row.Nest.ConditionalSkills[0] = monster.ConditionalSkill{SkillID: 22, Data: 100}
		pop.instances.set(actor.Gid, row)
		ops.Monsters.mu.Unlock()
		mover, gen, ok := ops.Monsters.MoverGeneration(monsterTestDivision, actor.Gid)
		if !ok {
			t.Fatal("fixture actor has no mover row")
		}
		plan := &legPlan{actor: behaviorActorSnapshot{instance: actor, mover: mover, moverGen: gen, ok: true}}
		planOps := *ops
		planOps.plan = plan
		skill, selected := planOps.selectStepConditionalSkill(monsterTestDivision, actor.Gid)
		if !selected || skill != 22 {
			t.Fatalf("stale=%v: selection = %d, %v, want the conditional skill", stale, skill, selected)
		}
		live, _ := ops.Monsters.Get(monsterTestDivision, actor.Gid)
		if live.ConditionalUsed != 0 {
			t.Fatalf("stale=%v: selection spent the entry at plan time", stale)
		}
		planned := mover
		planned.BehaviorDeadlineMs = mover.BehaviorDeadlineMs + 5000
		plan.recordMover(planned)
		if stale {
			other := mover
			other.BehaviorDeadlineMs = mover.BehaviorDeadlineMs + 9000
			if !ops.Monsters.CommitMover(monsterTestDivision, actor.Gid, other) {
				t.Fatal("the other lane's set was refused")
			}
		}
		work := legBatchWork{division: monsterTestDivision, push: &fakePusher{}, nowMs: 10000}
		ops.commitLegPlan(work, plan, &legStats{})
		live, _ = ops.Monsters.Get(monsterTestDivision, actor.Gid)
		if stale && live.ConditionalUsed != 0 {
			t.Fatalf("a dropped plan spent the entry: %#x", live.ConditionalUsed)
		}
		if !stale && live.ConditionalUsed != 1 {
			t.Fatalf("a landed plan did not spend the entry: %#x", live.ConditionalUsed)
		}
	}
}

/*
================
TestSeededLegWorkerMatrix

The seeded leg at worker counts 1, 2, 4 and GOMAXPROCS must
produce one identical observable output - every pushed frame, the
order the hits execute in, the final mover values - so the pool
changes nothing a player or a replay could see. The 1-worker hash
is pinned: a behaviour change in the pooled path moves it, exactly
as the serial golden pins the serial path.

Re-pinned for #2136 (idle monsters step at their real dues): the
run's 722 frames are the same multiset and the final movers are
identical to the pre-change run; frames land on different legs
because an idle actor's first timer check - which draws its
acquisition interval from the state stream - now happens at its
first due instead of the next tick, re-phasing later scans. The
serial golden did not move.
================
*/
func TestSeededLegWorkerMatrix(t *testing.T) {
	const pooledGolden = "cb7b180ce7ea94aad07d2c8a3c58f3671d197d2cdde07657cc2bf6297962f668"
	counts := []int{1, 2, 4, runtime.GOMAXPROCS(0)}
	hashes := make([]string, 0, len(counts))
	totalAttacks := 0
	for _, workers := range counts {
		ops, sessions, now := seededLegWorld(t, 240)
		ops.LegWorkers = workers
		var attacks []string
		ops.BasicAttack = func(_ string, instance monster.Instance, targetGid, skillID uint32, _ int64) MonsterAttackResult {
			attacks = append(attacks, fmt.Sprintf("%d>%d/%d", instance.Gid, targetGid, skillID))
			return MonsterAttackResult{
				Accepted:    true,
				TargetAlive: true,
				Frames: []Frame{{Opcode: 0xB245, Payload: []byte{
					byte(instance.Gid), byte(instance.Gid >> 8), byte(targetGid), byte(skillID),
				}}},
			}
		}
		hash := sha256.New()
		record := func(line string) { hash.Write([]byte(line)) }
		pusher := &fakePusher{}
		for tick := 0; tick < 300; tick++ {
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
			for _, attack := range attacks {
				record(fmt.Sprintf("A %s\n", attack))
			}
			totalAttacks += len(attacks)
			attacks = attacks[:0]
		}
		// The leg's gids in ascending order for the final-mover digest.
		instances := ops.Monsters.InstancesInRegions(monsterTestDivision, []uint16{monsterTestRegion})
		gids := make([]uint32, 0, len(instances))
		for _, instance := range instances {
			gids = append(gids, instance.Gid)
		}
		sort.Slice(gids, func(i, j int) bool { return gids[i] < gids[j] })
		for _, gid := range gids {
			mover, ok := ops.Monsters.Mover(monsterTestDivision, gid)
			if !ok {
				t.Fatalf("workers=%d: final mover %d missing", workers, gid)
			}
			record(fmt.Sprintf("M %d %d %+v %+v %+v %d %d %d %d %d\n", gid, mover.Mode(), mover.Pose, mover.From, mover.To,
				mover.DepartMs, mover.ArriveMs, mover.BehaviorDeadlineMs, mover.NextAttackMs, mover.Activity))
		}
		hashes = append(hashes, hex.EncodeToString(hash.Sum(nil)))
	}
	if totalAttacks == 0 {
		t.Fatal("the seeded leg never attacked; the matrix would not cover deferred hits")
	}
	t.Logf("pooled seeded leg: %d attacks over %d worker counts, hash %s", totalAttacks, len(counts), hashes[0])
	for i := 1; i < len(hashes); i++ {
		if hashes[i] != hashes[0] {
			t.Fatalf("worker counts diverge: 1 worker = %s, count %d = %s", hashes[0], counts[i], hashes[i])
		}
	}
	if pooledGolden != "POOLED_GOLDEN_UNSET" && hashes[0] != pooledGolden {
		t.Fatalf("pooled seeded leg hash = %s, want golden %s", hashes[0], pooledGolden)
	}
}
