/*
===========================================================================

skillheal_test.go - tests for skillheal.go: the aura heal's amounts

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// faithFixtureUp is the HLRU percent the fixture's Faith passive sets.
const faithFixtureUp = 10

/*
================
faithPassive

A passive row that sets the caster's HLRU, as Faith does.
================
*/
func faithPassive(up uint32) enterworld.SkillRow {
	var faith enterworld.SkillRow
	faith.ID, faith.Group, faith.Level = 90002, 90002, 1
	faith.PassiveParameters.Pinned = true
	faith.PassiveParameters.Mask = enterworld.SkillParameterMask(1) << enterworld.ParameterHealRecoveryUp
	faith.PassiveParameters.Values[enterworld.ParameterHealRecoveryUp] = up
	return faith
}

/*
==================
TestRecoveryAuraHealReadsFaithLikeACast

Recovery Division is heal(445,0,0,0) with getv HLRU. Its pulse runs the
cast's code (CastLifecycle_ProcessPersistent -> SkillCombat_EngageSkill ->
593F50), whose 59425E adds the caster's HLRU to both percent words; the
aura base then heals that percent of the maximum in place of the flat word.
A caster who learned Faith (10) therefore restores 10 % of max HP and of
max MP each pulse.
==================
*/
func TestRecoveryAuraHealReadsFaithLikeACast(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_RECOVERYA_GROUP_A_01")
	if !skill.Aura.Eshp || skill.Heal.HP == 0 || skill.Heal.MP != 0 || skill.Heal.MPPercent != 0 ||
		skill.Heal.HPPercent != 0 || !skill.Attack.Parameters.Has(enterworld.ParameterHealRecoveryUp) {
		t.Fatalf("recovery division heal %+v mask %v", skill.Heal, skill.Attack.Parameters)
	}
	affordable(&skill)
	p := newSupportPair(t, skill, faithPassive(faithFixtureUp))
	r := p.rt.HandleTargetInteract(testDivision, p.c, wire.SkillAction{ActionId: skill.ID}.Encode())
	if r.DiagnosticRefusal != "" || len(r.Frames) == 0 || r.Frames[0].Payload[0] == 2 {
		t.Fatalf("recovery division refused %q", r.DiagnosticRefusal)
	}

	// The first update heals the caster (select bit 0) before the mate joins.
	stats, _, err := p.rt.playerCombatStats(testDivision, p.c)
	if err != nil {
		t.Fatal(err)
	}
	weaponHP, weaponMP, ok := p.rt.weaponHealBonus(testDivision, p.c, skill.Heal)
	if !ok {
		t.Fatal("weapon term unavailable")
	}
	maxHP, maxMP, _, _ := p.rt.playerKeeperVitals(testDivision, p.c)
	raised := skill.Heal
	raised.HPPercent += faithFixtureUp
	raised.MPPercent += faithFixtureUp
	baseHP, baseMP := auraHealBase(raised, maxHP, maxMP)
	scaleHP, _ := stats.Param(0xaa)
	scaleMP, _ := stats.Param(0xab)
	wantHP := healScale(baseHP, scaleHP) + weaponHP
	wantMP := healScale(baseMP, scaleMP) + weaponMP
	if wantMP == 0 {
		t.Fatal("fixture heals no MP; the test would not see the percent words")
	}
	p.c.CurrentHP, p.c.CurrentMP = testInt64(40), testInt64(10)
	p.rt.TickHook()(p.clock.NowMs())

	if got := *p.c.CurrentHP - 40; got != min(wantHP, maxHP-40) {
		t.Errorf("healed %d HP, want %d (%d %% of %d)", got, wantHP, faithFixtureUp, maxHP)
	}
	if got := *p.c.CurrentMP - 10; got != min(wantMP, maxMP-10) {
		t.Errorf("restored %d MP, want %d (%d %% of %d)", got, wantMP, faithFixtureUp, maxMP)
	}
}
