/*
===========================================================================

targetsnapshot_bench_test.go - what target resolution pays per verdict

#1820 part C: resolution used to detach a whole character record for
every target and judge the copy. The Rejected arms weigh targets the
gates refuse (dead players, dead companions' owners judged through
their COS); the Admitted arms are the control - a target that passes
still detaches the same record, so its cost must not move. Run:

	go test -run xxx -bench TargetSnapshot -benchmem ./internal/game/action/

===========================================================================
*/
package action

import (
	"fmt"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

// targetSnapshotBenchPlayers stands one attacker beside count victims
// in a PvP region. hp is every victim's current HP: zero makes every
// victim a rejected target, 100 admits them all.
func targetSnapshotBenchRuntime(b *testing.B, count int, hp int64) (*Runtime, *enterworld.Character, []*enterworld.Character, int64) {
	b.Helper()
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}
	attacker := &enterworld.Character{
		ID:            3,
		Name:          "asd2",
		ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		Level:         testInt64(1),
		MaxLevel:      testInt64(1),
		CurrentHP:     testInt64(100),
		Strength:      testInt64(20),
		Intellect:     testInt64(20),
	}
	characters := []*enterworld.Character{attacker}
	victims := make([]*enterworld.Character, 0, count)
	attacker.Aggressions = map[uint32]uint32{}
	for i := 0; i < count; i++ {
		victim := *attacker
		victim.ID = int64(100 + i)
		victim.Name = fmt.Sprintf("bench%02d", i)
		victim.CurrentHP = testInt64(hp)
		victim.Aggressions = nil
		characters = append(characters, &victim)
		victims = append(victims, &victim)
		attacker.Aggressions[enterworld.ObjectIDForCharacter(&victim)] = playerAggressionTicks
	}
	for _, c := range characters {
		spawn := enterworld.WorldSpawn{RegionID: testInt64(int64(pvpRegion)), X: testFloat64(960), Y: testFloat64(20), Z: testFloat64(458), Angle: testInt64(0)}
		c.World = &enterworld.CharacterWorld{Spawn: &spawn, SpawnSet: true}
	}
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: characters},
		Roster: &enterworld.Roster{
			Format: "sro-server-character-authority", Version: 3,
			Models: []enterworld.RosterModel{{
				Codename: "CHAR_CH_MAN_ADVENTURER", RefObjID: 1907, BodyRadius: 4, Knockdown: 3, KORecoverMs: 3000,
			}},
		},
		Items: testItems(),
	}
	rt := NewRuntime(deps, nil)
	rt.Now = clock.Now
	rt.RewardActorPresent = func(string, string) bool { return true }
	for _, c := range characters {
		rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
			func(w *simulation.WorldState) {
				w.Spawn = simulation.Spawn{RegionID: pvpRegion, X: 960, Y: 20, Z: 458}
				w.SpawnSet = true
			})
	}
	return rt, attacker, victims, clock.NowMs()
}

/*
================
testFloat64
================
*/
func testFloat64(value float64) *float64 { return &value }

/*
================
BenchmarkTargetSnapshotAreaRejected

Every present player is dead: the old walk detached all 24 records
and dropped each copy. No copy should be made now.
================
*/
func BenchmarkTargetSnapshotAreaRejected(b *testing.B) {
	rt, attacker, _, now := targetSnapshotBenchRuntime(b, 24, 0)
	skill := enterworld.SkillRow{Attack: enterworld.SkillAttack{Present: true}}
	q := areaQuery{selects: areaSelectHostile, division: testDivision, caster: attacker, skill: skill,
		center: rt.liveSpawn(simulation.WorldKey(testDivision, attacker.Name), attacker, now), reach: 50, now: now}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := rt.areaPlayerCandidates(q); len(got) != 0 {
			b.Fatalf("dead crowd produced %d candidates", len(got))
		}
	}
}

