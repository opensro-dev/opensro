/*
===========================================================================

passiveprograms_test.go - setv-less passives through the production owners

Blockade and Protection reach the runtime only through
playerCombatStats (learned passives on the keeper) and, for Blockade, the
block roll of resolveCombat. The fixture keeps the level-one stat row; only
the slot-6 weapon's TID4 changes, so the reqi walk is the one variable.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
learnShippedPassive

Teach the fixture character one authored passive rank, admitted by the
production compiler, and return it.
================
*/
func learnShippedPassive(t *testing.T, rt *Runtime, c *enterworld.Character, codename string) enterworld.SkillRow {
	t.Helper()
	row, ok := shippedSkillSource(t).SkillByCodename(codename)
	if !ok || !row.PassiveParameters.Pinned {
		t.Fatalf("%s not admitted: %+v", codename, row.PassiveParameters)
	}
	rt.deps.SkillData().(staticSkillSource)[row.ID] = row
	c.Skills = append(c.Skills, row.ID)
	return row
}

/*
================
equipWeaponKind

Retype the fixture's slot-6 weapon to the given TID4 (2 Chinese sword,
7 one-hand sword, 9 dual axe).
================
*/
func equipWeaponKind(rt *Runtime, c *enterworld.Character, kind int64) {
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = kind
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
}

/*
================
TestBlockadeBlocksAMonsterHitOnlyWithAOneHandSword

Blockade rank 1 (br 15 2, reqi 6 7) is the defender's only block source.
With a roll of 0 any non-zero chance blocks (58F0C1..58F0F3), so a monster's
physical hit is blocked while a one-hand sword is in slot 6 and lands while
the slot holds the fixture's Chinese sword.
================
*/
func TestBlockadeBlocksAMonsterHitOnlyWithAOneHandSword(t *testing.T) {
	rt, _, c, target := newCombatTestRuntime(t, 100000)
	learnShippedPassive(t, rt, c, "SKILL_EU_WARRIOR_SHIELDP_BLOCK_A_01")
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	attacker, err := combat.MonsterInstanceStats(target)
	if err != nil {
		t.Fatal(err)
	}
	hit, _ := rt.deps.SkillData().SkillByID(2)
	blocked := func() (bool, uint8) {
		t.Helper()
		defender, _, err := rt.playerCombatStats(testDivision, c)
		if err != nil {
			t.Fatal(err)
		}
		result, err := rt.resolveCombat(criticalActor{division: testDivision, monster: target.Gid}, hit, attacker, defender)
		if err != nil {
			t.Fatal(err)
		}
		return result.Blocked, combat.BlockChance(attacker, defender, hit.Attack.Flags, hit.Ck)
	}
	if got, chance := blocked(); got || chance != 0 {
		t.Fatalf("Chinese sword: blocked %v chance %d", got, chance)
	}
	equipWeaponKind(rt, c, 7)
	if got, chance := blocked(); !got || chance != 2 {
		t.Fatalf("one-hand sword: blocked %v chance %d, want blocked at 2", got, chance)
	}
}

/*
================
TestProtectionFillsResistanceOnlyWithADualAxe

Protection rank 1 (reat 63 40, real 0x17FAFC0 50 3, reqi 6 9): with a dual
axe the six flat status reductions 0x91..0x96 rise by 40 and the masked
statuses' resistance buckets hold flat 50 at grade 3 in the snapshot the
abnormal roll reads; with the Chinese sword the snapshot is unchanged.
================
*/
func TestProtectionFillsResistanceOnlyWithADualAxe(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	base, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	learnShippedPassive(t, rt, c, "SKILL_EU_WARRIOR_DUALP_ABNORMAL_A_01")
	check := func(equipped bool) {
		t.Helper()
		stats, _, err := rt.playerCombatStats(testDivision, c)
		if err != nil {
			t.Fatal(err)
		}
		for id := uint16(0x91); id <= 0x96; id++ {
			before, _ := base.Param(id)
			after, _ := stats.Param(id)
			if want := before + 40; equipped && after != want || !equipped && after != before {
				t.Fatalf("equipped %v: parameter %x = %v (base %v)", equipped, id, after, before)
			}
		}
		filed := 0
		for _, bucket := range stats.StatusResistance {
			if bucket.Flat == 50 && bucket.Grade == 3 {
				filed++
			} else if bucket.Flat != 0 || bucket.Grade != 0 {
				t.Fatalf("equipped %v: bucket %+v", equipped, bucket)
			}
		}
		if (filed != 0) != equipped {
			t.Fatalf("equipped %v: %d buckets filed", equipped, filed)
		}
	}
	check(false)
	equipWeaponKind(rt, c, 9)
	check(true)
}
