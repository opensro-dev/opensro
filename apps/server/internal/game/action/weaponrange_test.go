/*
===========================================================================

weaponrange_test.go - basic weapon ranges

Authored basic-attack ranges, caster pursuit and reach parameters.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestAuthoredBasicWeaponRangesAndCasterPursuit
================
*/
func TestAuthoredBasicWeaponRangesAndCasterPursuit(t *testing.T) {
	licensed.RequireGameData(t)
	source := enterworld.NewTextdataSkills(licensed.RetailTextdataDir(t))
	for _, tc := range []struct {
		code       string
		kind       uint8
		item, want float64
	}{
		{"SKILL_PUNCH_01", 1, 0, 6},
		{"SKILL_CH_SWORD_BASE_01", 2, 6, 6}, {"SKILL_CH_SPEAR_BASE_01", 4, 6, 6}, {"SKILL_CH_SWORD_BASE_01", 3, 6, 6}, {"SKILL_CH_SPEAR_BASE_01", 5, 6, 6}, {"SKILL_CH_BOW_BASE_01", 6, 180, 180},
		{"SKILL_EU_SWORD_BASE_01", 7, 6, 6}, {"SKILL_EU_TSWORD_BASE_01", 8, 6, 6}, {"SKILL_EU_AXE_BASE_01", 9, 6, 6},
		{"SKILL_EU_CROSSBOW_BASE_01", 12, 180, 180}, {"SKILL_EU_DAGGER_BASE_01", 13, 6, 6},
		{"SKILL_EU_STAFF_BASE_01", 11, 6, 150}, {"SKILL_EU_WAND_WARLOCK_BASE_01", 10, 6, 150},
		{"SKILL_EU_HARP_BASE_01", 14, 6, 150}, {"SKILL_EU_WAND_CLERIC_BASE_01", 15, 6, 150},
	} {
		t.Run(tc.code, func(t *testing.T) {
			skill, ok := source.SkillByCodename(tc.code)
			if !ok {
				t.Fatal("missing authored row")
			}
			if got := skillActionReach(skill, combat.Loadout{HasWeapon: tc.kind != 1, WeaponKind: tc.kind, ActionRange: tc.item}, combat.Stats{}); float64(got) != tc.want {
				t.Fatalf("reach=%g want %g", got, tc.want)
			}
			if tc.want != 150 {
				return
			}
			for _, distance := range []float64{100, 250} {
				rt, clock, c, target := newCombatTestRuntime(t, 100000)
				c.RaceIndex = testInt64(enterworld.RaceEurope)
				c.ModelCodename = "CHAR_EU_MAN_NOBLE"
				c.Skills = []uint32{skill.ID}
				*c.World.Spawn.X = target.Spawn.X - distance
				weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
				weapon.TypeIDs[3] = int64(tc.kind)
				c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
				rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
				result := rt.beginBasicAttack(testDivision, c, wire.BasicAttackEngage{TargetGid: target.Gid}, rt.Now().UnixMilli())
				if result.DiagnosticRefusal != "" {
					t.Fatal(result.DiagnosticRefusal)
				}
				state := rt.Worlds.Snapshot(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) })
				if distance == 100 {
					if state.MoveSegment.Valid() {
						t.Fatal("ranged basic walked into melee")
					}
					rt.advanceProjectileCasts(clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1)
					after, _ := rt.Monsters.Get(testDivision, target.Gid)
					if after.CurrentHP >= target.CurrentHP {
						t.Fatalf("in-range basic did not damage: %+v", result)
					}
				} else {
					intents := rt.combatIntentSnapshot()
					if len(intents) != 1 || intents[0].ActionReach != 150 || !intents[0].HasApproach {
						t.Fatalf("wrong pursuit: %+v", intents)
					}
				}
			}
		})
	}
}

/*
================
TestReachAddsCasterRangeParameters
================
*/
// 4ADAB8: a row that asks for CBRA or WIRU adds the caster's value; a row
// that does not ignores it. The shipped passives set exactly those values.
func TestReachAddsCasterRangeParameters(t *testing.T) {
	crossbow := shippedOffense(t, "SKILL_EU_CROSSBOW_BASE_01")
	sword := shippedOffense(t, "SKILL_EU_SWORD_BASE_01")
	if !crossbow.Attack.Parameters.Has(enterworld.ParameterCrossbowRange) {
		t.Fatal("crossbow base lost its CBRA getv")
	}
	for code, want := range map[string]struct {
		slot  enterworld.SkillParameter
		value uint32
	}{
		"SKILL_EU_ROG_BOWP_RANGE_A_04":      {enterworld.ParameterCrossbowRange, 40},
		"SKILL_EU_WIZARD_MANAP_RANGE_A_03":  {enterworld.ParameterWizardRange, 30},
		"SKILL_EU_ROG_STEALTHP_DAMAGE_A_01": {enterworld.ParameterStealthStrike, 62},
	} {
		passive := shippedOffense(t, code).PassiveParameters
		if !passive.Pinned || !passive.Mask.Has(want.slot) || passive.Values[want.slot] != want.value {
			t.Errorf("%s passive %+v", code, passive)
		}
	}

	var caster combat.Stats
	caster.SkillParameters[enterworld.ParameterCrossbowRange] = 40
	bow := combat.Loadout{HasWeapon: true, WeaponKind: 12, ActionRange: 180}
	if got := skillActionReach(crossbow, bow, caster); got != 220 {
		t.Fatalf("crossbow reach %g, want 220", got)
	}
	blade := combat.Loadout{HasWeapon: true, WeaponKind: 7, ActionRange: 6}
	if got := skillActionReach(sword, blade, caster); got != 6 {
		t.Fatalf("sword took the crossbow bonus: %g", got)
	}
}
