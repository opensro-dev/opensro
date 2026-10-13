/*
===========================================================================

monster_route_bench_test.go - what one monster path probe and route cost

The live GameWorld spends most of its monster-leg time in the route
search's path probes (findMonsterRoute -> PlanMonsterPathFrom), each a full
clip plus surface walk against every object placement around the chord.
These measure one probe and one route over a fixed set of real Jangan
requests, so a change to that path reports its ns/op and allocations.

===========================================================================
*/
package movement

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

// benchRouteRegion is Jangan: object-dense, the costliest probes.
const benchRouteRegion = 0x61A8

/*
================
benchRouteRequests

A fixed grid of standing points in Jangan, each with a goal 25 units away
in each of eight directions (the probes) and 150 units east (the routes).
================
*/
func benchRouteRequests(b *testing.B, v *WaterValidator) (probes, routes [][2]monster.Pose) {
	for z := 60.0; z < 1920; z += 120 {
		for x := 60.0; x < 1920; x += 120 {
			y, ok := v.TerrainHeightAt(benchRouteRegion, x, z)
			if !ok {
				continue
			}
			from := monster.Pose{RegionID: benchRouteRegion, X: x, Y: y, Z: z}
			for d := 0; d < 8; d++ {
				goal := from
				goal.X += math.Cos(float64(d)*math.Pi/4) * 25
				goal.Z += math.Sin(float64(d)*math.Pi/4) * 25
				probes = append(probes, [2]monster.Pose{from, goal})
			}
			goal := from
			goal.X += 150
			routes = append(routes, [2]monster.Pose{from, goal})
		}
	}
	if len(probes) == 0 {
		b.Fatal("no Jangan standing points")
	}
	return probes, routes
}

/*
================
BenchmarkMonsterPathProbe
================
*/
func BenchmarkMonsterPathProbe(b *testing.B) {
	v := realAuthorityValidator(b)
	probes, _ := benchRouteRequests(b, v)
	for _, p := range probes {
		v.PlanMonsterPath(p[0], p[1]) // warm every object set
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := probes[i%len(probes)]
		v.PlanMonsterPath(p[0], p[1])
	}
}

/*
================
BenchmarkMonsterRoute
================
*/
func BenchmarkMonsterRoute(b *testing.B) {
	v := realAuthorityValidator(b)
	_, routes := benchRouteRequests(b, v)
	for _, r := range routes {
		v.PlanMonsterRoute(r[0], r[1])
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := routes[i%len(routes)]
		v.PlanMonsterRoute(r[0], r[1])
	}
}
