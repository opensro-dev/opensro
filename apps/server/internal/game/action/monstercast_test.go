package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

func TestPreparingMonsterFatalityCommitsOnlyAtRelease(t *testing.T) {
	rt, clock, c, m := newCombatTestRuntime(t, 100)
	m.Ref.DefaultSkillIDs[0] = 2
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.ActionCastingTimeMs = 1000
	skill.Attack.Min = 100
	skill.Attack.Max = 100
	skill.Attack.Percent = 100
	skills[2] = skill
	c.CurrentHP = testInt64(1)
	c.BattleUntilMs = 0 // battle exit on death is pinned by the battle-state tests
	start := rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
	if !start.Accepted || !start.TargetAlive || len(start.Frames) != 1 || enterworld.CurrentHP(c) != 1 {
		t.Fatalf("preparation killed target: %+v", start)
	}
	token := binary.LittleEndian.Uint32(start.Frames[0].Payload[10:])
	if packets := rt.advanceMonsterCasts(clock.NowMs() + 1000); len(packets) != 0 {
		t.Fatalf("release is strictly after casting time: %+v", packets)
	}
	packets := rt.advanceMonsterCasts(clock.NowMs() + 1001)
	// The release, then the victim's private death notice.
	if enterworld.CurrentHP(c) != 0 || len(packets) != 2 || len(packets[0].Frames) != 3 ||
		len(packets[1].Frames) != 1 || packets[1].Frames[0].Opcode != wire.OpDeathNotice {
		t.Fatalf("missing fatal release transaction: %+v", packets)
	}
	frames := packets[0].Frames
	if frames[0].Opcode != 0xb505 || binary.LittleEndian.Uint32(frames[0].Payload[1:]) != token || frames[1].Opcode != 0x33a6 || frames[2].Opcode != wire.OpObjectStateRefresh {
		t.Fatalf("wrong fatal release order: %+v", frames)
	}
	if packets := rt.advanceMonsterCasts(clock.NowMs() + 2000); len(packets) != 0 {
		t.Fatalf("duplicate release: %+v", packets)
	}
}

func TestPreparingMonsterCancellationNeverChargesTarget(t *testing.T) {
	for _, cause := range []string{"attacker-dead", "target-dead", "target-status", "disconnect"} {
		t.Run(cause, func(t *testing.T) {
			rt, clock, c, m := newCombatTestRuntime(t, 100)
			m.Ref.DefaultSkillIDs[0] = 2
			skills := rt.deps.SkillData().(staticSkillSource)
			skill := skills[2]
			skill.ActionCastingTimeMs = 1000
			skills[2] = skill
			start := rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
			if !start.Accepted {
				t.Fatal("not preparing")
			}
			switch cause {
			case "attacker-dead":
				rt.Monsters.ApplyDamage(testDivision, m.Gid, m.CurrentHP)
			case "target-dead":
				c.CurrentHP = testInt64(0)
			case "target-status":
				c.NativeBodyStatus = 7
			case "disconnect":
				rt.clearSkillFinalizes(testDivision, c.Name)
			}
			before := enterworld.CurrentHP(c)
			frames := rt.advanceMonsterCasts(clock.NowMs() + 1001)
			if cause == "disconnect" {
				frames = rt.drainSkillFinalizes(clock.NowMs() + 1001)
			}
			if enterworld.CurrentHP(c) != before || len(frames) != 1 || len(frames[0].Frames) != 1 || frames[0].Frames[0].Opcode != 0xb505 || frames[0].Frames[0].Payload[0] != 2 {
				t.Fatalf("cancel charged damage or failed to close: %+v", frames)
			}
			if len(rt.pendingMonsterCasts) != 0 {
				t.Fatal("pending cast leaked")
			}
		})
	}
}

func TestZeroCastingMonsterProjectileEngagesImmediately(t *testing.T) {
	rt, clock, c, m := newCombatTestRuntime(t, 100)
	m.Ref.DefaultSkillIDs[0] = 2
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.ActionCastingTimeMs = 0
	skill.ProjectileSpeed = 100
	skill.Attack.Min = 1
	skill.Attack.Max = 1
	skill.Attack.Percent = 100
	skills[2] = skill
	before := enterworld.CurrentHP(c)
	got := rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
	if !got.Accepted || len(got.Frames) == 0 || got.Frames[0].Opcode != 0xb245 || len(got.Frames[0].Payload) <= 19 || enterworld.CurrentHP(c) >= before || len(rt.pendingMonsterCasts) != 0 {
		t.Fatalf("zero-casting projectile incorrectly entered preparation: %+v", got)
	}
}
