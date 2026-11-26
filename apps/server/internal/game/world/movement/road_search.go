/*
===========================================================================

road_search.go - incremental, port-only caravan road detours

Uses the monster route queue and authored surface probes, but retains work
between ticks instead of spending a whole long-road search on one frame.
The ordinary monster planner and its fixed budget are unchanged.

===========================================================================
*/
package movement

import (
	"container/heap"
	"math"
	"opensro.online/server/internal/game/world/monster"
)

const (
	roadSearchProbeLimit = 524288
	// A weighted road search prefers forward coarse steps while fine steps retain narrow gates.
	roadSearchHeuristicWeight = 2.0
)

/*
================
RoadSearch
================
*/
type RoadSearch struct {
	from, goal monster.Pose
	step       func(monster.Pose, monster.Pose, bool) (float64, bool, bool)
	open       routeQueue
	best       map[[3]int]float64
	count      int
	grid       float64
	result     *monster.NavigationRoute
}

/*
================
NewRoadSearch
================
*/
func (v *WaterValidator) NewRoadSearch(from, goal monster.Pose) *RoadSearch {
	return newRoadSearch(from, goal, v.probeMonsterRouteStep)
}

/*
================
newRoadSearch
================
*/
func newRoadSearch(from, goal monster.Pose, step func(monster.Pose, monster.Pose, bool) (float64, bool, bool)) *RoadSearch {
	goal = routeNormalize(goal)
	start := &routeNode{pose: from, priority: routeDistance(from, goal) * roadSearchHeuristicWeight}
	grid := monsterRouteGrid
	if routeDistance(from, goal) > 2048 {
		grid *= 4
	}
	return &RoadSearch{from: from, goal: goal, step: step, grid: grid, open: routeQueue{start}, best: map[[3]int]float64{{0, 0, int(math.Round(from.Y))}: 0}}
}

/*
================
Advance

Nil means the next frame must resume the search. A finished result,
including failure, remains stable for the caller's immutable geometry.
================
*/
func (s *RoadSearch) Advance(nodes int) *monster.NavigationRoute {
	if s.result != nil {
		return s.result
	}
	if s.count == 0 {
		s.count++
		_, _, available := s.step(s.from, s.from, true)
		if !available {
			s.result = monster.UnresolvedNavigationRoute(s.goal, monster.NavigationGeometryUnavailable)
			return s.result
		}
		_, clear, _ := s.step(s.from, s.goal, false)
		if clear {
			s.result = monster.NewNavigationRoute(s.goal, []monster.Pose{s.goal})
			return s.result
		}
	}
	directions := [8][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}, {1, 1}, {-1, 1}, {-1, -1}, {1, -1}}
	for expanded := 0; expanded < nodes && s.open.Len() > 0 && s.count < roadSearchProbeLimit; expanded++ {
		n := heap.Pop(&s.open).(*routeNode)
		if n.cost > s.best[[3]int{n.x, n.z, int(math.Round(n.pose.Y))}] {
			continue
		}
		s.count++
		if _, clear, _ := s.step(n.pose, s.goal, false); clear {
			points := []monster.Pose{s.goal}
			for at := n; at.parent != nil; at = at.parent {
				points = append(points, at.pose)
			}
			for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
				points[i], points[j] = points[j], points[i]
			}
			s.result = monster.NewNavigationRoute(s.goal, points).WithProbes(s.count)
			return s.result
		}
		for _, d := range directions {
			x, z := n.x+d[0], n.z+d[1]
			p := s.from
			p.X += float64(x) * s.grid
			p.Z += float64(z) * s.grid
			p.Y = n.pose.Y
			p = routeNormalize(p)
			s.count++
			y, clear, _ := s.step(n.pose, p, false)
			if !clear {
				continue
			}
			p.Y = y
			cost := n.cost + routeDistance(n.pose, p)
			key := [3]int{x, z, int(math.Round(y))}
			if old, ok := s.best[key]; ok && old <= cost {
				continue
			}
			s.best[key] = cost
			heap.Push(&s.open, &routeNode{pose: p, x: x, z: z, cost: cost, priority: cost + routeDistance(p, s.goal)*roadSearchHeuristicWeight, parent: n, order: s.count})
		}
	}
	if s.open.Len() == 0 || s.count >= roadSearchProbeLimit {
		if s.grid > 8 {
			s.grid /= 2
			s.count = 0
			s.open = routeQueue{&routeNode{pose: s.from, priority: routeDistance(s.from, s.goal) * roadSearchHeuristicWeight}}
			s.best = map[[3]int]float64{{0, 0, int(math.Round(s.from.Y))}: 0}
			return nil
		}
		status := monster.NavigationRouteBlocked
		if s.count >= roadSearchProbeLimit {
			status = monster.NavigationSearchExhausted
		}
		s.result = monster.UnresolvedNavigationRoute(s.goal, status).WithProbes(s.count)
		return s.result
	}
	return nil
}
