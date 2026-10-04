package simulation

import (
	"testing"
	"time"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

func TestDefaultPopulationDoesNotOwnForeignWorldNests(t *testing.T) {
	nests := []monster.NestRow{
		{WorldCode: "INS_DEFAULT", SpawnPoint: monster.SpawnPoint{RegionID: 1}},
		{WorldCode: "INS_SIEGE_DUNGEON", SpawnPoint: monster.SpawnPoint{RegionID: 1}},
		{WorldCode: "INS_FORTRESS_JANGAN", SpawnPoint: monster.SpawnPoint{RegionID: 1}},
	}
	s := NewMonsterState(monster.TemplateFromParts(nil, nests))
	s.SetTimeSource(func() time.Time { return time.UnixMilli(1) })
	s.StartDivision("a")
	state := s.divs["a"]
	if len(state.nests) != 1 || state.nests[0] == nil {
		t.Fatalf("foreign nests admitted to default population: %+v", state.nests)
	}
	if lease, ok := s.worldAllocators["a"].Lookup(instance.Pack(1, 1)); !ok || state.lease != lease {
		t.Fatal("population was not bound to allocated world lifetime")
	}
	s.StartDivision("b")
	if s.divs["a"] == s.divs["b"] || s.worldAllocators["a"] == s.worldAllocators["b"] {
		t.Fatal("server divisions share mutable world authority")
	}
}

/*
================
TestDivisionOpensEveryPermanentWorld

SR_ShardManager 65BD60 opens layer 1 of every type-0 RefGameWorld at boot,
so the fortress worlds exist (and own their authored nests) before anyone
travels there; instance worlds stay closed until requested.
================
*/
func TestDivisionOpensEveryPermanentWorld(t *testing.T) {
	nests := []monster.NestRow{
		{WorldCode: "INS_DEFAULT", SpawnPoint: monster.SpawnPoint{RegionID: 1}},
		{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RegionID: 2}},
	}
	s := NewMonsterState(monster.TemplateFromParts(nil, nests))
	s.SetTimeSource(func() time.Time { return time.UnixMilli(1) })
	s.StartDivision("a")
	for _, definition := range instance.Shipped() {
		_, open := s.PopulationLease("a", instance.Pack(definition.ID, 1))
		if open != (definition.NativeType == 0) {
			t.Fatalf("%s (type %d) open=%v at boot", definition.CodeName, definition.NativeType, open)
		}
	}
	fort, _ := s.PopulationLease("a", instance.Pack(2, 1))
	state := s.worldPopulations[populationKey{"a", fort}]
	if state == nil || len(state.nests) != 1 || state.nests[1] == nil {
		t.Fatalf("INS_FORT_JA population does not own its nest: %+v", state)
	}
}

