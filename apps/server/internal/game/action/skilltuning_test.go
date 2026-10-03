/*
===========================================================================

skilltuning_test.go - Tuning Noise through the single-target offense owner

A pdmg hit takes its authored amount from a monster whatever its defense,
and the dmgt share of the HP it actually took comes back as the Bard's MP,
capped at maximum MP (Bard specification, rule 10).

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// tuningNoiseCode is the last tier: pdmg(1115) dmgt(0,100) getv(BDMD).
	tuningNoiseCode   = "SKILL_EU_BARD_FORGETA_MPABSORB_A_12"
	tuningNoiseAmount = 1115
	// tuningDefense dwarfs any level-1 attack: an att lane against it
	// would fall to the 5 % minimum-damage fallback.
	tuningDefense = 1000000
	// tuningMonsterHP keeps the monster alive through the hit.
	tuningMonsterHP = 1000000
	// tuningIntellect lifts the level-1 caster's maximum MP well above
	// the amount, so the drain is observable below the cap.
	tuningIntellect = 2000
)

/*
================
tuningFixture

The combat fixture's monster rebuilt with tuningDefense on both lanes and
hp maximum HP, and a European Bard with Tuning Noise learned, a harp in
the weapon slot and currentMP.
================
*/
func tuningFixture(t *testing.T, hp uint32, currentMP int64) (*Runtime, *enterworld.Character, enterworld.SkillRow, monster.Instance) {
	t.Helper()
	rt, clock, c, primary := newCombatTestRuntime(t, hp)
	row, ok := shippedSkills(t).SkillByCodename(tuningNoiseCode)
	if !ok || !row.FixedDamage.Present || row.OffenseRefusal != "" {
		t.Fatalf("%s not admitted: %q", tuningNoiseCode, row.OffenseRefusal)
	}
	ref := primary.Ref
	ref.PhysicalDefense, ref.MagicalDefense = tuningDefense, tuningDefense
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{ref.RefObjID: ref},
		[]monster.NestRow{{SpawnPoint: primary.Spawn, RetailEvidence: true, MaxCount: 1}}))
	rt.Monsters.SetTimeSource(clock.Now)
	rt.Monsters.StartDivision(testDivision)
	rt.Monsters.AdvancePopulation(rt.Monsters.CurrentTimeMillis())
	monsters := rt.Monsters.InstancesInRegions(testDivision, []uint16{primary.Spawn.RegionID})
	if len(monsters) != 1 {
		t.Fatalf("materialized %d monsters, want one", len(monsters))
	}
	rt.deps.SkillData().(staticSkillSource)[row.ID] = row
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	c.Skills = []uint32{row.ID}
	c.Intellect = testInt64(tuningIntellect)
	c.CurrentMP = testInt64(currentMP)
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = int64(row.RequiredWeaponKinds[0])
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	return rt, c, row, monsters[0]
}

/*
================
castTuning

Press the skill on the monster; the row has no preparation, so the hit
lands in the command.
================
*/
func castTuning(t *testing.T, rt *Runtime, c *enterworld.Character, skill enterworld.SkillRow, target monster.Instance) OpResult {
	t.Helper()
	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if open, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || open.Payload[0] != 1 {
		t.Fatalf("Tuning Noise refused: %+v", out)
	}
	return out
}

/*
================
TestTuningNoiseDealsItsAmountThroughDefense

A monster whose defense would floor any att hit loses exactly the pdmg
amount, and the Bard gets exactly that much MP back after paying the
prepared cost, published in a private 0x33A6.
================
*/
func TestTuningNoiseDealsItsAmountThroughDefense(t *testing.T) {
	rt, c, skill, target := tuningFixture(t, tuningMonsterHP, 0)
	prepared, err := rt.preparedExecutionMPCost(testDivision, c, skill)
	if err != nil || prepared == 0 {
		t.Fatalf("prepared cost %d: %v", prepared, err)
	}
	c.CurrentMP = testInt64(prepared)
	_, maxMP, _, _ := rt.playerKeeperVitals(testDivision, c)
	if maxMP < tuningNoiseAmount {
		t.Fatalf("maximum MP %d below the amount", maxMP)
	}

	out := castTuning(t, rt, c, skill, target)
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	if lost := target.CurrentHP - after.CurrentHP; lost != tuningNoiseAmount {
		t.Fatalf("monster lost %d, want the pdmg amount %d", lost, tuningNoiseAmount)
	}
	if _, _, _, mp := rt.playerKeeperVitals(testDivision, c); mp != tuningNoiseAmount {
		t.Fatalf("MP %d after the cast, want %d (prepared %d spent, %d drained)", mp, tuningNoiseAmount, prepared, tuningNoiseAmount)
	}
	if _, ok := findFrame(out.ActorPrivate, simulation.OpVitalsUpdate); !ok {
		t.Fatal("no private 0x33A6 for the drained MP")
	}
}

/*
================
TestTuningNoiseDrainStopsAtMaximumMP

A Bard at full MP pays the cost and drains back only up to the maximum.
================
*/
func TestTuningNoiseDrainStopsAtMaximumMP(t *testing.T) {
	rt, c, skill, target := tuningFixture(t, tuningMonsterHP, 0)
	_, maxMP, _, _ := rt.playerKeeperVitals(testDivision, c)
	c.CurrentMP = testInt64(maxMP)
	castTuning(t, rt, c, skill, target)
	if _, _, _, mp := rt.playerKeeperVitals(testDivision, c); mp != maxMP {
		t.Fatalf("MP %d, want the maximum %d", mp, maxMP)
	}
}

/*
================
TestTuningNoiseDrainsOnlyTheHPItTook

A monster with less HP left than the amount dies, and the Bard drains
only the HP the hit actually took.
================
*/
func TestTuningNoiseDrainsOnlyTheHPItTook(t *testing.T) {
	const remaining = 400
	rt, c, skill, target := tuningFixture(t, remaining, 0)
	prepared, err := rt.preparedExecutionMPCost(testDivision, c, skill)
	if err != nil {
		t.Fatal(err)
	}
	c.CurrentMP = testInt64(prepared)
	castTuning(t, rt, c, skill, target)
	if after, _ := rt.Monsters.Get(testDivision, target.Gid); after.CurrentHP != 0 {
		t.Fatalf("monster kept %d HP", after.CurrentHP)
	}
	if _, _, _, mp := rt.playerKeeperVitals(testDivision, c); mp != remaining {
		t.Fatalf("MP %d, want the %d HP taken", mp, remaining)
	}
}
