/*
===========================================================================

earthghost_ownership_test.go - earth ghost homing and approach ownership

===========================================================================
*/

package simulation

import (
	"fmt"
	"opensro.online/server/internal/game/abnormal"
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
approachSnapshot
================
*/
func approachSnapshot(s *MonsterState, division string, gid uint32) (uint32, monster.ApproachSlots, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	target := state.approachActors[gid]
	if group := state.approachTargets[target]; group != nil {
		return target, group.slots, len(group.members)
	}
	return target, monster.ApproachSlots{}, 0
}

/*
================
TestEarthGhostHomeBoundaryAndPolicyModes
================
*/
func TestEarthGhostHomeBoundaryAndPolicyModes(t *testing.T) {
	for _, tc := range []struct {
		name              string
		distance, y       float64
		boundary          uint8
		limit             int32
		detached, abandon bool
	}{
		{"inside", 1539, 0, 1, 500, false, false},
		{"equality", 1540, 0, 1, 500, false, false},
		{"outside", 1541, 0, 1, 500, false, true},
		{"home_is_planar", 1540, 1000, 1, 500, false, false},
		{"boundary_zero", 1700, 0, 0, 500, false, false},
		{"boundary_two", 1700, 0, 2, 500, false, false},
		{"zero_limit_Timer6_still_applies", 1700, 0, 1, 0, false, true},
		{"detached_nest", 1700, 0, 1, 500, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ops, a, target := earthGhostFixture(t)
			a.Nest.Controls.TraceBoundary = tc.boundary
			a.Nest.Controls.TraceData = tc.limit
			a.NestDetached = tc.detached
			m, _ := ops.Monsters.Mover(monsterTestDivision, a.Gid)
			m.Pose.X += tc.distance
			m.Pose.Y += tc.y
			m.Pose = normalizeMonsterPose(m.Pose)
			s := ops.Monsters
			s.mu.Lock()
			state := s.populationForObject(monsterTestDivision, a.Gid)
			state.instances.set(a.Gid, a)
			state.movers.set(a.Gid, m)
			s.mu.Unlock()
			target.Pose = poseToSpawn(m.Pose)
			target.Pose.X += 10
			target.Pose = NormalizeSpawnFrame(target.Pose)
			ops.advancePursuitControls(monsterTestDivision, a, m, target, m.Pose, 10000)
			after, _ := s.Mover(monsterTestDivision, a.Gid)
			if (after.TargetGID() == 0) != tc.abandon {
				t.Fatalf("mode=%s target=%d want abandon=%v", after.Mode(), after.TargetGID(), tc.abandon)
			}
		})
	}
}

/*
==================
TestEarthGhostDistantHomeRegionAbandonsOnlyBoundedTrace

545E50 continues trace boundary 0/2 before 430CE0 tests whether home lies
within the adjacent regions (545ECC); TraceEnabled owns that gate here, so
only a bounded row abandons. The return leg beyond the population cannot
commit, so assert the decision rather than the committed target.
==================
*/
func TestEarthGhostDistantHomeRegionAbandonsOnlyBoundedTrace(t *testing.T) {
	for _, tc := range []struct {
		boundary uint8
		abandon  bool
	}{{0, false}, {1, true}, {2, false}} {
		t.Run(fmt.Sprint("boundary_", tc.boundary), func(t *testing.T) {
			ops, a, target := earthGhostFixture(t)
			a.Nest.Controls.TraceBoundary = tc.boundary
			m, _ := ops.Monsters.Mover(monsterTestDivision, a.Gid)
			m.Pose.X += 2 * NativeRegionSize
			m.Pose = normalizeMonsterPose(m.Pose)
			if monster.FollowLocationCompatible(m.Pose.RegionID, anchorPose(a).RegionID) {
				t.Fatal("fixture did not leave the adjacent home regions")
			}
			s := ops.Monsters
			s.mu.Lock()
			state := s.populationForObject(monsterTestDivision, a.Gid)
			state.instances.set(a.Gid, a)
			state.movers.set(a.Gid, m)
			s.mu.Unlock()
			target.Pose = poseToSpawn(m.Pose)
			target.Pose.X += 10
			target.Pose = NormalizeSpawnFrame(target.Pose)
			if _, handled := ops.advancePursuitControls(monsterTestDivision, a, m, target, m.Pose, 10000); handled != tc.abandon {
				t.Fatalf("pursuit handled=%v want abandon=%v", handled, tc.abandon)
			}
		})
	}
}

