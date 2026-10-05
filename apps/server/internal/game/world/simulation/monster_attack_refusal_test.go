package simulation

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

func TestMonsterRefusalKeepsCombatAndReselectsOnlyAfterCommandError(t *testing.T) {
	for _, refusal := range []MonsterAttackRefusal{MonsterAttackApproachRequired, MonsterAttackCommandRejected} {
		t.Run(map[MonsterAttackRefusal]string{MonsterAttackApproachRequired: "approach", MonsterAttackCommandRejected: "command-error"}[refusal], func(t *testing.T) {
			ops, actor := monsterLegFixture(t, aggressiveTactics())
			const now = int64(100000)
			target := playerPose{Gid: PlayerObjectID(1), Pose: Spawn{RegionID: monsterTestRegion, X: 1005, Y: 20, Z: 1000}, BodyRadius: 4}
			if !ops.Monsters.ArmRetaliation(monsterTestDivision, actor.Gid, target.Gid) {
				t.Fatal("retaliation not armed")
			}
			choices, draws, calls := 0, 0, 0
			ops.Rand = func() float64 { draws++; return .5 }
			ops.AttackPlan = func(_ monster.Instance, requested uint32, sample float64) (MonsterAttackPlan, bool) {
				if requested == 0 {
					choices++
				} else if requested != 7 || sample != 0 {
					t.Fatal("retained selection changed")
				}
				return MonsterAttackPlan{SkillID: 7, Reach: 6, CooldownMs: 1000, ActionLifecycleMs: 500}, true
			}
			ops.BasicAttack = func(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
				calls++
				if calls == 1 {
					return MonsterAttackResult{TargetAlive: true, Refusal: refusal}
				}
				return MonsterAttackResult{Accepted: true, TargetAlive: true}
			}
			m, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
			ops.tryMonsterAttack(monsterTestDivision, actor, aggressiveTactics(), m, []playerPose{target}, now)
			m, _ = ops.Monsters.Mover(monsterTestDivision, actor.Gid)
			if m.TargetGID() != target.Gid || !m.Retaliating() || m.NextAttackMs != 0 || calls != 1 || choices != 1 || draws != 2 {
				t.Fatalf("refusal lost combat, retained timer, or retried synchronously: %+v choices=%d draws=%d calls=%d", m, choices, draws, calls)
			}
			wantSkill, wantMode := uint32(7), monster.MoverChasing
			if refusal == MonsterAttackCommandRejected {
				wantSkill, wantMode = 0, monster.MoverAttacking
			}
			if m.AttackSkillID != wantSkill || m.Mode() != wantMode {
				t.Fatalf("wrong refusal continuation: %+v", m)
			}
			interval := m.AttackIntervalMs
			// The next decision sees the moved target. Both cases must pursue it
			// without another callback, and only command error reselects/jitters.
			target.Pose.X = 1100
			ops.tryMonsterAttack(monsterTestDivision, actor, aggressiveTactics(), m, []playerPose{target}, now+100)
			m, _ = ops.Monsters.Mover(monsterTestDivision, actor.Gid)
			wantChoices := 1
			if refusal == MonsterAttackCommandRejected {
				wantChoices = 2
			}
			if m.Mode() != monster.MoverChasing || !m.InFlight(now+100) || m.TargetGID() != target.Gid || calls != 1 || choices != wantChoices || draws != wantChoices*2 {
				t.Fatalf("next tick failed to chase live target: %+v calls/choices/draws=%d/%d/%d", m, calls, choices, draws)
			}
			if refusal == MonsterAttackApproachRequired && m.AttackIntervalMs != interval {
				t.Fatal("approach refusal changed interval")
			}
			// Return the target into reach of the live monster; prove this is
			// an executable continuation, not merely a retained target field.
			live := m.LivePoseAt(now+200, nil)
			target.Pose = poseToSpawn(live)
			ops.tryMonsterAttack(monsterTestDivision, actor, aggressiveTactics(), m, []playerPose{target}, now+200)
			m, _ = ops.Monsters.Mover(monsterTestDivision, actor.Gid)
			if calls != 2 || m.Mode() != monster.MoverAttacking || m.NextAttackMs <= now+200 || choices != wantChoices {
				t.Fatalf("refused attack never resumed: %+v calls=%d", m, calls)
			}
		})
	}
}

func TestMonsterRefusalCannotOverwriteRetaliationOrRetainLostTarget(t *testing.T) {
	for _, newerTarget := range []bool{false, true} {
		ops, actor := monsterLegFixture(t, aggressiveTactics())
		target := playerPose{Gid: PlayerObjectID(1), Pose: Spawn{RegionID: monsterTestRegion, X: 1005, Y: 20, Z: 1000}, BodyRadius: 4}
		ops.Monsters.ArmRetaliation(monsterTestDivision, actor.Gid, target.Gid)
		ops.BasicAttack = func(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
			if newerTarget {
				ops.Monsters.ArmRetaliation(monsterTestDivision, actor.Gid, PlayerObjectID(2))
			}
			return MonsterAttackResult{TargetAlive: true, Refusal: MonsterAttackCommandRejected}
		}
		m, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
		ops.tryMonsterAttack(monsterTestDivision, actor, aggressiveTactics(), m, []playerPose{target}, 100000)
		m, _ = ops.Monsters.Mover(monsterTestDivision, actor.Gid)
		if newerTarget {
			if m.TargetGID() != PlayerObjectID(2) || !m.RetaliationPending() || m.NextAttackMs == 0 {
				t.Fatalf("stale refusal overwrote newer combat owner: %+v", m)
			}
		} else {
			ops.tryMonsterAttack(monsterTestDivision, actor, aggressiveTactics(), m, nil, 100100)
			m, _ = ops.Monsters.Mover(monsterTestDivision, actor.Gid)
			if m.TargetGID() != 0 || m.Mode() == monster.MoverAttacking {
				t.Fatalf("lost target survived refusal retry: %+v", m)
			}
		}
	}
}
