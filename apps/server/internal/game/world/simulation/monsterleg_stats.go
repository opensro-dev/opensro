/*
===========================================================================

monsterleg_stats.go - slow-leg attribution for the monster leg (#1820 C)

Metrics only: no behaviour reads these numbers. RunMonsterLeg fills one
record per leg (phase walls - the batch phase split into its queue-build
wall, the action lane's wall outside the step, and the step wall itself -
woken/resumed counts, route plans, memo hits, wander clips, the movement
layer's cold-cache delta and the GC pause/assist delta) and, when the leg
reaches SlowHookThreshold, emits
one structured line through the package log. The next optimization step
(the dense lookup index, a shared navigation field, memoizing the
return path) is chosen from which phase dominates those lines.

GC is sampled once per leg at its start and end, never per actor.

===========================================================================
*/

package simulation

import (
	"fmt"
	"math"
	"runtime/metrics"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/world/monster"
)

// The runtime metric names sampled around each leg.
const (
	gcPausesMetric = "/gc/pauses:seconds"
	gcAssistMetric = "/cpu/classes/gc/mark/assist:cpu-seconds"
)

/*
================
gcSample

Cumulative GC pause and mark-assist CPU since boot, in seconds. The
pause total is the midpoint estimate over the runtime histogram (the
top bucket prices at its lower edge), so it attributes a leg, it does
not audit the collector.
================
*/
type gcSample struct {
	pause, assist float64
}

/*
================
readGCSample
================
*/
func readGCSample() gcSample {
	samples := []metrics.Sample{{Name: gcPausesMetric}, {Name: gcAssistMetric}}
	metrics.Read(samples)
	var out gcSample
	if samples[0].Value.Kind() == metrics.KindFloat64Histogram {
		h := samples[0].Value.Float64Histogram()
		for i, count := range h.Counts {
			if count == 0 {
				continue
			}
			mid := h.Buckets[i]
			if hi := h.Buckets[i+1]; !math.IsInf(hi, 1) {
				mid = (mid + hi) / 2
			}
			out.pause += float64(count) * mid
		}
	}
	if samples[1].Value.Kind() == metrics.KindFloat64 {
		out.assist = samples[1].Value.Float64()
	}
	return out
}

/*
================
legStats

One RunMonsterLeg's attribution. advance is nested inside batch (both
advance paths accumulate into it). Written only by the shard goroutine
running the leg, through the ops copy it owns.
================
*/
type legStats struct {
	total                time.Duration
	dormancy, visibility time.Duration
	batch, advance       time.Duration
	// queue is the batch phase's queue-build wall (behaviorBatchesForDivision:
	// due pops, reschedules, batch sort); lane is the RunAction wall outside
	// the step (per-actor division-lock entry plus the in-lock actor
	// resolution). batch - advance - queue - lane is the unmeasured residue
	// (player-pose building, liveness probes) the next timer round splits
	// if a slow leg ever hides there.
	queue, lane       time.Duration
	actors            int
	woken, resumed    int
	plans             [navCallerCount][navOutcomeCount]int
	planProbes        [navCallerCount]int
	planTime          [navCallerCount]time.Duration
	memo              [navCallerCount]int
	clips             [navCallerCount]int
	maxPlanTime       time.Duration
	maxPlanProbes     int
	gcPause, gcAssist time.Duration
	navMisses         uint64
	gcStart           gcSample
	navStart          uint64
}

/*
================
legStats.notePlan

One PlanRoute call: its caller, outcome, probe count and wall time.
================
*/
func (s *legStats) notePlan(caller navCaller, route *monster.NavigationRoute, elapsed time.Duration) {
	if s == nil {
		return
	}
	outcome, probes := navOutcomeProbes(route)
	s.plans[caller][outcome]++
	s.planProbes[caller] += probes
	s.planTime[caller] += elapsed
	if elapsed > s.maxPlanTime {
		s.maxPlanTime = elapsed
	}
	if probes > s.maxPlanProbes {
		s.maxPlanProbes = probes
	}
}

/*
================
legStats.noteMemo

One failed-route memo hit (part B): a retry answered without a search.
================
*/
func (s *legStats) noteMemo(caller navCaller) {
	if s != nil {
		s.memo[caller]++
	}
}

/*
================
legStats.noteClip

One planPath call (wander and sight probes included).
================
*/
func (s *legStats) noteClip(caller navCaller) {
	if s != nil {
		s.clips[caller]++
	}
}

/*
================
legStats.noteAdvance

advanceAndPublish time, cumulative across the direct dispatch and the
RunAction closure path.
================
*/
func (s *legStats) noteAdvance(elapsed time.Duration) {
	if s != nil {
		s.advance += elapsed
	}
}