// 53FD04 installs 549460 instead of 548BE0 for dungeon actors. It steers to
// the slot point and never applies 548CF1's moving-target center extension.
/*
================
TestDungeonApproachKeepsSlotPointForMovingTarget
================
*/
func TestDungeonApproachKeepsSlotPointForMovingTarget(t *testing.T) {
	spacing := CombatSpacing{ActorBodyRadius: 3, TargetBodyRadius: 4, ActionReach: 7}
	for _, region := range []uint16{0x8101, 26007} {
		standing := playerPose{Gid: PlayerObjectID(1), Pose: Spawn{RegionID: region, X: 500, Y: 10, Z: 500}, BodyRadius: 4}
		moving := standing
		moving.MovementIntent = playerMovementIntent{present: true, inFlight: true, destination: Spawn{RegionID: region, X: 900, Y: 10, Z: 500}}
		live := monster.Pose{RegionID: region, X: 440, Y: 10, Z: 470}
		for slot := 0; slot < 8; slot++ {
			slotPoint := squadApproachGoal(live, standing, spacing, slot)
			if got := squadApproachGoal(live, moving, spacing, slot); (got == slotPoint) != IsDungeonRegion(region) {
				t.Fatalf("region %#x slot %d: moving goal %+v, slot point %+v", region, slot, got, slotPoint)
			}
		}
	}
}

/*
================
TestEarthGhostHomePlanRejectsChangedNest
================
*/
func TestEarthGhostHomePlanRejectsChangedNest(t *testing.T) {
	for _, field := range []string{"radius", "center", "detached"} {
		t.Run(field, func(t *testing.T) {
			ops, a, target := earthGhostFixture(t)
			s := ops.Monsters
			m, _ := s.Mover(monsterTestDivision, a.Gid)
			m.Pose.X += 1600
			m.Pose = normalizeMonsterPose(m.Pose)
			s.CommitMover(monsterTestDivision, a.Gid, m)
			target.Pose = poseToSpawn(m.Pose)
			target.Pose.X += 10
			target.Pose = NormalizeSpawnFrame(target.Pose)
			changed := false
			ops.Rand = func() float64 {
				if !changed {
					changed = true
					s.mu.Lock()
					state := s.populationForObject(monsterTestDivision, a.Gid)
					current := state.instances.get(a.Gid)
					switch field {
					case "radius":
						current.Nest.Radius += 1
					case "center":
						current.Nest.X += 1
					case "detached":
						current.NestDetached = true
					}
					state.instances.set(a.Gid, current)
					s.mu.Unlock()
				}
				return .5
			}
			frames, _ := ops.advancePursuitControls(monsterTestDivision, a, m, target, m.Pose, 10000)
			after, _ := s.Mover(monsterTestDivision, a.Gid)
			if !changed || len(frames) != 0 || after != m {
				t.Fatal("stale home decision escaped admission")
			}
		})
	}
}