func TestAllocatedPopulationsHaveIndependentNestsAndLifetimes(t *testing.T) {
	definition, _ := instance.Lookup(10)
	nest := lifecycleNest(100)
	nest.WorldCode, nest.HiveKey, nest.HiveMaxCount = definition.CodeName, "quest-hive", 1
	w := newLifecycleWorld(t, lifecycleRef(1), nest)
	a, status := w.s.AllocatePopulation("division", instance.Pack(10, 1))
	if status != instance.Success {
		t.Fatal(status)
	}
	b, status := w.s.AllocatePopulation("division", instance.Pack(10, 2))
	if status != instance.Success {
		t.Fatal(status)
	}
	regions := []uint16{lifecycleRegion}
	if len(w.s.PopulationInstances("division", a, regions)) != 0 {
		t.Fatal("allocation bypassed spawn clock")
	}
	w.s.AdvancePopulation(w.now.UnixMilli())
	aa, bb := w.s.PopulationInstances("division", a, regions), w.s.PopulationInstances("division", b, regions)
	if len(aa) != 1 || len(bb) != 1 || aa[0].Gid == bb[0].Gid {
		t.Fatal("isolated populations/global identity", aa, bb)
	}
	viewer := worldgeom.RegionXZ{RegionID: aa[0].Spawn.RegionID, X: aa[0].Spawn.X, Z: aa[0].Spawn.Z}
	if seen := w.s.PopulationInterestInstances("division", a, viewer, w.now.UnixMilli()); len(seen) != 1 || seen[0].Gid != aa[0].Gid {
		t.Fatal("interest crossed population", seen)
	}
	if _, ok := w.s.GetInPopulation("division", a, bb[0].Gid); ok {
		t.Fatal("foreign GID admitted")
	}
	if len(w.s.MaterializedInstances("division")) != 0 {
		t.Fatal("quest population leaked into default world")
	}
	if got, ok := w.s.Get("division", aa[0].Gid); !ok || got != aa[0] {
		t.Fatal("GID resolver lost instance owner")
	}
	// A batch cannot span two population doors, even at identical coordinates.
	if _, ok := w.s.ApplyDamageBatch("division", []MonsterDamagePlan{
		{GID: aa[0].Gid, ExpectedHP: aa[0].CurrentHP, Damage: 1},
		{GID: bb[0].Gid, ExpectedHP: bb[0].CurrentHP, Damage: 1},
	}); ok {
		t.Fatal("cross-layer damage batch accepted")
	}
	if hit, ok := w.s.ApplyDamage("division", aa[0].Gid, aa[0].CurrentHP); !ok || !hit.Fatal {
		t.Fatal("fatal transition rejected")
	}
	w.s.ArmNestFromReward("division", aa[0].Gid, 2)
	if !w.s.worldPopulations[populationKey{"division", a}].nests[0].partyArmed || w.s.worldPopulations[populationKey{"division", b}].nests[0].partyArmed {
		t.Fatal("nest arming crossed layers")
	}
	if got, _ := w.s.Get("division", bb[0].Gid); got.CurrentHP != bb[0].CurrentHP {
		t.Fatal("damage crossed layers")
	}
	if !w.s.ReleasePopulation("division", a) {
		t.Fatal("release rejected")
	}
	if _, ok := w.s.Get("division", aa[0].Gid); ok {
		t.Fatal("released actor still resolves")
	}
	if _, ok := w.s.ApplyDamage("division", aa[0].Gid, 1); ok {
		t.Fatal("stale callback mutated retired actor")
	}
	replacement, status := w.s.AllocatePopulation("division", a.ID)
	if status != instance.Success || replacement == a {
		t.Fatal("reused wire ID did not get fresh lifetime")
	}
	if w.s.ReleasePopulation("division", a) || len(w.s.PopulationInstances("division", a, regions)) != 0 {
		t.Fatal("stale lifetime reached replacement")
	}
	if len(w.s.PopulationInterestInstances("division", a, viewer, w.now.UnixMilli())) != 0 {
		t.Fatal("retired observer lease adopted replacement")
	}
	w.now = w.now.Add(time.Second)
	w.s.AdvancePopulation(w.now.UnixMilli())
	cc := w.s.PopulationInstances("division", replacement, regions)
	if len(cc) != 1 || cc[0].Gid == aa[0].Gid || cc[0].Gid == bb[0].Gid {
		t.Fatal("replacement reused live identities", cc)
	}
	if got, _ := w.s.Get("division", bb[0].Gid); got != bb[0] {
		t.Fatal("release changed other population")
	}
}

func TestPopulationRetirementRequestSurvivesLastResidentDeparture(t *testing.T) {
	s := NewMonsterState(monster.TemplateFromParts(nil, nil))
	lease, status := s.AllocatePopulation("a", instance.Pack(10, 1))
	if status != instance.Success {
		t.Fatal(status)
	}
	if s.AdmitPopulationPC("a", lease, 42, false) != instance.Success {
		t.Fatal("admission")
	}
	if roster, ok := s.BeginPopulationRetirement("a", lease); !ok || len(roster) != 1 {
		t.Fatal("retirement")
	}
	if len(s.PendingPopulationRetirements("a")) != 0 {
		t.Fatal("release requested before evacuation")
	}
	if status, requested := s.LeavePopulationPC("a", lease, 42); status != instance.Success || !requested {
		t.Fatal(status, requested)
	}
	for n := 0; n < 2; n++ {
		if pending := s.PendingPopulationRetirements("a"); len(pending) != 1 || pending[0] != lease {
			t.Fatal("request lost", pending)
		}
	}
	if !s.ReleasePopulation("a", lease) || len(s.PendingPopulationRetirements("a")) != 0 {
		t.Fatal("release did not acknowledge request")
	}
}
