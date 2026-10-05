/*
===========================================================================

skilloverheal_test.go - Over Healing through the prepared offense owner

Over Healing is Tuning Noise's fixed hit without the drain: after its
preparation the monster loses the pdmg amount whatever its defense, and
the Cleric's MP only pays the cast. Glut Healing spreads the same hit over
its area.

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
	// overHealingCode is the last tier: pdmg(7925) cm(2,1), 1834 ms
	// preparation.
	overHealingCode   = "SKILL_EU_CLERIC_BATTLEA_OVERHEAL_A_12"
	overHealingAmount = 7925
)

/*
================
overHealingFixture

The combat fixture's monster rebuilt with tuningDefense on both lanes,
and a European Cleric with Over Healing learned, its weapon in the
weapon slot and enough MP for the cast.
================
*/
func overHealingFixture(t *testing.T) (*Runtime, *enterworld.Character, enterworld.SkillRow, monster.Instance) {
	t.Helper()
	rt, clock, c, primary := newCombatTestRuntime(t, tuningMonsterHP)
	row, ok := shippedSkills(t).SkillByCodename(overHealingCode)
	if !ok || !row.FixedDamage.Present || row.OffenseRefusal != "" || row.ActionCastingTimeMs == 0 {
		t.Fatalf("%s not admitted as a prepared fixed hit: %q", overHealingCode, row.OffenseRefusal)
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
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = int64(row.RequiredWeaponKinds[0])
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	return rt, c, row, monsters[0]
}

/*
================
TestOverHealingDealsItsAmountThroughDefenseWithoutDrain

The press opens the preparation and leaves the monster untouched; the
release takes exactly the pdmg amount from a monster whose defense would
floor any att hit, and the Cleric keeps only what the cast left.
================
*/
func TestOverHealingDealsItsAmountThroughDefenseWithoutDrain(t *testing.T) {
	rt, c, skill, target := overHealingFixture(t)
	prepared, err := rt.preparedExecutionMPCost(testDivision, c, skill)
	if err != nil || prepared == 0 {
		t.Fatalf("prepared cost %d: %v", prepared, err)
	}
	_, maxMP, _, _ := rt.playerKeeperVitals(testDivision, c)
	if maxMP < prepared {
		t.Fatalf("maximum MP %d below the cost %d", maxMP, prepared)
	}
	c.CurrentMP = testInt64(maxMP)

	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if open, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || open.Payload[0] != 1 {
		t.Fatalf("Over Healing refused: %+v", out)
	}
	if before, _ := rt.Monsters.Get(testDivision, target.Gid); before.CurrentHP != target.CurrentHP {
		t.Fatalf("monster hit during the preparation: %d -> %d", target.CurrentHP, before.CurrentHP)
	}

	releasePreparedSkillForTest(t, rt, rt.Now().UnixMilli()+int64(skill.ActionCastingTimeMs)+1)
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	if lost := target.CurrentHP - after.CurrentHP; lost != overHealingAmount {
		t.Fatalf("monster lost %d, want the pdmg amount %d", lost, overHealingAmount)
	}
	if _, _, _, mp := rt.playerKeeperVitals(testDivision, c); mp != maxMP-prepared {
		t.Fatalf("MP %d after the cast, want %d (maximum %d less the cost %d)", mp, maxMP-prepared, maxMP, prepared)
	}
}

/*
================
TestGlutHealingFallsOffAcrossItsArea

Glut Healing's fixed hit through the area owner: after the preparation the
target loses the pdmg amount, and the next two victims lose it at the
area's running percent (65, then 42).
================
*/
func TestGlutHealingFallsOffAcrossItsArea(t *testing.T) {
	const glutHealingCode, glutHealingAmount = "SKILL_EU_CLERIC_BATTLEA_OVERHEAL_B_01", 4780
	rt, targets := areaFixture(t, tuningMonsterHP)
	c := rt.findCharacter(testDivision, "asd2")
	skill := shippedOffense(t, glutHealingCode)
	if !skill.FixedDamage.Present || skill.OffensiveArea.MaxTargets != 3 || skill.OffensiveArea.ReductionPercent != 35 {
		t.Fatalf("%s not admitted as an area fixed hit: %q %+v", glutHealingCode, skill.OffenseRefusal, skill.OffensiveArea)
	}
	// The level-1 fixture cannot afford the authored MP or wield a staff;
	// the fixed hit and its falloff are what is under test.
	skill.RequiredWeaponKinds = [2]uint8{0xff, 0xff}
	skill.Reqi = enterworld.SkillReqi{}
	skill.Consumption.MP = 10
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentMP = testInt64(150)

	start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: targets[0].Gid}.Encode())
	if len(start.Frames) == 0 || start.Frames[0].Opcode != wire.OpSkillCastResult || start.Frames[0].Payload[0] != 1 {
		t.Fatalf("Glut Healing refused: %+v", start)
	}
	releasePreparedSkillForTest(t, rt, rt.Now().UnixMilli()+int64(skill.ActionCastingTimeMs)+1)
	var lost []uint32
	for _, target := range targets {
		after, _ := rt.Monsters.Get(testDivision, target.Gid)
		if taken := target.CurrentHP - after.CurrentHP; taken != 0 {
			lost = append(lost, taken)
		}
	}
	want := []uint32{glutHealingAmount, glutHealingAmount * 65 / 100, glutHealingAmount * 42 / 100}
	if len(lost) != len(want) {
		t.Fatalf("victims lost %v, want %v", lost, want)
	}
	for i := range want {
		if lost[i] != want[i] {
			t.Fatalf("victims lost %v, want %v", lost, want)
		}
	}
}
