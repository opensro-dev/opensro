/*
===========================================================================

monsterstate_damage_test.go - damage door, area candidate and spatial tests

Verifies HP mutation serialization, structure exclusion, and exact equivalence
and performance of CombatCandidatesInPopulation against the full-population
linear scan.

===========================================================================
*/

package simulation

import (
	"math/rand"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
damageTestState
================
*/
func damageTestState() *MonsterState {
	template := monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{
			1933: {RefObjID: 1933, MaxHP: 54, WalkSpeed: 8, RunSpeed: 22, ScaleDenom: 100},
		},
		[]monster.NestRow{{
			SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: 0x62AA, X: 100, Y: 20, Z: 100},
		}},
	)
	return NewMonsterState(template)
}

/*
================
firstDamageTestMonster
================
*/
func firstDamageTestMonster(t *testing.T, state *MonsterState, divisionID string) monster.Instance {
	t.Helper()
	state.StartDivision(divisionID)
	state.AdvancePopulation(state.CurrentTimeMillis())
	instances := state.InstancesInRegions(divisionID, []uint16{0x62AA})
	if len(instances) == 0 {
		t.Fatal("test template materialized no monsters")
	}
	return instances[0]
}

/*
================
TestApplyDamageOwnsCurrentHPAndLeavesRemovalExplicit
================
*/
func TestApplyDamageOwnsCurrentHPAndLeavesRemovalExplicit(t *testing.T) {
	registry := damageTestState()
	instance := firstDamageTestMonster(t, registry, "damage")

	hit, ok := registry.ApplyDamage("damage", instance.Gid, 7)
	if !ok {
		t.Fatal("ApplyDamage rejected a live monster")
	}
	if hit.BeforeHP != 54 || hit.Applied != 7 || hit.CurrentHP != 47 || hit.Fatal {
		t.Fatalf("first hit = %+v, want 54 -> 47 with 7 applied", hit)
	}
	if hit.Instance.CurrentHP != hit.CurrentHP {
		t.Fatalf("result snapshot HP = %d, committed HP = %d",
			hit.Instance.CurrentHP, hit.CurrentHP)
	}

	// Result snapshots are values; mutating one cannot become a second HP
	// owner outside the registry door.
	hit.Instance.CurrentHP = 999
	stored, found := registry.Get("damage", instance.Gid)
	if !found || stored.CurrentHP != 47 {
		t.Fatalf("stored monster after snapshot mutation = %+v, found %v", stored, found)
	}

	fatal, ok := registry.ApplyDamage("damage", instance.Gid, 1000)
	if !ok {
		t.Fatal("overkill rejected a live monster")
	}
	if fatal.BeforeHP != 47 || fatal.Applied != 47 ||
		fatal.CurrentHP != 0 || !fatal.Fatal {
		t.Fatalf("fatal hit = %+v, want 47 -> 0 with overkill clamped", fatal)
	}
	if _, found := registry.Get("damage", instance.Gid); !found {
		t.Fatal("fatal HP transition removed the entity before death publication")
	}

	afterDeath, ok := registry.ApplyDamage("damage", instance.Gid, 1)
	if !ok || afterDeath.Applied != 0 || afterDeath.Fatal ||
		afterDeath.CurrentHP != 0 {
		t.Fatalf("post-fatal hit = %+v, found %v; want stable zero HP", afterDeath, ok)
	}

	if !registry.Defeat("damage", instance.Gid, time.UnixMilli(10_000)) {
		t.Fatal("explicit lifecycle defeat rejected the zero-HP population monster")
	}
	if _, found := registry.Get("damage", instance.Gid); found {
		t.Fatal("explicit Defeat did not remove the population identity")
	}
}

/*
================
TestApplyDamageSerializesConcurrentHits
================
*/
func TestApplyDamageSerializesConcurrentHits(t *testing.T) {
	registry := damageTestState()
	instance := firstDamageTestMonster(t, registry, "concurrent-damage")

	var appliedTotal atomic.Uint32
	var fatalTransitions atomic.Uint32
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, ok := registry.ApplyDamage("concurrent-damage", instance.Gid, 10)
			if !ok {
				return
			}
			appliedTotal.Add(result.Applied)
			if result.Fatal {
				fatalTransitions.Add(1)
			}
		}()
	}
	wait.Wait()

	stored, ok := registry.Get("concurrent-damage", instance.Gid)
	if !ok {
		t.Fatal("damage door unexpectedly removed the instance")
	}
	if stored.CurrentHP != 0 {
		t.Fatalf("current HP = %d, want 0", stored.CurrentHP)
	}
	if appliedTotal.Load() != 54 {
		t.Fatalf("total applied damage = %d, want exactly the original 54 HP",
			appliedTotal.Load())
	}
	if fatalTransitions.Load() != 1 {
		t.Fatalf("fatal transitions = %d, want exactly one", fatalTransitions.Load())
	}
}

