package simulation

import (
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

func TestChaseTicksRetainAttackWithoutChoiceDraws(t *testing.T) {
	ops, actor := monsterLegFixture(t, aggressiveTactics())
	choices, resolutions := 0, 0
	ops.AttackPlan = func(_ monster.Instance, requested uint32, sample float64) (MonsterAttackPlan, bool) {
		if requested == 0 {
			choices++
		} else {
			resolutions++
			if requested != 7 || sample != 0 {
				t.Fatal("chase resolution re-entered randomized selection")
			}
		}
		return MonsterAttackPlan{SkillID: 7, Reach: 6, CooldownMs: 1000, ActionLifecycleMs: 500}, true
	}
	if !ops.Monsters.ArmRetaliation(monsterTestDivision, actor.Gid, PlayerObjectID(1)) {
		t.Fatal("retaliation refused")
	}
	target := playerPose{Gid: PlayerObjectID(1), Pose: Spawn{RegionID: monsterTestRegion, X: 1200, Y: 20, Z: 1000}, BodyRadius: 4}
	for tick := int64(100000); tick <= 100500; tick += 100 {
		ops.advanceInstance(monsterTestDivision, actor, []playerPose{target}, tick)
		mover, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
		if mover.Mode() != monster.MoverChasing || mover.AttackSkillID != 7 {
			t.Fatalf("retained chase not reached: %v/%d", mover.Mode(), mover.AttackSkillID)
		}
	}
	if choices != 1 || resolutions < 5 {
		t.Fatalf("choice/resolution counts=%d/%d", choices, resolutions)
	}
}

func TestRetainedAttackResolutionConsumesNoSelectionOrJitterDraw(t *testing.T) {
	draws := 0
	ops := &MonsterMoverOps{Rand: func() float64 { draws++; return .5 }, AttackPlan: func(_ monster.Instance, requested uint32, sample float64) (MonsterAttackPlan, bool) {
		if requested != 0 && (requested != 7 || sample != 0) {
			t.Fatal("retained skill entered randomized selection")
		}
		return MonsterAttackPlan{SkillID: 7, Reach: 20, CooldownMs: 1000, ActionLifecycleMs: 500}, true
	}}
	var mover monster.MoverState
	plan, ok := ops.selectMonsterAttack("", monster.Instance{}, 0)
	if !ok {
		t.Fatal("initial selection refused")
	}
	ops.adoptMonsterAttack(&mover, plan)
	if draws != 2 {
		t.Fatalf("choice plus adoption draws=%d", draws)
	}
	interval := mover.AttackIntervalMs
	for i := 0; i < 5; i++ {
		plan, ok = ops.selectMonsterAttack("", monster.Instance{}, mover.AttackSkillID)
		if !ok {
			t.Fatal("retained resolution refused")
		}
		ops.adoptMonsterAttack(&mover, plan)
		if draws != 2 || mover.AttackIntervalMs != interval {
			t.Fatal("retained skill consumed entropy or replaced its interval")
		}
	}
	// Existing action-completion ownership retires the selection; choosing
	// the same skill again is a new selection and must get a new interval.
	mover.AttackSkillID = 0
	plan, _ = ops.selectMonsterAttack("", monster.Instance{}, 0)
	ops.adoptMonsterAttack(&mover, plan)
	if draws != 4 {
		t.Fatalf("new selection draws=%d", draws)
	}
}
