/*
===========================================================================

bardmana_test.go - tests for the Bard's MP recovery skills

Mana Breeze (party heal, skillrecovery.go), Mana Wind (targeted heal with
a shape-6 secondary selection) and Mana Cycle / Mana Orbit (heals over
time). The shipped rows are driven through HandleTargetInteract and the
simulation tick, as the client drives them (Bard specification, rules 7
and 8).

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// bardHarpKind is the TypeID3 of a harp, the weapon the Bard's rows require.
const bardHarpKind = 14

/*
================
equipHarp

The support fixture's caster holds a cleric rod; a Bard row needs a harp.
================
*/
func equipHarp(p supportPair) {
	weapon := p.rt.deps.ItemReferences().(staticItemSource)[p.c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = bardHarpKind
	p.c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
}

/*
================
manaHealWant

The MP one cast of skill gives member before any reduction: the flat MP
word scaled by the member's 0xAB, plus the caster's mwmh weapon term.
================
*/
func manaHealWant(t *testing.T, p supportPair, member *enterworld.Character, skill enterworld.SkillRow) int64 {
	t.Helper()
	stats, _, err := p.rt.playerCombatStats(testDivision, member)
	if err != nil {
		t.Fatal(err)
	}
	_, weapon, ok := p.rt.weaponHealBonus(testDivision, p.c, skill.Heal)
	if !ok || weapon == 0 {
		t.Fatalf("fixture weapon adds no mwmh term (%v)", ok)
	}
	scale, _ := stats.Param(0xab)
	return healScale(int64(skill.Heal.MP), scale) + weapon
}

/*
================
gaugeMP

What an MP gain of amount leaves on an empty gauge.
================
*/
func gaugeMP(t *testing.T, p supportPair, member *enterworld.Character, amount int64) int64 {
	t.Helper()
	_, maxMP, _, _ := p.rt.playerKeeperVitals(testDivision, member)
	return min(maxMP, amount)
}

/*
==================
TestManaBreezeGivesThePartyInRangeItsMP

Mana Breeze is efr(1,1,300,8,0,4) heal(0,0,mp,0) mwmh: every party member
within 300 of the caster receives the fixed MP at once. Select 4 leaves the
caster out; a member past 300 and a player outside the party get nothing.
==================
*/
func TestManaBreezeGivesThePartyInRangeItsMP(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_BARD_RECOVERA_MANATRANS_B_01")
	if skill.Heal.MP == 0 || !skill.Heal.WeaponMP ||
		skill.Abnormal.EffectArea.Radius != 300 || skill.Abnormal.EffectArea.Select != 4 {
		t.Fatalf("mana breeze recovery %+v heal %+v area %+v", skill.Recovery, skill.Heal, skill.Abnormal.EffectArea)
	}
	affordable(&skill)
	p := newSupportParty(t, skill, 250, 400)
	equipHarp(p.supportPair)
	near, far := p.mates[1], p.mates[2]
	stranger := nearbyCharacter(p.rt, p.c, 30, "stranger", 1)
	for _, c := range []*enterworld.Character{p.m, near, far, stranger} {
		c.CurrentMP = testInt64(0)
	}
	p.c.CurrentMP = testInt64(150)

	p.castReleased(t, skill, wire.SkillAction{ActionId: skill.ID})

	for _, member := range []*enterworld.Character{p.m, near} {
		if want := gaugeMP(t, p.supportPair, member, manaHealWant(t, p.supportPair, member, skill)); *member.CurrentMP != want {
			t.Errorf("%s in range: mp %d want %d", member.Name, *member.CurrentMP, want)
		}
	}
	for _, other := range []*enterworld.Character{far, stranger} {
		if *other.CurrentMP != 0 {
			t.Errorf("%s outside the selection got mp %d", other.Name, *other.CurrentMP)
		}
	}
	if *p.c.CurrentMP >= 150 {
		t.Errorf("caster (select 4) was healed: mp %d", *p.c.CurrentMP)
	}
}