/*
================
TestEarthGhostApproachReservationsAreLifecycleOwned
================
*/
func TestEarthGhostApproachReservationsAreLifecycleOwned(t *testing.T) {
	for _, event := range []string{"target_lost", "retarget", "damage_fatal", "burn_fatal", "despawn", "controller"} {
		t.Run(event, func(t *testing.T) {
			ops, a, target := earthGhostFixture(t)
			s := ops.Monsters
			b := secondEarthGhost(t, ops, a, target)
			target = earthGhostMovingTarget(target.Pose, 0)
			ops.advanceInstance(monsterTestDivision, a, []playerPose{target}, 10000)
			_, slots, members := approachSnapshot(s, monsterTestDivision, a.Gid)
			if members != 2 {
				t.Fatal("missing squad membership")
			}
			owned := false
			for _, owner := range slots {
				owned = owned || owner == a.Gid
			}
			if !owned {
				t.Fatal("actor has no initial reservation")
			}
			switch event {
			case "target_lost":
				ops.advanceInstance(monsterTestDivision, a, nil, 10100)
			case "retarget":
				s.ArmRetaliation(monsterTestDivision, a.Gid, target.Gid+1)
			case "damage_fatal":
				s.ApplyDamage(monsterTestDivision, a.Gid, a.CurrentHP)
			case "burn_fatal":
				s.SetAbnormalContext(testAbnormalContext{})
				burn := abnormal.Record{Status: abnormal.Burn, Level: 30, DurationMs: 30 * 750, Rate24: 5533, Scale20: 1, SourceGID: target.Gid, SourceName: "fixture"}
				if r := s.ApplyDamageSequence(monsterTestDivision, a.Gid, a.CurrentHP, []MonsterDamagePlan{{GID: a.Gid, CreditGID: target.Gid, Abnormal: []abnormal.Record{burn}, AbnormalSources: s.PrepareAbnormalSources(monsterTestDivision, []abnormal.Record{burn})}}); len(r) != 1 {
					t.Fatal("burn not applied")
				}
				plan, _ := s.PlanAbnormalUpdate(monsterTestDivision, a.Gid, 10001)
				result, ok := s.CommitAbnormalUpdate(plan, 10001)
				if !ok || !result.Fatal {
					t.Fatal("burn not fatal")
				}
			case "despawn":
				if !s.Defeat(monsterTestDivision, a.Gid, time.UnixMilli(10100)) {
					t.Fatal("despawn rejected")
				}
			case "controller":
				m, _ := s.Mover(monsterTestDivision, a.Gid)
				m.BindController(monster.ControlOwned, target.Gid+1)
				s.CommitMover(monsterTestDivision, a.Gid, m)
			}
			oldTarget, after, count := approachSnapshot(s, monsterTestDivision, b.Gid)
			if oldTarget != target.Gid || count != 1 {
				t.Fatal("peer membership changed", oldTarget, count)
			}
			for _, owner := range after {
				if owner == a.Gid {
					t.Fatal("released actor retained slot")
				}
			}
			for index, owner := range slots {
				if owner == b.Gid && after[index] != b.Gid {
					t.Fatal("actor released peer's reservation")
				}
			}
		})
	}
}

/*
================
TestEarthGhostApproachAndNavigationCommitTogether
================
*/
func TestEarthGhostApproachAndNavigationCommitTogether(t *testing.T) {
	for _, event := range []string{"retarget_actor", "move_peer", "retarget_peer", "kill_actor"} {
		t.Run(event, func(t *testing.T) {
			ops, a, target := earthGhostFixture(t)
			s := ops.Monsters
			b := secondEarthGhost(t, ops, a, target)
			target = earthGhostMovingTarget(target.Pose, 0)
			called := false
			ops.PlanPath = func(from, to monster.Pose) *monster.NavigationPath {
				if !called {
					called = true
					switch event {
					case "retarget_actor":
						s.ArmRetaliation(monsterTestDivision, a.Gid, target.Gid+1)
					case "move_peer":
						m, _ := s.Mover(monsterTestDivision, b.Gid)
						m.Pose.X += 1
						s.CommitMover(monsterTestDivision, b.Gid, m)
					case "retarget_peer":
						s.ArmRetaliation(monsterTestDivision, b.Gid, target.Gid+1)
					case "kill_actor":
						s.ApplyDamage(monsterTestDivision, a.Gid, a.CurrentHP)
					}
				}
				return monster.NewNavigationPath(from, to, to, 0, func(_ float64, _ monster.Pose) (float64, bool) { return to.Y, true })
			}
			frames, _ := ops.advanceInstance(monsterTestDivision, a, []playerPose{target}, 10000)
			if !called || len(frames) != 0 {
				t.Fatalf("stale navigation published %d frames", len(frames))
			}
			m, _ := s.Mover(monsterTestDivision, a.Gid)
			if m.InFlight(10000) {
				t.Fatal("stale segment committed")
			}
			got, slots, _ := approachSnapshot(s, monsterTestDivision, a.Gid)
			if event == "retarget_actor" && got != target.Gid+1 {
				t.Fatal("old group overwrote retarget")
			}
			for _, owner := range slots {
				if owner != 0 {
					t.Fatal("speculative reservation leaked")
				}
			}
		})
	}
}