/*
================
BenchmarkTargetSnapshotAreaAdmitted

The control: every present player is a living, in-range candidate and
still detaches its record.
================
*/
func BenchmarkTargetSnapshotAreaAdmitted(b *testing.B) {
	rt, attacker, _, now := targetSnapshotBenchRuntime(b, 24, 100)
	skill := enterworld.SkillRow{Attack: enterworld.SkillAttack{Present: true}}
	q := areaQuery{selects: areaSelectHostile, division: testDivision, caster: attacker, skill: skill,
		center: rt.liveSpawn(simulation.WorldKey(testDivision, attacker.Name), attacker, now), reach: 50, now: now}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rt.areaPlayerCandidates(q)
	}
}

/*
================
benchUnsummonedPets

Give every victim per unsummoned companions in its bag: owned records
the strike may never take, which must cost the area walk nothing.
================
*/
func benchUnsummonedPets(victims []*enterworld.Character, per int) {
	for i, victim := range victims {
		for j := 0; j < per; j++ {
			victim.MissionInventory = append(victim.MissionInventory, enterworld.InventoryRow{
				Slot: int64(13 + j),
				Summon: &enterworld.CharacterCOS{
					GID: uint32(50000 + i*per + j), RefObjID: 2001, Codename: "COS_P_WOLF_001",
					CurrentHP: 100, CurrentMP: 10, Summoned: false, Satiety: 100,
				},
			})
		}
	}
}

/*
================
BenchmarkTargetSnapshotAreaUnsummonedPets

Every present player is a living candidate carrying eight unsummoned
companions: the companions add no candidate and no resolution cost -
the walk still detaches exactly the players it admits.
================
*/
func BenchmarkTargetSnapshotAreaUnsummonedPets(b *testing.B) {
	rt, attacker, victims, now := targetSnapshotBenchRuntime(b, 24, 100)
	benchUnsummonedPets(victims, 8)
	skill := enterworld.SkillRow{Attack: enterworld.SkillAttack{Present: true}}
	q := areaQuery{selects: areaSelectHostile, division: testDivision, caster: attacker, skill: skill,
		center: rt.liveSpawn(simulation.WorldKey(testDivision, attacker.Name), attacker, now), reach: 50, now: now}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := rt.areaPlayerCandidates(q); len(got) != 24 {
			b.Fatalf("crowd with unsummoned pets produced %d candidates, want 24", len(got))
		}
	}
}

/*
================
BenchmarkTargetSnapshotResolveRejected
================
*/
func BenchmarkTargetSnapshotResolveRejected(b *testing.B) {
	rt, attacker, victims, now := targetSnapshotBenchRuntime(b, 1, 0)
	gid := enterworld.ObjectIDForCharacter(victims[0])
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := rt.resolveCombatTarget(testDivision, attacker, gid, now); ok {
			b.Fatal("the dead victim resolves")
		}
	}
}

/*
================
BenchmarkTargetSnapshotResolveAdmitted
================
*/
func BenchmarkTargetSnapshotResolveAdmitted(b *testing.B) {
	rt, attacker, victims, now := targetSnapshotBenchRuntime(b, 1, 100)
	gid := enterworld.ObjectIDForCharacter(victims[0])
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := rt.resolveCombatTarget(testDivision, attacker, gid, now); !ok {
			b.Fatal("the living victim does not resolve")
		}
	}
}

/*
================
BenchmarkTargetSnapshotExistsRejected

#2139: the exists-only verdict on a refused target detaches nothing.
================
*/
func BenchmarkTargetSnapshotExistsRejected(b *testing.B) {
	rt, attacker, victims, _ := targetSnapshotBenchRuntime(b, 1, 0)
	gid := enterworld.ObjectIDForCharacter(victims[0])
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if rt.combatTargetExists(testDivision, attacker, gid) {
			b.Fatal("the dead victim exists")
		}
	}
}

/*
================
BenchmarkTargetSnapshotExistsAdmitted

#2139: the control above still pays the whole record for its answer;
the exists-only verdict on an admitted target must not.
================
*/
func BenchmarkTargetSnapshotExistsAdmitted(b *testing.B) {
	rt, attacker, victims, _ := targetSnapshotBenchRuntime(b, 1, 100)
	gid := enterworld.ObjectIDForCharacter(victims[0])
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !rt.combatTargetExists(testDivision, attacker, gid) {
			b.Fatal("the living victim does not exist")
		}
	}
}
