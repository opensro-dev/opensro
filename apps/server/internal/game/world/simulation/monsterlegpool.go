/*
===========================================================================

monsterlegpool.go - the monster leg's worker pool (#2137 slice 3, #2205)

Port-only concurrency, not native behaviour: the serial leg stays the
default and this file changes nothing until SRO_MONSTER_LEG_WORKERS
asks for more than one worker. When it does, each batch's due actors
are PLANNED on a small pool (one bin per region cluster, a region is
never split) and COMMITTED on the leg goroutine in the batch's exact
order, so the observable leg - frame order per session, attack
execution order, settlement order, the set of actors stepped - is the
serial leg's. A plan reads leg-start state (the legSnapshot view plus
locked reads the barrier has not yet overwritten); commit
revalidation - the mover rows' commit generation (#2137 slice 1),
commitNavigation's value guard, commitActivity's value guard, the
attack door's own admission - absorbs the one relaxation that buys
the parallelism, exactly as #2137's design (§4) records. A plan whose
recorded commits no longer land at commit is dropped whole: the
actor replans at its next due, the design's stale-plan rule.

The step itself is unchanged. A planning copy of the ops records its
commit funnel calls (monsterpublish.go's commitStepped* helpers, the
activity gate's commit, the navigation transaction, the follow
transaction) into a legPlan instead of applying them, draws from a
per-actor random stream seeded by (leg time, gid) so outcomes cannot
depend on scheduling, and defers a monster->player hit as an intent
(monsterattack.go); the commit replays the record in order, applies
the step's consumptions (its tactics-event drain and once-only
conditional-skill spends - a dropped plan has consumed nothing),
executes the hit through the same RunAction door the serial dispatch
uses, runs the hit's post-attack logic on the real result, and only
then publishes.

===========================================================================
*/

package simulation

