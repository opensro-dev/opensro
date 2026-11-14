/*
===========================================================================
petcrow_rework_test.go - actual restored-mana thresholds and clone gear effects.
===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestCrowRechargeUsesHighestBook
================
*/
func TestCrowRechargeUsesHighestBook(t *testing.T) {
	_, clock, c, _, _ := tailwindFixture(t, 1)
	skill := learnPetBook(t, c, 51)
	c.ActiveCOS.Training.Levels[skill.Index()] = 10
	row, _ := crowRechargeRow(c.ActiveCOS, clock.NowMs())
	if row.RechargeMs != 900000 || row.ReadyAtMs != clock.NowMs()+900000 || row.Capacity != 3 || row.Charges != 1 {
		t.Fatal("highest book recharge", row)
	}
}

/*
================
TestCrowMagicStrikeKeepsNativeFlagOff
================
*/
func TestCrowMagicStrikeKeepsNativeFlagOff(t *testing.T) {
	rt, clock, c, mob, key := tailwindFixture(t, 1)
	step, ok := rt.elementalCompanion(testDivision, c, key.gid, "Crow", clock.NowMs())
	if !ok {
		t.Fatal("crow unavailable")
	}
	target, ok := rt.resolvePetCombatTarget(step, mob.Gid)
	if !ok {
		t.Fatal("target unavailable")
	}
	native, ok := rt.petAttackSkill(step, &petCombatIntent{}, simulation.AttackTarget{})
	if !ok {
		t.Fatal("native crow strike unavailable")
	}
	magic, _ := rt.petTrainedDecide(step, native, target)
	if magic.Attack.Flags&8 == 0 || magic.Attack.Flags&4 != 0 {
		t.Fatal("crow did not use magical damage")
	}
	rt.AttackPetExpansion = false
	off, trained := rt.petTrainedDecide(step, native, target)
	if off.Attack != native.Attack || trained != nil {
		t.Fatal("crow rework changed native flag-off combat")
	}
}

/*
================
TestManaSpringActualRestorationBurst
================
*/
func TestManaSpringActualRestorationBurst(t *testing.T) {
	rt, clock, c, mob, key := tailwindFixture(t, 1)
	skill := learnPetBook(t, c, 54)
	c.ActiveCOS.Training.Levels[skill.Index()] = 10
	c.Intellect = testInt64(3000)
	c.CurrentMP = testInt64(1)
	c.MissionInventory = nil
	step, ok := rt.elementalCompanion(testDivision, c, key.gid, "Crow", clock.NowMs())
	if !ok {
		t.Fatal("crow unavailable")
	}
	step.trained = &petTrainedStrike{}
	if _, _, err := rt.playerCombatStats(testDivision, c); err != nil {
		t.Fatal(err)
	}
	rt.crowManaSpring(step, 30000)
	if *c.CurrentMP != 9001 || step.state.crowManaRestored != 9000 {
		t.Fatal("actual MP progress", *c.CurrentMP, step.state.crowManaRestored)
	}
	rt.advanceCrowKit(key, clock.NowMs())
	if step.state.crowManaRestored != 9000 {
		t.Fatal("unlearned Raven's Refuge reset Mana Spring")
	}
	before, _ := rt.Monsters.Get(testDivision, mob.Gid)
	rt.crowManaSpring(step, 10000)
	after, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if before.CurrentHP-after.CurrentHP != 5000 || step.state.crowManaRestored != 2000 {
		t.Fatal("threshold true damage/carry", before.CurrentHP, after.CurrentHP, step.state.crowManaRestored)
	}
	stats, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	_, maxMP, _, _ := keeperVitalsFrom(c, stats, nil)
	c.CurrentMP = testInt64(maxMP)
	rt.crowManaSpring(step, 10000)
	if step.state.crowManaRestored != 2000 {
		t.Fatal("full mana generated burst credit")
	}
	c.CurrentMP = testInt64(maxMP - 100)
	rt.crowManaSpring(step, 10000)
	if step.state.crowManaRestored != 2100 {
		t.Fatal("overheal counted instead of actual MP recovery")
	}
}

/*
================
TestBroodGearProcsBothTriggers
================
*/
func TestBroodGearProcsBothTriggers(t *testing.T) {
	for _, taken := range []bool{false, true} {
		rt, clock, c, mob, key := tailwindFixture(t, 1)
		group := "ProcFire"
		if taken {
			group = "ProcFireTaken"
		}
		wearProc(t, c, group, 9)
		skill := learnPetBook(t, c, 49)
		rt.castCrowBrood(testDivision, c, key.gid, skill, clock.NowMs())
		if len(c.Brood) != 1 {
			t.Fatal("egg missing")
		}
		at := simulation.Spawn{RegionID: mob.Spawn.RegionID, X: mob.Spawn.X, Y: mob.Spawn.Y, Z: mob.Spawn.Z}
		egg := c.Brood[0]
		rt.petGearProcs(testDivision, c, egg.GID, mob.Gid, at, taken, clock.NowMs())
		before, _ := rt.Monsters.Get(testDivision, mob.Gid)
		if before.CurrentHP != mob.CurrentHP {
			t.Fatal("egg triggered offensive gear")
		}
		clock.now = clock.now.Add(30 * time.Second)
		rt.advanceCrowBrood(key, clock.NowMs())
		rt.CombatRoll = func() (uint32, error) { return 0, nil }
		rt.petGearProcs(testDivision, c, egg.GID, mob.Gid, at, taken, clock.NowMs())
		after, _ := rt.Monsters.Get(testDivision, mob.Gid)
		if after.CurrentHP >= before.CurrentHP || companion.ModifiersFor(egg).LeechPct != 0 {
			t.Fatal("clone lost gear proc or became healable", taken, before.CurrentHP, after.CurrentHP)
		}
	}
}
