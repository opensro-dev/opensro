/*
===========================================================================

path.go - a bounded route search over the server navigation geometry

The bot walks around an obstacle with a route of legal chords. The search
mirrors the server's monster route policy (movement/monster_route.go): a
bounded grid A* with eight directions and a fixed probe budget. The edge test
is the caller's chord validator, so the brain stays free of the movement
package and a test injects a fake.

The last waypoint equals the goal. A direct legal chord returns one waypoint.

Phase 4 of issue #74 (issue #98).

===========================================================================
*/
package bot

import (
	"container/heap"
	"math"

	"opensro.online/server/internal/game/world/simulation"
)

// Point is one position in the region-local frame the object-list rows and
// the 0x7738 body share.
type Point struct {
	Region uint16
	X      float64
	Y      float64
	Z      float64
}

// ChordValidator reports whether a straight move from -> to is a legal chord
// the server accepts. A blocked chord reports false.
type ChordValidator interface {
	Walkable(from, to Point) bool
}

// Pathfinder plans a route of waypoints. The last waypoint is the goal.
type Pathfinder interface {
	Plan(from, to Point) ([]Point, bool)
}

// GridPathfinder is a bounded A* over a grid of legal chords.
type GridPathfinder struct {
	Validator ChordValidator
	// Grid is the grid step in world units. A zero value uses 32.
	Grid float64
	// Budget caps the chord probes. A zero value uses 256.
	Budget int
}

type routeNode struct {
	point    Point
	x, z     int
	cost     float64
	priority float64
	parent   *routeNode
	order    int
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
func (q *routeQueue) Push(value any) {
	*q = append(*q, value.(*routeNode))
}
func (q *routeQueue) Pop() any {
	n := len(*q) - 1
	node := (*q)[n]
	*q = (*q)[:n]
	return node
}

// Plan returns the route from -> to. The last waypoint equals to. A direct
// legal chord returns one waypoint. A search that finds nothing reports false.
func (p *GridPathfinder) Plan(from, to Point) ([]Point, bool) {
	if p == nil || p.Validator == nil {
		return nil, false
	}
	grid := p.Grid
	if grid <= 0 {
		grid = 32
	}
	budget := p.Budget
	if budget <= 0 {
		budget = 256
	}
	from = normalizePoint(from)
	to = normalizePoint(to)
	if p.Validator.Walkable(from, to) {
		return []Point{to}, true
	}
	count := 1
	start := &routeNode{point: from, priority: pointDistance(from, to)}
	open := routeQueue{start}
	type key struct{ x, z, y int }
	best := map[key]float64{{0, 0, int(math.Round(from.Y))}: 0}
	directions := [8][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}, {1, 1}, {-1, 1}, {-1, -1}, {1, -1}}
	for open.Len() > 0 && count < budget {
		node := heap.Pop(&open).(*routeNode)
		if node.cost > best[key{node.x, node.z, int(math.Round(node.point.Y))}] {
			continue
		}
		if node != start {
			count++
			if p.Validator.Walkable(node.point, to) {
				points := []Point{to}
				for at := node; at != start; at = at.parent {
					points = append(points, at.point)
				}
				reversePoints(points)
				return points, true
			}
		}
		for _, direction := range directions {
			if count >= budget {
				break
			}
			x, z := node.x+direction[0], node.z+direction[1]
			next := from
			next.X += float64(x) * grid
			next.Z += float64(z) * grid
			next.Y = node.point.Y
			next = normalizePoint(next)
			count++
			if !p.Validator.Walkable(node.point, next) {
				continue
			}
			cost := node.cost + pointDistance(node.point, next)
			k := key{x, z, int(math.Round(next.Y))}
			if previous, ok := best[k]; ok && previous <= cost {
				continue
			}
			best[k] = cost
			heap.Push(&open, &routeNode{
				point: next, x: x, z: z, cost: cost,
				priority: cost + pointDistance(next, to), parent: node, order: count,
			})
		}
	}
	return nil, false
}

func reversePoints(points []Point) {
	for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
		points[i], points[j] = points[j], points[i]
	}
}

// pointDistance is the frame-correct planar distance.
func pointDistance(from, to Point) float64 {
	return simulation.WorldDistance2D(
		simulation.Spawn{RegionID: from.Region, X: from.X, Z: from.Z},
		simulation.Spawn{RegionID: to.Region, X: to.X, Z: to.Z},
	)
}

// normalizePoint folds a point into its canonical region-local frame.
func normalizePoint(p Point) Point {
	s := simulation.NormalizeSpawnFrame(simulation.Spawn{RegionID: p.Region, X: p.X, Y: p.Y, Z: p.Z})
	return Point{Region: s.RegionID, X: s.X, Y: s.Y, Z: s.Z}
}
