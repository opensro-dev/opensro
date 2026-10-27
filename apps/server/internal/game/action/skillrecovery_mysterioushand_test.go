/*
===========================================================================

skillrecovery_mysterioushand_test.go - Mysterious Hand heals its target

Mysterious Hand (SKILL_CH_WATER_HEAL_D, efr(1,6,300,2,50,7) eshp heal) is
a targeted force heal whose chain carries eshp. It was refused as if it
were the Cleric's eshp aura, so the skill never cast. Natively it heals its
target and then, at half the heal, the party member nearest it: its
primary target skips the eshp HP-ratio sort (the native chain
selection), which only Healing Division's targetless chain takes.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// mysteriousHand09 is SKILL_CH_WATER_HEAL_D_09.
const mysteriousHand09 = 19861

// castTickMs and castSettleMs step the tick through a cast and past it.
const (
	castTickMs   = 100
	castSettleMs = 1000
)

/*
================
mysteriousHandFixture

A caster who knows Mysterious Hand with MP enough to cast it (its cost is
not under test), and a cast on gid that runs the tick through the row's
casting time, so the heal lands as it does live.
================
*/
func mysteriousHandFixture(t *testing.T) (*Runtime, *enterworld.Character, func(gid uint32) OpResult) {
	t.Helper()
	rt, clock, c := concealmentFixture(t, mysteriousHand09)
	row := rt.deps.SkillData().(staticSkillSource)[mysteriousHand09]
	if !row.TargetRequired || !row.Aura.Eshp || row.Aura.Present || row.Abnormal.EffectArea.Shape != 6 {
		t.Fatalf("Mysterious Hand row %+v %+v", row.Aura, row.Abnormal.EffectArea)
	}
	row.Consumption.MP = 10
	rt.deps.SkillData().(staticSkillSource)[mysteriousHand09] = row
	cast := func(gid uint32) OpResult {
		r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: mysteriousHand09, HasTarget: true, TargetGid: gid}.Encode())
		for step := int64(0); step*castTickMs <= int64(row.ActionCastingTimeMs)+castSettleMs; step++ {
			clock.Advance(time.Duration(castTickMs) * time.Millisecond)
			rt.TickHook()(clock.NowMs())
		}
		return r
	}
	return rt, c, cast
}

/*
================
TestMysteriousHandHealsItsTarget
================
*/
func TestMysteriousHandHealsItsTarget(t *testing.T) {
	rt, c, cast := mysteriousHandFixture(t)
	maxHP, _, _, _ := rt.playerKeeperVitals(testDivision, c)
	c.CurrentHP = testInt64(maxHP / 10)
	before := enterworld.CurrentHP(c)
	if r := cast(enterworld.ObjectIDForCharacter(c)); r.DiagnosticRefusal != "" || len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 {
		t.Fatalf("Mysterious Hand on self refused: %q %+v", r.DiagnosticRefusal, r.Frames)
	}
	if enterworld.CurrentHP(c) <= before {
		t.Fatalf("caster HP %d -> %d, want healed", before, enterworld.CurrentHP(c))
	}
}

/*
================
TestMysteriousHandChainsTheNearestFirst

The target, then one more (MaxTargets 2): of two members in range, the
nearer one at 50 % takes the half share, not the farther one at 20 %
(the caster, select bit 0, stands 20 away from the target: no tie).
================
*/
func TestMysteriousHandChainsTheNearestFirst(t *testing.T) {
	rt, c, cast := mysteriousHandFixture(t)
	target := nearbyCharacter(rt, c, 12, "target", 20)
	nearer := nearbyCharacter(rt, c, 13, "nearer", 30)
	lower := nearbyCharacter(rt, c, 14, "lower", 150)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(target),
			enterworld.ObjectIDForCharacter(nearer), enterworld.ObjectIDForCharacter(lower)}}}
	}
	set := func(m *enterworld.Character, percent int64) int64 {
		maxHP, _, _, _ := rt.playerKeeperVitals(testDivision, m)
		m.CurrentHP = testInt64(maxHP * percent / 100)
		return enterworld.CurrentHP(m)
	}
	targetBefore, nearerBefore, lowerBefore := set(target, 30), set(nearer, 50), set(lower, 20)
	if r := cast(enterworld.ObjectIDForCharacter(target)); r.DiagnosticRefusal != "" || len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 {
		t.Fatalf("Mysterious Hand on a party member refused: %q", r.DiagnosticRefusal)
	}
	if enterworld.CurrentHP(target) <= targetBefore {
		t.Fatal("the target was not healed")
	}
	if enterworld.CurrentHP(nearer) <= nearerBefore {
		t.Fatal("the nearest member did not take the chain share")
	}
	if enterworld.CurrentHP(lower) != lowerBefore {
		t.Fatalf("the farther, lower-ratio member was healed %d -> %d", lowerBefore, enterworld.CurrentHP(lower))
	}
}