/*
================
TestApplyDamageMissingIdentityDoesNotInventState
================
*/
func TestApplyDamageMissingIdentityDoesNotInventState(t *testing.T) {
	registry := damageTestState()
	if result, ok := registry.ApplyDamage("missing", monster.GidBase+999, 1); ok {
		t.Fatalf("missing gid produced %+v", result)
	}
	if len(registry.MaterializedInstances("missing")) != 0 {
		t.Fatal("missing damage materialized or invented a monster")
	}
}

/*
================
TestNativeSignedDamageDoesNotTurnNegativeIntoFatalHit
================
*/
func TestNativeSignedDamageDoesNotTurnNegativeIntoFatalHit(t *testing.T) {
	for _, damage := range []uint32{0, 0x80000000, 0xffffffff} {
		s := damageTestState()
		m := firstDamageTestMonster(t, s, "signed-damage")
		rows := s.ApplyDamageSequence("signed-damage", m.Gid, m.CurrentHP,
			[]MonsterDamagePlan{{GID: m.Gid, Damage: damage, CreditGID: 77}})
		if len(rows) != 1 || rows[0].Applied != 0 || rows[0].Fatal || rows[0].CurrentHP != m.CurrentHP {
			t.Fatalf("damage %#x changed HP: %+v", damage, rows)
		}
		// NPC credit is accumulated before the common signed HP gate.
		if damage != 0 {
			credit := s.divs["signed-damage"].contributionSnapshot(m.Gid)
			if len(credit) != 1 || credit[0].Damage != damage {
				t.Fatalf("credit lost: %+v", credit)
			}
		}
	}
}

/*
================
TestAreaCandidatesSkipStructures

The retail server's can-attack check admits only an aimed basic attack on
a fortress structure, so the
area, chain and secondary candidate scans never return one.
================
*/
func TestAreaCandidatesSkipStructures(t *testing.T) {
	s := NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{
			1933:  {RefObjID: 1933, MaxHP: 54, WalkSpeed: 8, RunSpeed: 22, ScaleDenom: 100},
			19536: {RefObjID: 19536, MaxHP: 500, ScaleDenom: 100, Structure: true},
		},
		[]monster.NestRow{
			{SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: 0x62AA, X: 100, Y: 20, Z: 100}},
			{SpawnPoint: monster.SpawnPoint{RefObjID: 19536, RegionID: 0x62AA, X: 102, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1},
		},
	))
	s.StartDivision("area")
	s.AdvancePopulation(s.CurrentTimeMillis())
	if n := len(s.InstancesInRegions("area", []uint16{0x62AA})); n != 2 {
		t.Fatalf("fixture materialized %d instances, want the monster and the structure", n)
	}
	center := Spawn{RegionID: 0x62AA, X: 101, Y: 20, Z: 100}
	got := s.CombatCandidatesInWorld("area", instance.Pack(1, 1), center, 20, s.CurrentTimeMillis(), false)
	if len(got) != 1 || got[0].Ref.Structure {
		t.Fatalf("area candidates %+v, want only the monster", got)
	}
}

/*
================
TestCombatCandidatesStayInTheirWorld

A world-keyed candidate scan reads only its own layer's population: the
same pose in the default world, in layer 1 and in layer 2 of one
instanced definition answers three different single-candidate scans,
and the sphere convenience scan stays the default world. This is the
property a caster-world fallback relies on; before it, a chain query
issued outside the default world silently scanned the default world.
================
*/
func TestCombatCandidatesStayInTheirWorld(t *testing.T) {
	s := NewMonsterState(monster.Template{})
	s.StartDivision("iso")
	defaultLease, ok := s.PopulationLease("iso", instance.Pack(1, 1))
	if !ok {
		t.Fatal("the default world has no population")
	}
	layerOne, openedOne := s.OpenInstanceLayer("iso", 10)
	layerTwo, openedTwo := s.OpenInstanceLayer("iso", 10)
	if !openedOne || !openedTwo || layerOne.ID == layerTwo.ID {
		t.Fatalf("instance layers did not open: %v %v", layerOne, layerTwo)
	}
	center := Spawn{RegionID: 0x62AA, X: 100, Y: 20, Z: 100}
	place := func(lease instance.Lease, gid uint32) *divisionMonsterState {
		div := &divisionMonsterState{
			lease:     lease,
			instances: newMonsterStorage(nil),
			movers:    newMoverStorage(nil),
			byRegion:  make(map[uint16][]uint32),
		}
		div.instances.archive = s.archive
		div.instances.set(gid, monster.Instance{
			Gid:       gid,
			CurrentHP: 100,
			Ref:       monster.MonsterRef{RefObjID: 1933, MaxHP: 100},
			Spawn:     monster.SpawnPoint{RegionID: center.RegionID, X: center.X, Y: center.Y, Z: center.Z},
		})
		div.movers.set(gid, monster.MoverState{Pose: monster.Pose{RegionID: center.RegionID, X: center.X, Y: center.Y, Z: center.Z}})
		s.worldPopulations[populationKey{division: "iso", lease: lease}] = div
		return div
	}
	// populationForLease prefers the division's own state for the default
	// lease, so the hand-built default population must be installed there
	// too (the construction combatCandidateBenchmarkState uses).
	s.divs["iso"] = place(defaultLease, monster.GidBase+1)
	place(layerOne, monster.GidBase+2)
	place(layerTwo, monster.GidBase+3)

	now := s.CurrentTimeMillis()
	for _, tc := range []struct {
		name  string
		world instance.ID
		want  uint32
	}{
		{"default world", defaultLease.ID, monster.GidBase + 1},
		{"layer 1", layerOne.ID, monster.GidBase + 2},
		{"layer 2", layerTwo.ID, monster.GidBase + 3},
	} {
		got := s.CombatCandidatesInWorld("iso", tc.world, center, 20, now, true)
		if len(got) != 1 || got[0].Gid != tc.want {
			t.Fatalf("%s candidates %+v, want only gid %d", tc.name, got, tc.want)
		}
	}
}