import (
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

// EnvMonsterLegWorkers selects the monster leg's worker count.
// Unset, unparsable, or <= 0 keeps the serial leg (the native-shape
// default); an explicit 1 runs the pool on one worker - the
// determinism baseline across worker counts (the same plan/commit
// path with no parallelism; it is not the serial leg, whose steps
// decide on live state where pooled plans decide from leg-start
// state); larger values cap at the batch's region count in
// partitionLegPlans. Port-only: no native server threading is
// known or claimed.
const EnvMonsterLegWorkers = "SRO_MONSTER_LEG_WORKERS"

/*
================
MonsterLegWorkersFromEnv

The wired worker count: 0 means the serial leg. A malformed value
falls back to serial rather than refusing the boot - this knob can
only cost performance, never correctness, so it fails closed to the
default.
================
*/
func MonsterLegWorkersFromEnv() int {
	raw := strings.TrimSpace(os.Getenv(EnvMonsterLegWorkers))
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

//============================================================================

/*
================
legPlanOpKind

One recorded commit-funnel call, replayed in order at commit.
================
*/
type legPlanOpKind uint8

const (
	// legPlanOpMover is a commitSteppedMover / commitSteppedMoverFrames
	// call: a generation-chained whole-value mover commit.
	legPlanOpMover legPlanOpKind = iota
	// legPlanOpNavigation is a commitSteppedNavigation call:
	// commitNavigation re-prepared against the row at commit.
	legPlanOpNavigation
	// legPlanOpActivity is the activity gate's commitActivity call:
	// value-guarded on (before, after), reschedules included.
	legPlanOpActivity
	// legPlanOpRebase marks a point where the running step re-read the
	// row's generation and chained its later commits off it
	// (rebaseStepBase); commit adopts the row's generation there,
	// only while the row still holds the value the step expects.
	legPlanOpRebase
	// legPlanOpFollow is a commitSteppedFollow call: the follow
	// transaction replayed at commit, where its own validation
	// (instance, mover, timers, leader facts) is the revalidation.
	legPlanOpFollow
)

/*
================
legPlanOp

mover carries the value to commit for Mover, Navigation and
Follow, and for Rebase the row value the step expects to find
(its last recorded commit, or the snapshot it started from);
before and after carry the gate's pair for Activity; follow
carries the follow transaction's prepared plan for Follow.
================
*/
type legPlanOp struct {
	kind   legPlanOpKind
	mover  monster.MoverState
	frames []Frame // Navigation: the frames the transaction publishes
	before monster.MoverState
	after  monster.MoverState
	follow monsterFollowPlan
}

/*
================
legPlanConditional

One conditional-skill selection the step made (monsterattack.go):
the once-only spend is recorded, not taken, and lands at commit.
================
*/
type legPlanConditional struct {
	skill uint32
	slot  int
}

/*
================
legPlanAttack

A deferred monster->player hit: everything finishMonsterAttack
needs to run the post-hit logic at commit on the real result.
================
*/
type legPlanAttack struct {
	instance monster.Instance
	tactics  monster.Tactics
	mover    monster.MoverState
	target   playerPose
	plan     MonsterAttackPlan
}

/*
================
legPlan

One due actor's planned leg: the recorded commits in step order, the
step's output (published only when every recorded commit lands), the
deferred hit if the step reached one, and the shadow mover - the
value the actor's row will hold if the recorded commits land in
order, which is what the step's own mid-step re-reads must see
(stepMoverGeneration).
================
*/
type legPlan struct {
	actor      behaviorActorSnapshot
	ops        []legPlanOp
	frames     []Frame
	targeted   []MonsterPrivateFrames
	attack     *legPlanAttack
	shadow     monster.MoverState
	shadowed   bool
	panicValue any
	// The step's consumptions, applied at commit only: the tactics
	// events it read (aiEventsTaken marks the read, as a serial
	// drain also reads once) and its conditional-skill selections.
	aiEvents      []monsterAIEvent
	aiEventsTaken bool
	conditionals  []legPlanConditional
}

/*
================
legPlan.recordMover
================
*/
func (plan *legPlan) recordMover(mover monster.MoverState) {
	plan.ops = append(plan.ops, legPlanOp{kind: legPlanOpMover, mover: mover})
	plan.shadow, plan.shadowed = mover, true
}

/*
================
legPlan.recordNavigation
================
*/
func (plan *legPlan) recordNavigation(next monster.MoverState, frames []Frame) {
	plan.ops = append(plan.ops, legPlanOp{kind: legPlanOpNavigation, mover: next, frames: frames})
	plan.shadow, plan.shadowed = next, true
}

/*
================
legPlan.recordActivity
================
*/
func (plan *legPlan) recordActivity(before, after monster.MoverState) {
	plan.ops = append(plan.ops, legPlanOp{kind: legPlanOpActivity, before: before, after: after})
	plan.shadow, plan.shadowed = after, true
}

/*
================
legPlan.recordRebase

The expectation is the value the step believes the row holds here:
its last recorded commit, or the snapshot when it has committed
nothing yet. The commit adopts the row's generation only while the
row still holds exactly that - a slower lane's set in between makes
the plan stale instead of letting its later commits land on top.
================
*/
func (plan *legPlan) recordRebase() {
	expected := plan.actor.mover
	if plan.shadowed {
		expected = plan.shadow
	}
	plan.ops = append(plan.ops, legPlanOp{kind: legPlanOpRebase, mover: expected})
}

/*
================
legPlan.recordFollow
================
*/
func (plan *legPlan) recordFollow(follow monsterFollowPlan, mover monster.MoverState, frames []Frame) {
	plan.ops = append(plan.ops, legPlanOp{kind: legPlanOpFollow, follow: follow, mover: mover, frames: frames})
	plan.shadow, plan.shadowed = mover, true
}

/*
================
legPlan.conditionalUsed

The once-only bits this plan's recorded selections spend, so a
later selection in the same step masks them exactly as the serial
step's already-spent row would.
================
*/
func (plan *legPlan) conditionalUsed() uint32 {
	used := uint32(0)
	for _, burn := range plan.conditionals {
		used |= 1 << burn.slot
	}
	return used
}

//============================================================================

/*
================
legRand

One actor's random stream for one leg (splitmix64): seeded by the
leg time and the actor's gid, so the draws a step makes cannot
depend on which worker ran it or when. Statistical quality only -
wander and idle jitter - the same class as the process-global stream
production draws today; determinism is what the pool needs from it.
================
*/
type legRand struct {
	state uint64
}

/*
================
newLegRand
================
*/
func newLegRand(nowMs int64, gid uint32) *legRand {
	rand := &legRand{state: uint64(gid)*0x9E3779B97F4A7C15 ^ uint64(nowMs)*0xBF58476D1CE4E5B9}
	rand.Float64() // warm the mix so neighbouring gids diverge at once
	return rand
}

/*
================
legRand.Float64
================
*/
func (rand *legRand) Float64() float64 {
	rand.state += 0x9E3779B97F4A7C15
	z := rand.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	z ^= z >> 31
	return float64(z>>11) * (1.0 / 9007199254740992.0)
}

/*
================
legPlanDeferredAttack

Installed as a planning copy's BasicAttack so the step's capability
check sees an attack door. The step's plan branch records the hit
before this can be called; reaching it means the recording branch
was bypassed - a programmer error, never a runtime answer.
================
*/
func legPlanDeferredAttack(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
	panic("simulation: a planning step reached the deferred attack capability")
}

//============================================================================

/*
================
legPlanBin

One worker's share of a batch: the plan positions it owns, plus the
per-bin bookkeeping (activity memo, step stats) the commit merges -
the shared memo map and leg record are not worker-safe, so a bin
accumulates its own and hands them over at the join.
================
*/
type legPlanBin struct {
	positions []int
	memo      map[uint32]activityBlockMemo
	stats     legStats
}

/*
================
partitionLegPlans

Group the batch's due actors by region cluster and pack the clusters
into at most `workers` bins balanced by due count. A region is never
split - a hot region is one bin and caps that batch's speedup, as
the design accepts. Deterministic: clusters go largest first (region
id breaks ties) onto the least-loaded bin (lowest index wins ties),
so the same batch always partitions the same way.
================
*/
func partitionLegPlans(actors []behaviorActorSnapshot, workers int) []legPlanBin {
	byRegion := make(map[uint16][]int)
	regions := make([]uint16, 0, len(actors))
	for pos, actor := range actors {
		region := actor.mover.Pose.RegionID
		if _, seen := byRegion[region]; !seen {
			regions = append(regions, region)
		}
		byRegion[region] = append(byRegion[region], pos)
	}
	sort.Slice(regions, func(i, j int) bool {
		a, b := regions[i], regions[j]
		if len(byRegion[a]) != len(byRegion[b]) {
			return len(byRegion[a]) > len(byRegion[b])
		}
		return a < b
	})
	count := workers
	if count > len(regions) {
		count = len(regions)
	}
	if count < 1 {
		count = 1
	}
	bins := make([]legPlanBin, count)
	loads := make([]int, count)
	for _, region := range regions {
		least := 0
		for bin := 1; bin < count; bin++ {
			if loads[bin] < loads[least] {
				least = bin
			}
		}
		bins[least].positions = append(bins[least].positions, byRegion[region]...)
		loads[least] += len(byRegion[region])
	}
	return bins
}

/*
================
legBatchWork

One batch's commit context: everything the ordered commit needs
that the workers must not touch.
================
*/
type legBatchWork struct {
	key      populationKey
	division string
	players  []playerPose
	sessions []SessionSnapshot
	push     Pusher
	nowMs    int64
}

//============================================================================

/*
================
MonsterMoverOps.pooledLeg

Whether this leg runs batches through the pool. Serial remains the
default: the field is zero unless wiring (or a test) sets it.
================
*/
func (ops *MonsterMoverOps) pooledLeg() bool {
	return ops.LegWorkers >= 1
}

/*
================
MonsterMoverOps.runPooledBatch

One batch through the pool: the leg goroutine first walks the batch
in its serial order keeping every dispatch decision it makes today
(liveness, the resumed note, the watchdog cadence, the snapshot
read), then workers plan the survivors per region-cluster bin, and
the leg goroutine commits the plans in that same order. The serial
loop in RunMonsterLeg is untouched; this is the pooled twin of its
per-batch half.
================
*/
func (ops *MonsterMoverOps) runPooledBatch(batch behaviorBatch, view *legSnapshot, players []playerPose, sessions []SessionSnapshot, push Pusher, nowMs int64, stats *legStats, stepped int) int {
	work := legBatchWork{
		key:      batch.key,
		division: batch.key.division,
		players:  players,
		sessions: sessions,
		push:     push,
		nowMs:    nowMs,
	}
	actors := make([]behaviorActorSnapshot, 0, len(batch.actors))
	for i, ref := range batch.actors {
		// The scheduler carries identities, not a second copy of the
		// world: the same dispatch decisions, in the same order, as
		// the serial loop (monstertick.go) makes them.
		if !ops.Monsters.behaviorActorLiveAt(batch.key, ref) {
			continue
		}
		if ops.noteResumed(batch.key, ref.gid) {
			stats.resumed++
		}
		if stepped++; stepped%monsterLegStepActors == 0 {
			ops.watch.step()
		}
		actor, ok := view.actorAt(i)
		if !ok {
			continue
		}
		actors = append(actors, actor)
	}
	if len(actors) == 0 {
		return stepped
	}

	plans := make([]legPlan, len(actors))
	for pos, actor := range actors {
		plans[pos].actor = actor
	}
	bins := partitionLegPlans(actors, ops.LegWorkers)
	var wg sync.WaitGroup
	for bin := range bins {
		bins[bin].memo = make(map[uint32]activityBlockMemo, len(bins[bin].positions))
		wg.Add(1)
		go func(bin *legPlanBin) {
			defer wg.Done()
			ops.runLegPlanBin(bin, plans, work)
		}(&bins[bin])
	}
	wg.Wait()
	for bin := range bins {
		for gid, memo := range bins[bin].memo {
			ops.activityBlocks[gid] = memo
		}
		stats.absorbBin(&bins[bin].stats)
	}
	for pos := range plans {
		if plans[pos].panicValue != nil {
			// A worker's panic is re-raised here, at the actor's own
			// commit position, so the leg fails the way the serial
			// dispatch would have failed on this actor.
			panic(plans[pos].panicValue)
		}
		ops.commitLegPlan(work, &plans[pos], stats)
	}
	return stepped
}

/*
================
MonsterMoverOps.runLegPlanBin

Plan one bin's actors on this goroutine. The bin works on copies of
the ops: its own activity memo and leg record (the shard's are not
worker-safe), the plan sink set per actor, a per-actor random
stream, and - when the commit has an attack door - the deferred
attack capability.
================
*/
func (ops *MonsterMoverOps) runLegPlanBin(bin *legPlanBin, plans []legPlan, work legBatchWork) {
	binOps := *ops
	binOps.leg = &bin.stats
	binOps.activityBlocks = bin.memo
	canAttack := ops.RunAction != nil || ops.BasicAttack != nil
	for _, pos := range bin.positions {
		plan := &plans[pos]
		owned := binOps
		owned.plan = plan
		owned.Rand = newLegRand(work.nowMs, plan.actor.instance.Gid).Float64
		if canAttack {
			owned.BasicAttack = legPlanDeferredAttack
		}
		started := time.Now()
		planLegActor(&owned, plan, work)
		bin.stats.noteAdvance(time.Since(started))
	}
}

/*
================
planLegActor

Plan one actor: the step advanceInstanceSnapshot runs (the training
dummy guard and the step itself), minus its resolveStuck tail,
which belongs to the commit. A panic is ferried to the plan, never
left to kill a pool goroutine silently.
================
*/
func planLegActor(owned *MonsterMoverOps, plan *legPlan, work legBatchWork) {
	defer func() {
		if recovered := recover(); recovered != nil {
			plan.panicValue = recovered
		}
	}()
	actor := plan.actor
	if actor.instance.Ref.TrainingDummy {
		return
	}
	plan.frames, plan.targeted = owned.advanceInstanceStep(
		work.division, actor.instance, actor.mover, true, work.players, work.nowMs)
}

/*
================
MonsterMoverOps.commitLegPlan

Commit one plan on the leg goroutine: replay the recorded commits
in step order through the same armed helpers the serial step uses;
if one no longer lands, the whole plan is dropped and the actor
replans at its next due - and nothing the step consumed is spent.
Then apply the step's consumptions (tactics-event drain,
conditional-skill spends), execute the deferred hit and its
post-hit logic, run the step's resolveStuck tail, and publish -
public frames, the private consequences, the safe-zone vanish - in
advanceAndPublish's order.
================
*/
func (ops *MonsterMoverOps) commitLegPlan(work legBatchWork, plan *legPlan, stats *legStats) {
	actor := plan.actor
	gid := actor.instance.Gid
	armed := *ops
	armed.stepBaseGen, armed.stepGenArmed = actor.moverGen, true
	armed.stepSlot, armed.stepMonsterSlot = actor.slot, actor.monsterSlot
	started := time.Now()
	defer func() { stats.noteAdvance(time.Since(started)) }()

	landed := true
	for _, op := range plan.ops {
		switch op.kind {
		case legPlanOpMover:
			landed = armed.commitSteppedMover(work.division, gid, op.mover)
		case legPlanOpNavigation:
			armed.commitSteppedNavigation(work.division,
				ops.Monsters.prepareNavigation(work.division, gid), op.mover, op.frames)
			// commitNavigation's frame return cannot tell a landed
			// commit with no frames from a dropped one; the stored
			// value equalling next is the acceptance witness, the
			// same one commitSteppedNavigation chains off.
			stored, _, ok := ops.Monsters.MoverGenerationAt(work.division, gid, actor.slot, actor.monsterSlot)
			landed = ok && stored == op.mover
		case legPlanOpActivity:
			landed = ops.Monsters.commitActivityAt(work.division, gid, actor.slot, actor.monsterSlot, op.before, op.after, work.nowMs)
		case legPlanOpFollow:
			// commitFollow hands back the frames it was given; the
			// step's frames already ride the plan's output.
			_, landed = ops.Monsters.commitFollow(op.follow, op.mover, op.frames)
		case legPlanOpRebase:
			stored, gen, ok := ops.Monsters.MoverGenerationAt(work.division, gid, actor.slot, actor.monsterSlot)
			landed = ok && stored == op.mover
			if landed {
				armed.stepBaseGen = gen
			}
		}
		if !landed {
			// The replan is due at once: the batch build armed the
			// actor from its leg-start row, which for an idle actor
			// is now a far due (#2136), not the next tick this
			// stale-plan rule promises.
			ops.Monsters.wakeBehavior(work.division, gid)
			return
		}
	}
	// The step's consumptions land only now that the plan has: the
	// serial step spends both mid-step, but nothing outside the
	// actor's own row can observe the exact point, and a dropped
	// plan must leave the event queue and the once-only bits
	// untouched. The conditional spends precede the deferred hit,
	// as selection precedes command admission in the serial step.
	if plan.aiEventsTaken && len(plan.aiEvents) > 0 {
		ops.Monsters.consumeAIEvents(work.division, gid, plan.aiEvents)
	}
	for _, burn := range plan.conditionals {
		ops.Monsters.consumeConditionalSkill(work.division, gid, burn.skill, burn.slot)
	}
	if plan.attack != nil {
		frames, targeted := ops.executeLegPlanAttack(&armed, work, plan.attack, stats)
		plan.frames = append(plan.frames, frames...)
		plan.targeted = append(plan.targeted, targeted...)
	}
	frames := armed.resolveStuck(work.division, gid, work.players, work.nowMs, plan.frames)
	for _, session := range ops.divisionSessions(work.division, work.sessions) {
		if len(frames) > 0 && ops.shownMonsters[session.SessionID][gid] {
			work.push.PushToSession(session.SessionID, frames)
		}
	}
	deliverMonsterTargetFrames(work.division, plan.targeted, work.sessions, work.push)
	ops.vanishInSafeZone(work.division, gid, work.nowMs)
}

/*
================
MonsterMoverOps.executeLegPlanAttack

Execute one deferred hit at commit, through the same door the
serial dispatch uses: RunAction's transaction in production, the
bare capability in fixtures, no door at all (a zero result, which
finishMonsterAttack weighs exactly as today's detached seam does)
when neither is wired.
================
*/
func (ops *MonsterMoverOps) executeLegPlanAttack(armed *MonsterMoverOps, work legBatchWork, attack *legPlanAttack, stats *legStats) ([]Frame, []MonsterPrivateFrames) {
	var result MonsterAttackResult
	switch {
	case ops.RunAction != nil:
		// The lane wall outside the step, as the serial dispatch
		// attributes it (attribution only; no behaviour reads it).
		laneStarted := time.Now()
		ops.RunAction(work.division, func(capability MonsterAttackOperation) {
			result = capability(work.division, attack.instance, attack.target.Gid, attack.plan.SkillID, work.nowMs)
		})
		if outside := time.Since(laneStarted); outside > 0 {
			stats.lane += outside
		}
	case ops.BasicAttack != nil:
		result = ops.BasicAttack(work.division, attack.instance, attack.target.Gid, attack.plan.SkillID, work.nowMs)
	}
	return armed.finishMonsterAttack(
		work.division, attack.instance, attack.tactics, attack.mover,
		attack.target, attack.plan, work.nowMs, nil, result)
}

/*
================
legStats.absorbBin

Fold one bin's accumulated step attribution into the leg's record.
Only the counters a planning step can move are absorbed; the leg's
own phase walls and samples stay the leg goroutine's.
================
*/
func (stats *legStats) absorbBin(bin *legStats) {
	stats.advance += bin.advance
	for caller := range bin.plans {
		for outcome := range bin.plans[caller] {
			stats.plans[caller][outcome] += bin.plans[caller][outcome]
		}
		stats.planProbes[caller] += bin.planProbes[caller]
		stats.planTime[caller] += bin.planTime[caller]
		stats.memo[caller] += bin.memo[caller]
		stats.clips[caller] += bin.clips[caller]
	}
	if bin.maxPlanTime > stats.maxPlanTime {
		stats.maxPlanTime = bin.maxPlanTime
	}
	if bin.maxPlanProbes > stats.maxPlanProbes {
		stats.maxPlanProbes = bin.maxPlanProbes
	}
}
