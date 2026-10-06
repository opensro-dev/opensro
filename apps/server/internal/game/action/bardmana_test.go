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
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const (
	// manaCyclePulses is dura(16000) / puls(2000): one pulse every 2 s,
	// the first 2 s after release, the last at 16 s.
	manaCyclePulses = 8
	manaCyclePulse  = 2000 * time.Millisecond
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

/*
==================
TestManaWindCompoundsNearbyMemberShares

Mana Wind is efr(1,6,100,3,50,4) heal(0,0,mp,0) mwmh: the target receives
the whole heal and at most two other party members within 100 of the
target, nearest first, receive 50 % and 25 % of its flat word plus the full
weapon contribution. A third member in range, a
member past 100, a player outside the party and the caster get nothing.
==================
*/
func TestManaWindCompoundsNearbyMemberShares(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_BARD_RECOVERA_MANATRANS_A_01")
	area := skill.Abnormal.EffectArea
	if !skill.TargetRequired || skill.Heal.MP == 0 || area.Shape != 6 || area.Radius != 100 ||
		area.MaxTargets != 3 || area.Reduction != 50 || area.Select != 4 {
		t.Fatalf("mana wind heal %+v area %+v", skill.Heal, area)
	}
	affordable(&skill)
	p := newSupportParty(t, skill, 90, 50, 80, 150)
	equipHarp(p.supportPair)
	third, first, second, far := p.mates[1], p.mates[2], p.mates[3], p.mates[4]
	stranger := nearbyCharacter(p.rt, p.c, 30, "stranger", 10)
	for _, c := range []*enterworld.Character{p.m, first, second, third, far, stranger} {
		c.CurrentMP = testInt64(0)
	}
	p.c.CurrentMP = testInt64(150)

	p.castReleased(t, skill, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(p.m)})

	whole := manaHealWant(t, p.supportPair, p.m, skill)
	if want := gaugeMP(t, p.supportPair, p.m, whole); *p.m.CurrentMP != want {
		t.Errorf("target mp %d want %d", *p.m.CurrentMP, want)
	}
	for index, member := range []*enterworld.Character{first, second} {
		shared := skill
		shared.Heal.MP = skill.Heal.MP * uint32(50>>index) / 100
		half := manaHealWant(t, p.supportPair, member, shared)
		if want := gaugeMP(t, p.supportPair, member, half); *member.CurrentMP != want || want == gaugeMP(t, p.supportPair, member, whole) {
			t.Errorf("%s nearby: mp %d want chain share %d", member.Name, *member.CurrentMP, want)
		}
	}
	for _, other := range []*enterworld.Character{third, far, stranger} {
		if *other.CurrentMP != 0 {
			t.Errorf("%s outside the two nearest members got mp %d", other.Name, *other.CurrentMP)
		}
	}
	if *p.c.CurrentMP >= 150 {
		t.Errorf("caster was healed: mp %d", *p.c.CurrentMP)
	}
}

/*
================
emptyForCast

Leaves every gauge empty once the cast is paid: the caster keeps exactly
the row's MP cost, everyone else holds none. A one-shot heal at release
then shows as MP on a recipient.
================
*/
func emptyForCast(p supportPair, skill enterworld.SkillRow, characters ...*enterworld.Character) {
	for _, c := range characters {
		c.CurrentMP = testInt64(0)
		if c == p.c {
			c.CurrentMP = testInt64(int64(skill.Consumption.MP))
		}
	}
}

