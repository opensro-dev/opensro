package simulation

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

// Reference and nest values are loaded by the production catalog from the
// supplied v1.150 bundle plus the repository's existing population evidence.
// The player, clock, and open ground are deterministic test inputs, NOT a live trace.
func earthGhostFixture(t *testing.T) (*MonsterMoverOps, monster.Instance, playerPose) {
	t.Helper()
	var data struct {
		Ref   monster.MonsterRef
		Nest  monster.NestRow
		Skill MonsterAttackPlan
	}
	b, err := os.ReadFile("testdata/earth-ghost-v1150.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	if data.Ref.RefObjID != 1963 || data.Ref.RunSpeed != 75 || data.Skill.Reach != 7 || data.Nest.Controls.TraceData != 500 {
		t.Fatal("unexpected fixture authority")
	}
	ops, actor := monsterLegFixture(t, aggressiveTactics())
	ops.TacticsFor = nil
	ops.Monsters.clock = func() time.Time { return time.UnixMilli(10000) }
	actor.Ref = data.Ref
	actor.Nest = data.Nest
	actor.CurrentHP = data.Ref.MaxHP
	actor.Spawn = data.Nest.SpawnPoint
	m := monster.NewSpawnMover(actor, 10000)
	mustMoverTransition(&m, monster.MoverEventSpawnHoldElapsed, 0)
	m.BehaviorDeadlineMs = 100000
	s := ops.Monsters
	s.mu.Lock()
	state := s.populationForObject(monsterTestDivision, actor.Gid)
	state.instances.set(actor.Gid, actor)
	state.movers.set(actor.Gid, m)
	s.mu.Unlock()
	target := playerPose{Gid: PlayerObjectID(1), Pose: poseToSpawn(m.Pose), BodyRadius: 4}
	target.Pose.X += 40
	target.Pose = NormalizeSpawnFrame(target.Pose)
	if !s.ArmRetaliation(monsterTestDivision, actor.Gid, target.Gid) {
		t.Fatal("retaliation failed")
	}
	ops.AttackPlan = func(monster.Instance, uint32, AttackPick) (MonsterAttackPlan, bool) { return data.Skill, true }
	return ops, actor, target
}

func earthGhostMovingTarget(start Spawn, elapsed int64) playerPose {
	pose := start
	pose.X += 50 * float64(elapsed) / 1000
	pose = NormalizeSpawnFrame(pose)
	end := pose
	end.X += 1000
	end = NormalizeSpawnFrame(end)
	return playerPose{Gid: PlayerObjectID(1), Pose: pose, BodyRadius: 4, MovementIntent: playerMovementIntent{present: true, inFlight: true, destination: end}}
}

func TestEarthGhostMovingTargetAdmitsAnAttack(t *testing.T) {
	for _, step := range []int64{100, 150, 200} {
		t.Run(fmt.Sprintf("%dms", step), func(t *testing.T) {
			ops, actor, target := earthGhostFixture(t)
			start := target.Pose
			attacks := 0
			ops.BasicAttack = func(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
				attacks++
				return MonsterAttackResult{Accepted: true, TargetAlive: true}
			}
			for dt := int64(0); dt <= 6000; dt += step {
				target = earthGhostMovingTarget(start, dt)
				ops.advanceInstance(monsterTestDivision, actor, []playerPose{target}, 10000+dt)
			}
			m, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
			distance := WorldDistance2D(poseToSpawn(m.LivePoseAt(16000, nil)), target.Pose)
			t.Logf("accepted=%d final target distance=%.3f admission radius=14 mode=%s", attacks, distance, m.Mode())
			if attacks == 0 {
				t.Fatal("faster pursuer never entered attack admission")
			}
		})
	}
}

func TestEarthGhostCommittedAttackDoesNotRepath(t *testing.T) {
	ops, actor, target := earthGhostFixture(t)
	target.Pose.X = actor.Spawn.X + 10
	attacks := 0
	ops.BasicAttack = func(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
		attacks++
		return MonsterAttackResult{Accepted: true, TargetAlive: true}
	}
	ops.advanceInstance(monsterTestDivision, actor, []playerPose{target}, 10000)
	before, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if attacks != 1 || before.Mode() != monster.MoverAttacking {
		t.Fatal("attack was not admitted")
	}
	target.Pose.X += 20
	target.MovementIntent = playerMovementIntent{present: true, inFlight: true, destination: target.Pose}
	frames, _ := ops.advanceInstance(monsterTestDivision, actor, []playerPose{target}, 10100)
	after, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if after.Mode() != monster.MoverAttacking || after.InFlight(10100) || len(frames) != 0 {
		t.Fatalf("target movement took attack ownership: %s frames=%d", after.Mode(), len(frames))
	}
	ops.advanceInstance(monsterTestDivision, actor, []playerPose{target}, before.NextAttackMs)
	after, _ = ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if after.Mode() != monster.MoverChasing {
		t.Fatal("expired action failed to resume pursuit")
	}
}

func TestEarthGhostFarFromHomeAbandonsCloseTarget(t *testing.T) {
	ops, actor, target := earthGhostFixture(t)
	m, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	m.Pose.X += 1600
	m.Pose = normalizeMonsterPose(m.Pose)
	ops.Monsters.CommitMover(monsterTestDivision, actor.Gid, m)
	target.Pose = poseToSpawn(m.Pose)
	target.Pose.X += 10
	target.Pose = NormalizeSpawnFrame(target.Pose)
	frames, _ := ops.advanceInstance(monsterTestDivision, actor, []playerPose{target}, 10000)
	after, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if after.TargetGID() != 0 || after.Mode() != monster.MoverReturning {
		t.Fatalf("close target defeated home bound: %s target=%d", after.Mode(), after.TargetGID())
	}
	if len(frames) == 0 {
		t.Fatal("homing transition was not published")
	}
}

