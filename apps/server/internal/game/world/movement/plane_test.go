/*
===========================================================================

plane_test.go - the plane owner picks a plane by region, and each plane
wraps today's navigation (#1646 step 1), except the cave's route

The parity tests call every Plane method and the code it wraps on the same
real inputs (the Jangan veranda outdoors, the Donwhang cave anchors) and
require identical answers, so moving callers onto the planes cannot change
what they get. One deliberate exception: #1645 replaced the cave's
NextWaypoint with the floor's region route, so the cave test pins the new
route's contract (cave_route_test.go proves the corridor itself) while
every other method keeps the wrap.

===========================================================================
*/
package movement

import (
	"math"
	"reflect"
	"testing"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	planeOutdoorRegion uint16 = 0x61A7 // Jangan
	planeCaveRegion    uint16 = 0x8001 // the Donwhang cave
	// planeOtherCaveRegion is a second cave region, so Between's cave pair
	// spans two regions.
	planeOtherCaveRegion uint16 = 0x8003
	// planeProbeReach is how far each parity probe walks from its anchor.
	planeProbeReach = 8.0
	// planeProbeDirections is how many headings each anchor is probed in.
	planeProbeDirections = 8
)

/*
================
TestPlanesPickThePlaneByRegion
================
*/
func TestPlanesPickThePlaneByRegion(t *testing.T) {
	planes := NewPlanes(&Runtime{}, &WaterValidator{})
	if kind := planes.For(planeOutdoorRegion).Kind(); kind != simulation.PlaneOutdoor {
		t.Fatalf("outdoor region plane %d", kind)
	}
	if kind := planes.For(planeCaveRegion).Kind(); kind != simulation.PlaneCave {
		t.Fatalf("cave region plane %d", kind)
	}
	if _, ok := planes.Between(planeOutdoorRegion, planeCaveRegion); ok {
		t.Fatal("a pair across the outdoor/cave boundary was given a plane")
	}
	if plane, ok := planes.Between(planeCaveRegion, planeOtherCaveRegion); !ok || plane.Kind() != simulation.PlaneCave {
		t.Fatalf("two cave regions: %v, %t", plane, ok)
	}
}

/*
================
planeProbes

Each anchor walked planeProbeReach in planeProbeDirections headings.
================
*/
func planeProbes(anchors []simulation.Spawn) [][2]simulation.Spawn {
	var probes [][2]simulation.Spawn
	for _, anchor := range anchors {
		for dir := 0; dir < planeProbeDirections; dir++ {
			angle := float64(dir) * 2 * math.Pi / planeProbeDirections
			to := anchor
			to.X += planeProbeReach * math.Cos(angle)
			to.Z += planeProbeReach * math.Sin(angle)
			probes = append(probes, [2]simulation.Spawn{anchor, to})
		}
	}
	return probes
}

/*
================
poseOf
================
*/
func poseOf(s simulation.Spawn) monster.Pose {
	return monster.Pose{RegionID: s.RegionID, X: s.X, Y: s.Y, Z: s.Z, Heading: s.Angle}
}

// planePathSamples are the segment fractions samePath walks a path at.
var planePathSamples = []int64{0, 250, 500, 750, 1000}

/*
================
samePath

Two plans agree as a mover sees them: the same goal, rest and native
result, and the same live pose and surface owner along the segment. The
path's height sampler is a function, so it is compared by what it answers.
================
*/
func samePath(from monster.Pose, a, b *monster.NavigationPath) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Goal() != b.Goal() || a.Rest() != b.Rest() || a.Result() != b.Result() {
		return false
	}
	end := planePathSamples[len(planePathSamples)-1]
	ma := monster.MoverState{From: from, To: a.Rest(), DepartMs: 0, ArriveMs: end}
	mb := ma
	ma.AdoptNavigation(a)
	mb.AdoptNavigation(b)
	for _, ms := range planePathSamples {
		if ma.LivePoseAt(ms, nil) != mb.LivePoseAt(ms, nil) || ma.LiveNavOwner(ms) != mb.LiveNavOwner(ms) {
			return false
		}
	}
	return true
}

