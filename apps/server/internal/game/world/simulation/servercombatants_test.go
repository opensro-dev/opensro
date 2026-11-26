/*
===========================================================================
servercombatants_test.go - port-only actors are targets, never socket viewers
===========================================================================
*/
package simulation

import (
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

/*
================
TestServerCombatantCanBeAttackedWithoutAViewer
================
*/
func TestServerCombatantCanBeAttackedWithoutAViewer(t *testing.T) {
	const now = int64(1_784_000_000_000)
	ops, _ := monsterLegFixture(t, aggressiveTactics())
	actor := playerSessionAt(10_000_000, 1005, 1000)
	actor.SessionID = ""
	ops.ServerCombatants = func() []SessionSnapshot { return []SessionSnapshot{actor} }
	ops.AttackPlan = func(monster.Instance, uint32, AttackPick) (MonsterAttackPlan, bool) {
		return MonsterAttackPlan{SkillID: 0x1234, Reach: 6, CooldownMs: 1000, ActionLifecycleMs: 600}, true
	}
	hit := false
	ops.BasicAttack = func(_ string, _ monster.Instance, gid, _ uint32, _ int64) MonsterAttackResult {
		hit = gid == PlayerObjectID(actor.CharacterID)
		return MonsterAttackResult{Accepted: true, TargetAlive: true}
	}
	push := &fakePusher{}
	ops.RunMonsterLeg(now, nil, push)
	if !hit {
		t.Fatal("native monster ignored the server-owned actor")
	}
	if len(push.toSession) > 0 || len(push.toDivision) > 0 {
		t.Fatal("server-owned actor was treated as a viewer")
	}
}
