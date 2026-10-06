/*
===========================================================================

mercenary_combat_test.go - soldier area/status skills reach the shared victims

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestMercenaryAreaUsesOneCastAndCumulativeDamage
================
*/
func TestMercenaryAreaUsesOneCastAndCumulativeDamage(t *testing.T) {
	rt, clock, c, monsters := statusCastAreaFixture(t)
	ref := equipCombatTestPet(t, rt, c, domain.MercenaryBand)
	ref.Parameters.HitRate = 10000
	rt.BindPetSession(testDivision, c, 1)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	skill := rt.deps.SkillData().(staticSkillSource)[2]
	skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent = 100, 100, 100
	skill.ActionArea = enterworld.SkillOffensiveArea{Shape: 2, Radius: 20, MaxTargets: 3, ReductionPercent: 50, Select: 24}
	state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
	step := petCombatStep{key: petOwnerKey{division: testDivision, name: c.Name, gid: c.ActiveCOS.GID}, state: state, snapshot: c,
		pet: c.ActiveCOS, ref: ref, nowMs: clock.NowMs()}
	target, found := rt.resolvePetCombatTarget(step, monsters[0].Gid)
	if !found {
		t.Fatal("missing primary")
	}
	result, ok := rt.strikePetTargets(step, target, skill)
	if !ok {
		t.Fatal("area refused")
	}
	casts := 0
	for _, frame := range result.frames {
		if frame.Opcode == wire.OpSkillCastResult {
			casts++
		}
	}
	if casts != 1 {
		t.Fatalf("cast count %d", casts)
	}
	var damage []uint32
	for _, before := range monsters {
		after, _ := rt.Monsters.Get(testDivision, before.Gid)
		damage = append(damage, before.CurrentHP-after.CurrentHP)
	}
	if damage[0] == 0 || damage[1] != damage[0]/2 || damage[2] != damage[0]/4 || damage[3] != 0 || damage[4] != 0 {
		t.Fatalf("area damage %v", damage)
	}
}

/*
================
TestMercenaryColdSkillAppliesWithoutDamage
================
*/
func TestMercenaryColdSkillAppliesWithoutDamage(t *testing.T) {
	rt, clock, c, monster := newPetCombatRuntime(t, 1000000, domain.MercenaryBand)
	skill := shippedOffense(t, "GSKILL_CH_COLD_BINGPAN_A_001")
	if !skill.CreatureStatusCast || skill.StatusCast || skill.Attack.Present {
		t.Fatalf("creature admission %+v", skill)
	}
	ref, _ := rt.cosReference(c.ActiveCOS)
	state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
	step := petCombatStep{key: petOwnerKey{division: testDivision, name: c.Name, gid: c.ActiveCOS.GID}, state: state,
		snapshot: c, pet: c.ActiveCOS, ref: ref, nowMs: clock.NowMs()}
	index, _ := abnormal.SourceIndex(0x667a)
	skill.Abnormal.Params[index].Args[1] = 100
	target, ok := rt.resolvePetCombatTarget(step, monster.Gid)
	if !ok {
		t.Fatal("missing target")
	}
	before, _ := rt.Monsters.Get(testDivision, monster.Gid)
	if _, ok = rt.strikePetTargets(step, target, skill); !ok {
		t.Fatal("status strike refused")
	}
	after, _ := rt.Monsters.Get(testDivision, monster.Gid)
	if after.CurrentHP != before.CurrentHP || after.AbnormalMask() == 0 {
		t.Fatalf("status HP %d -> %d mask %x", before.CurrentHP, after.CurrentHP, after.AbnormalMask())
	}
}

/*
================
TestCompanionVictimKnockdownOwnsPositionAndHold
================
*/
func TestCompanionVictimKnockdownOwnsPositionAndHold(t *testing.T) {
	for _, immune := range []bool{false, true} {
		rt, clock, c, attacker := newPetCombatRuntime(t, 1000000, domain.MercenaryBand)
		ref, _ := rt.cosReference(c.ActiveCOS)
		ref.Parameters.Knockdown, ref.Parameters.KORecoverMs = 1, 1000
		if immune {
			ref.Parameters.Knockdown = 0
		}
		skill := shippedOffense(t, "SKILL_CH_SWORD_KNOCKDOWN_A_01")
		skill.Knockdown.Chance = 100
		state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
		before := state.follower.Position(clock.NowMs())
		from := monster.Pose{RegionID: before.RegionID, X: before.X, Y: before.Y, Z: before.Z}
		from.X = before.X - 100
		from.RegionID, from.Z = before.RegionID, before.Z
		outcome := rt.monsterStrikeCOS(monsterStrikeInput{division: testDivision, instance: attacker, skill: skill, now: clock.NowMs(), from: from, percent: fullAreaPercent,
			attacker: combat.Stats{Level: 1, HitRate: 10000, PhysicalAttackMin: 1, PhysicalAttackMax: 1}}, c, c.ActiveCOS, ref, before)
		if !outcome.committed || outcome.strike.fatal {
			t.Fatal("nonfatal hit refused")
		}
		if immune {
			if state.displacement != nil {
				t.Fatal("immune actor displaced")
			}
			continue
		}
		if state.displacement == nil || !state.displacement.down {
			t.Fatal("knockdown missing")
		}
		if after := state.follower.Position(clock.NowMs()); after.X != before.X+20 {
			t.Fatalf("position %v -> %v", before, after)
		}
		hold := state.displacement.untilMs
		rt.advancePet(petOwnerKey{division: testDivision, name: c.Name, gid: c.ActiveCOS.GID}, hold-1)
		if state.displacement == nil || state.combat != nil {
			t.Fatal("hold allowed combat")
		}
		rt.advancePet(petOwnerKey{division: testDivision, name: c.Name, gid: c.ActiveCOS.GID}, hold)
		if state.displacement != nil {
			t.Fatal("hold did not expire")
		}
	}
}