func TestEarthGhostHomeCheckDoesNotWaitForTraceTimer(t *testing.T) {
	ops, actor, target := earthGhostFixture(t)
	m, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	ops.advancePursuitControls(monsterTestDivision, actor, m, target, m.Pose, 10000)
	m.Pose.X += 1600
	m.Pose = normalizeMonsterPose(m.Pose)
	ops.Monsters.CommitMover(monsterTestDivision, actor.Gid, m)
	target.Pose = poseToSpawn(m.Pose)
	target.Pose.X += 10
	target.Pose = NormalizeSpawnFrame(target.Pose)
	ops.advanceInstance(monsterTestDivision, actor, []playerPose{target}, 10100)
	after, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if after.TargetGID() != 0 {
		t.Fatal("closed Timer 6 bypassed independent home bound")
	}
}

func TestEarthGhostTargetLossStillEndsAttackOwnership(t *testing.T) {
	ops, actor, target := earthGhostFixture(t)
	target.Pose.X = actor.Spawn.X + 10
	ops.BasicAttack = func(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
		return MonsterAttackResult{Accepted: true, TargetAlive: true}
	}
	ops.advanceInstance(monsterTestDivision, actor, []playerPose{target}, 10000)
	ops.advanceInstance(monsterTestDivision, actor, nil, 10100)
	after, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if after.TargetGID() != 0 || after.Mode() == monster.MoverAttacking {
		t.Fatal("invalid target retained attack ownership")
	}
}

func TestEarthGhostContinuousRetreatEventuallyDisengages(t *testing.T) {
	ops, actor, target := earthGhostFixture(t)
	start := target.Pose
	attacks := 0
	firstReturn := int64(-1)
	ops.BasicAttack = func(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
		attacks++
		return MonsterAttackResult{Accepted: true, TargetAlive: true}
	}
	for dt := int64(0); dt <= 50000; dt += 100 {
		target = earthGhostMovingTarget(start, dt)
		ops.advanceInstance(monsterTestDivision, actor, []playerPose{target}, 10000+dt)
		m, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
		if m.Mode() == monster.MoverReturning && m.TargetGID() == 0 && firstReturn < 0 {
			firstReturn = dt
		}
	}
	m, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if firstReturn < 0 || m.TargetGID() != 0 || attacks == 0 || m.Mode() != monster.MoverIdle || needsHoming(actor, m.LivePoseAt(60000, nil)) {
		t.Fatalf("retreat failed: attacks=%d firstReturn=%d mode=%s target=%d", attacks, firstReturn, m.Mode(), m.TargetGID())
	}
	t.Logf("accepted attacks=%d first return after %dms final mode=%s", attacks, firstReturn, m.Mode())
}

func secondEarthGhost(t *testing.T, ops *MonsterMoverOps, actor monster.Instance, target playerPose) monster.Instance {
	t.Helper()
	actor.Gid++
	m := monster.NewSpawnMover(actor, 10000)
	mustMoverTransition(&m, monster.MoverEventSpawnHoldElapsed, 0)
	m.BehaviorDeadlineMs = 100000
	s := ops.Monsters
	s.mu.Lock()
	state := s.divs[monsterTestDivision]
	state.instances.set(actor.Gid, actor)
	state.movers.set(actor.Gid, m)
	s.mu.Unlock()
	if !s.ArmRetaliation(monsterTestDivision, actor.Gid, target.Gid) {
		t.Fatal("second actor not admitted")
	}
	return actor
}

func TestEarthGhostTwoPursuersSeparateAndBothAttack(t *testing.T) {
	ops, a, target := earthGhostFixture(t)
	b := secondEarthGhost(t, ops, a, target)
	start := target.Pose
	attacks := map[uint32]int{}
	maxSeparation := 0.0
	sameGoal := false
	ops.BasicAttack = func(_ string, actor monster.Instance, _, _ uint32, _ int64) MonsterAttackResult {
		attacks[actor.Gid]++
		return MonsterAttackResult{Accepted: true, TargetAlive: true}
	}
	for dt := int64(0); dt <= 6000; dt += 100 {
		target = earthGhostMovingTarget(start, dt)
		for _, actor := range []monster.Instance{a, b} {
			ops.advanceInstance(monsterTestDivision, actor, []playerPose{target}, 10000+dt)
		}
		ma, _ := ops.Monsters.Mover(monsterTestDivision, a.Gid)
		mb, _ := ops.Monsters.Mover(monsterTestDivision, b.Gid)
		if dt == 0 {
			sameGoal = ma.MovementGoal() == mb.MovementGoal()
		}
		d := WorldDistance2D(poseToSpawn(ma.LivePoseAt(10000+dt, nil)), poseToSpawn(mb.LivePoseAt(10000+dt, nil)))
		if d > maxSeparation {
			maxSeparation = d
		}
	}
	t.Logf("accepted=%v maximum separation=%.3f same initial goal=%v", attacks, maxSeparation, sameGoal)
	if sameGoal || attacks[a.Gid] == 0 || attacks[b.Gid] == 0 || maxSeparation < 2 {
		t.Fatal("two-pursuer regression")
	}
}
