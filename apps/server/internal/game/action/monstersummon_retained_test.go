package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"
)

func TestUniqueRetainedSkillSurvivesDamageAndHealthBandChange(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	refs := monster.LoadMonsterRefs(dir)
	rt := NewRuntime(&enterworld.Deps{Skills: enterworld.NewTextdataSkills(dir)}, nil)
	var ref monster.MonsterRef
	for _, r := range refs {
		if r.Codename == "MOB_CH_TIGERWOMAN" {
			ref = r
			break
		}
	}
	if ref.RefObjID == 0 {
		t.Fatal("shipped unique absent")
	}
	actor := monster.Instance{Ref: ref}
	actor.CurrentHP = actor.EffectiveMaxHP()
	ordinary, ok := rt.MonsterAttackPlan(actor, 0, simulation.AttackPick{})
	if !ok || ordinary.Summon {
		t.Fatal("ordinary selection not reached")
	}
	actor.CurrentHP = actor.EffectiveMaxHP() * 9 / 10
	actor.DamageSinceSummon = actor.EffectiveMaxHP() / 5
	first, ok := rt.MonsterAttackPlan(actor, 0, simulation.AttackPick{})
	if !ok || !first.Summon {
		t.Fatal("first summon band not reached")
	}
	kept, ok := rt.MonsterAttackPlan(actor, ordinary.SkillID, simulation.AttackPick{Sample: .999})
	if !ok || kept != ordinary {
		t.Fatal("new damage replaced retained ordinary action")
	}
	actor.CurrentHP = actor.EffectiveMaxHP() / 10
	next, ok := rt.MonsterAttackPlan(actor, 0, simulation.AttackPick{})
	if !ok || !next.Summon || next.SkillID == first.SkillID {
		t.Fatal("distinct lower band not reached")
	}
	kept, ok = rt.MonsterAttackPlan(actor, first.SkillID, simulation.AttackPick{Sample: .999})
	if !ok || kept != first {
		t.Fatal("HP band replaced retained summon")
	}
	if _, ok = rt.MonsterAttackPlan(actor, 0xffffffff, simulation.AttackPick{}); ok {
		t.Fatal("foreign retained ID accepted")
	}
}
