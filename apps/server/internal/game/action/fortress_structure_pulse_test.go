/*
===========================================================================

fortress_structure_pulse_test.go - the tower and stone pulses at war

===========================================================================
*/

package action

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestCreatureImpactCountRunsTheRatioRecord

A pdm2 program has one impact per hit even though it authors no
attack block; a status cast keeps its one record; an authored count
stands.
================
*/
func TestCreatureImpactCountRunsTheRatioRecord(t *testing.T) {
	ratio := enterworld.SkillRow{RatioDamage: enterworld.SkillRatioDamage{Present: true, Percent: 14}}
	if got := creatureImpactCount(ratio); got != 1 {
		t.Fatalf("ratio skill impacts = %d, want 1", got)
	}
	authored := enterworld.SkillRow{RatioDamage: enterworld.SkillRatioDamage{Present: true, Percent: 14}}
	authored.Attack.ImpactCount = 3
	if got := creatureImpactCount(authored); got != 3 {
		t.Fatalf("authored impacts = %d, want 3", got)
	}
	if got := creatureImpactCount(enterworld.SkillRow{}); got != 0 {
		t.Fatalf("empty skill impacts = %d, want 0", got)
	}
}

/*
================
pulseFixture

A Jangan fortress world whose fort stone and guard tower carry their
authored default skills (the stone's at 22 percent, the tower's at
14), the shipped skill table behind them, and the fixture character
inside in guild 77.
================
*/
func pulseFixture(t *testing.T) (*Runtime, *enterworld.Character, *fakeClock) {
	t.Helper()
	template := monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{
			19553: {RefObjID: 19553, MaxHP: 900, ScaleDenom: 100, Structure: true, CombatPinned: true, TypeID4: structureKindFortStone,
				DefaultSkillIDs: [10]uint32{25046}},
			19536: {RefObjID: 19536, MaxHP: 500, ScaleDenom: 100, Structure: true, CombatPinned: true, TypeID4: structureKindGuardTower, CanRevive: true, CostRevive: 100,
				DefaultSkillIDs: [10]uint32{20530}},
		},
		[]monster.NestRow{
			{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: 19553, RegionID: 0x62aa, X: 100, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1, EventStructID: 84},
			{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: 19536, RegionID: 0x62aa, X: 140, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1, EventStructID: 85},
		},
	)
	rt, c, clock := fortressFixtureWithPopulation(t, testFieldFortGate, template)
	// The shared fixture leaves the level watermark at 1 under a level-20
	// character; combat stats refuse that, so no strike could land.
	watermark := int64(20)
	c.MaxLevel = &watermark
	deps := rt.deps.(*enterworld.Deps)
	deps.Skills = shippedSkills(t)
	// The shared fixture installs no roster; without one no body radius
	// resolves and the area candidate scan sees nobody.
	deps.Roster = &enterworld.Roster{
		Format: "sro-server-character-authority", Version: 3,
		Models: []enterworld.RosterModel{{
			Codename: "CHAR_CH_MAN_ADVENTURER", RefObjID: 1907, BodyRadius: 4, Knockdown: 3, KORecoverMs: 3000,
		}},
	}
	guild := int64(77)
	c.GuildID = &guild
	enterFortress(t, rt, c)
	rt.Monsters.AdvancePopulation(clock.NowMs() + monster.NestHiveTickMs)
	if rt.Monsters.StandingStructures(testDivision, instance.Pack(2, 1), structureKindGuardTower) != 1 {
		t.Fatal("the fortress world holds no guard tower")
	}
	return rt, c, clock
}

/*
================
placeResident

Stands a resident on a point of the fortress world.
================
*/
func placeResident(rt *Runtime, c *enterworld.Character, at simulation.Spawn) {
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(world *simulation.WorldState) { world.Spawn = at })
}

/*
================
fellStructure

A fatal batch on one standing structure, without the defeat queue.
================
*/
func fellStructure(t *testing.T, rt *Runtime, kind uint8) {
	t.Helper()
	for _, row := range rt.Monsters.WorldStructures(testDivision, instance.Pack(2, 1)) {
		if row.Ref.TypeID4 != kind || row.CurrentHP == 0 {
			continue
		}
		results, ok := rt.Monsters.ApplyDamageBatch(testDivision, []simulation.MonsterDamagePlan{{
			GID: row.Gid, ExpectedHP: row.CurrentHP, Damage: row.CurrentHP,
		}})
		if !ok || len(results) != 1 || !results[0].Fatal {
			t.Fatalf("structure %d did not fall", row.Ref.RefObjID)
		}
		return
	}
	t.Fatalf("no standing structure of kind %d", kind)
}