/*
================
combatCandidateBenchmarkState

Builds a realistic continent population of count monsters spread across 20x20
outdoor sectors around 0x62AA plus a dungeon sector, mixing compact pending
movers and in-flight live movers.
================
*/
var benchmarkCombatCandidateCenters = [4]Spawn{
	{RegionID: 0x62AA, X: 960, Y: 20, Z: 960},
	{RegionID: 0x63AB, X: 100, Y: 20, Z: 1850},
	{RegionID: 0x61A9, X: 1880, Y: 20, Z: 40},
	{RegionID: 0x64AC, X: 500, Y: 20, Z: 500},
}

func combatCandidateBenchmarkState(seed int64, count int) (*MonsterState, instance.Lease) {
	r := rand.New(rand.NewSource(seed))
	s := NewMonsterState(monster.Template{})
	lease := instance.Lease{ID: instance.Pack(1, 1), Generation: 1}
	div := &divisionMonsterState{
		lease:     lease,
		instances: newMonsterStorage(nil),
		movers:    newMoverStorage(nil),
		byRegion:  make(map[uint16][]uint32),
	}
	div.instances.archive = s.archive
	for i := 1; i <= count; i++ {
		gid := monster.GidBase + uint32(i)
		var region uint16
		if i%25 == 0 {
			region = 0x8001 + uint16(r.Intn(2))
		} else {
			sx := 0xA0 + (i % 20)
			sz := 0x58 + ((i / 20) % 20)
			region = uint16((sz << 8) | sx)
		}
		pose := monster.Pose{
			RegionID: region,
			X:        r.Float64() * 1920,
			Y:        r.Float64() * 40,
			Z:        r.Float64() * 1920,
		}
		if i <= len(benchmarkCombatCandidateCenters) {
			c := benchmarkCombatCandidateCenters[i-1]
			pose = monster.Pose{RegionID: c.RegionID, X: c.X, Y: c.Y, Z: c.Z}
		}
		radius := float64(2 + (i % 15))
		hp := uint32(100)
		if i > len(benchmarkCombatCandidateCenters) && i%31 == 0 {
			hp = 0
		}
		inst := monster.Instance{
			Gid:       gid,
			CurrentHP: hp,
			Ref: monster.MonsterRef{
				RefObjID:    uint32(1900 + (i % 20)),
				BodyRadius:  radius,
				MaxHP:       100,
				Structure:   i > len(benchmarkCombatCandidateCenters) && i%47 == 0,
				MonsterType: uint8(i % 5),
			},
			Spawn: monster.SpawnPoint{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z},
		}
		if i > len(benchmarkCombatCandidateCenters) && i%7 == 0 {
			div.instances.set(gid, inst)
			div.instances.freeze(gid)
		} else {
			div.instances.set(gid, inst)
		}
		row := monster.MoverState{Pose: pose}
		if i > len(benchmarkCombatCandidateCenters) && i%4 == 0 {
			to := pose
			to.X += (r.Float64() - 0.5) * 600
			to.Z += (r.Float64() - 0.5) * 600
			row.From, row.To = pose, to
			row.DepartMs = 1000
			row.ArriveMs = 1000 + int64(500+r.Intn(4000))
		}
		div.movers.set(gid, row)
	}
	s.divs["bench"] = div
	s.worldPopulations = map[populationKey]*divisionMonsterState{
		{division: "bench", lease: lease}: div,
	}
	return s, lease
}

