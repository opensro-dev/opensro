package simulation

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

// The competing death runs at transaction exit, precisely where the old
// damage-only lock allowed it to overtake the returned attack frames.
func TestMonsterActionPublishesBeforeCompetingDeath(t *testing.T) {
	const now = int64(1_784_000_000_000)
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	ops.AttackPlan = func(monster.Instance, uint32, AttackPick) (MonsterAttackPlan, bool) {
		return MonsterAttackPlan{SkillID: 1, Reach: 50, CooldownMs: 1000, ActionLifecycleMs: 600}, true
	}
	attack := func(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
		return MonsterAttackResult{Accepted: true, TargetAlive: true, Frames: []Frame{{Opcode: wire.OpSkillCastResult}}, Private: []MonsterPrivateFrames{{CharacterID: 1, Frames: []Frame{{Opcode: 0x3057}}}}}
	}
	push := &fakePusher{}
	ops.BasicAttack = func(d string, m monster.Instance, target, skill uint32, at int64) MonsterAttackResult {
		result := attack(d, m, target, skill, at)
		push.PushToSession("1", []Frame{{Opcode: wire.OpObjectStateRefresh}})
		return result
	}
	transactions := 0
	deaths := 0
	ops.RunAction = func(_ string, run func(MonsterAttackOperation)) {
		transactions++
		run(attack)
		if deaths != 0 {
			return
		}
		deaths++
		ops.Monsters.ApplyDamage(monsterTestDivision, instance.Gid, instance.CurrentHP)
		push.PushToSession("1", []Frame{{Opcode: wire.OpObjectStateRefresh}})
	}
	sessions := []SessionSnapshot{playerSessionAt(1, 1005, 1000)}
	recordTestMonsterBootstrap(ops, sessions, now)
	ops.RunMonsterLeg(now, sessions, push)
	frames := monsterFrames(push)
	if transactions == 0 || len(frames) != 4 || frames[0].Opcode != wire.OpObjectSourceCorrection || frames[1].Opcode != wire.OpSkillCastResult || frames[2].Opcode != 0x3057 || frames[3].Opcode != wire.OpObjectStateRefresh {
		t.Fatalf("transaction count %d, publication order %+v; want facing, attack, private tail, then competing death", transactions, frames)
	}
}
