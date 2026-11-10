/*
===========================================================================

navigation_metrics.go - monster route plans, path clips and the leg's
own census, counted

Read-only instrument for the server-lag work (P4a): how often monsters
plan a route, what each plan cost in path queries and how it ended, and how
many path clips each caller makes. Counts are since boot; compare windows
by diffing two reads. Atomics only: the tick never waits on a reader.

The leg census has its own snapshot (LegSnapshot, the monster_leg
metrics block): how many actors a leg dispatched, stepped and settled
without a step, which rest guard refused each resting step, and how
the steps split by mover mode. Until 2026-11-10 those counts existed
only on the slow-leg log line, so a live capture could not say what
the monster leg's actors were doing, only that there were many; the
census first rode this file's navigation block and moved out on
2026-11-11 (#3119) - navigation owns pathfinding, the census is leg
dispatch.

===========================================================================
*/

package simulation

import (
	"sync/atomic"

	"opensro.online/server/internal/game/world/monster"
)

// navCaller names who asked for a plan or a clip.
type navCaller uint8

const (
	navChase navCaller = iota
	navFollow
	navReturn
	navWander
	navSight
	navOther
	navCallerCount
)

var navCallerNames = [navCallerCount]string{"chase", "follow", "return", "wander", "sight", "other"}

// navOutcome is how one route plan ended.
type navOutcome uint8

const (
	navReadyDirect navOutcome = iota
	navReadyDetour
	navBlocked
	navExhausted
	navUnavailable
	navOutcomeCount
)

var navOutcomeNames = [navOutcomeCount]string{"ready_direct", "ready_detour", "blocked", "exhausted", "unavailable"}

// navProbeBounds are the upper bounds of the probes-per-plan histogram;
// one more bucket holds the rest (the 256 budget, monster_route.go).
var navProbeBounds = [...]int{1, 8, 32, 128}

var navProbeNames = [len(navProbeBounds) + 1]string{"le1", "le8", "le32", "le128", "gt128"}

/*
================
NavigationMetrics
================
*/
type NavigationMetrics struct {
	plans    [navCallerCount][navOutcomeCount]atomic.Uint64
	probes   atomic.Uint64
	byCost   [len(navProbeBounds) + 1]atomic.Uint64
	clips    [navCallerCount]atomic.Uint64
	memoHits [navCallerCount]atomic.Uint64

	legs, legActors, legSteps, legRechecked atomic.Uint64
	legCadenceArms, legSuspended            atomic.Uint64
	legRest                                 [restGuardCount]atomic.Uint64
	legModeSteps                            [stepModeCount]atomic.Uint64
}

/*
================
callerForMode
================
*/
func callerForMode(mode monster.MoverMode) navCaller {
	switch mode {
	case monster.MoverChasing:
		return navChase
	case monster.MoverFollowing:
		return navFollow
	case monster.MoverReturning:
		return navReturn
	case monster.MoverWandering:
		return navWander
	}
	return navOther
}

/*
================
NavigationMetrics.plan

One route plan and its result. A nil receiver records nothing.
================
*/
/*
================
navOutcomeProbes

How one route plan ended and what it cost in search expansions. Shared
by the boot counters and the per-leg attribution (#1820 part C).
================
*/
func navOutcomeProbes(route *monster.NavigationRoute) (navOutcome, int) {
	if route == nil {
		return navUnavailable, 0
	}
	probes := route.Probes()
	switch route.Status() {
	case monster.NavigationRouteReady:
		if route.Len() > 1 {
			return navReadyDetour, probes
		}
		return navReadyDirect, probes
	case monster.NavigationRouteBlocked:
		return navBlocked, probes
	case monster.NavigationSearchExhausted:
		return navExhausted, probes
	}
	return navUnavailable, probes
}

