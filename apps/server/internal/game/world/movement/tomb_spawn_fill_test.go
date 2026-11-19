/*
===========================================================================

tomb_spawn_fill_test.go - Ch'in Tomb Floor 3 camps stand where they belong

Players reported on 2026-11-18 that Floor 3 (region 0x8005) looked thinly
populated and that its monsters drifted off their camps. The tomb nests had
been imported from ISRO-R's archived Tab_RefNest_Back, which gives them a
300-500 unit generate radius; ISRO-R's live Tab_RefNest gives 40. The native
nest hatch places each instance inside that radius of the nest centre, so
the archived value spread each camp across the neighbouring corridors:
measured on the cave geometry, the floor filled as fast either way, but the
median monster stood 366 units from its camp instead of 30. These tests run the population with
the production ground resolver and move test on the shipped cave geometry.

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
	// tombFillSimulatedMs is ten simulated minutes: with the deployment's
	// 1-3 s cave respawn every nest has had over two hundred hatch chances.
	tombFillSimulatedMs = 10 * 60 * 1000
	tombFillStepMs      = 100
	tombFillSeed        = 0x8005
	// tombFillMinPercent is the share of Floor 3's ordinary slots that must
	// stand after the fill.
	tombFillMinPercent = 90
	// tombCampMedianMax is the most the median monster may stand from its
	// nest centre: inside the live 40-unit generate radius.
	tombCampMedianMax = 40
	// archivedGenerateRadius is Tab_RefNest_Back's radius on 348 of the
	// floor's 396 nests, the value the tomb shipped with until 2026-11-18.
	archivedGenerateRadius = 500
)

/*
================
floor3Template

The tomb template cut down to Floor 3's ordinary respawning nests, so the
fill measures only the floor the players reported. generateRadius, when
nonzero, replaces every nest's generate radius.
================
*/
func floor3Template(t *testing.T, generateRadius float64) (monster.Template, int) {
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
		if generateRadius > 0 {
			nest.GenerateRadius = generateRadius
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
planar distance from each to its nest centre.
================
*/
func fillFloor3(t *testing.T, template monster.Template) (int, float64) {
	t.Helper()
	s, _ := fillFloor3State(t, template)
	var distances []float64
	for _, instance := range s.MaterializedInstances("tomb") {
		if instance.Spawn.RegionID != tombFloor3Region {
			continue
		}
		distances = append(distances, math.Hypot(instance.Spawn.X-instance.Nest.X, instance.Spawn.Z-instance.Nest.Z))
	}
	if len(distances) == 0 {
		return 0, 0
	}
	sort.Float64s(distances)
	return len(distances), distances[len(distances)/2]
}

/*
================
TestTombFloor3CampsStandTogether

The shipped radii fill Floor 3 to tombFillMinPercent of its slots with the
median monster inside tombCampMedianMax of its camp; the archived radius,
on the same geometry and seed, scatters them past it: the defect the
2026-11-18 reports described.
================
*/
func TestTombFloor3CampsStandTogether(t *testing.T) {
	tier.Long(t, "Floor 3 of the Ch'in Tomb fills with its camps together on the real cave geometry")
	shipped, slots := floor3Template(t, 0)
	standing, median := fillFloor3(t, shipped)
	archived, _ := floor3Template(t, archivedGenerateRadius)
	archivedStanding, archivedMedian := fillFloor3(t, archived)
	t.Logf("Floor 3: %d slots; shipped radii %d standing, median %.0f from camp; archived %d-unit radius %d standing, median %.0f",
		slots, standing, median, archivedGenerateRadius, archivedStanding, archivedMedian)
	if standing*100 < slots*tombFillMinPercent {
		t.Fatalf("Floor 3 stands at %d of %d slots after %d s, under %d%%",
			standing, slots, tombFillSimulatedMs/1000, tombFillMinPercent)
	}
	if median > tombCampMedianMax {
		t.Fatalf("the median monster stands %.0f from its camp, over %d", median, tombCampMedianMax)
	}
	if archivedMedian <= tombCampMedianMax {
		t.Fatalf("the archived radius keeps camps at %.0f: the fixture no longer shows the defect", archivedMedian)
	}
}