/*
================
planeParity

Every method of plane against the code it wraps, on probes.
================
*/
func planeParity(
	t *testing.T,
	plane simulation.Plane,
	rt *Runtime,
	v *WaterValidator,
	probes [][2]simulation.Spawn,
) {
	t.Helper()
	for _, probe := range probes {
		from, to := probe[0], probe[1]
		gotSpawn, gotWalk, gotErr := plane.Clip("PlaneParity", from, simulation.NavOwner{}, to)
		wantSpawn, wantWalk, wantErr := rt.ConstrainMovementFrom("PlaneParity", from, simulation.NavOwner{}, to)
		if gotSpawn != wantSpawn || !reflect.DeepEqual(gotWalk, wantWalk) || !reflect.DeepEqual(gotErr, wantErr) {
			t.Fatalf("%+v -> %+v Clip (%+v, %v), wrapped (%+v, %v)", from, to, gotSpawn, gotErr, wantSpawn, wantErr)
		}
		if got, want := plane.LineOfSight(from, simulation.NavOwner{}, to), rt.LineOfSight(from, simulation.NavOwner{}, to); got != want {
			t.Fatalf("%+v -> %+v LineOfSight %t, wrapped %t", from, to, got, want)
		}
		path := plane.PlanPath(poseOf(from), simulation.NavOwner{}, poseOf(to))
		if !samePath(poseOf(from), path, v.PlanMonsterPathFrom(poseOf(from), simulation.NavOwner{}, poseOf(to))) {
			t.Fatalf("%+v -> %+v PlanPath differs from the wrapped plan", from, to)
		}
		a := worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z}
		b := worldgeom.RegionXZ{RegionID: to.RegionID, X: to.X, Z: to.Z}
		gotX, gotZ := plane.Relative(a, b)
		wantX, wantZ := worldgeom.Delta(a, b)
		if gotX != wantX || gotZ != wantZ {
			t.Fatalf("%+v -> %+v Relative (%v, %v), wrapped (%v, %v)", a, b, gotX, gotZ, wantX, wantZ)
		}
		if got, want := plane.SameNeighbourhood(from.RegionID, to.RegionID), worldgeom.SamePlaneAdjacent(from.RegionID, to.RegionID); got != want {
			t.Fatalf("%x/%x SameNeighbourhood %t, wrapped %t", from.RegionID, to.RegionID, got, want)
		}
	}
}

/*
================
TestOutdoorPlaneWrapsTodaysNavigation
================
*/
func TestOutdoorPlaneWrapsTodaysNavigation(t *testing.T) {
	v := realAuthorityValidator(t)
	rt := &Runtime{ClientClip: &ClientClip{Mode: ClipApply, Validator: v}}
	plane := NewPlanes(rt, v).For(planeOutdoorRegion)
	anchors := []simulation.Spawn{
		{RegionID: planeOutdoorRegion, X: 623, Y: 3.04, Z: 1271},
		{RegionID: planeOutdoorRegion, X: 680, Y: 3.04, Z: 1271}, // at the veranda rail
	}
	probes := planeProbes(anchors)
	planeParity(t, plane, rt, v, probes)
	// The outdoor route still wraps the open-terrain search exactly.
	for _, probe := range probes {
		route := plane.NextWaypoint(poseOf(probe[0]), poseOf(probe[1]))
		if want := v.PlanMonsterRoute(poseOf(probe[0]), poseOf(probe[1])); !reflect.DeepEqual(route, want) {
			t.Fatalf("%+v -> %+v NextWaypoint differs from the wrapped route", probe[0], probe[1])
		}
	}
}

/*
================
TestCavePlaneWrapsTodaysNavigation
================
*/
func TestCavePlaneWrapsTodaysNavigation(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	rt := &Runtime{ClientClip: &ClientClip{Mode: ClipApply, Validator: v}}
	plane := NewPlanes(rt, v).For(planeCaveRegion)
	anchors := []simulation.Spawn{
		{RegionID: planeCaveRegion, X: 993.197, Y: 0.00034, Z: -307.099},
		{RegionID: planeCaveRegion, X: 629, Y: -2.22, Z: 74},
	}
	// The cave floors exist under the anchors, so the probes compare real
	// walks rather than two misses.
	for _, anchor := range anchors {
		if _, ok := v.dungeonSpawnHeightAt(anchor.RegionID, anchor.X, anchor.Y, anchor.Z); !ok {
			t.Fatalf("no cave floor under %+v", anchor)
		}
	}
	probes := planeProbes(anchors)
	planeParity(t, plane, rt, v, probes)
	// NextWaypoint is the one deliberate break with the wrap (#1645):
	// the cave answers with its region route. Around an anchor a ready
	// route must end at the goal and every leg of it must arrive.
	for _, probe := range probes {
		from, goal := poseOf(probe[0]), poseOf(probe[1])
		if _, ok := v.dungeonSpawnHeightAt(goal.RegionID, goal.X, goal.Y, goal.Z); !ok {
			// No floor under the destination: not a cave walk at
			// all, and the route contract does not apply.
			continue
		}
		route := plane.NextWaypoint(from, goal)
		if route == nil || route.Goal() != goal {
			t.Fatalf("%+v -> %+v route %+v", from, goal, route)
		}
		if route.Status() != monster.NavigationRouteReady {
			continue
		}
		if route.Len() == 0 || route.Point(route.Len()-1) != goal {
			t.Fatalf("%+v -> %+v route does not end at the goal", from, goal)
		}
		current := from
		for i := 0; i < route.Len(); i++ {
			if !routeClear(v.PlanMonsterPath(current, route.Point(i)), route.Point(i)) {
				t.Fatalf("%+v -> %+v leg %d of the cave route does not arrive", from, goal, i)
			}
			current = route.Point(i)
		}
	}
}