func (m *NavigationMetrics) plan(caller navCaller, route *monster.NavigationRoute) {
	if m == nil {
		return
	}
	outcome, probes := navOutcomeProbes(route)
	m.plans[caller][outcome].Add(1)
	m.probes.Add(uint64(probes))
	bucket := len(navProbeBounds)
	for i, bound := range navProbeBounds {
		if probes <= bound {
			bucket = i
			break
		}
	}
	m.byCost[bucket].Add(1)
}

/*
================
NavigationMetrics.clip
================
*/
func (m *NavigationMetrics) clip(caller navCaller) {
	if m != nil {
		m.clips[caller].Add(1)
	}
}

/*
================
NavigationMetrics.memo

One failed-route memo hit: a retry answered from the mover's record
instead of a recomputed search (#1820 part B).
================
*/
func (m *NavigationMetrics) memo(caller navCaller) {
	if m != nil {
		m.memoHits[caller].Add(1)
	}
}

/*
================
NavigationMetrics.leg

One finished leg's census, folded into the since-boot counters.
================
*/
func (m *NavigationMetrics) leg(stats *legStats) {
	if m == nil || stats == nil {
		return
	}
	m.legs.Add(1)
	m.legActors.Add(uint64(stats.actors))
	m.legSteps.Add(uint64(stats.steps))
	m.legRechecked.Add(uint64(stats.rechecked))
	m.legCadenceArms.Add(uint64(stats.cadenceArms))
	m.legSuspended.Add(uint64(stats.gateSuspended))
	for guard := range stats.restGuards {
		if count := stats.restGuards[guard]; count != 0 {
			m.legRest[guard].Add(uint64(count))
		}
	}
	for mode := range stats.modeSteps {
		if count := stats.modeSteps[mode]; count != 0 {
			m.legModeSteps[mode].Add(uint64(count))
		}
	}
}

/*
================
NavigationMetrics.Snapshot

Flat counters for /transport/metrics: plan.<caller>.<outcome>,
route_probes, plan_probes.<bucket>, clip.<caller>. Zero counters are
left out. The leg census is LegSnapshot below, not this block (#3119).
================
*/
func (m *NavigationMetrics) Snapshot() map[string]uint64 {
	out := make(map[string]uint64)
	if m == nil {
		return out
	}
	put := func(key string, value uint64) {
		if value != 0 {
			out[key] = value
		}
	}
	for c := range navCallerCount {
		for o := range navOutcomeCount {
			put("plan."+navCallerNames[c]+"."+navOutcomeNames[o], m.plans[c][o].Load())
		}
		put("clip."+navCallerNames[c], m.clips[c].Load())
		put("plan."+navCallerNames[c]+".memo", m.memoHits[c].Load())
	}
	put("route_probes", m.probes.Load())
	for i := range m.byCost {
		put("plan_probes."+navProbeNames[i], m.byCost[i].Load())
	}
	return out
}

/*
================
NavigationMetrics.LegSnapshot

The monster_leg document (#3119): the leg census under its own
block - legs folded (count), actors dispatched (actors), step
invocations (steps) and the steps split by the mode each step read
(mode.<bucket>: idle, wandering, chasing, attacking, returning,
pending, other), re-checks settled without a step (rechecked),
cadence arms (cadence_arms), gate suspensions (suspended), and the
rest-guard partition of the resting steps (rest.<guard>). Zero
counters omitted, same convention as Snapshot.
================
*/
func (m *NavigationMetrics) LegSnapshot() map[string]uint64 {
	if m == nil {
		return nil
	}
	out := make(map[string]uint64, 32)
	put := func(key string, value uint64) {
		if value != 0 {
			out[key] = value
		}
	}
	put("count", m.legs.Load())
	put("actors", m.legActors.Load())
	put("steps", m.legSteps.Load())
	put("rechecked", m.legRechecked.Load())
	put("cadence_arms", m.legCadenceArms.Load())
	put("suspended", m.legSuspended.Load())
	for guard := range m.legRest {
		put("rest."+restGuardNames[guard], m.legRest[guard].Load())
	}
	for mode := range m.legModeSteps {
		put("mode."+stepModeNames[mode], m.legModeSteps[mode].Load())
	}
	return out
}
