/*
===========================================================================

monsterselfeffect_stacking_test.go - tests for an event's stacking self cast

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestStackingCastBeginsThenLandsOnItsCaster

The Roc's enrage path on a live monster: the cast begins untargeted, and
after its casting time the buff lands once, under its own token, raising
the caster's damage rates by one step. A row the mom parse did not pin is
never cast.
================
*/
func TestStackingCastBeginsThenLandsOnItsCaster(t *testing.T) {
	rt, clock, _, target := newCombatTestRuntime(t, 1000000)
	enrage := enterworld.SkillRow{ID: 31102, Codename: rocEnrageSkill, ActionCastingTimeMs: 2633, ActionDurationMs: 3000,
		MonsterStackingEffect: enterworld.SkillMonsterStackingEffect{Pinned: true, PeriodMs: 600000, StepValue: 10}}
	rt.deps.SkillData().(staticSkillSource)[enrage.ID] = enrage
	before, _ := rt.Monsters.Get(testDivision, target.Gid)
	calm, err := combat.MonsterInstanceStats(before)
	if err != nil {
		t.Fatal(err)
	}
	now := clock.NowMs()
	cast, begun, ok := rt.beginMonsterStackingCast(target.Gid, rocEnrageSkill, now)
	if !ok || cast.releaseAtMs != now+2633 || begun.Opcode != wire.SkillCastSelfFrame(wire.SkillCastSuccess{}).Opcode {
		t.Fatalf("begin: %+v %x %v", cast, begun.Opcode, ok)
	}
	landed, ok := rt.landMonsterStackingCast(testDivision, cast, cast.releaseAtMs)
	if !ok || len(landed) != 2 || landed[1].Opcode != wire.OpAttachedEffect {
		t.Fatalf("land: %v %v", landed, ok)
	}
	live, _ := rt.Monsters.Get(testDivision, target.Gid)
	raged, err := combat.MonsterInstanceStats(live)
	if err != nil {
		t.Fatal(err)
	}
	if raged.PhysicalBasicRate != calm.PhysicalBasicRate+10 || raged.MagicalSkillRate != calm.MagicalSkillRate+10 {
		t.Fatalf("one step: rates %v/%v from %v/%v", raged.PhysicalBasicRate, raged.MagicalSkillRate, calm.PhysicalBasicRate, calm.MagicalSkillRate)
	}
	if _, again := rt.landMonsterStackingCast(testDivision, cast, cast.releaseAtMs+1); again {
		t.Fatal("the buff landed twice")
	}
	unpinned := enrage
	unpinned.ID, unpinned.Codename, unpinned.MonsterStackingEffect = 31103, "MSKILL_UNPINNED", enterworld.SkillMonsterStackingEffect{}
	rt.deps.SkillData().(staticSkillSource)[unpinned.ID] = unpinned
	if _, _, ok := rt.beginMonsterStackingCast(target.Gid, unpinned.Codename, now); ok {
		t.Fatal("an unpinned row was cast")
	}
}
