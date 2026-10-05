package simulation

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

func TestMonsterAttackReleaseTransitionsCannotStrandReturningMover(t *testing.T) {
	const (
		t0                = int64(1_784_000_000_000)
		rangeU            = 6.0
		skillID           = uint32(0x1234)
		actionLifecycleMs = int64(600)
	)
	tests := []struct {
		name         string
		result       MonsterAttackResult
		releaseEvent monster.MoverEvent
		recovering   bool
	}{
		{
			name: "target defeated",
			result: MonsterAttackResult{
				Frames: []Frame{{Opcode: wire.OpSkillCastResult}}, Accepted: true, TargetAlive: false,
			},
			releaseEvent: monster.MoverEventTargetDefeated,
			recovering:   true,
		},
		{
			name: "attack refused",
			result: MonsterAttackResult{
				Frames: []Frame{{Opcode: wire.OpSkillCastResult}}, Accepted: false, TargetAlive: true,
			},
			releaseEvent: monster.MoverEventAttackRefused,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ops, instance := monsterLegFixture(t, aggressiveTactics())
			ops.AttackPlan = func(monster.Instance, uint32, AttackPick) (MonsterAttackPlan, bool) {
				return MonsterAttackPlan{
					SkillID: skillID, Reach: ActionReach(rangeU), CooldownMs: 1000, ActionLifecycleMs: actionLifecycleMs,
				}, true
			}
			ops.BasicAttack = func(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
				return test.result
			}

			push := &fakePusher{}
			recordTestMonsterBootstrap(ops, []SessionSnapshot{playerSessionAt(1, 1000+rangeU-1, 1000)}, t0)
			ops.RunMonsterLeg(t0, []SessionSnapshot{playerSessionAt(1, 1000+rangeU-1, 1000)}, push)
			frames := monsterFrames(push)
			if len(frames) != 2 || frames[0].Opcode != wire.OpObjectSourceCorrection ||
				frames[1].Opcode != wire.OpSkillCastResult {
				t.Fatalf("release frames = %+v, want facing correction then preserved attack result", frames)
			}
			mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
			if test.recovering {
				if mover.Mode() != monster.MoverRecovering || mover.TargetGID() != 0 ||
					mover.BehaviorDeadlineMs != t0+actionLifecycleMs || mover.LastEvent() != test.releaseEvent {
					t.Fatalf("fatal attack recovery ownership = %+v", mover)
				}
				push.toSession, push.toDivision = nil, nil
				ops.RunMonsterLeg(t0+actionLifecycleMs-1, []SessionSnapshot{playerSessionAt(1, 1000+rangeU-1, 1000)}, push)
				if frames := monsterFrames(push); len(frames) != 0 {
					t.Fatalf("recovery moved before authored action end: %+v", frames)
				}
				push.toSession, push.toDivision = nil, nil
				ops.RunMonsterLeg(t0+actionLifecycleMs, []SessionSnapshot{playerSessionAt(1, 1000+rangeU-1, 1000)}, push)
				mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
				if mover.Mode() != monster.MoverIdle || mover.TargetGID() != 0 ||
					mover.LastEvent() != monster.MoverEventSegmentArrived ||
					mover.PreviousEvent() != monster.MoverEventRecoveryElapsed {
					t.Fatalf("completed fatal recovery = %+v", mover)
				}
				return
			}
			if mover.Mode() != monster.MoverIdle || mover.TargetGID() != 0 ||
				mover.Retaliating() || mover.RetaliationPending() {
				t.Fatalf("release stranded behavior ownership: %+v", mover)
			}
			if mover.LastEvent() != monster.MoverEventSegmentArrived || mover.TransitionSerial() != 5 {
				t.Fatalf("release causal ledger = %s/%d, want %s then segment-arrived at serial 5",
					mover.LastEvent(), mover.TransitionSerial(), test.releaseEvent)
			}
			if mover.PreviousEvent() != test.releaseEvent {
				t.Fatalf("release causal ledger lost reason: previous=%s want=%s",
					mover.PreviousEvent(), test.releaseEvent)
			}
		})
	}
}
