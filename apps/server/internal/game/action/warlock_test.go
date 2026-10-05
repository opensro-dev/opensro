/*
===========================================================================

warlock_test.go - Life Drain, Soul Chaos and Blood Increase

Life Drain takes HP from its target and the caster recovers it (40F750);
Soul Chaos strikes the enemies around its owner every two seconds with a
fixed hit (40F5F0); Blood Increase raises both through BSHP and SAAA.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestWarlockRowsAdmit
================
*/
func TestWarlockRowsAdmit(t *testing.T) {
	a := shippedOffense(t, "SKILL_EU_WARLOCK_BLOODA_LIFEDRAIN_A_01")
	if a.OffenseRefusal != "" || a.LifeSteal != (enterworld.SkillLifeSteal{Present: true, Amount: 27, WeaponPercent: 80, Power: true}) {
		t.Fatalf("Life Drain A = %+v refusal %q", a.LifeSteal, a.OffenseRefusal)
	}
	if a.OffensiveArea.Radius != 0 || !a.Abnormal.Present() {
		t.Fatalf("Life Drain A area %+v, statuses present %v", a.OffensiveArea, a.Abnormal.Present())
	}
	b := shippedOffense(t, "SKILL_EU_WARLOCK_BLOODA_LIFEDRAIN_B_01")
	if !b.LifeSteal.Present || b.OffensiveArea.Radius != 150 || b.OffensiveArea.MaxTargets != 5 || b.OffensiveArea.Shape != 1 {
		t.Fatalf("Life Drain B = %+v area %+v", b.LifeSteal, b.OffensiveArea)
	}
	chaos := shippedOffense(t, "SKILL_EU_WARLOCK_SOULA_CHAOS_A_01").TimedEffect
	if !chaos.Pinned || !chaos.PulseArea.Present || chaos.PulseArea.PeriodMs != 2000 ||
		chaos.PulseArea.Fixed.Amount != 126 || !chaos.PulseArea.Fixed.Power || chaos.PulseArea.Area.Radius != 50 {
		t.Fatalf("Soul Chaos = %+v", chaos)
	}
	blood := shippedOffense(t, "SKILL_EU_WARLOCK_BLOODP_INCREASE_A_01").PassiveParameters
	if !blood.Pinned || blood.Values[enterworld.ParameterLifeStealPower] != 21 || blood.Values[enterworld.ParameterFixedDamagePower] != 28 {
		t.Fatalf("Blood Increase = %+v", blood)
	}
}

/*
================
TestFixedAndLifeStealFormulas
================
*/
func TestFixedAndLifeStealFormulas(t *testing.T) {
	if got := combat.FixedSkillDamage(126, 28, 30, 30); got != 154 {
		t.Fatalf("even-level fixed = %d, want 154", got)
	}
	// Four levels up: (1 - 0.1) * 154 = 138.6 -> 138.
	if got := combat.FixedSkillDamage(126, 28, 30, 34); got != 138 {
		t.Fatalf("higher target fixed = %d, want 138", got)
	}
	// Forty levels up: the floor, 154 * 10 / 100 = 15.
	if got := combat.FixedSkillDamage(126, 28, 30, 70); got != 15 {
		t.Fatalf("floored fixed = %d, want 15", got)
	}
	if got := combat.LifeSteal(100, 30, 30, 1000, 65); got != 65 {
		t.Fatalf("area life steal = %d, want 65", got)
	}
	if got := combat.LifeSteal(100, 30, 30, 40, 65); got != 40 {
		t.Fatalf("capped life steal = %d, want the target's 40 HP unscaled", got)
	}
}

/*
================
warlockFixture

The combat fixture's caster as a European with one Warlock skill learned
and BSHP / SAAA set by a learned Blood Increase.
================
*/
func warlockFixture(t *testing.T, code string) (*Runtime, *fakeClock, *enterworld.Character, enterworld.SkillRow, monster.Instance) {
	t.Helper()
	rt, clock, c, mob := newCombatTestRuntime(t, 100000)
	row, ok := shippedSkills(t).SkillByCodename(code)
	if !ok {
		t.Fatalf("%s missing", code)
	}
	blood, _ := shippedSkills(t).SkillByCodename("SKILL_EU_WARLOCK_BLOODP_INCREASE_A_01")
	skills := rt.deps.SkillData().(staticSkillSource)
	skills[row.ID], skills[blood.ID] = row, blood
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	c.Skills = []uint32{row.ID, blood.ID}
	c.CurrentMP = testInt64(10000)
	c.Intellect = testInt64(2000)
	if row.RequiredWeaponKinds[0] != 0 {
		weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
		weapon.TypeIDs[3] = int64(row.RequiredWeaponKinds[0])
		c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	}
	// The pulse selects in the owner's admitted population; stand on the mob.
	if err := rt.admitPopulationSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	mover, _ := rt.Monsters.Mover(testDivision, mob.Gid)
	pose := mover.LivePoseAt(clock.NowMs(), nil)
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}
			w.SpawnSet = true
		})
	return rt, clock, c, row, mob
}

/*
================
TestLifeDrainHealsTheCaster
================
*/
func TestLifeDrainHealsTheCaster(t *testing.T) {
	rt, clock, c, row, mob := warlockFixture(t, "SKILL_EU_WARLOCK_BLOODA_LIFEDRAIN_A_01")
	c.CurrentHP = testInt64(10)
	before, _ := rt.Monsters.Get(testDivision, mob.Gid)
	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID, HasTarget: true, TargetGid: mob.Gid}.Encode())
	if open, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || open.Payload[0] != 1 {
		t.Fatalf("Life Drain refused: %+v", out)
	}
	if len(rt.advanceProjectileCasts(clock.NowMs()+int64(row.ActionCastingTimeMs)+1)) == 0 {
		t.Fatal("Life Drain never released")
	}
	after, _ := rt.Monsters.Get(testDivision, mob.Gid)
	taken := int64(before.CurrentHP - after.CurrentHP)
	if taken < 27+21 {
		t.Fatalf("life taken %d, want at least lfst + BSHP = 48", taken)
	}
	if *c.CurrentHP != 10+taken {
		t.Fatalf("caster HP %d, want 10 + %d", *c.CurrentHP, taken)
	}
}

/*
================
TestSoulChaosPulses

One period after the cast the instance strikes the monster beside its
owner for pdmg + SAAA at even levels: 126 + 28.
================
*/
func TestSoulChaosPulses(t *testing.T) {
	rt, _, c, row, mob := warlockFixture(t, "SKILL_EU_WARLOCK_SOULA_CHAOS_A_01")
	now := rt.Now().UnixMilli()
	if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: c.Name, SkillID: row.ID,
		SkillGroup: row.Group, InstanceToken: 9, State: statuseffect.StateActive,
		StartedAtMs: now - 2000, DurationPresent: true, ExpiresAtMs: now + 14000}) {
		t.Fatal("Soul Chaos did not install")
	}
	rt.trackPulseArea(testDivision, c.Name)
	before, _ := rt.Monsters.Get(testDivision, mob.Gid)
	frames := rt.advancePulseAreas(now)
	after, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if before.CurrentHP-after.CurrentHP != 154 {
		t.Fatalf("pulse took %d, want 154", before.CurrentHP-after.CurrentHP)
	}
	if len(frames) == 0 || frames[0].Frames[0].Opcode != wire.OpSkillPulse {
		t.Fatalf("pulse frames = %+v", frames)
	}
	if again := rt.advancePulseAreas(now + 1000); len(again) != 0 {
		t.Fatal("a second pulse inside the period")
	}
}
