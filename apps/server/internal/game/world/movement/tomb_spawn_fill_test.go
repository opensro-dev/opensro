/*
===========================================================================

tomb_spawn_fill_test.go - Ch'in Tomb Floor 3 monsters spread over the floor

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
floor3Template

The tomb template cut down to Floor 3's ordinary respawning nests, so the
fill measures only the floor the players reported. operator replaces every
nest's radii with ISRO-R's active-table tuning.
================
*/
func floor3Template(t *testing.T, operator bool) (monster.Template, int) {
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
		if nest.RegionID != tombFloor3Region || !nest.Respawn || monster.NestIsUnique(*nest, ref) {
			nest.MaxCount = 0
			continue
		}
		if operator {
			nest.Radius, nest.GenerateRadius = operatorRadius, operatorGenerateRadius
		}
		slots += nest.MaxCount
	}
	if slots == 0 {
		t.Fatal("no Floor 3 nests in the tomb template")
	}
	tomb.Nests = nests
	return tomb, slots
}

/*
================
fillFloor3State

Runs the population for tombFillSimulatedMs with the production spawn
authority and returns it with the validator that placed it.
================
*/
func fillFloor3State(t *testing.T, template monster.Template) (*simulation.MonsterState, *WaterValidator) {
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
fillFloor3

Runs the population for tombFillSimulatedMs with the production spawn
authority and returns how many monsters stand on Floor 3 and the median
planar distance from each to its nearest neighbour.
================
*/
func fillFloor3(t *testing.T, template monster.Template) (int, float64) {
	t.Helper()
	s, _ := fillFloor3State(t, template)
	var points [][2]float64
	for _, instance := range s.MaterializedInstances("tomb") {
		if instance.Spawn.RegionID == tombFloor3Region {
			points = append(points, [2]float64{instance.Spawn.X, instance.Spawn.Z})
		}
	}
	if len(points) < 2 {
		return len(points), 0
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
	return len(points), nearest[len(nearest)/2]
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
	shipped, slots := floor3Template(t, false)
	standing, spacing := fillFloor3(t, shipped)
	operator, _ := floor3Template(t, true)
	operatorStanding, operatorSpacing := fillFloor3(t, operator)
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