/*
================
expectPulses

Drives the tick through a heal over time released on recipients, whose
gauges emptyForCast emptied: no MP at release, none between pulses, the whole heal on every one of the eight
pulses 2 s apart (each recipient's gauge is emptied before each pulse), and
nothing once the 16 s are over, when the effect is gone too. outsiders
must never gain MP.
================
*/
func expectPulses(t *testing.T, p supportPair, skill enterworld.SkillRow, recipients, outsiders []*enterworld.Character) {
	t.Helper()
	empty := func() {
		for _, c := range append(append([]*enterworld.Character(nil), recipients...), outsiders...) {
			c.CurrentMP = testInt64(0)
		}
	}
	check := func(stage string, healed bool) {
		t.Helper()
		for _, c := range recipients {
			want := int64(0)
			if healed {
				want = gaugeMP(t, p, c, manaHealWant(t, p, c, skill))
			}
			if *c.CurrentMP != want {
				t.Fatalf("%s: %s mp %d want %d", stage, c.Name, *c.CurrentMP, want)
			}
		}
		for _, c := range outsiders {
			if *c.CurrentMP != 0 {
				t.Fatalf("%s: %s outside the heal got mp %d", stage, c.Name, *c.CurrentMP)
			}
		}
	}

	p.rt.TickHook()(p.clock.NowMs())
	check("release", false)
	for _, c := range recipients {
		if !hasSkillEffect(p.rt, c.Name, skill.ID) {
			t.Fatalf("%s holds no %s effect", c.Name, skill.Codename)
		}
	}
	for pulse := 1; pulse <= manaCyclePulses; pulse++ {
		p.clock.Advance(manaCyclePulse / 2)
		empty()
		p.rt.TickHook()(p.clock.NowMs())
		check("between pulses", false)
		p.clock.Advance(manaCyclePulse / 2)
		p.rt.TickHook()(p.clock.NowMs())
		check("pulse", true)
	}
	p.clock.Advance(manaCyclePulse)
	empty()
	p.rt.TickHook()(p.clock.NowMs())
	check("after 16 s", false)
	for _, c := range recipients {
		if hasSkillEffect(p.rt, c.Name, skill.ID) {
			t.Fatalf("%s still holds %s after its duration", c.Name, skill.Codename)
		}
	}
}

/*
==================
TestManaCycleHealsItsTargetOverTime

Mana Cycle is dura(16000) puls(2000) heal(0,0,mp,0) mwmh: the selected
target, another member or the caster itself, holds the effect for 16 s and
receives the whole MP heal every 2 s, not once at release. The client sends
the caster's own gid or, with nothing selected, no target; the row admits
Self (column 26), so both land on the caster.
==================
*/
func TestManaCycleHealsItsTargetOverTime(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_BARD_RECOVERA_MPHEAL_A_01")
	if !skill.TargetRequired || !skill.Targets.Self || skill.Heal.MP == 0 || !skill.Heal.WeaponMP ||
		skill.EffectDurationMs != 16000 {
		t.Fatalf("mana cycle targets %+v heal %+v duration %d", skill.Targets, skill.Heal, skill.EffectDurationMs)
	}
	affordable(&skill)
	for _, tc := range []struct {
		name string
		cast func(p supportPair) (wire.SkillAction, *enterworld.Character, *enterworld.Character)
	}{
		{"member", func(p supportPair) (wire.SkillAction, *enterworld.Character, *enterworld.Character) {
			return wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(p.m)}, p.m, p.c
		}},
		{"own gid", func(p supportPair) (wire.SkillAction, *enterworld.Character, *enterworld.Character) {
			return wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(p.c)}, p.c, p.m
		}},
		{"no target", func(p supportPair) (wire.SkillAction, *enterworld.Character, *enterworld.Character) {
			return wire.SkillAction{ActionId: skill.ID}, p.c, p.m
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newSupportPair(t, skill)
			equipHarp(p)
			cast, recipient, other := tc.cast(p)
			emptyForCast(p, skill, p.c, p.m)
			p.castReleased(t, skill, cast)
			expectPulses(t, p, skill, []*enterworld.Character{recipient}, []*enterworld.Character{other})
		})
	}
}

/*
==================
TestManaOrbitHealsThePartyInRangeOverTime

Mana Orbit is efr(1,1,300,8,0,5) dura(16000) puls(2000) heal(0,0,mp,0)
mwmh: every party member within 300 of the caster, the caster included
(select 5), holds the effect and receives the MP heal every 2 s for 16 s.
A member past 300 and a player outside the party get nothing.
==================
*/
func TestManaOrbitHealsThePartyInRangeOverTime(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_BARD_RECOVERA_MPHEAL_B_01")
	area := skill.Abnormal.EffectArea
	if skill.TargetRequired || skill.Heal.MP == 0 || area.Radius != 300 || area.Select != 5 || skill.EffectDurationMs != 16000 {
		t.Fatalf("mana orbit heal %+v area %+v duration %d", skill.Heal, area, skill.EffectDurationMs)
	}
	affordable(&skill)
	p := newSupportParty(t, skill, 250, 400)
	equipHarp(p.supportPair)
	near, far := p.mates[1], p.mates[2]
	stranger := nearbyCharacter(p.rt, p.c, 30, "stranger", 10)
	emptyForCast(p.supportPair, skill, p.c, p.m, near, far, stranger)

	p.castReleased(t, skill, wire.SkillAction{ActionId: skill.ID})
	expectPulses(t, p.supportPair, skill, []*enterworld.Character{p.c, p.m, near}, []*enterworld.Character{far, stranger})
}
