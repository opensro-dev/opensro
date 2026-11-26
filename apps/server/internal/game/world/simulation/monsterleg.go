/*
===========================================================================
monsterleg.go - monster leg admission, candidate capture and dispatch
===========================================================================
*/
package simulation

import (
	worldgeom "opensro.online/server/internal/game/world"
	"time"
)

// RunMonsterLeg advances actors independently of observer interest.
/*
================
RunMonsterLeg
================
*/
func (ops *MonsterMoverOps) RunMonsterLeg(nowMs int64, sessions []SessionSnapshot, push Pusher) {
	if ops == nil || ops.Monsters == nil {
		return
	}
	// The scheduler mirrors the production tactics resolver; tell it
	// when this ops resolves through a replacement instead, so its
	// idle due stays conservative (idleBehaviorDue).
	ops.Monsters.noteTacticsReplaced(ops.TacticsFor != nil)
	combatants := sessions
	if ops.ServerCombatants != nil {
		if extra := ops.ServerCombatants(); len(extra) > 0 {
			combatants = append(append([]SessionSnapshot(nil), sessions...), extra...)
		}
	}

	// The activity gate's memo lives on the ops, but the dispatch below
	// binds each actor to a value copy (owned := *ops): a memo map first
	// made inside the gate lands on that discarded copy, so on the
	// production path the memo survives no actor at all - every due gate
	// allocates a fresh map and re-walks the actor's surface. Make the
	// map on the leg's own ops before dispatch; every copy then shares
	// the one map header, as the memo's shard-owned design intends. The
	// gate keeps its lazy make for direct callers outside a leg.
	if ops.activityBlocks == nil {
		ops.activityBlocks = make(map[uint32]*activityBlockMemo)
	}

	// Slow-leg attribution (#1820 part C): one record per leg, filled
	// below and by the planners through ops.leg; nothing reads it to
	// decide behaviour.
	legStarted := time.Now()
	stats := &legStats{gcStart: readGCSample()}
	if ops.NavCacheMisses != nil {
		stats.navStart = ops.NavCacheMisses()
	}
	ops.leg = stats
	defer func() { ops.leg = nil }()

	ops.legViewers = retainedLegMap(ops.legViewers, len(sessions))
	ops.legLive = retainedLegMap(ops.legLive, len(sessions))
	ops.legSessionBacking = retainedLegMap(ops.legSessionBacking, len(sessions))
	viewers := ops.legViewers
	live := ops.legLive
	legSessions := ops.legSessionBacking
	for _, session := range sessions {
		live[session.SessionID] = true
		pose := session.World.LiveSpawnAt(nowMs)
		viewers[session.SessionID] = worldgeom.RegionXZ{RegionID: pose.RegionID, X: pose.X, Z: pose.Z}
		legSessions[session.DivisionID] = append(legSessions[session.DivisionID], session)
	}
	ops.legSessions = legSessions
	defer func() { ops.legSessions = nil }()

	// Scope visibility FIRST, mover frames SECOND: a viewer must have
	// received a monster's spawn before any 0xB738/0xB2F5 for its gid
	// (the movement handlers ResolveGidObjectOrAssert - an unknown gid
	// ASSERTS in the native client, REV seq116 / WIP seq140 #3; only
	// 0x30E3 is find-or-skip safe). Delivering mover frames per-session
	// against the just-updated shown sets makes spawn-before-goal and
	// never-goal-after-despawn true by construction.
	phaseStarted := time.Now()
	wokenSets := ops.Monsters.prepareDormancy(nowMs, ops.divisionID, combatants)
	stats.dormancy = time.Since(phaseStarted)
	ops.noteWoken(stats, wokenSets)
	phaseStarted = time.Now()
	ops.runScopeVisibility(nowMs, sessions, viewers, live, push)
	stats.visibility = time.Since(phaseStarted)

	ops.activity = ops.captureActivity(combatants, nowMs)
	defer func() { ops.activity = nil }()
	ops.legDivisionSet = retainedLegMap(ops.legDivisionSet, len(sessions))
	divisionSet := ops.legDivisionSet
	// One dispatch for the whole leg. A variable captured by the closure
	// handed to RunAction escapes through that indirect call: captured per
	// actor, every monster's Instance snapshot and the closure itself were a
	// heap allocation per behaviour tick (1 GB a minute with one player
	// online, which kept the collector busy on three cores). RunAction runs
	// its closure before returning, so the slots are reused safely.
	var dispatch struct {
		division string
		key      populationKey
		index    int
		players  []playerPose
		view     *legSnapshot
	}
	act := func(attack MonsterAttackOperation) {
		// Bind the capability to a value copy, not the shard's dependency
		// set. Mutable actor state remains in MonsterState.
		owned := *ops
		owned.BasicAttack = attack
		// The step starts from the leg-start snapshot (monsterlegsnapshot.go),
		// not a resolve inside this lock: a mover set by a slower lane
		// (an abnormal retime, a GM or impact set) while the leg waited
		// for the lock has advanced the row's commit generation, so the
		// step's whole-value commits drop instead of overwriting it -
		// the #2047 overwrite hazard is now carried by the generation
		// guard rather than by resolve-inside-the-lock timing.
		actor, ok := dispatch.view.actorAt(dispatch.index)
		if !ok {
			return
		}
		owned.advanceAndPublish(dispatch.division, actor, dispatch.players, nowMs, sessions, push)
	}
	stepped := 0
	phaseStarted = time.Now()
	queueStarted := time.Now()
	batches := ops.Monsters.behaviorBatchesForDivision(nowMs, ops.divisionID)
	// The batches die with this leg: their refs backings return to
	// the scratch pool once every batch below is dispatched (#2030).
	defer ops.Monsters.releaseBehaviorBatches(batches)
	// The build consumed the actors' dues without re-arming them
	// (behaviorBatchesForDivision): the sweep after the dispatch
	// arms the remainder, and this backstop runs the same sweep on
	// the way out when a step panics mid-leg, so a popped actor is
	// never left unscheduled. An actor a door re-armed meanwhile is
	// skipped by the sweep either way.
	remaindersArmed := false
	defer func() {
		if !remaindersArmed {
			ops.Monsters.armBehaviorRemainders(batches, nowMs)
		}
	}()
	stats.queue = time.Since(queueStarted)
	for batchIndex, batch := range batches {
		divisionID := batch.key.division
		divisionSet[divisionID] = true
		ops.activity.askWorld(activityWorld{divisionID, uint32(batch.key.lease.ID), batch.key.lease.Generation})
		// The suspended actors' activity re-check is answered for the
		// whole batch first (monsterpending_recheck.go); only the
		// actors it leaves are snapshot and stepped. The batch keeps
		// the shortened list: the settled ones hold the due their
		// commit armed, and the remainder sweep has nothing to add.
		batch.actors = ops.recheckPendingBatch(batch, nowMs, stats)
		batches[batchIndex].actors = batch.actors
		var players []playerPose
		for _, session := range combatants {
			if session.DivisionID != divisionID || session.Population != batch.key.lease || !session.CombatEligible {
				continue
			}
			player := sessionPlayerPose(session, nowMs)
			if ops.FirstAttackGuard != nil {
				player.Guard = ops.FirstAttackGuard(divisionID, player.Gid, nowMs)
			}
			players = append(players, player)
			players = appendCompanionTargets(players, ops.companionTargets(divisionID, player, nowMs))
		}
		view := ops.Monsters.legSnapshot(batch.key, batch.actors)
		if ops.pooledLeg() && len(batch.actors) > 1 {
			stepped = ops.runPooledBatch(batch, view, players, sessions, push, nowMs, stats, stepped)
			ops.Monsters.releaseLegSnapshot(view)
			continue
		}
		for index, ref := range batch.actors {
			// The scheduler carries identities, not a second copy of the world.
			// All dispatch decides is that the actor is still there: a
			// despawn between scheduling and dispatch drops it here. Its
			// instance and mover come from the batch's leg-start
			// snapshot; a slower lane's set since then has advanced the
			// mover's commit generation, so the step's whole-value
			// commits drop instead of overwriting the newer value
			// (#2047 review, carried by the generation since #2137
			// slice 1). The direct path below reads the same view at
			// the same adjacency it always had.
			if !ops.Monsters.behaviorActorLiveAt(batch.key, ref) {
				continue
			}
			if ops.noteResumed(batch.key, ref.gid) {
				stats.resumed++
			}
			if stepped++; stepped%monsterLegStepActors == 0 {
				ops.watch.step()
			}
			if ops.RunAction != nil {
				dispatch.division, dispatch.key, dispatch.index, dispatch.players, dispatch.view = divisionID, batch.key, index, players, view
				advanceBefore := stats.advance
				laneStarted := time.Now()
				ops.RunAction(divisionID, act)
				// The lane wall outside the step: the per-actor
				// division-lock entry plus the snapshot read act
				// runs before advanceAndPublish.
				if outside := time.Since(laneStarted) - (stats.advance - advanceBefore); outside > 0 {
					stats.lane += outside
				}
				continue
			}
			actor, ok := view.actorAt(index)
			if !ok {
				continue
			}
			ops.advanceAndPublish(divisionID, actor, players, nowMs, sessions, push)
		}
		ops.Monsters.releaseLegSnapshot(view)
	}
	queueArmStarted := time.Now()
	ops.Monsters.armBehaviorRemainders(batches, nowMs)
	stats.queue += time.Since(queueArmStarted)
	remaindersArmed = true
	stats.batch = time.Since(phaseStarted)
	stats.actors = stepped
	for divisionID := range divisionSet {
		if frames := ops.Monsters.DrainUniqueNotices(divisionID); len(frames) > 0 {
			push.PushToDivision(divisionID, frames, "")
		}
	}
	stats.total = time.Since(legStarted)
	var navMissesEnd uint64
	if ops.NavCacheMisses != nil {
		navMissesEnd = ops.NavCacheMisses()
	}
	stats.finish(navMissesEnd, readGCSample())
	ops.Navigation.leg(stats)
	ops.lastLeg = stats
	if stats.total >= SlowHookThreshold {
		stats.logSlow(ops.divisionID)
	}
}
