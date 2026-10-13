/*
===========================================================================

monster_route_equivalence_test.go - the monster planner's exact results, swept

A performance change to the clip or the route search must leave every
result identical. This sweeps real authority data in dense towns and open
fields: short and long clips in eight directions from a grid of standing
points, and the route search toward far goals. Every result is written as
one exact line; SRO_ROUTE_SWEEP_OUT names the file. Two runs (before and
after a change) must produce byte-identical files. Opt-in: it is long.

===========================================================================
*/
package movement

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"math"
	"os"
	"regexp"
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// routeSweepRegions are dense towns and their surroundings: object-heavy
// chords are where the clip spends its time.
var routeSweepRegions = []uint16{
	0x61A8, // Jangan
	0x6699, // Donwhang
	0x694F, // Mengzi
	0x6687, // Fertile Temple
	0x6A2C, // Constantinople
	0x6E4A, // Europe field
}

const (
	routeSweepStep      = 40.0
	routeSweepRouteStep = 4 // every fourth standing point also routes
)

var routeSweepLengths = []float64{6, 25, 60}

// routeSweepFunction strips a printed function value: its address differs
// run to run. The surface it reads is sampled by value instead.
var routeSweepFunction = regexp.MustCompile(`height:0x[0-9a-f]+`)

/*
================
TestMonsterRouteSweep
================
*/
func TestMonsterRouteSweep(t *testing.T) {
	out := os.Getenv("SRO_ROUTE_SWEEP_OUT")
	if out == "" {
		t.Skip("set SRO_ROUTE_SWEEP_OUT to write the sweep")
	}
	v := realAuthorityValidator(t)
	file, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	w := bufio.NewWriter(file)
	defer w.Flush()
	sum := sha256.New()
	emit := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		w.WriteString(line)
		w.WriteString("\n")
		sum.Write([]byte(line))
	}
	clips, routes := 0, 0
	for _, region := range routeSweepRegions {
		index := 0
		for z := routeSweepStep / 2; z < simulation.NativeRegionSize; z += routeSweepStep {
			for x := routeSweepStep / 2; x < simulation.NativeRegionSize; x += routeSweepStep {
				y, ok := v.TerrainHeightAt(region, x, z)
				if !ok {
					continue
				}
				from := monster.Pose{RegionID: region, X: x, Y: y, Z: z}
				for d := 0; d < 8; d++ {
					angle := float64(d) * math.Pi / 4
					for _, length := range routeSweepLengths {
						goal := from
						goal.X += math.Cos(angle) * length
						goal.Z += math.Sin(angle) * length
						clips++
						spawnFrom := simulation.Spawn{RegionID: region, X: from.X, Y: from.Y, Z: from.Z}
						spawnTo := simulation.Spawn{RegionID: region, X: goal.X, Y: goal.Y, Z: goal.Z}
						emit("clip %04x %v %v %d %v: %+v", region, x, z, d, length, v.ClipMovementPathFrom(spawnFrom, simulation.NavOwner{}, spawnTo))
						if p := v.PlanMonsterPath(from, goal); p != nil {
							line := routeSweepFunction.ReplaceAllString(fmt.Sprintf("%+v", *p), "height:fn")
							rest := p.Rest()
							walk := v.ownerWalk(spawnFrom, simulation.NavOwner{}, simulation.Spawn{RegionID: rest.RegionID, X: rest.X, Y: rest.Y, Z: rest.Z})
							for _, at := range []float64{0, .25, .5, .75, 1} {
								y, ok := walk.heightAt(at)
								line += fmt.Sprintf(" h%v=%v/%v", at, y, ok)
							}
							emit("path %04x %v %v %d %v: %s", region, x, z, d, length, line)
						} else {
							emit("path %04x %v %v %d %v: nil", region, x, z, d, length)
						}
					}
				}
				index++
				if index%routeSweepRouteStep != 0 {
					continue
				}
				for _, far := range [][2]float64{{150, 0}, {-110, 110}} {
					goal := from
					goal.X += far[0]
					goal.Z += far[1]
					routes++
					route := v.PlanMonsterRoute(from, goal)
					line := fmt.Sprintf("route %04x %v %v %v: %v %d %d", region, x, z, far, route.Status(), route.Probes(), route.Len())
					for i := 0; i < route.Len(); i++ {
						line += fmt.Sprintf(" %+v", route.Point(i))
					}
					emit("%s", line)
				}
			}
		}
	}
	t.Logf("swept %d clips and %d routes; sha256 %x", clips, routes, sum.Sum(nil))
}
