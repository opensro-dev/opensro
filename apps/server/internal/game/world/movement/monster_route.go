package movement

import (
	"container/heap"
	"math"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

// An AI-only local detour policy. Player click-to-move still uses its native
// single clipped segment. All search edges go through the same authored
// geometry/surface owner as ordinary movement; endpoints alone prove nothing.
const monsterRouteProbeBudget = 256
const monsterRouteGrid = 32.0

func (v *WaterValidator) PlanMonsterRoute(from, goal monster.Pose) *monster.NavigationRoute {
	return findMonsterRoute(from, goal, v.PlanMonsterPath)
}

type routeNode struct {
	pose           monster.Pose
	x, z           int
	cost, priority float64
	parent         *routeNode
	order          int
}
type routeQueue []*routeNode

func (q routeQueue) Len() int { return len(q) }
func (q routeQueue) Less(i, j int) bool {
	if q[i].priority == q[j].priority {
		return q[i].order < q[j].order
	}
	return q[i].priority < q[j].priority
}
func (q routeQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *routeQueue) Push(v any)   { *q = append(*q, v.(*routeNode)) }
func (q *routeQueue) Pop() any     { n := len(*q) - 1; v := (*q)[n]; *q = (*q)[:n]; return v }
func routeDistance(a, b monster.Pose) float64 {
	return worldgeom.Distance(worldgeom.RegionXZ{RegionID: a.RegionID, X: a.X, Z: a.Z}, worldgeom.RegionXZ{RegionID: b.RegionID, X: b.X, Z: b.Z})
}
func routeNormalize(p monster.Pose) monster.Pose {
	r := worldgeom.NormalizeOutdoor(worldgeom.RegionXZ{RegionID: p.RegionID, X: math.Round(p.X), Z: math.Round(p.Z)})
	p.RegionID, p.X, p.Z = r.RegionID, r.X, r.Z
	return p
}
func routeClear(p *monster.NavigationPath, goal monster.Pose) bool {
	return p != nil && p.Result()&(monster.NavResultBlocked|monster.NavResultClipped) == 0 && routeDistance(p.Rest(), goal) < .01
}

func findMonsterRoute(from, goal monster.Pose, probe func(monster.Pose, monster.Pose) *monster.NavigationPath) *monster.NavigationRoute {
	goal = routeNormalize(goal)
	first := probe(from, goal)
	if first == nil {
		return monster.UnresolvedNavigationRoute(goal, monster.NavigationGeometryUnavailable).WithProbes(1)
	} // unavailable geometry is not an obstacle to guess around
	if routeClear(first, goal) {
		return monster.NewNavigationRoute(goal, []monster.Pose{goal}).WithProbes(1)
	}
	// Fixed work budget and deterministic tie order bound crowded-server cost.
	// Retried requests are paced and cached by the mover, not by this adapter.
	count := 1
	start := &routeNode{pose: from, priority: routeDistance(from, goal)}
	open := routeQueue{start}
	type key struct{ x, z, y int }
	best := map[key]float64{{0, 0, int(math.Round(from.Y))}: 0}
	directions := [8][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}, {1, 1}, {-1, 1}, {-1, -1}, {1, -1}}
	for open.Len() > 0 && count < monsterRouteProbeBudget {
		n := heap.Pop(&open).(*routeNode)
		if n.cost > best[key{n.x, n.z, int(math.Round(n.pose.Y))}] {
			continue
		}
		if n != start {
			count++
			if p := probe(n.pose, goal); routeClear(p, goal) {
				points := []monster.Pose{goal}
				for at := n; at != start; at = at.parent {
					points = append(points, at.pose)
				}
				for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
					points[i], points[j] = points[j], points[i]
				}
				return monster.NewNavigationRoute(goal, points).WithProbes(count)
			}
		}
		for _, d := range directions {
			if count >= monsterRouteProbeBudget {
				break
			}
			x, z := n.x+d[0], n.z+d[1]
			p := from
			p.X += float64(x) * monsterRouteGrid
			p.Z += float64(z) * monsterRouteGrid
			p.Y = n.pose.Y
			p = routeNormalize(p)
			count++
			path := probe(n.pose, p)
			if !routeClear(path, p) {
				continue
			}
			p.Y = path.Rest().Y
			cost := n.cost + routeDistance(n.pose, p)
			k := key{x, z, int(math.Round(p.Y))}
			if previous, ok := best[k]; ok && previous <= cost {
				continue
			}
			best[k] = cost
			heap.Push(&open, &routeNode{pose: p, x: x, z: z, cost: cost, priority: cost + routeDistance(p, goal), parent: n, order: count})
		}
	}
	status := monster.NavigationRouteBlocked
	if count >= monsterRouteProbeBudget {
		status = monster.NavigationSearchExhausted
	}
	return monster.UnresolvedNavigationRoute(goal, status).WithProbes(count)
}
