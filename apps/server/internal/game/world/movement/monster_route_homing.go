/*
===========================================================================

monster_route_homing.go - the homing search, run in slices across ticks

The homing search (PlanMonsterGlobalRoute, #4124) may spend a quarter
million probes. Run in one call it held the world tick for 0.7 to 1.0 s
on the live shard (2026-11-27, three exhausted searches: every player
froze). Each call now advances the mover's open search by one slice and
answers NavigationSearchPending until it settles; the route it settles
on is the one-shot search's, probe for probe (routeSearch).

A search is keyed by the exact from and goal poses: a waiting mover
stands still, so its retries name the same search, and a mover that
moved or re-aimed starts a new one. Searches nobody advances are
dropped (homingSearchIdleCalls), and at most maxHomingSearches stay
open, because an exhausted one holds every node it expanded.

A full table never evicts a search in progress: a mover with no slot
waits (pending, no probes) until one settles or goes idle. Evicting
instead livelocks once more movers home than there are slots (each new
search throws out an older one, so none ever settles). Each open
search is advanced once per tick by its own mover (busy, and the
mover's one-tick retry), so one tick spends at most maxHomingSearches
slices on homing.

===========================================================================
*/
package movement

import (
	"sync"

	"opensro.online/server/internal/game/world/monster"
)

const (
	// monsterGlobalRouteSliceProbes is one call's share of a homing
	// search: about 15 ms of probes at the live rate (262144 probes in
	// 982 ms, 2026-11-27), so the gorge crossing (117046 probes) settles
	// in 29 calls and an unreachable home in 64.
	monsterGlobalRouteSliceProbes = 4096
	// maxHomingSearches bounds the open searches, and with them one
	// tick's homing work (two slices, about 30 ms) and memory: an
	// exhausted search holds up to the whole budget's nodes.
	maxHomingSearches = 2
	// homingSearchIdleCalls drops a search no call has advanced in this
	// many homing calls: its mover died, moved or found another way.
	homingSearchIdleCalls = 512
)

// homingKey names one search: the waiting mover's pose and its goal.
type homingKey struct{ from, goal monster.Pose }

// homingEntry is one open search. busy marks a slice in progress on a
// tick shard, so a second shard never advances it at the same time.
type homingEntry struct {
	search   *routeSearch
	busy     bool
	lastCall uint64
}

/*
================
homingSearches

The open homing searches of one validator. mu guards the map and the
entry flags only; a slice runs with mu released.
================
*/
type homingSearches struct {
	mu    sync.Mutex
	calls uint64
	open  map[homingKey]*homingEntry
}

/*
================
homingSearches.plan

Advances the search for key by one slice, opening it with start when
none is open and a slot is free. Returns the settled route, or a
pending route carrying the probes this call spent (none while the
mover waits for a slot).
================
*/
func (h *homingSearches) plan(key homingKey, start func() *routeSearch, slice int) *monster.NavigationRoute {
	h.mu.Lock()
	h.calls++
	if h.open == nil {
		h.open = map[homingKey]*homingEntry{}
	}
	h.dropIdleLocked()
	entry := h.open[key]
	if entry == nil && len(h.open) < maxHomingSearches {
		entry = &homingEntry{}
		h.open[key] = entry
	}
	if entry == nil || entry.busy {
		h.mu.Unlock()
		return monster.UnresolvedNavigationRoute(routeNormalize(key.goal), monster.NavigationSearchPending)
	}
	entry.busy = true
	entry.lastCall = h.calls
	h.mu.Unlock()

	if entry.search == nil {
		entry.search = start()
	}
	route, spent := entry.search.advance(slice)

	h.mu.Lock()
	entry.busy = false
	if route != nil && h.open[key] == entry {
		delete(h.open, key)
	}
	h.mu.Unlock()
	if route == nil {
		return monster.UnresolvedNavigationRoute(entry.search.goal, monster.NavigationSearchPending).WithProbes(spent)
	}
	return route
}

/*
================
homingSearches.dropIdleLocked
================
*/
func (h *homingSearches) dropIdleLocked() {
	for key, entry := range h.open {
		if !entry.busy && h.calls-entry.lastCall > homingSearchIdleCalls {
			delete(h.open, key)
		}
	}
}
