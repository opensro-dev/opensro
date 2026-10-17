package bot_test

import (
	"math"
	"testing"

	"opensro.online/server/internal/bot"
)

// wallValidator blocks any segment that crosses the rectangle x in (80,120),
// z in (-100,100). It samples the segment, so it is independent of the grid.
type wallValidator struct{}

func (wallValidator) Walkable(from, to bot.Point) bool {
	distance := math.Hypot(to.X-from.X, to.Z-from.Z)
	steps := int(distance/5) + 1
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x := from.X + (to.X-from.X)*t
		z := from.Z + (to.Z-from.Z)*t
		if x > 80 && x < 120 && z > -100 && z < 100 {
			return false
		}
	}
	return true
}

func TestGridPathfinderRoutesAroundAWall(t *testing.T) {
	pathfinder := &bot.GridPathfinder{Validator: wallValidator{}, Grid: 32, Budget: 256}
	from := bot.Point{Region: 1, X: 0, Z: 0}
	to := bot.Point{Region: 1, X: 200, Z: 0}
	route, ok := pathfinder.Plan(from, to)
	if !ok {
		t.Fatal("no route around the wall")
	}
	if len(route) < 2 {
		t.Fatalf("route = %v, want an intermediate waypoint", route)
	}
	if last := route[len(route)-1]; last != to {
		t.Fatalf("last waypoint = %+v, want the goal", last)
	}
	previous := from
	offAxis := false
	for _, p := range route {
		if !(wallValidator{}).Walkable(previous, p) {
			t.Fatalf("leg %+v -> %+v is not walkable", previous, p)
		}
		if math.Abs(p.Z) > 100 {
			offAxis = true
		}
		previous = p
	}
	if !offAxis {
		t.Fatalf("route = %v, want a detour past the wall", route)
	}
}

func TestGridPathfinderReturnsADirectChord(t *testing.T) {
	pathfinder := &bot.GridPathfinder{Validator: wallValidator{}, Grid: 32, Budget: 256}
	from := bot.Point{Region: 1, X: 0, Z: 0}
	to := bot.Point{Region: 1, X: 40, Z: 0}
	route, ok := pathfinder.Plan(from, to)
	if !ok || len(route) != 1 || route[0] != to {
		t.Fatalf("route = %v ok = %v, want the direct goal", route, ok)
	}
}

func TestGridPathfinderReportsNoRoute(t *testing.T) {
	pathfinder := &bot.GridPathfinder{Validator: wallValidator{}, Grid: 32, Budget: 4}
	if _, ok := pathfinder.Plan(bot.Point{Region: 1, X: 0, Z: 0}, bot.Point{Region: 1, X: 200, Z: 0}); ok {
		t.Fatal("a tiny budget found a route")
	}
}

func TestGridPathfinderNilValidator(t *testing.T) {
	pathfinder := &bot.GridPathfinder{}
	if _, ok := pathfinder.Plan(bot.Point{Region: 1}, bot.Point{Region: 1, X: 10}); ok {
		t.Fatal("a nil validator planned a route")
	}
}