/*
================
TestEarthGhostFailedNavigationReleasesOnlyItsReservation
================
*/
func TestEarthGhostFailedNavigationReleasesOnlyItsReservation(t *testing.T) {
	ops, a, target := earthGhostFixture(t)
	s := ops.Monsters
	b := secondEarthGhost(t, ops, a, target)
	target = earthGhostMovingTarget(target.Pose, 0)
	ops.PlanPath = func(monster.Pose, monster.Pose) *monster.NavigationPath { return nil }
	frames, _ := ops.advanceInstance(monsterTestDivision, a, []playerPose{target}, 10000)
	_, slots, count := approachSnapshot(s, monsterTestDivision, a.Gid)
	if len(frames) == 0 || count != 2 {
		t.Fatal("failed navigation did not publish hold / preserve membership")
	}
	for _, owner := range slots {
		if owner == a.Gid {
			t.Fatal("unusable actor reservation retained")
		}
	}
	ops.PlanPath = nil
	ops.advanceInstance(monsterTestDivision, b, []playerPose{target}, 10000)
	_, slots, _ = approachSnapshot(s, monsterTestDivision, b.Gid)
	owned := false
	for _, owner := range slots {
		owned = owned || owner == b.Gid
	}
	if !owned {
		t.Fatal("failed peer prevented approach")
	}
}

/*
================
TestEarthGhostEightSlotsAreNotAnAttackerCap
================
*/
func TestEarthGhostEightSlotsAreNotAnAttackerCap(t *testing.T) {
	ops, a, target := earthGhostFixture(t)
	s := ops.Monsters
	target = earthGhostMovingTarget(target.Pose, 0)
	actors := []monster.Instance{a}
	for i := 0; i < 8; i++ {
		actors = append(actors, secondEarthGhost(t, ops, actors[len(actors)-1], target))
	}
	for _, actor := range actors {
		ops.advanceInstance(monsterTestDivision, actor, []playerPose{target}, 10000)
	}
	_, slots, count := approachSnapshot(s, monsterTestDivision, a.Gid)
	if count != 9 {
		t.Fatal("membership capped", count)
	}
	occupied := 0
	seen := map[uint32]bool{}
	for _, owner := range slots {
		if owner != 0 {
			occupied++
			if seen[owner] {
				t.Fatal("duplicate reservation")
			}
			seen[owner] = true
		}
	}
	if occupied != 8 {
		t.Fatal("unexpected reservation count", occupied)
	}
	for _, actor := range actors {
		m, _ := s.Mover(monsterTestDivision, actor.Gid)
		if !m.InFlight(10000) {
			t.Fatal("full squad rejected attacker", actor.Gid)
		}
	}
}

/*
================
TestEarthGhostApproachGroupsDoNotCrossDivisions
================
*/
func TestEarthGhostApproachGroupsDoNotCrossDivisions(t *testing.T) {
	ops, a, target := earthGhostFixture(t)
	s := ops.Monsters
	original, _ := s.Mover(monsterTestDivision, a.Gid)
	s.mu.Lock()
	s.divs["other"] = &divisionMonsterState{instances: newMonsterStorage(map[uint32]monster.Instance{a.Gid: a}), movers: newMoverStorage(map[uint32]monster.MoverState{a.Gid: original})}
	s.divs["other"].syncApproachActor(a.Gid, original)
	s.mu.Unlock()
	target = earthGhostMovingTarget(target.Pose, 0)
	ops.advanceInstance(monsterTestDivision, a, []playerPose{target}, 10000)
	got, slots, count := approachSnapshot(s, "other", a.Gid)
	if got != target.Gid || count != 1 || slots != (monster.ApproachSlots{}) {
		t.Fatal("reservation crossed population", fmt.Sprint(slots))
	}
}
