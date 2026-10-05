// The performance-ring subcommand measures the monster materialization and AI
// candidate ring against shipped textdata. For every region where a viewer
// could stand, it calls simulation.RegionScopeRing + MonsterState.InstancesInRegions
// and reports the worst-case candidate count and distinct-model count. This
// is an upper bound on object-list rows: bootstrap and live visibility narrow
// the ring to the native 320-unit block interest (MonsterState.InterestInstances).
//
// The registry PRNG is fixed below, so generated positions and every reported
// count are reproducible. Read-only over textdata; no server, no wire.
package main

import (
	"fmt"
	"math/rand"
	"sort"
	"time"

	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/gamedata"
)

type ringResult struct {
	center         uint16
	rows           int
	authoredRows   int
	distinctRefs   int
	distinctModels int
	wraps          bool
}

type probeNestKey struct {
	refObjID uint32
	regionID uint16
	x        float64
	y        float64
	z        float64
}

func nestKey(nest monster.NestRow) probeNestKey {
	return probeNestKey{
		refObjID: nest.RefObjID,
		regionID: nest.RegionID,
		x:        nest.X,
		y:        nest.Y,
		z:        nest.Z,
	}
}

// ringWraps reports whether the 3x3 ring around a non-dungeon center
// crosses a byte edge, i.e. RegionIDForSectors' &0xff mask wrapped a
// neighbor to the far side of the map.
func ringWraps(center uint16) bool {
	if simulation.IsDungeonRegion(center) {
		return false
	}
	sx, sy := simulation.SectorX(center), simulation.SectorY(center)
	return sx == 0 || sx == 0xff || sy == 0 || sy == 0xff
}