/*
================
legStats.finish

Closes the record with the leg's closing samples: the cold-cache delta
and the GC pause/assist spent inside the leg.
================
*/
func (s *legStats) finish(navMissesEnd uint64, end gcSample) {
	s.navMisses = navMissesEnd - s.navStart
	s.gcPause = time.Duration((end.pause - s.gcStart.pause) * float64(time.Second))
	s.gcAssist = time.Duration((end.assist - s.gcStart.assist) * float64(time.Second))
}

/*
================
legStats.logSlow

The one structured line for a leg at SlowHookThreshold or beyond. Every
value is this leg's own count or wall time.
================
*/
func (s *legStats) logSlow(divisionID string) {
	var plans, outcomes, clips strings.Builder
	for c := range navCallerCount {
		calls, probes := 0, s.planProbes[c]
		for o := range navOutcomeCount {
			calls += s.plans[c][o]
			if s.plans[c][o] > 0 {
				fmt.Fprintf(&outcomes, "%s.%s:%d ", navCallerNames[c], navOutcomeNames[o], s.plans[c][o])
			}
		}
		if calls > 0 {
			fmt.Fprintf(&plans, "%s:%d/%dprobes/%v/memo%d ", navCallerNames[c], calls, probes,
				s.planTime[c].Round(time.Microsecond), s.memo[c])
		}
		if s.clips[c] > 0 {
			fmt.Fprintf(&clips, "%s:%d ", navCallerNames[c], s.clips[c])
		}
	}
	log.Warnf("simulation: slow monster leg division=%s total=%v dormancy=%v visibility=%v batch=%v advance=%v "+
		"actors=%d woken=%d resumed=%d plans{%s} outcomes{%s} maxplan=%v/%dprobes clips{%s} navmiss=+%d gcpause=%v gcassist=%v "+
		"queue=%v lane=%v",
		divisionID, s.total.Round(time.Microsecond), s.dormancy.Round(time.Microsecond), s.visibility.Round(time.Microsecond),
		s.batch.Round(time.Microsecond), s.advance.Round(time.Microsecond), s.actors, s.woken, s.resumed,
		strings.TrimSpace(plans.String()), strings.TrimSpace(outcomes.String()),
		s.maxPlanTime.Round(time.Microsecond), s.maxPlanProbes, strings.TrimSpace(clips.String()),
		s.navMisses, s.gcPause.Round(time.Microsecond), s.gcAssist.Round(time.Microsecond),
		s.queue.Round(time.Microsecond), s.lane.Round(time.Microsecond))
}

/*
================
MonsterMoverOps.planRouteTimed

PlanRoute with the leg's attribution when a leg is running (part C);
outside a leg it is the plain call, so fixtures cost nothing.
================
*/
func (ops *MonsterMoverOps) planRouteTimed(caller navCaller, from monster.Pose, fromOwner NavOwner, goal monster.Pose) *monster.NavigationRoute {
	if ops.leg == nil {
		return ops.PlanRoute(from, fromOwner, goal)
	}
	started := time.Now()
	route := ops.PlanRoute(from, fromOwner, goal)
	ops.leg.notePlan(caller, route, time.Since(started))
	return route
}

/*
================
MonsterMoverOps.noteWoken

Folds one prepareDormancy pass's woken sets into the leg record and
into the pending-resume set the actor loop drains (resumed counts).
================
*/
func (ops *MonsterMoverOps) noteWoken(stats *legStats, sets map[populationKey]map[uint32]struct{}) {
	for key, set := range sets {
		stats.woken += len(set)
		if len(set) == 0 {
			continue
		}
		if ops.resumePending == nil {
			ops.resumePending = make(map[populationKey]map[uint32]struct{})
		}
		pending := ops.resumePending[key]
		if pending == nil {
			pending = make(map[uint32]struct{}, len(set))
			ops.resumePending[key] = pending
		}
		for gid := range set {
			pending[gid] = struct{}{}
		}
	}
}

/*
================
MonsterMoverOps.noteResumed

True (once) when a mover woken from dormancy reaches its first
dispatch. An actor removed before dispatching keeps its entry; it is
bounded by the woken cohort and drops on the actor's next appearance.
================
*/
func (ops *MonsterMoverOps) noteResumed(key populationKey, gid uint32) bool {
	set := ops.resumePending[key]
	if set == nil {
		return false
	}
	if _, ok := set[gid]; !ok {
		return false
	}
	delete(set, gid)
	if len(set) == 0 {
		delete(ops.resumePending, key)
	}
	return true
}
