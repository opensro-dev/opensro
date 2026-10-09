/*
===========================================================================

monsterfollow_lifecycle_test.go - simulation monster follow lifecycle test ownership

===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

// The owner lookup temporarily disappears and returns with the SAME identity.
// This fixture models lookup availability, not native resurrection or a new
// spawn inheriting an old GID. The existing binding must survive every cycle.
/*
================
TestFollowRepeatedControllerLookupLossAndReturn
================
*/
func TestFollowRepeatedControllerLookupLossAndReturn(t *testing.T) {
	ops, parent, child, now := followFixture(t)
	s := ops.Monsters
	mover := startFixtureFollow(t, ops, child, now)
	ops.stopOrAdvanceFollow("summon", child, mover, now+1)
	mover, _ = s.Mover("summon", child.Gid)
	if !mover.InFlight(now + 1) {
		t.Fatal("fixture never accepted movement")
	}
	leader, _ := s.Mover("summon", parent.Gid)
	parent, _ = s.Get("summon", parent.Gid)
	for cycle := int64(0); cycle < 3; cycle++ {
		lostAt := now + 101 + cycle*200
		s.DevelopmentRemoveLeader("summon", parent.Gid)
		before := mover
		frames, _ := ops.stopOrAdvanceFollow("summon", child, mover, lostAt)
		mover, _ = s.Mover("summon", child.Gid)
		timers := followTimers(s, child.Gid)
		if len(frames) != 0 || mover != before || timers.GetTimer(0).LastCheckMs != uint32(lostAt) {
			t.Fatalf("cycle %d lost accepted motion, binding or timer service", cycle)
		}
		// Fixture-only lookup restoration under the state owner's mutex.
		leader.Pose.Z += 200
		s.mu.Lock()
		state := s.division("summon")
		state.instances.set(parent.Gid, parent)
		state.movers.set(parent.Gid, leader)
		state.byRegion[parent.Spawn.RegionID] = append(state.byRegion[parent.Spawn.RegionID], parent.Gid)
		s.mu.Unlock()
		frames, _ = ops.stopOrAdvanceFollow("summon", child, mover, lostAt+100)
		mover, _ = s.Mover("summon", child.Gid)
		if len(frames) == 0 || mover.Mode() != monster.MoverFollowing || mover.ControllerGID() != parent.Gid || mover.To == before.To || mover.ArriveMs <= lostAt+100 {
			t.Fatalf("cycle %d did not refresh accepted goal after lookup return", cycle)
		}
	}
}

/*
================
TestFollowStationaryLeaderRetainsAcceptedGoalSilently
================
*/
func TestFollowStationaryLeaderRetainsAcceptedGoalSilently(t *testing.T) {
	ops, _, child, now := followFixture(t)
	s := ops.Monsters
	mover := startFixtureFollow(t, ops, child, now)
	ops.stopOrAdvanceFollow("summon", child, mover, now+1)
	accepted, _ := s.Mover("summon", child.Gid)
	if !accepted.InFlight(now + 1) {
		t.Fatal("fixture never accepted movement")
	}
	for step := int64(1); step <= 5; step++ {
		at := now + 1 + step*100
		mover, _ = s.Mover("summon", child.Gid)
		frames, _ := ops.stopOrAdvanceFollow("summon", child, mover, at)
		current, _ := s.Mover("summon", child.Gid)
		if len(frames) != 0 || current.To != accepted.To || current.ArriveMs != accepted.ArriveMs || current.Mode() != monster.MoverFollowing {
			t.Fatalf("stationary leader refreshed an accepted goal at step %d", step)
		}
	}
}

/*
================
TestFollowReplacementLeaderDoesNotInheritBinding
================
*/
func TestFollowReplacementLeaderDoesNotInheritBinding(t *testing.T) {
	ops, parent, child, now := followFixture(t)
	s := ops.Monsters
	mover := startFixtureFollow(t, ops, child, now)
	ops.stopOrAdvanceFollow("summon", child, mover, now+1)
	before, _ := s.Mover("summon", child.Gid)
	s.DevelopmentRemoveLeader("summon", parent.Gid)
	replacement, err := s.DevelopmentCreateLeader("summon", parent.Ref.RefObjID, before.Pose, now+10000)
	if err != nil || replacement.Gid == parent.Gid {
		t.Fatalf("replacement fixture: %+v %v", replacement, err)
	}
	frames, _ := ops.stopOrAdvanceFollow("summon", child, before, now+101)
	after, _ := s.Mover("summon", child.Gid)
	if len(frames) != 0 || after != before || after.ControllerGID() != parent.Gid {
		t.Fatal("same-reference replacement inherited old controller binding")
	}
}

