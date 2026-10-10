/*
===========================================================================

navigation_metrics.go - monster route plans and path clips, counted

Read-only instrument for the server-lag work (P4a): how often monsters
plan a route, what each plan cost in path queries and how it ended, and how
many path clips each caller makes. Counts are since boot; compare windows
by diffing two reads. Atomics only: the tick never waits on a reader.

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
	plans  [navCallerCount][navOutcomeCount]atomic.Uint64
	probes atomic.Uint64
	byCost [len(navProbeBounds) + 1]atomic.Uint64
	clips  [navCallerCount]atomic.Uint64
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
func (m *NavigationMetrics) plan(caller navCaller, route *monster.NavigationRoute) {
	if m == nil {
		return
	}
	outcome, probes := navUnavailable, 0
	if route != nil {
		probes = route.Probes()
		switch route.Status() {
		case monster.NavigationRouteReady:
			outcome = navReadyDirect
			if route.Len() > 1 {
				outcome = navReadyDetour
			}
		case monster.NavigationRouteBlocked:
			outcome = navBlocked
		case monster.NavigationSearchExhausted:
			outcome = navExhausted
		}
	}
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
NavigationMetrics.Snapshot

Flat counters for /transport/metrics: plan.<caller>.<outcome>,
route_probes, plan_probes.<bucket> and clip.<caller>. Zero counters are
left out.
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
	}
	put("route_probes", m.probes.Load())
	for i := range m.byCost {
		put("plan_probes."+navProbeNames[i], m.byCost[i].Load())
	}
	return out
}