/*
================
TestFortressTowerAndStonePulseInWar

With no war the structures are silent. In the war the tower (14) and
the stone (22) each take their percent of an enemy resident's maximum
hit points every five seconds, the first pulse one period after the
structures are first seen standing in the war; the holder's guild and
an enemy beyond the area take nothing. Once the tower falls only the
stone keeps pulsing, and the war's end silences both.
================
*/
func TestFortressTowerAndStonePulseInWar(t *testing.T) {
	rt, c, clock := pulseFixture(t)
	now := clock.NowMs()
	jangan := uint32(0)
	for _, record := range rt.Fortresses.Records(testDivision) {
		if record.CodeName == "FORTRESS_JANGAN" {
			jangan = record.ID
		}
	}
	if jangan == 0 {
		t.Fatal("no Jangan fortress record")
	}
	rt.Fortresses.Occupy(testDivision, jangan, 77)
	enemy := seatAtTheStone(t, rt, c, 51, "PulseEnemy", 88)
	far := seatAtTheStone(t, rt, c, 52, "PulseFar", 88)
	placeResident(rt, enemy, simulation.Spawn{RegionID: 0x62aa, X: 150, Y: 20, Z: 100})
	placeResident(rt, far, simulation.Spawn{RegionID: 0x62aa, X: 1140, Y: 20, Z: 100})
	// The holder's own resident stands inside the area too: only the
	// side test keeps him whole.
	placeResident(rt, c, simulation.Spawn{RegionID: 0x62aa, X: 160, Y: 20, Z: 100})
	heal := func(ch *enterworld.Character) int64 {
		full, _, _, _ := rt.playerKeeperVitals(testDivision, ch)
		ch.CurrentHP = &full
		return full
	}
	maxHP, selfMax := heal(enemy), heal(c)
	heal(far)
	if maxHP <= 0 {
		t.Fatal("the enemy resident has no hit points")
	}
	towerHit := int64(math.Round(0.14 * float64(maxHP)))
	stoneHit := int64(math.Round(0.22 * float64(maxHP)))

	// No war: nothing pulses and nothing is scheduled.
	if out := rt.advanceFortressStructurePulses(now); len(out) != 0 {
		t.Fatalf("pulses outside the war: %+v", out)
	}
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	// First sight anchors the clocks; the first pulse is one period in.
	if out := rt.advanceFortressStructurePulses(now); len(out) != 0 {
		t.Fatalf("a pulse on first sight: %+v", out)
	}
	if out := rt.advanceFortressStructurePulses(now + 4999); len(out) != 0 {
		t.Fatalf("a pulse before its period: %+v", out)
	}
	out := rt.advanceFortressStructurePulses(now + 5000)
	if len(out) == 0 {
		t.Fatal("the first pulse sent no frames")
	}
	if got, want := enterworld.CurrentHP(enemy), maxHP-towerHit-stoneHit; got != want {
		t.Fatalf("enemy at %d hit points after one cycle, want %d (max %d)", got, want, maxHP)
	}
	if enterworld.CurrentHP(c) != selfMax || enterworld.CurrentHP(far) != maxHP {
		t.Fatalf("defender at %d of %d, far enemy at %d of %d", enterworld.CurrentHP(c), selfMax, enterworld.CurrentHP(far), maxHP)
	}

	// The tower falls; only the stone keeps pulsing.
	fellStructure(t, rt, structureKindGuardTower)
	rt.advanceFortressStructurePulses(now + 10000)
	if got, want := enterworld.CurrentHP(enemy), maxHP-towerHit-2*stoneHit; got != want {
		t.Fatalf("enemy at %d hit points after the stone's second pulse, want %d", got, want)
	}

	// The war ends: silence again.
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, false)
	rt.advanceFortressStructurePulses(now + 15000)
	if got, want := enterworld.CurrentHP(enemy), maxHP-towerHit-2*stoneHit; got != want {
		t.Fatalf("enemy at %d hit points after the war, want %d", got, want)
	}
}
