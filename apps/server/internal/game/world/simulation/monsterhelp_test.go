package simulation

import (
	"encoding/binary"
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

func helpPayload(kind uint8, sender, tactics uint32, parameter uint8, target uint32) []byte {
	b := make([]byte, 14)
	b[0], b[9] = kind, parameter
	binary.LittleEndian.PutUint32(b[1:], sender)
	binary.LittleEndian.PutUint32(b[5:], tactics)
	binary.LittleEndian.PutUint32(b[10:], target)
	return b
}

func helpFixture(t *testing.T) (*MonsterMoverOps, monster.Instance, playerPose) {
	ops, instance, mover, target := pursuitControlsFixture(t)
	mover.Pose.X = 1000
	mustMoverTransition(&mover, monster.MoverEventTraceAbandoned, 0)
	mover, _ = ops.planIdleEntry(&instance, mover, 10000)
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	ops.PlanPath = func(from, to monster.Pose) *monster.NavigationPath {
		return monster.NewNavigationPath(from, to, to, 0, func(float64, monster.Pose) (float64, bool) { return 20, true })
	}
	return ops, instance, target
}

func TestExternalHelpProductionTickConsumesAndEntersBattle(t *testing.T) {
	ops, instance, target := helpFixture(t)
	payload := helpPayload(1, target.Gid, 999, 0, target.Gid)
	if err := ops.Monsters.DeliverMonsterHelp(monsterTestDivision, instance.Gid, payload); err != nil {
		t.Fatal(err)
	}
	instance, _ = ops.Monsters.Get(monsterTestDivision, instance.Gid)
	frames, _ := ops.advanceInstance(monsterTestDivision, instance, []playerPose{target}, 10000)
	m, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	row, _ := ops.Monsters.Get(monsterTestDivision, instance.Gid)
	if m.TargetGID() != target.Gid || !m.HelpLatched() || len(frames) != 1 || row.Opponents[0].GID != target.Gid {
		t.Fatalf("help not committed: %+v %+v", m, frames)
	}
	if _, pending := row.Help.Pending(); pending {
		t.Fatal("consumed help remained pending")
	}
	// A parameter-1 help during BATTLE runs native OnExit and loses the
	// newly set target; do not turn this branch into damage retaliation.
	if err := ops.Monsters.DeliverMonsterHelp(monsterTestDivision, instance.Gid, helpPayload(1, target.Gid, 999, 1, target.Gid)); err != nil {
		t.Fatal(err)
	}
	instance, _ = ops.Monsters.Get(monsterTestDivision, instance.Gid)
	ops.advanceInstance(monsterTestDivision, instance, []playerPose{target}, 10001)
	m, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if m.Mode() != monster.MoverBattleReentry || m.TargetGID() != 0 {
		t.Fatal("battle reentry skipped native exit")
	}
	instance, _ = ops.Monsters.Get(monsterTestDivision, instance.Gid)
	ops.advanceInstance(monsterTestDivision, instance, []playerPose{target}, 10002)
	m, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if m.Mode() != monster.MoverIdle {
		t.Fatal("empty battle did not leave on its tick")
	}
}

func TestHelpRejectedEventStillOwnsTickAndNavigationBitIsNotBoolean(t *testing.T) {
	for _, row := range []struct {
		result   uint32
		accepted bool
	}{{0, true}, {monster.NavResultClipped, false}, {monster.NavResultBlocked, true}} {
		ops, instance, target := helpFixture(t)
		ops.PlanPath = func(from, to monster.Pose) *monster.NavigationPath {
			return monster.NewNavigationPath(from, to, from, row.result, func(float64, monster.Pose) (float64, bool) { return 20, true })
		}
		if err := ops.Monsters.DeliverMonsterHelp(monsterTestDivision, instance.Gid, helpPayload(1, target.Gid, 0, 0, target.Gid)); err != nil {
			t.Fatal(err)
		}
		instance, _ = ops.Monsters.Get(monsterTestDivision, instance.Gid)
		before, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		frames, _ := ops.advanceInstance(monsterTestDivision, instance, []playerPose{target}, 10000)
		after, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if (after.TargetGID() != 0) != row.accepted {
			t.Fatalf("native result %#x changed meaning", row.result)
		}
		if !row.accepted && (len(frames) != 0 || after != before) {
			t.Fatal("rejected event fell through into ordinary AI")
		}
		current, _ := ops.Monsters.Get(monsterTestDivision, instance.Gid)
		if _, p := current.Help.Pending(); p {
			t.Fatal("rejected event not consumed")
		}
	}
}

func TestHelpReplacementDuringNavigationIsNotLost(t *testing.T) {
	ops, instance, target := helpFixture(t)
	first := helpPayload(1, target.Gid, 0, 0, target.Gid)
	replacement := helpPayload(0, target.Gid, 777, 0, target.Gid)
	if err := ops.Monsters.DeliverMonsterHelp(monsterTestDivision, instance.Gid, first); err != nil {
		t.Fatal(err)
	}
	instance, _ = ops.Monsters.Get(monsterTestDivision, instance.Gid)
	ops.PlanPath = func(from, to monster.Pose) *monster.NavigationPath {
		if err := ops.Monsters.DeliverMonsterHelp(monsterTestDivision, instance.Gid, replacement); err != nil {
			t.Fatal(err)
		}
		return monster.NewNavigationPath(from, to, to, 0, func(float64, monster.Pose) (float64, bool) { return 20, true })
	}
	frames, _ := ops.advanceInstance(monsterTestDivision, instance, []playerPose{target}, 10000)
	m, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	current, _ := ops.Monsters.Get(monsterTestDivision, instance.Gid)
	event, pending := current.Help.Pending()
	if len(frames) != 0 || m.TargetGID() != 0 || !pending || event.SenderTacticsID != 777 {
		t.Fatal("stale help consumed replacement or published motion")
	}
}

func TestHelpRejectsActorChangesDuringNavigation(t *testing.T) {
	for _, scenario := range []string{"same-target-hit", "receiver-death", "maximum-hp", "home", "source-death", "source-removal", "retaliation"} {
		t.Run(scenario, func(t *testing.T) {
			ops, instance, target := helpFixture(t)
			s := ops.Monsters
			source := instance
			source.Gid += 77
			s.mu.Lock()
			s.division(monsterTestDivision).instances.set(source.Gid, source)
			s.mu.Unlock()
			if err := s.DeliverMonsterHelp(monsterTestDivision, instance.Gid, helpPayload(1, source.Gid, 0, 0, target.Gid)); err != nil {
				t.Fatal(err)
			}
			instance, _ = s.Get(monsterTestDivision, instance.Gid)
			planned := false
			var admitted monster.MoverState
			ops.PlanPath = func(from, to monster.Pose) *monster.NavigationPath {
				planned = true
				if scenario == "retaliation" {
					s.ArmRetaliation(monsterTestDivision, instance.Gid, target.Gid)
				} else {
					s.mu.Lock()
					state := s.division(monsterTestDivision)
					row := state.instances.get(instance.Gid)
					switch scenario {
					case "same-target-hit":
						row.Opponents[0].LastHitMs++
					case "receiver-death":
						row.CurrentHP = 0
					case "maximum-hp":
						row.Ref.MaxHP *= 2
					case "home":
						row.Spawn.X += 1000
					case "source-death":
						changed := state.instances.get(source.Gid)
						changed.CurrentHP = 0
						state.instances.set(source.Gid, changed)
					case "source-removal":
						state.instances.remove(source.Gid)
					}
					state.instances.set(instance.Gid, row)
					s.mu.Unlock()
				}
				admitted, _ = s.Mover(monsterTestDivision, instance.Gid)
				return monster.NewNavigationPath(from, to, to, 0, func(float64, monster.Pose) (float64, bool) { return 20, true })
			}
			frames, _ := ops.advanceInstance(monsterTestDivision, instance, []playerPose{target}, 10000)
			after, _ := s.Mover(monsterTestDivision, instance.Gid)
			current, _ := s.Get(monsterTestDivision, instance.Gid)
			_, pending := current.Help.Pending()
			if !planned || len(frames) != 0 || after != admitted || !pending {
				t.Fatalf("stale help committed: planned=%v frames=%d pending=%v", planned, len(frames), pending)
			}
		})
	}
}
