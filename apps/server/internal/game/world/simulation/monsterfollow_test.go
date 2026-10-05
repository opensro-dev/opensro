package simulation

import (
	"math"
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

func TestFollowAdmissionDistanceBoundary(t *testing.T) {
	for _, sample := range []struct {
		name     string
		distance float64
		admitted bool
	}{
		{"below", 299, false}, {"equal", 300, true}, {"above", 301, true}, {"unordered", math.NaN(), false},
	} {
		t.Run(sample.name, func(t *testing.T) {
			ops, parent, child, now := followFixture(t)
			s := ops.Monsters
			mover, _ := s.Mover("summon", child.Gid)
			mover.Pose = monster.Pose{RegionID: 0x62ab, X: 100, Y: 20, Z: 100}
			s.CommitMover("summon", child.Gid, mover)
			leader, _ := s.Mover("summon", parent.Gid)
			leader.Pose = mover.Pose
			leader.Pose.X += sample.distance
			s.CommitMover("summon", parent.Gid, leader)
			child.SummonerFollowRange = 300
			s.mu.Lock()
			s.division("summon").instances.set(child.Gid, child)
			s.mu.Unlock()
			frames, _ := ops.followSummoner("summon", child, mover, now)
			current, _ := s.Mover("summon", child.Gid)
			if (current.Mode() == monster.MoverFollowing) != sample.admitted || len(frames) != 0 {
				t.Fatal("wrong threshold admission")
			}
		})
	}
}

func followFixture(t *testing.T) (*MonsterMoverOps, monster.Instance, monster.Instance, int64) {
	t.Helper()
	s, parent, wave, ranges, now := summonFixture(t)
	children, ok := s.CommitSummon("summon", parent, wave, *now, *now, ranges)
	if !ok || len(children) == 0 {
		t.Fatal("summon refused")
	}
	leader, _ := s.Mover("summon", parent.Gid)
	leader.Pose.X += 900
	if !s.CommitMover("summon", parent.Gid, leader) {
		t.Fatal("leader fixture refused")
	}
	return &MonsterMoverOps{Monsters: s, TacticsFor: fixedTactics(passiveTactics()), Rand: func() float64 { return 0 }}, parent, children[0], *now
}

func followTimers(s *MonsterState, gid uint32) monster.AITimeManager {
	s.mu.Lock()
	defer s.mu.Unlock()
	return *s.division("summon").aiTimers[gid]
}

func startFixtureFollow(t *testing.T, ops *MonsterMoverOps, child monster.Instance, now int64) monster.MoverState {
	t.Helper()
	mover, _ := ops.Monsters.Mover("summon", child.Gid)
	frames, _ := ops.followSummoner("summon", child, mover, now)
	mover, _ = ops.Monsters.Mover("summon", child.Gid)
	if len(frames) != 0 || mover.Mode() != monster.MoverFollowing || mover.FollowLeaderGID() != child.SummonerGID {
		t.Fatal("FOLLOW entry failed")
	}
	return mover
}

func TestFollowAdmissionLocationAndTimer7(t *testing.T) {
	ops, parent, child, now := followFixture(t)
	s := ops.Monsters
	leader, _ := s.Mover("summon", parent.Gid)
	original := leader
	leader.Pose.RegionID += 3
	s.CommitMover("summon", parent.Gid, leader)
	mover, _ := s.Mover("summon", child.Gid)
	if frames, handled := ops.followSummoner("summon", child, mover, now); handled || len(frames) != 0 {
		t.Fatal("incompatible region admitted")
	}
	timers := followTimers(s, child.Gid)
	if got := timers.GetTimer(7); got.ArmedImmediate || got.LastCheckMs != uint32(now) {
		t.Fatalf("negative scan did not consume Timer 7: %+v", got)
	}
	s.CommitMover("summon", parent.Gid, original)
	if frames, handled := ops.followSummoner("summon", child, mover, now+29); handled || len(frames) != 0 {
		t.Fatal("admitted before Timer 7")
	}
	startFixtureFollow(t, ops, child, now+30)
	timers = followTimers(s, child.Gid)
	if timer := timers.GetTimer(0); !timer.ArmedImmediate || timer.IntervalMs != 100 {
		t.Fatalf("entry Timer 0: %+v", timer)
	}
}

func TestFollowTimerAndMovementCompletionAreIndependent(t *testing.T) {
	ops, parent, child, now := followFixture(t)
	s := ops.Monsters
	mover := startFixtureFollow(t, ops, child, now)
	if mover.InFlight(now) {
		t.Fatal("entry invented movement")
	}
	initialTimers := followTimers(s, child.Gid)
	seven := initialTimers.GetTimer(7)
	if frames, _ := ops.stopOrAdvanceFollow("summon", child, mover, now+1); len(frames) == 0 {
		t.Fatal("first Timer 0 callback did not steer")
	}
	mover, _ = s.Mover("summon", child.Gid)
	if frames, _ := ops.stopOrAdvanceFollow("summon", child, mover, now+100); len(frames) != 0 {
		t.Fatal("steered before Timer 0")
	}
	s.ApplyDamage("summon", parent.Gid, parent.CurrentHP)
	before := mover
	if frames, _ := ops.stopOrAdvanceFollow("summon", child, mover, now+101); len(frames) != 0 {
		t.Fatal("dead controller emitted cancellation")
	}
	mover, _ = s.Mover("summon", child.Gid)
	if mover != before {
		t.Fatal("dead controller changed movement/state")
	}
	if got := followTimers(s, child.Gid); got.GetTimer(7) != seven || got.GetTimer(0).LastCheckMs != uint32(now+101) {
		t.Fatal("wrong timer owner/order")
	}
	arrival := mover.ArriveMs
	frames, _ := ops.stopOrAdvanceFollow("summon", child, mover, arrival)
	mover, _ = s.Mover("summon", child.Gid)
	if len(frames) != 1 || mover.Mode() != monster.MoverFollowing || mover.ArriveMs != 0 || mover.ControllerGID() != parent.Gid {
		t.Fatal("arrival must settle movement while retaining FOLLOW and binding")
	}
}

func TestFollowMissingControllerAndCreationProvenanceDoNotCancel(t *testing.T) {
	for _, reason := range []string{"death", "removal", "provenance"} {
		t.Run(reason, func(t *testing.T) {
			ops, parent, child, now := followFixture(t)
			s := ops.Monsters
			mover := startFixtureFollow(t, ops, child, now)
			ops.stopOrAdvanceFollow("summon", child, mover, now+1)
			mover, _ = s.Mover("summon", child.Gid)
			switch reason {
			case "death":
				s.ApplyDamage("summon", parent.Gid, parent.CurrentHP)
			case "removal":
				s.Defeat("summon", parent.Gid, time.UnixMilli(now+2))
			case "provenance":
				s.mu.Lock()
				child.SummonerGID = 0
				s.division("summon").instances.set(child.Gid, child)
				s.mu.Unlock()
			}
			frames, _ := ops.stopOrAdvanceFollow("summon", child, mover, now+2)
			current, _ := s.Mover("summon", child.Gid)
			if len(frames) != 0 || current != mover {
				t.Fatal("closed timer/controller lifetime changed movement")
			}
		})
	}
}

func TestFollowRetaliationBypassesClosedGatesAndRemovalRetiresTimers(t *testing.T) {
	ops, _, child, now := followFixture(t)
	s := ops.Monsters
	mover := startFixtureFollow(t, ops, child, now)
	ops.stopOrAdvanceFollow("summon", child, mover, now+1)
	before := followTimers(s, child.Gid)
	ops.AttackPlan = func(monster.Instance, uint32, AttackPick) (MonsterAttackPlan, bool) {
		return MonsterAttackPlan{SkillID: 1, Reach: 20, CooldownMs: 1000, ActionLifecycleMs: 500}, true
	}
	attacker := playerPose{Gid: PlayerObjectID(1), Pose: poseToSpawn(mover.LivePoseAt(now+2, nil)), BodyRadius: 4}
	attacker.Pose.X -= 100
	if !s.ArmRetaliation("summon", child.Gid, attacker.Gid) {
		t.Fatal("retaliation refused")
	}
	frames, _ := ops.advanceInstance("summon", child, []playerPose{attacker}, now+2)
	current, _ := s.Mover("summon", child.Gid)
	if len(frames) == 0 || current.Mode() != monster.MoverChasing || current.TargetGID() != attacker.Gid || current.FollowLeaderGID() != 0 {
		t.Fatal("FOLLOW blocked immediate retaliation")
	}
	if before != followTimers(s, child.Gid) {
		t.Fatal("retaliation consumed FOLLOW timers")
	}
	if !s.Defeat("summon", child.Gid, time.UnixMilli(now+3)) {
		t.Fatal("child removal refused")
	}
	s.mu.Lock()
	_, retained := s.division("summon").aiTimers[child.Gid]
	s.mu.Unlock()
	if retained {
		t.Fatal("removed child retained timer banks")
	}
}

func TestRejectedFollowPlansPreservePacketsAndBothTimers(t *testing.T) {
	for _, phase := range []string{"entry", "refresh", "arrival"} {
		for _, reason := range []string{"retaliation", "leader death", "leader move", "child death", "control", "timer"} {
			t.Run(phase+"/"+reason, func(t *testing.T) {
				ops, parent, child, now := followFixture(t)
				s := ops.Monsters
				mover, _ := s.Mover("summon", child.Gid)
				if phase != "entry" {
					mover = startFixtureFollow(t, ops, child, now)
				}
				plan, ok := s.prepareFollow("summon", child, mover, now)
				if !ok {
					t.Fatal("snapshot refused")
				}
				staged := mover
				if phase == "entry" {
					plan.timers.CheckTimer(7, uint32(now))
					mustMoverTransition(&staged, monster.MoverEventFollowStarted, parent.Gid)
				} else {
					plan.timers.CheckTimer(0, uint32(now))
				}
				switch reason {
				case "retaliation":
					s.ArmRetaliation("summon", child.Gid, PlayerObjectID(1))
				case "leader death":
					s.ApplyDamage("summon", parent.Gid, parent.CurrentHP)
				case "leader move":
					leader, _ := s.Mover("summon", parent.Gid)
					leader.Pose.X++
					s.CommitMover("summon", parent.Gid, leader)
				case "child death":
					s.ApplyDamage("summon", child.Gid, child.CurrentHP)
				case "control":
					changed := mover
					changed.ReleaseController(parent.Gid)
					s.CommitMover("summon", child.Gid, changed)
				case "timer":
					s.checkAITimer("summon", child.Gid, 1, now)
				}
				before := followTimers(s, child.Gid)
				frames, accepted := s.commitFollow(plan, staged, []Frame{correctionFrame(child.Gid, staged.Pose)})
				if accepted || len(frames) != 0 || followTimers(s, child.Gid) != before {
					t.Fatal("stale plan published frames/state/timers")
				}
			})
		}
	}
}

func TestRejectedFollowArrivalKeepsRetaliationAndCadence(t *testing.T) {
	ops, _, child, now := followFixture(t)
	s := ops.Monsters
	mover := startFixtureFollow(t, ops, child, now)
	plan, ok := s.prepareFollow("summon", child, mover, mover.ArriveMs)
	if !ok {
		t.Fatal("arrival snapshot refused")
	}
	// Explicit owner-boundary interleaving: no terrain callback runs after a
	// segment has matured. This test makes no network-delivery claim.
	mover.Pose = mover.To
	mover.From, mover.To = monster.Pose{}, monster.Pose{}
	mover.DepartMs, mover.ArriveMs = 0, 0
	mustMoverTransition(&mover, monster.MoverEventSegmentArrived, 0)
	s.ArmRetaliation("summon", child.Gid, PlayerObjectID(1))
	frames, accepted := s.commitFollow(plan, mover, []Frame{correctionFrame(child.Gid, mover.Pose)})
	current, _ := s.Mover("summon", child.Gid)
	if accepted || len(frames) != 0 || current.TargetGID() != PlayerObjectID(1) || current.FollowLeaderGID() != 0 || followTimers(s, child.Gid) != plan.timersBefore {
		t.Fatal("stale arrival overwrote retaliation or published correction")
	}
}
