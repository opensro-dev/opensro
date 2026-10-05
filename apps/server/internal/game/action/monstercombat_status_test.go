package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

// These are consumer/transaction regressions, not evidence of a supported
// status-producing skill or GM command. The competing writer is explicit.
func TestMonsterDamageRevalidatesStatusAtCharacterCommit(t *testing.T) {
	for _, status := range []uint8{2, 3, 4, 6, 7} {
		t.Run(string(rune('0'+status)), func(t *testing.T) {
			rt, clock, character, instance := newCombatTestRuntime(t, 100)
			instance.Ref.DefaultSkillIDs[0] = 2
			skills := rt.deps.SkillData().(staticSkillSource)
			skill := skills[2]
			skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent = 1, 1, 100
			skills[2] = skill
			before := enterworld.CurrentHP(character)
			deps := rt.deps.(*enterworld.Deps)
			visited := false
			deps.UpdateCharacter = func(c *enterworld.Character, label string, update func() bool) bool {
				if label != "monster-basic-attack" {
					t.Fatalf("unexpected transaction %q", label)
				}
				visited = true
				// The status changes after the detached admission snapshot but
				// before the damage closure enters the character mutation door.
				c.NativeBodyStatus = status
				return update()
			}
			result := rt.MonsterBasicAttack(testDivision, instance, enterworld.ObjectIDForCharacter(character), 2, clock.NowMs())
			if !visited {
				t.Fatal("fixture never reached the damage transaction")
			}
			if result.Accepted || !result.TargetAlive || len(result.Frames) != 0 || len(result.Private) != 0 ||
				enterworld.CurrentHP(character) != before || rt.castTokenCounter != 0 {
				t.Fatalf("stale attack committed damage or publication: %+v hp=%d", result, enterworld.CurrentHP(character))
			}
		})
	}
}

func TestMonsterDamageStatusDetectionDoesNotOverrideLife(t *testing.T) {
	for _, dead := range []bool{false, true} {
		rt, clock, character, instance := newCombatTestRuntime(t, 100)
		instance.Ref.DefaultSkillIDs[0] = 2
		instance.Nest.NativeTacticsFlags = 0x200
		skills := rt.deps.SkillData().(staticSkillSource)
		skill := skills[2]
		skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent = 1, 1, 100
		skills[2] = skill
		character.NativeBodyStatus = 6
		if dead {
			character.CurrentHP = testInt64(0)
		}
		result := rt.MonsterBasicAttack(testDivision, instance, enterworld.ObjectIDForCharacter(character), 2, clock.NowMs())
		if result.Accepted == dead {
			t.Fatalf("dead=%v result=%+v", dead, result)
		}
	}
}

func TestDeadMonsterCannotAttackFromStaleLivingSnapshot(t *testing.T) {
	rt, clock, character, instance := newCombatTestRuntime(t, 100)
	instance.Ref.DefaultSkillIDs[0] = 2
	before := enterworld.CurrentHP(character)
	if _, ok := rt.Monsters.ApplyDamage(testDivision, instance.Gid, instance.CurrentHP); !ok {
		t.Fatal("failed to kill fixture monster")
	}
	result := rt.MonsterBasicAttack(testDivision, instance, enterworld.ObjectIDForCharacter(character), 2, clock.NowMs())
	if result.Accepted || len(result.Frames) != 0 || enterworld.CurrentHP(character) != before || rt.castTokenCounter != 0 {
		t.Fatalf("dead attacker committed a new hit: %+v", result)
	}
}