func runPerformanceRing(_ []string) error {
	dataPaths, err := gamedata.Resolve()
	if err != nil {
		return fmt.Errorf("game data: %w", err)
	}
	dir := dataPaths.TextdataDir

	template := monster.LoadTemplate(dir)
	fmt.Printf("template: %d monster nest rows, %d refs, %d spawnable refs\n",
		len(template.Nests), len(template.Refs), len(template.SpawnableRefs()))

	// Candidate viewer centers: every spawn region and (for non-dungeon
	// spawn regions) its 8 neighbors — a center whose ring holds no spawn
	// region scores 0 rows, so all maxima live in this set. Dungeon
	// regions scope to exactly themselves per the runtime predicate.
	spawnRegions := map[uint16]bool{}
	for _, nest := range template.Nests {
		spawnRegions[nest.RegionID] = true
	}
	candidates := map[uint16]bool{}
	for regionID := range spawnRegions {
		candidates[regionID] = true
		if simulation.IsDungeonRegion(regionID) {
			continue
		}
		sx, sy := simulation.SectorX(regionID), simulation.SectorY(regionID)
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				candidates[simulation.RegionIDForSectors(sx+dx, sy+dy)] = true
			}
		}
	}
	fmt.Printf("spawn regions: %d (dungeon: %d), candidate viewer centers: %d\n",
		len(spawnRegions), countDungeon(spawnRegions), len(candidates))

	registry := simulation.NewMonsterState(template)
	groundedRandom := rand.New(rand.NewSource(0x1188))
	registry.SetRandomSource(groundedRandom.Float64)
	auditNow := time.UnixMilli(0)
	registry.SetTimeSource(func() time.Time { return auditNow })
	authoredRegistry := simulation.NewMonsterState(template)
	authoredRandom := rand.New(rand.NewSource(0x1188))
	authoredRegistry.SetRandomSource(authoredRandom.Float64)
	water := movement.NewAuthorityValidator(dataPaths.WorldAuthorityDir)
	if err := water.ValidateSecurityAssets(); err != nil {
		return fmt.Errorf("production spawn authority unavailable: %w", err)
	}
	registry.SetSpawnGroundResolver(water.WalkableSpawnHeightAt)
	registry.SetSpawnCollisionTest(water.SpawnMoveTest)
	authoredRegistry.SetTimeSource(func() time.Time { return auditNow })
	registry.StartDivision("perf-audit")
	authoredRegistry.StartDivision("perf-audit")
	registry.AdvancePopulation(auditNow.UnixMilli())
	authoredRegistry.AdvancePopulation(auditNow.UnixMilli())
	var results []ringResult
	candidateIDs := make([]int, 0, len(candidates))
	for center := range candidates {
		candidateIDs = append(candidateIDs, int(center))
	}
	sort.Ints(candidateIDs)
	for _, candidate := range candidateIDs {
		center := uint16(candidate)
		ring := simulation.RegionScopeRing(center)
		instances := registry.InstancesInRegions("perf-audit", ring)
		authoredInstances := authoredRegistry.InstancesInRegions("perf-audit", ring)
		refs := map[uint32]bool{}
		models := map[string]bool{}
		for _, instance := range instances {
			refs[instance.Ref.RefObjID] = true
			models[instance.Ref.Codename] = true
		}
		results = append(results, ringResult{
			center:         center,
			rows:           len(instances),
			authoredRows:   len(authoredInstances),
			distinctRefs:   len(refs),
			distinctModels: len(models),
			wraps:          ringWraps(center),
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].rows != results[j].rows {
			return results[i].rows > results[j].rows
		}
		return results[i].center < results[j].center
	})

	fmt.Println("\nTOP 10 viewer centers by object-list rows (RegionScopeRing + InstancesInRegions):")
	for i, r := range results {
		if i >= 10 {
			break
		}
		kind := "field"
		if simulation.IsDungeonRegion(r.center) {
			kind = "DUNGEON(self-only)"
		}
		wrap := ""
		if r.wraps {
			wrap = " [ring wraps byte edge]"
		}
		fmt.Printf("  center=0x%04X (x=%d,y=%d) %s: %d/%d admitted/authored rows, %d distinct refObjIDs, %d distinct codenames%s\n",
			r.center, simulation.SectorX(r.center), simulation.SectorY(r.center), kind,
			r.rows, r.authoredRows, r.distinctRefs, r.distinctModels, wrap)
	}

	worst := results[0]
	maxRefs, maxModels := 0, 0
	var worstRefsCenter, worstModelsCenter uint16
	wrappingNonZero := 0
	for _, r := range results {
		if r.distinctRefs > maxRefs {
			maxRefs, worstRefsCenter = r.distinctRefs, r.center
		}
		if r.distinctModels > maxModels {
			maxModels, worstModelsCenter = r.distinctModels, r.center
		}
		if r.wraps && r.rows > 0 {
			wrappingNonZero++
		}
	}

	var worstDungeon ringResult
	for _, r := range results {
		if simulation.IsDungeonRegion(r.center) && r.rows > worstDungeon.rows {
			worstDungeon = r
		}
	}

	// Named viewer centers: the two race start regions (login rings a
	// real character actually gets) for the live-probe expectation.
	fmt.Println("\nSTART-REGION rings (login expectation for the live probe):")
	for _, named := range []struct {
		label  string
		center uint16
	}{
		{"CHINA start (Jangan) 0x62A8", 0x62A8},
		{"EUROPE start (Constantinople) 0x6B4F", 0x6B4F},
		{"probe char saved spawn 0x61A0", 0x61A0},
	} {
		ring := simulation.RegionScopeRing(named.center)
		instances := registry.InstancesInRegions("perf-audit", ring)
		authoredInstances := authoredRegistry.InstancesInRegions("perf-audit", ring)
		models := map[string]int{}
		for _, instance := range instances {
			models[instance.Ref.Codename]++
		}
		fmt.Printf("  %s: %d/%d admitted/authored rows, %d distinct codenames\n",
			named.label, len(instances), len(authoredInstances), len(models))
		names := make([]string, 0, len(models))
		for name := range models {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Printf("    %s x%d\n", name, models[name])
		}
	}

	// Near-field counts catch a different failure class than the ring totals:
	// a correct 3x3 wire scope may still look empty if authored nests are far
	// from the actual player. This is asd2's normalized persisted spawn
	// (0x5E9E/5682.786/5119.231 folds to 0x60A0/1842.786/1279.231).
	probe := grounditem.Point{
		RegionID: 0x60A0,
		X:        1842.786447160981,
		Z:        1279.23114342619,
	}
	probeRegistry := simulation.NewMonsterState(template)
	probeRandom := rand.New(rand.NewSource(0x1188))
	probeRegistry.SetRandomSource(probeRandom.Float64)
	probeRegistry.SetSpawnGroundResolver(water.WalkableSpawnHeightAt)
	probeRegistry.SetSpawnCollisionTest(water.SpawnMoveTest)
	probeNow := time.UnixMilli(0)
	probeRegistry.SetTimeSource(func() time.Time { return probeNow })
	probeRegistry.StartDivision("asd2-probe")
	probeRegistry.AdvancePopulation(probeNow.UnixMilli())
	probeInstances := probeRegistry.InstancesInRegions(
		"asd2-probe",
		simulation.RegionScopeRing(probe.RegionID),
	)
	initialProbeRows := len(probeInstances)
	for second := 1; second <= 10; second++ {
		probeNow = time.UnixMilli(int64(second) * 1000)
		probeRegistry.AdvancePopulation(probeNow.UnixMilli())
		probeInstances = probeRegistry.InstancesInRegions(
			"asd2-probe",
			simulation.RegionScopeRing(probe.RegionID),
		)
	}
	retailEvidenceRows := 0
	for _, instance := range probeInstances {
		if instance.Nest.RetailEvidence {
			retailEvidenceRows++
		}
	}
	fmt.Printf(
		"\nASD2 NEAR-FIELD density (production terrain authority; %d initial rows, %d after ten one-second refill passes, %d backed by v1.188 nest evidence):\n",
		initialProbeRows,
		len(probeInstances),
		retailEvidenceRows,
	)
	for _, radius := range []float64{50, 100, 200, 300, 500, 1000} {
		count := 0
		for _, instance := range probeInstances {
			at := grounditem.Point{
				RegionID: instance.Spawn.RegionID,
				X:        float32(instance.Spawn.X),
				Z:        float32(instance.Spawn.Z),
			}
			if grounditem.Distance2D(probe, at) <= radius {
				count++
			}
		}
		fmt.Printf("  radius %4.0f: %d monsters\n", radius, count)
	}
	sort.Slice(probeInstances, func(i, j int) bool {
		left := grounditem.Distance2D(probe, grounditem.Point{
			RegionID: probeInstances[i].Spawn.RegionID,
			X:        float32(probeInstances[i].Spawn.X),
			Z:        float32(probeInstances[i].Spawn.Z),
		})
		right := grounditem.Distance2D(probe, grounditem.Point{
			RegionID: probeInstances[j].Spawn.RegionID,
			X:        float32(probeInstances[j].Spawn.X),
			Z:        float32(probeInstances[j].Spawn.Z),
		})
		return left < right
	})
	fmt.Println("  nearest 10:")
	for index, instance := range probeInstances {
		if index >= 10 {
			break
		}
		distance := grounditem.Distance2D(probe, grounditem.Point{
			RegionID: instance.Spawn.RegionID,
			X:        float32(instance.Spawn.X),
			Z:        float32(instance.Spawn.Z),
		})
		fmt.Printf(
			"    %6.1f  %s (region 0x%04X, %.1f, %.1f)\n",
			distance,
			instance.Ref.Codename,
			instance.Spawn.RegionID,
			instance.Spawn.X,
			instance.Spawn.Z,
		)
	}
	probeMaterialized := probeRegistry.MaterializedInstances("asd2-probe")
	actualByNest := make(map[probeNestKey]int)
	for _, instance := range probeMaterialized {
		actualByNest[nestKey(instance.Nest)]++
	}
	probeRing := simulation.RegionScopeRing(probe.RegionID)
	probeRegions := make(map[uint16]bool, len(probeRing))
	for _, regionID := range probeRing {
		probeRegions[regionID] = true
	}
	fmt.Printf(
		"  materialized nest slots: %d (the ring excludes generated positions that crossed beyond its sectors)\n",
		len(probeMaterialized),
	)
	fmt.Println("  genuinely underfilled authored nests:")
	underfilledNests := 0
	for _, nest := range template.Nests {
		if !probeRegions[nest.RegionID] {
			continue
		}
		expected := 1
		if nest.RetailEvidence {
			expected = nest.MaxCount
		}
		actual := actualByNest[nestKey(nest)]
		if actual >= expected {
			continue
		}
		underfilledNests++
		_, anchorWalkable := water.WalkableTerrainHeightAt(nest.RegionID, nest.X, nest.Z)
		fmt.Printf(
			"    %s region 0x%04X anchor %.1f,%.1f radius %.0f: %d/%d live, anchorWalkable=%t\n",
			template.Refs[nest.RefObjID].Codename,
			nest.RegionID,
			nest.X,
			nest.Z,
			nest.GenerateRadius,
			actual,
			expected,
			anchorWalkable,
		)
	}
	if underfilledNests == 0 {
		fmt.Println("    none")
	}

	fmt.Printf("\nWORST-CASE MEASURED (runtime predicate over shipped textdata):\n")
	fmt.Printf("  rows:            %d  (center 0x%04X)\n", worst.rows, worst.center)
	fmt.Printf("  distinct refs:   %d  (center 0x%04X)\n", maxRefs, worstRefsCenter)
	fmt.Printf("  distinct models: %d  (center 0x%04X)\n", maxModels, worstModelsCenter)
	fmt.Printf("  worst dungeon (self-only scope): 0x%04X = %d rows, %d models\n",
		worstDungeon.center, worstDungeon.rows, worstDungeon.distinctModels)
	fmt.Printf("  centers whose ring wraps a byte edge AND scores >0 rows: %d\n", wrappingNonZero)
	initialFullDivisionRows := len(registry.MaterializedInstances("perf-audit"))
	for second := 1; second <= 10; second++ {
		auditNow = time.UnixMilli(int64(second) * 1000)
		registry.AdvancePopulation(auditNow.UnixMilli())
	}
	materializedGlobal := registry.MaterializedInstances("perf-audit")
	fmt.Printf("  full division ceiling after every region is visited: %d initial, %d after ten refill passes, %d authored live instances\n",
		initialFullDivisionRows,
		len(materializedGlobal),
		len(authoredRegistry.MaterializedInstances("perf-audit")))
	actualGlobalByNest := make(map[probeNestKey]int)
	for _, instance := range materializedGlobal {
		actualGlobalByNest[nestKey(instance.Nest)]++
	}
	missingByCodename := make(map[string]int)
	missingSlots := 0
	missingNests := 0
	missingAnchorWalkable := 0
	var missingOutdoorNests []monster.NestRow
	for _, nest := range template.Nests {
		expected := 1
		if nest.RetailEvidence {
			expected = nest.MaxCount
		}
		missing := expected - actualGlobalByNest[nestKey(nest)]
		if missing <= 0 {
			continue
		}
		missingSlots += missing
		missingNests++
		codename := template.Refs[nest.RefObjID].Codename
		missingByCodename[codename] += missing
		if _, ok := water.WalkableTerrainHeightAt(nest.RegionID, nest.X, nest.Z); ok {
			missingAnchorWalkable += missing
		}
		if !simulation.IsDungeonRegion(nest.RegionID) {
			missingOutdoorNests = append(missingOutdoorNests, nest)
		}
	}
	type missingSpecies struct {
		codename string
		slots    int
	}
	missingSpeciesRows := make([]missingSpecies, 0, len(missingByCodename))
	for codename, slots := range missingByCodename {
		missingSpeciesRows = append(missingSpeciesRows, missingSpecies{codename: codename, slots: slots})
	}
	sort.Slice(missingSpeciesRows, func(i, j int) bool {
		if missingSpeciesRows[i].slots != missingSpeciesRows[j].slots {
			return missingSpeciesRows[i].slots > missingSpeciesRows[j].slots
		}
		return missingSpeciesRows[i].codename < missingSpeciesRows[j].codename
	})
	fmt.Printf(
		"  persistent terrain rejects: %d slots across %d nests (%d missing slots have a walkable authored anchor)\n",
		missingSlots,
		missingNests,
		missingAnchorWalkable,
	)
	for index, row := range missingSpeciesRows {
		if index >= 20 {
			break
		}
		fmt.Printf("    %s: %d missing slots\n", row.codename, row.slots)
	}
	fmt.Println("  persistent outdoor nest rejects:")
	for _, nest := range missingOutdoorNests {
		expected := 1
		if nest.RetailEvidence {
			expected = nest.MaxCount
		}
		actual := actualGlobalByNest[nestKey(nest)]
		fmt.Printf(
			"    %s region 0x%04X anchor %.1f,%.1f y=%.1f radius %.0f: %d/%d live\n",
			template.Refs[nest.RefObjID].Codename,
			nest.RegionID,
			nest.X,
			nest.Z,
			nest.Y,
			nest.GenerateRadius,
			actual,
			expected,
		)
	}
	return nil
}

func countDungeon(regions map[uint16]bool) int {
	n := 0
	for regionID := range regions {
		if simulation.IsDungeonRegion(regionID) {
			n++
		}
	}
	return n
}