/*
================
TestFollowFamilyCleanupAtEveryFixturePhase
================
*/
func TestFollowFamilyCleanupAtEveryFixturePhase(t *testing.T) {
	for _, phase := range []string{"pending-cast", "spawned", "follow-entry", "moving", "satisfied", "controller-lost", "arrived-without-controller", "retaliation"} {
		t.Run(phase, func(t *testing.T) {
			s, parent, wave, ranges, clock := summonFixture(t)
			now := *clock
			ops := &MonsterMoverOps{Monsters: s, TacticsFor: fixedTactics(passiveTactics()), Rand: func() float64 { return 0 }}
			var child monster.Instance
			if phase == "pending-cast" {
				if children, ok := s.BeginSummon("summon", parent, wave, now, now+500, now+1000, ranges); !ok || len(children) != 0 {
					t.Fatal("pending cast not reached")
				}
			} else {
				children, ok := s.CommitSummon("summon", parent, wave, now, now, ranges)
				if !ok || len(children) == 0 {
					t.Fatal("child not created")
				}
				child = children[0]
				leader, _ := s.Mover("summon", parent.Gid)
				leader.Pose.X += 900
				s.CommitMover("summon", parent.Gid, leader)
				if phase != "spawned" {
					mover := startFixtureFollow(t, ops, child, now)
					if phase != "follow-entry" {
						ops.stopOrAdvanceFollow("summon", child, mover, now+1)
						mover, _ = s.Mover("summon", child.Gid)
						if !mover.InFlight(now + 1) {
							t.Fatal("movement not reached")
						}
						if phase == "controller-lost" || phase == "arrived-without-controller" {
							s.DevelopmentRemoveLeader("summon", parent.Gid)
							if phase == "arrived-without-controller" {
								ops.stopOrAdvanceFollow("summon", child, mover, mover.ArriveMs)
								mover, _ = s.Mover("summon", child.Gid)
								if mover.ArriveMs != 0 || mover.Mode() != monster.MoverFollowing {
									t.Fatal("arrival not reached")
								}
							}
						}
						if phase == "retaliation" {
							if !s.ArmRetaliation("summon", child.Gid, PlayerObjectID(1)) {
								t.Fatal("retaliation not armed")
							}
							mover, _ = s.Mover("summon", child.Gid)
							if mover.TargetGID() != PlayerObjectID(1) || mover.FollowLeaderGID() != 0 {
								t.Fatal("retaliation not reached")
							}
						}
						if phase == "satisfied" {
							leader.Pose = mover.LivePoseAt(now+101, nil)
							s.CommitMover("summon", parent.Gid, leader)
							ops.stopOrAdvanceFollow("summon", child, mover, now+101)
							mover, _ = s.Mover("summon", child.Gid)
							if mover.Mode() != monster.MoverIdle {
								t.Fatal("FOLLOW satisfaction not reached")
							}
						}
					}
				}
			}
			// A foreign active actor and a cold record must survive every phase.
			foreign := monster.Instance{Gid: 77, Ref: monster.MonsterRef{MaxHP: 9}, CurrentHP: 9, SummonerGID: 88}
			s.mu.Lock()
			state := s.division("summon")
			state.instances.set(foreign.Gid, foreign)
			state.instances.cold = map[uint32]archivedMonster{99: {}}
			s.mu.Unlock()
			s.DevelopmentRemoveFamily("summon", parent.Gid)
			s.DevelopmentRemoveFamily("summon", parent.Gid) // idempotence
			s.AdvanceSummons(now + 10000)
			if got := s.DevelopmentFollowSnapshot("summon", parent.Gid, now+10000); len(got) != 0 {
				t.Fatal("family survived cleanup")
			}
			if got, ok := s.Get("summon", foreign.Gid); !ok || got != foreign {
				t.Fatal("foreign actor changed")
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			if len(state.instances.cold) != 1 || len(state.pendingSummons) != 0 {
				t.Fatal("archive changed or pending wave survived")
			}
			if state.movers.len() != 0 || len(state.aiTimers) != 0 || len(state.storedAITimers) != 0 {
				t.Fatal("family runtime owners survived")
			}
			for _, gids := range state.byRegion {
				for _, gid := range gids {
					if gid != foreign.Gid {
						t.Fatal("family region index survived")
					}
				}
			}
		})
	}
}