/*
================
referenceCombatCandidatesInPopulation

The full-population linear scan before Slice 2: visits every mover via
livePoses(nowMs, nil) without spatial sector filtering.
================
*/
func (s *MonsterState) referenceCombatCandidatesInPopulation(divisionID string, lease instance.Lease, center Spawn, reach float64, nowMs int64, nearest bool) []monster.Instance {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(divisionID, lease)
	if state == nil {
		return nil
	}
	var out []monster.Instance
	var distances map[uint32]float64
	if nearest {
		distances = make(map[uint32]float64)
	}
	for gid, pose := range state.movers.livePoses(nowMs, nil) {
		if IsDungeonRegion(center.RegionID) != IsDungeonRegion(pose.RegionID) {
			continue
		}
		distance := WorldDistance2D(center, Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z})
		distance = float64(distance)
		dy := pose.Y - center.Y
		distance = hypot2(distance, dy)
		limit := reach
		if !nearest {
			_, radius := state.instances.metadata(gid)
			limit += radius * float64(monster.GradeScale(4))
		}
		if distance <= limit {
			inst, exists := state.instances.lookup(gid)
			if !exists || inst.CurrentHP == 0 || inst.Ref.Structure {
				continue
			}
			if !nearest && distance > reach+inst.BodyRadius() {
				continue
			}
			out = append(out, inst)
			if nearest {
				distances[gid] = distance
			}
		}
	}
	slices.SortFunc(out, func(a, b monster.Instance) int {
		if nearest && distances[a.Gid] != distances[b.Gid] {
			if distances[a.Gid] < distances[b.Gid] {
				return -1
			}
			return 1
		}
		if a.Gid < b.Gid {
			return -1
		}
		if a.Gid > b.Gid {
			return 1
		}
		return 0
	})
	return out
}

/*
================
hypot2
================
*/
func hypot2(a, b float64) float64 {
	return WorldDistance2D(Spawn{X: 0, Z: 0}, Spawn{X: a, Z: b})
}

/*
================
TestCombatCandidatesInPopulationMatchesReferenceScan
================
*/
func TestCombatCandidatesInPopulationMatchesReferenceScan(t *testing.T) {
	r := rand.New(rand.NewSource(99))
	s, lease := combatCandidateBenchmarkState(77, 1024)
	for i := 0; i < 400; i++ {
		var region uint16
		if i%10 == 0 {
			region = 0x8001 + uint16(r.Intn(2))
		} else {
			sx := 0xA0 + r.Intn(20)
			sz := 0x58 + r.Intn(20)
			region = uint16((sz << 8) | sx)
		}
		center := Spawn{
			RegionID: region,
			X:        r.Float64() * 1920,
			Y:        r.Float64() * 40,
			Z:        r.Float64() * 1920,
		}
		reach := float64(10 + r.Intn(500))
		if i == 0 {
			reach = 1e30
		} else if i%25 == 0 {
			reach = float64(2000 + r.Intn(6000))
		}
		nowMs := int64(500 + r.Intn(5000))
		for _, nearest := range []bool{false, true} {
			got := s.CombatCandidatesInPopulation("bench", lease, center, reach, nowMs, nearest)
			want := s.referenceCombatCandidatesInPopulation("bench", lease, center, reach, nowMs, nearest)
			if len(got) != len(want) {
				t.Fatalf("iter %d nearest=%v center=%+v reach=%v: got %d want %d", i, nearest, center, reach, len(got), len(want))
			}
			for j := range got {
				if got[j].Gid != want[j].Gid {
					t.Fatalf("iter %d nearest=%v idx %d: got gid %d want %d", i, nearest, j, got[j].Gid, want[j].Gid)
				}
			}
		}
	}
}

/*
================
BenchmarkCombatCandidatesInPopulation
================
*/
func BenchmarkCombatCandidatesInPopulation(b *testing.B) {
	s, lease := combatCandidateBenchmarkState(42, 4096)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := benchmarkCombatCandidateCenters[i&3]
		got := s.CombatCandidatesInPopulation("bench", lease, c, 120, 2500, i&1 == 1)
		if len(got) == 0 {
			b.Fatal("unreachable")
		}
	}
}

/*
================
BenchmarkReferenceCombatCandidatesInPopulation
================
*/
func BenchmarkReferenceCombatCandidatesInPopulation(b *testing.B) {
	s, lease := combatCandidateBenchmarkState(42, 4096)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := benchmarkCombatCandidateCenters[i&3]
		got := s.referenceCombatCandidatesInPopulation("bench", lease, c, 120, 2500, i&1 == 1)
		if len(got) == 0 {
			b.Fatal("unreachable")
		}
	}
}
