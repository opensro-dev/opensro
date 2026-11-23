/*
===========================================================================

tomb_spawn_fill_test.go - Ch'in Tomb monsters spread over their floors

Players reported on 2026-11-20 (#4149, #4150) that the tomb floors look
empty between camps while each camp stands about ten monsters on top of each
other. The tomb nests had taken their balance from ISRO-R's active
Tab_RefNest (#3874, #3876): a 20-unit containment radius, a 40-unit generate
radius, a cap of 5 and 1-2 s respawns. That is the ISRO-R operator's own
tuning, not native. The same table carries it on 5,124 outdoor nests whose
archived row, Tab_RefNest_Back, holds the 700 / 600 the v1.188 server data
shows. The containment radius is below the generate radius, so every
monster hatched outside it walks back to the centre. These tests run the
population with the production ground resolver and move test on the shipped
cave geometry.

The same report reached Floor 1 (the level 80-82 camps) on 2026-11-23
(#4234), filed on a build that carried neither the native nest balance
(#4161) nor the homing march that ended battlefield parking (#4180, #4124).
The Floor 1 tests below are that report's measurement on this data: the
fill and spread of its nests, and a route home from the worst drag a
fight on the floor can make.

===========================================================================
*/
package movement

import (
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

const (
	tombFloor3Region = 0x8005
	tombFloor1Region = 0x8007
	// The six tomb floors, Floor 6 (0x8002) to Floor 1 (0x8007).
	tombFirstFloorRegion = 0x8002
	tombLastFloorRegion  = 0x8007
	// tombFillSimulatedMs is ten simulated minutes: with the deployment's
	// 1-3 s cave respawn every nest has had over two hundred hatch chances.
	tombFillSimulatedMs = 10 * 60 * 1000
	tombFillStepMs      = 100
	tombFillSeed        = 0x8005
	// tombFillMinPercent is the share of Floor 3's ordinary slots that must
	// stand after the fill.
	tombFillMinPercent = 90
	// tombNeighbourMedianMin is the least the median monster may stand from
	// its nearest neighbour: a camp spread over its generate radius, not a
	// pile. The native balance measures over a hundred, the operator's
	// tuning under ten.
	tombNeighbourMedianMin = 50
	// operatorRadius and operatorGenerateRadius are ISRO-R's active-table
	// values, the balance the reports describe.
	operatorRadius         = 20
	operatorGenerateRadius = 40
)

/*
================
floorTemplate

The tomb template cut down to one floor's ordinary respawning nests, so
the fill measures only the floor under test. operator replaces every
nest's radii with ISRO-R's active-table tuning.
================
*/
func floorTemplate(t *testing.T, region uint16, operator bool) (monster.Template, int) {
	t.Helper()
	textdata := gamedatatest.TextdataDir(t)
	tomb, err := monster.LoadTemplate(textdata).WithQinShiTomb(textdata)
	if err != nil {
		t.Fatalf("WithQinShiTomb: %v", err)
	}
	// Other template indexes name nests by position, so every row stays and
	// a nest outside the measured set is silenced with a zero cap.
	nests := make([]monster.NestRow, len(tomb.Nests))
	copy(nests, tomb.Nests)
	slots := 0
	for i := range nests {
		nest := &nests[i]
		ref := tomb.Refs[nest.RefObjID]
		if nest.RegionID != region || !nest.Respawn || monster.NestIsUnique(*nest, ref) {
			nest.MaxCount = 0
			continue
		}
		if operator {
			nest.Radius, nest.GenerateRadius = operatorRadius, operatorGenerateRadius
		}
		slots += nest.MaxCount
	}
	if slots == 0 {
		t.Fatalf("no nests in region %#x in the tomb template", region)
	}
	tomb.Nests = nests
	return tomb, slots
}

/*
================
fillFloorState

Runs the population for tombFillSimulatedMs with the production spawn
authority and returns it with the validator that placed it.
================
*/
func fillFloorState(t *testing.T, template monster.Template) (*simulation.MonsterState, *WaterValidator) {
	t.Helper()
	t.Setenv(simulation.EnvJanganCaveRespawn, "on")
	v := NewAuthorityValidator(gamedatatest.WorldAuthorityDir(t))
	s := simulation.NewMonsterState(template)
	s.SetRandomSource(rand.New(rand.NewSource(tombFillSeed)).Float64)
	now := time.Unix(100, 0)
	s.SetTimeSource(func() time.Time { return now })
	s.SetSpawnGroundResolver(v.WalkableSpawnHeightAt)
	s.SetSpawnCollisionTest(v.SpawnMoveTest)
	s.StartDivision("tomb")
	for elapsed := 0; elapsed < tombFillSimulatedMs; elapsed += tombFillStepMs {
		s.AdvancePopulation(s.CurrentTimeMillis())
		now = now.Add(tombFillStepMs * time.Millisecond)
	}
	return s, v
}

/*
================
fillFloor

Runs the population for tombFillSimulatedMs with the production spawn
authority and returns how many monsters stand on the floor and the median
planar distance from each to its nearest neighbour.
================
*/
func fillFloor(t *testing.T, template monster.Template, region uint16) (int, float64) {
	t.Helper()
	s, _ := fillFloorState(t, template)
	var points [][2]float64
	for _, instance := range s.MaterializedInstances("tomb") {
		if instance.Spawn.RegionID == region {
			points = append(points, [2]float64{instance.Spawn.X, instance.Spawn.Z})
		}
	}
	return len(points), medianNearest(points)
}

/*
================
medianNearest

The median planar distance from each point to its nearest neighbour: a
camp spread over its generate radius measures in the dozens, a pile in
single digits.
================
*/
func medianNearest(points [][2]float64) float64 {
	if len(points) < 2 {
		return 0
	}
	nearest := make([]float64, len(points))
	for i, p := range points {
		nearest[i] = math.Inf(1)
		for j, q := range points {
			if i != j {
				nearest[i] = math.Min(nearest[i], math.Hypot(p[0]-q[0], p[1]-q[1]))
			}
		}
	}
	sort.Float64s(nearest)
	return nearest[len(nearest)/2]
}

/*
================
TestTombFloor3MonstersDoNotPile

The shipped native balance fills Floor 3 to tombFillMinPercent of its slots
with the median monster at least tombNeighbourMedianMin from its nearest
neighbour; the operator's tuning, on the same geometry and seed, piles them:
the defect the 2026-11-20 reports described.
================
*/
func TestTombFloor3MonstersDoNotPile(t *testing.T) {
	tier.Long(t, "Floor 3 of the Ch'in Tomb fills with its monsters spread on the real cave geometry")
	shipped, slots := floorTemplate(t, tombFloor3Region, false)
	standing, spacing := fillFloor(t, shipped, tombFloor3Region)
	operator, _ := floorTemplate(t, tombFloor3Region, true)
	operatorStanding, operatorSpacing := fillFloor(t, operator, tombFloor3Region)
	t.Logf("Floor 3: %d slots; native %d standing, median neighbour %.0f; operator tuning %d standing, median neighbour %.0f",
		slots, standing, spacing, operatorStanding, operatorSpacing)
	if standing*100 < slots*tombFillMinPercent {
		t.Fatalf("Floor 3 stands at %d of %d slots after %d s, under %d%%",
			standing, slots, tombFillSimulatedMs/1000, tombFillMinPercent)
	}
	if spacing < tombNeighbourMedianMin {
		t.Fatalf("the median monster stands %.0f from its nearest neighbour, under %d", spacing, tombNeighbourMedianMin)
	}
	if operatorSpacing >= tombNeighbourMedianMin {
		t.Fatalf("the operator tuning spaces monsters %.0f apart: the fixture no longer shows the defect", operatorSpacing)
	}
}

// The level band of the camps the Floor 1 report named (#4234).
const (
	tombFloor1BandFirstLevel = 80
	tombFloor1BandLastLevel  = 82
)

/*
================
floor1Band

Floor 1's standing points, whole floor and the reported 80-82 band.
================
*/
func floor1Band(s *simulation.MonsterState) (all, band [][2]float64) {
	for _, instance := range s.MaterializedInstances("tomb") {
		if instance.Spawn.RegionID != tombFloor1Region {
			continue
		}
		point := [2]float64{instance.Spawn.X, instance.Spawn.Z}
		all = append(all, point)
		if instance.Ref.Level >= tombFloor1BandFirstLevel && instance.Ref.Level <= tombFloor1BandLastLevel {
			band = append(band, point)
		}
	}
	return all, band
}

/*
================
TestTombFloor1MonstersDoNotPile

The #4234 measurement on the shipped data: Floor 1 fills to
tombFillMinPercent of its slots with the median monster (and the median
monster of the reported 80-82 band) at least tombNeighbourMedianMin from
its nearest neighbour; the operator's tuning, on the same geometry and
seed, piles them - the report's single point.
================
*/
func TestTombFloor1MonstersDoNotPile(t *testing.T) {
	tier.Long(t, "Floor 1 of the Ch'in Tomb fills with its monsters spread on the real cave geometry")
	shipped, slots := floorTemplate(t, tombFloor1Region, false)
	s, _ := fillFloorState(t, shipped)
	all, band := floor1Band(s)
	bandSlots := 0
	for _, nest := range shipped.Nests {
		ref := shipped.Refs[nest.RefObjID]
		if nest.RegionID == tombFloor1Region && nest.MaxCount > 0 &&
			ref.Level >= tombFloor1BandFirstLevel && ref.Level <= tombFloor1BandLastLevel {
			bandSlots += nest.MaxCount
		}
	}
	operator, _ := floorTemplate(t, tombFloor1Region, true)
	operatorState, _ := fillFloorState(t, operator)
	operatorAll, operatorBand := floor1Band(operatorState)
	spacing, bandSpacing := medianNearest(all), medianNearest(band)
	operatorSpacing, operatorBandSpacing := medianNearest(operatorAll), medianNearest(operatorBand)
	t.Logf("Floor 1: %d slots, %d standing, median neighbour %.0f; 80-82 band %d slots, %d standing, median neighbour %.0f; operator tuning median neighbour %.0f (band %.0f)",
		slots, len(all), spacing, bandSlots, len(band), bandSpacing, operatorSpacing, operatorBandSpacing)
	if len(all)*100 < slots*tombFillMinPercent {
		t.Fatalf("Floor 1 stands at %d of %d slots after %d s, under %d%%", len(all), slots, tombFillSimulatedMs/1000, tombFillMinPercent)
	}
	if spacing < tombNeighbourMedianMin {
		t.Fatalf("the median Floor 1 monster stands %.0f from its nearest neighbour, under %d", spacing, tombNeighbourMedianMin)
	}
	if bandSlots == 0 || len(band)*100 < bandSlots*tombFillMinPercent || bandSpacing < tombNeighbourMedianMin {
		t.Fatalf("the 80-82 band stands %d of %d with median neighbour %.0f", len(band), bandSlots, bandSpacing)
	}
	if operatorSpacing >= tombNeighbourMedianMin || operatorBandSpacing >= tombNeighbourMedianMin {
		t.Fatalf("the operator tuning spaces Floor 1 monsters %.0f apart (band %.0f): the fixture no longer shows the defect",
			operatorSpacing, operatorBandSpacing)
	}
}

/*
================
TestTombFloor1HomingRoutesHome

The parking half of the #4234 report on this floor's geometry: a monster
dragged to the furthest standing point of its floor (the worst drag a
fight can make) gets home on the production cave route, or, where the
route cannot see, by the homing march of #4180 - never parked where the
fight ended. Forty monsters sampled across the filled floor.
================
*/
func TestTombFloor1HomingRoutesHome(t *testing.T) {
	tier.Long(t, "Floor 1 monsters dragged across the floor route or march home on the real cave geometry")
	template, _ := floorTemplate(t, tombFloor1Region, false)
	s, v := fillFloorState(t, template)
	var gids []uint32
	for _, instance := range s.MaterializedInstances("tomb") {
		if instance.Spawn.RegionID == tombFloor1Region {
			gids = append(gids, instance.Gid)
		}
	}
	if len(gids) < 40 {
		t.Fatalf("only %d Floor 1 monsters filled", len(gids))
	}
	now := s.CurrentTimeMillis()
	poseOf := func(gid uint32) (monster.Pose, bool) {
		mover, ok := s.Mover("tomb", gid)
		if !ok {
			return monster.Pose{}, false
		}
		return mover.Pose, true
	}
	sampled, routed, marched := 0, 0, 0
	for i := 0; i < len(gids); i += len(gids) / 40 {
		gid := gids[i]
		home, ok := poseOf(gid)
		if !ok {
			continue
		}
		mover, _ := s.Mover("tomb", gid)
		owner := mover.LiveNavOwner(now)
		// The kite point: the furthest other standing point on the floor.
		var kite monster.Pose
		kiteDist := -1.0
		for _, other := range gids {
			if other == gid {
				continue
			}
			if p, ok := poseOf(other); ok {
				if d := math.Hypot(p.X-home.X, p.Z-home.Z); d > kiteDist {
					kite, kiteDist = p, d
				}
			}
		}
		sampled++
		if route := v.planCaveRouteFrom(kite, owner, home); route != nil && route.Status() == monster.NavigationRouteReady {
			routed++
			continue
		}
		// No region route: the homing march (the furthest walkable pose
		// toward home, re-planned per leg), as planSegment falls back.
		cur := kite
		for leg := 0; leg < 400 && math.Hypot(cur.X-home.X, cur.Z-home.Z) > 25; leg++ {
			path := v.PlanMonsterPathFrom(cur, owner, home)
			if path == nil {
				break
			}
			next := path.Rest()
			if math.Hypot(next.X-cur.X, next.Z-cur.Z) < 0.5 {
				break
			}
			cur = next
		}
		if math.Hypot(cur.X-home.X, cur.Z-home.Z) <= 25 {
			marched++
			continue
		}
		t.Errorf("gid %d: no route home and the march wedges %.0f short of its nest", gid,
			math.Hypot(cur.X-home.X, cur.Z-home.Z))
	}
	t.Logf("Floor 1 homing: %d sampled, %d routed, %d marched", sampled, routed, marched)
	if sampled < 30 || routed+marched != sampled {
		t.Fatalf("homing answered %d of %d sampled monsters", routed+marched, sampled)
	}
}

/*
================
TestTombNestsContainTheirSpawns

Every ordinary tomb nest's containment radius covers its generate radius,
as on every native nest: a monster hatched outside it would walk straight
back to the centre and stand on the others.
================
*/
func TestTombNestsContainTheirSpawns(t *testing.T) {
	textdata := gamedatatest.TextdataDir(t)
	tomb, err := monster.LoadTemplate(textdata).WithQinShiTomb(textdata)
	if err != nil {
		t.Fatalf("WithQinShiTomb: %v", err)
	}
	checked := 0
	for _, nest := range tomb.Nests {
		if nest.RegionID < tombFirstFloorRegion || nest.RegionID > tombLastFloorRegion || nest.MaxCount == 0 {
			continue
		}
		checked++
		if nest.Radius < nest.GenerateRadius {
			t.Errorf("%s at region %#x (%.0f, %.0f): radius %.0f under its generate radius %.0f",
				tomb.Refs[nest.RefObjID].Codename, nest.RegionID, nest.X, nest.Z, nest.Radius, nest.GenerateRadius)
		}
	}
	if checked == 0 {
		t.Fatal("no tomb nests in the template")
	}
}
