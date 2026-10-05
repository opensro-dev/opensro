package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// Shipped child references/default skills and recovered tactics, production
// factory and public tick path. Geometry and damage delivery are test seams:
// this validates decisions, not live combat or native per-reference parity.
func testShippedSummonedChildDecisions(t *testing.T, rt *Runtime, refs map[uint32]monster.MonsterRef, leaderRef monster.MonsterRef, authored monster.Instance) {
	t.Helper()
	s := simulation.NewMonsterState(monster.TemplateFromParts(refs, nil))
	s.SetTimeSource(func() time.Time { return time.UnixMilli(1000) })
	s.SetRandomSource(func() float64 { return 0 })
	parent, err := s.DevelopmentCreateLeader("child", leaderRef.RefObjID, monster.Pose{RegionID: 0x62aa, X: 960, Z: 960}, 1000000)
	if err != nil {
		t.Fatal(err)
	}
	hit, _ := s.ApplyDamage("child", parent.Gid, parent.EffectiveMaxHP()/10+1)
	wave := monster.SummonSkill{Present: true, Entries: [9]monster.SummonEntry{{RefObjID: authored.Ref.RefObjID, Grade: authored.Rarity(), Minimum: 1, Maximum: 1}}}
	children, ok := s.CommitSummon("child", hit.Instance, wave, 1000, 1000, map[uint32]float64{leaderRef.RefObjID: 100})
	if !ok || len(children) != 1 {
		t.Fatal("isolated authored child was not created")
	}
	child := children[0]
	selected, known := monster.ResolveSummonTactics(child.Ref, child.Rarity(), func() float64 { return 0 })
	if !known || !child.NestDetached || selected.HasControls && (!child.Nest.HasControls || child.Nest.Controls != selected.Controls || child.Nest.ConditionalSkills != selected.ConditionalSkills) {
		t.Fatal("child factory lost complete tactics/bindings or attached a synthetic CNest")
	}
	lease, ok := s.ObjectPopulation("child", child.Gid)
	if !ok {
		t.Fatal("child population absent")
	}
	viewer := simulation.SessionSnapshot{SessionID: "viewer", DivisionID: "child", CharacterID: 1, Population: lease, WorldInstance: uint32(lease.ID), BodyRadius: 4,
		World: simulation.WorldState{Spawn: simulation.Spawn{RegionID: child.Spawn.RegionID, X: child.Spawn.X, Y: child.Spawn.Y, Z: child.Spawn.Z}, SpawnSet: true}}
	attacks := 0
	wantedTarget := simulation.PlayerObjectID(1)
	ops := &simulation.MonsterMoverOps{Monsters: s, Rand: func() float64 { return 0 }, AttackPlan: rt.MonsterAttackPlan}
	ops.BasicAttack = func(_ string, actor monster.Instance, target, skill uint32, _ int64) simulation.MonsterAttackResult {
		if actor.Gid == child.Gid {
			plan, known := rt.MonsterAttackPlan(actor, skill, simulation.AttackPick{})
			if !known || plan.Summon || plan.SkillID != skill || target != wantedTarget {
				t.Fatal("child selected invalid ordinary attack")
			}
			attacks++
		}
		return simulation.MonsterAttackResult{Accepted: true, TargetAlive: true}
	}
	tick := func(now int64) { ops.RunMonsterLeg(now, []simulation.SessionSnapshot{viewer}, &summonTickPusher{}) }
	tick(1001)
	m, ok := s.Mover("child", child.Gid)
	if !ok || m.TargetGID() != 0 || attacks != 0 {
		t.Fatal("ineligible observer caused acquisition")
	}
	viewer.CombatEligible = true
	for now := int64(1101); now <= 2101 && attacks == 0; now += 100 {
		tick(now)
	}
	if attacks == 0 {
		t.Fatalf("known child never acquired/attacked eligible local target: %+v", m)
	}
	viewer.CombatEligible = false
	for now := int64(5000); now <= 15000; now += 1000 {
		tick(now)
	}
	m, _ = s.Mover("child", child.Gid)
	if m.TargetGID() != 0 {
		t.Fatal("removed combat candidate remained targeted")
	}
	// A remembered leader opponent lies beyond local acquisition sight.
	viewer.CombatEligible = true
	viewer.World.Spawn.X = m.Pose.X + child.SummonSightRange + 200
	viewer.World.Spawn.Z = m.Pose.Z
	viewer.World.Spawn.RegionID = m.Pose.RegionID
	if !s.ArmRetaliation("child", parent.Gid, wantedTarget) {
		t.Fatal("leader opponent refused")
	}
	// A graded child runs at its grade's speed (gradescale.go) and may reach
	// the opponent within the window: pursuit is judged when it acquires.
	pursuing := false
	for now := int64(15101); now <= 17101; now += 100 {
		tick(now)
		m, _ = s.Mover("child", child.Gid)
		if m.TargetGID() == wantedTarget {
			pursuing = m.InFlight(now)
			break
		}
	}
	if m.TargetGID() != wantedTarget || !pursuing {
		t.Fatalf("child did not assist/pursue leader opponent: mode=%v target=%d", m.Mode(), m.TargetGID())
	}
	// Personal retaliation must beat a competing leader opponent.
	wantedTarget = simulation.PlayerObjectID(2)
	attacker := viewer
	attacker.CharacterID = 2
	attacker.SessionID = "attacker"
	live := m.LivePoseAt(17201, nil)
	attacker.World.Spawn = simulation.Spawn{RegionID: live.RegionID, X: live.X, Y: live.Y, Z: live.Z}
	beforeAttacks := attacks
	if !s.ArmRetaliation("child", child.Gid, wantedTarget) {
		t.Fatal("personal retaliation refused")
	}
	for now := int64(17201); now <= 20201; now += 100 {
		ops.RunMonsterLeg(now, []simulation.SessionSnapshot{viewer, attacker}, &summonTickPusher{})
		if attacks > beforeAttacks {
			break
		}
	}
	m, _ = s.Mover("child", child.Gid)
	if m.TargetGID() != wantedTarget || attacks == beforeAttacks {
		t.Fatal("personal retaliation lost to competing leader target")
	}
	// Remove both candidates, then move the leader beyond the authored
	// controller follow threshold. Entry and movement must both be reached.
	viewer.CombatEligible = false
	for now := int64(25000); now <= 30000; now += 1000 {
		tick(now)
	}
	// Let the parent's prior chase/home transaction settle before applying
	// a stationary leader stimulus. Clearing a RETURNING segment by hand
	// would create an illegal fixture state rather than test production AI.
	readyAt := int64(30000)
	leader, _ := s.Mover("child", parent.Gid)
	for leader.Mode() != monster.MoverIdle && leader.Mode() != monster.MoverWandering && readyAt < 150000 {
		readyAt += 1000
		tick(readyAt)
		leader, _ = s.Mover("child", parent.Gid)
	}
	if leader.Mode() != monster.MoverIdle && leader.Mode() != monster.MoverWandering {
		t.Fatalf("leader never settled: %v", leader.Mode())
	}
	ops.DevelopmentStopLeader("child", parent.Gid, readyAt, readyAt+100000)
	leader, _ = s.Mover("child", parent.Gid)
	m, _ = s.Mover("child", child.Gid)
	leader.Pose = m.LivePoseAt(readyAt, nil)
	leader.Pose.X += child.SummonerFollowRange + 400
	if !s.CommitMover("child", parent.Gid, leader) {
		t.Fatal("leader stimulus rejected")
	}
	followed, moved := false, false
	for now := readyAt + 101; now <= readyAt+10100; now += 100 {
		tick(now)
		m, _ = s.Mover("child", child.Gid)
		followed = followed || m.Mode() == monster.MoverFollowing
		moved = moved || (m.Mode() == monster.MoverFollowing && m.InFlight(now))
		if moved {
			break
		}
	}
	if !followed || !moved {
		t.Fatalf("child FOLLOW not reached: %v/%v mode=%v", followed, moved, m.Mode())
	}
	s.DevelopmentRemoveFamily("child", parent.Gid)
	if rows := s.DevelopmentFollowSnapshot("child", parent.Gid, 36000); len(rows) != 0 {
		t.Fatal("authored child family cleanup failed")
	}
}
