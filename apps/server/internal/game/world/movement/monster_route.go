/*
===========================================================================

monster_route.go - bounded A* local detour search for blocked monster goals

An AI-only local detour policy. Player click-to-move still uses its native
single clipped segment. All search edges go through the same authored
geometry/surface owner as ordinary movement; endpoints alone prove nothing.

===========================================================================
*/
package movement

import (
	"container/heap"
	"math"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

const monsterRouteProbeBudget = 256

// monsterGlobalRouteProbeBudget bounds the homing search
// (PlanMonsterGlobalRoute): a returning mover can be regions from home
// behind a terrain barrier, and the search only runs when the bounded
// search and the march have both failed. Sized from the real Taklamakan
// wedge (#4124): the rim-to-nest crossing measures 117046 probes because
// the Euclidean heuristic floods the gorge basin before it finds the
// pass, so the budget carries ~2.2x that with room for longer kites.
// The search runs in slices across ticks (monster_route_homing.go); in
// one call an exhausted search held the live tick for up to 1 s. See
// TestGlobalRouteCrossesTheGorgeRim.
const monsterGlobalRouteProbeBudget = 262144
const monsterRouteGrid = 32.0

/*
================
PlanMonsterRoute
================
*/
func (v *WaterValidator) PlanMonsterRoute(from, goal monster.Pose) *monster.NavigationRoute {
	return findMonsterRouteWithBudget(from, goal, v.probeMonsterRouteStep, monsterRouteProbeBudget)
}

/*
================
PlanMonsterGlobalRoute

The same search with the homing budget (monsterGlobalRouteProbeBudget):
a returning mover's stored goal can sit regions away behind terrain the
local budget cannot see around (the Taklamakan gorge rim, #4124), where
the native mover simply walks its global path home. Only the returning
mode's fallback consults this entry (simulation's globalRoute); chase
and follow keep the bounded budget above. Geometry, tie order and the
step probe are identical; the budget, and the slicing that answers
NavigationSearchPending until the search settles, are the difference.
================
*/
func (v *WaterValidator) PlanMonsterGlobalRoute(from, goal monster.Pose) *monster.NavigationRoute {
	return v.homing.plan(homingKey{from, goal}, func() *routeSearch {
		return newRouteSearch(from, goal, v.probeMonsterRouteStep, monsterGlobalRouteProbeBudget)
	}, monsterGlobalRouteSliceProbes)
}

/*
================
routeNode
================
*/
type routeNode struct {
	pose           monster.Pose
	x, z           int
	cost, priority float64
	parent         *routeNode
	order          int
}

/*
================
routeQueue
================
*/
type routeQueue []*routeNode

/*
================
Len
================
*/
func (q routeQueue) Len() int { return len(q) }

/*
================
Less
================
*/
func (q routeQueue) Less(i, j int) bool {
	if q[i].priority == q[j].priority {
		return q[i].order < q[j].order
	}
	return q[i].priority < q[j].priority
}

/*
================
Swap
================
*/
func (q routeQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }

/*
================
Push
================
*/
func (q *routeQueue) Push(v any) { *q = append(*q, v.(*routeNode)) }

/*
================
Pop
================
*/
func (q *routeQueue) Pop() any { n := len(*q) - 1; v := (*q)[n]; *q = (*q)[:n]; return v }

/*
================
routeDistance
================
*/
func routeDistance(a, b monster.Pose) float64 {
	return worldgeom.Distance(worldgeom.RegionXZ{RegionID: a.RegionID, X: a.X, Z: a.Z}, worldgeom.RegionXZ{RegionID: b.RegionID, X: b.X, Z: b.Z})
}

/*
================
routeNormalize
================
*/
func routeNormalize(p monster.Pose) monster.Pose {
	r := worldgeom.NormalizeOutdoor(worldgeom.RegionXZ{RegionID: p.RegionID, X: math.Round(p.X), Z: math.Round(p.Z)})
	p.RegionID, p.X, p.Z = r.RegionID, r.X, r.Z
	return p
}

/*
================
routeClear
================
*/
func routeClear(p *monster.NavigationPath, goal monster.Pose) bool {
	return p != nil && p.Result()&(monster.NavResultBlocked|monster.NavResultClipped) == 0 && routeDistance(p.Rest(), goal) < .01
}

/*
================
findMonsterRoute
================
*/
func findMonsterRoute(from, goal monster.Pose, probe func(monster.Pose, monster.Pose) *monster.NavigationPath) *monster.NavigationRoute {
	return findMonsterRouteWithStep(from, goal, func(a, b monster.Pose, _ bool) (float64, bool, bool) {
		p := probe(a, b)
		if p == nil {
			return 0, false, false
		}
		return p.Rest().Y, routeClear(p, b), true
	})
}

/*
================
findMonsterRouteWithStep
================
*/
func findMonsterRouteWithStep(from, goal monster.Pose, step func(monster.Pose, monster.Pose, bool) (float64, bool, bool)) *monster.NavigationRoute {
	return findMonsterRouteWithBudget(from, goal, step, monsterRouteProbeBudget)
}

/*
================
findMonsterRouteWithBudget

The detour search itself, parameterized only by its probe budget: the
bounded local search (PlanMonsterRoute) and the homing search
(PlanMonsterGlobalRoute) share every rule but how far they may look.
Run to completion in one call; the homing search runs the same search
in slices instead (monster_route_homing.go).
================
*/
func findMonsterRouteWithBudget(from, goal monster.Pose, step func(monster.Pose, monster.Pose, bool) (float64, bool, bool), budget int) *monster.NavigationRoute {
	route, _ := newRouteSearch(from, goal, step, budget).advance(math.MaxInt)
	return route
}

// routeKey is one search cell: grid offsets from the start and the
// rounded height, so stacked floors stay apart.
type routeKey struct{ x, z, y int }

/*
================
routeSearch

The detour search's whole state between slices: the open queue, the
best cost per cell and the probes spent. A search paused between two
node expansions resumes exactly where it stopped, so a sliced search
returns the same route, probe for probe, as one run to completion.
================
*/
type routeSearch struct {
	goal   monster.Pose
	step   func(monster.Pose, monster.Pose, bool) (float64, bool, bool)
	budget int
	count  int
	start  *routeNode
	open   routeQueue
	best   map[routeKey]float64
	result *monster.NavigationRoute
}

/*
================
newRouteSearch

Opens a search with its first, direct probe. A clear straight line or
unavailable geometry settles it at once.
================
*/
func newRouteSearch(from, goal monster.Pose, step func(monster.Pose, monster.Pose, bool) (float64, bool, bool), budget int) *routeSearch {
	goal = routeNormalize(goal)
	s := &routeSearch{goal: goal, step: step, budget: budget}
	_, firstClear, firstAvailable := step(from, goal, true)
	if !firstAvailable {
		// Unavailable geometry is not an obstacle to guess around.
		s.result = monster.UnresolvedNavigationRoute(goal, monster.NavigationGeometryUnavailable).WithProbes(1)
		return s
	}
	if firstClear {
		s.result = monster.NewNavigationRoute(goal, []monster.Pose{goal}).WithProbes(1)
		return s
	}
	// Fixed work budget and deterministic tie order bound crowded-server cost.
	// Retried requests are paced and cached by the mover, not by this adapter.
	s.count = 1
	s.start = &routeNode{pose: from, priority: routeDistance(from, goal)}
	s.open = routeQueue{s.start}
	s.best = map[routeKey]float64{{0, 0, int(math.Round(from.Y))}: 0}
	return s
}

/*
================
routeSearch.advance

Expands nodes until the search settles or this call has spent slice
probes; returns the settled route (nil while it is still open) and the
probes this call spent. The pause is only ever taken before a pop, and
one expansion can overrun slice by its eight neighbour probes.
================
*/
func (s *routeSearch) advance(slice int) (*monster.NavigationRoute, int) {
	if s.result != nil {
		return s.result, 0
	}
	spentFrom := s.count
	directions := [8][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}, {1, 1}, {-1, 1}, {-1, -1}, {1, -1}}
	for s.open.Len() > 0 && s.count < s.budget {
		if s.count-spentFrom >= slice {
			return nil, s.count - spentFrom
		}
		n := heap.Pop(&s.open).(*routeNode)
		if n.cost > s.best[routeKey{n.x, n.z, int(math.Round(n.pose.Y))}] {
			continue
		}
		if n != s.start {
			s.count++
			if _, clear, _ := s.step(n.pose, s.goal, false); clear {
				points := []monster.Pose{s.goal}
				for at := n; at != s.start; at = at.parent {
					points = append(points, at.pose)
				}
				for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
					points[i], points[j] = points[j], points[i]
				}
				return s.settle(monster.NewNavigationRoute(s.goal, points).WithProbes(s.count)), s.count - spentFrom
			}
		}
		for _, d := range directions {
			if s.count >= s.budget {
				break
			}
			x, z := n.x+d[0], n.z+d[1]
			p := s.start.pose
			p.X += float64(x) * monsterRouteGrid
			p.Z += float64(z) * monsterRouteGrid
			p.Y = n.pose.Y
			p = routeNormalize(p)
			s.count++
			restY, clear, _ := s.step(n.pose, p, false)
			if !clear {
				continue
			}
			p.Y = restY
			cost := n.cost + routeDistance(n.pose, p)
			k := routeKey{x, z, int(math.Round(p.Y))}
			if previous, ok := s.best[k]; ok && previous <= cost {
				continue
			}
			s.best[k] = cost
			heap.Push(&s.open, &routeNode{pose: p, x: x, z: z, cost: cost, priority: cost + routeDistance(p, s.goal), parent: n, order: s.count})
		}
	}
	status := monster.NavigationRouteBlocked
	if s.count >= s.budget {
		status = monster.NavigationSearchExhausted
	}
	return s.settle(monster.UnresolvedNavigationRoute(s.goal, status).WithProbes(s.count)), s.count - spentFrom
}

/*
================
routeSearch.settle

Records the final route and drops the queue and cell table, which can
hold a quarter million nodes on an exhausted homing search.
================
*/
func (s *routeSearch) settle(route *monster.NavigationRoute) *monster.NavigationRoute {
	s.result = route
	s.open, s.best, s.start = nil, nil, nil
	return route
}
