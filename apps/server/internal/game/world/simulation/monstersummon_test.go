package simulation

import (
	"encoding/binary"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func summonFixture(t *testing.T) (*MonsterState, monster.Instance, monster.SummonSkill, map[uint32]float64, *int64) {
	t.Helper()
	now := int64(1000)
	refs := map[uint32]monster.MonsterRef{1: {TidWord: 0x00C6, RefObjID: 1, Codename: "MOB_CH_TIGERWOMAN", MaxHP: 1000, MonsterType: 3, BodyRadius: 10, WalkSpeed: 8, RunSpeed: 20}, 2: {TidWord: 0x00C6, RefObjID: 2, Codename: "MOB_CH_MANGNYANG", MaxHP: 50, BodyRadius: 2, WalkSpeed: 8, RunSpeed: 20}}
	s := NewMonsterState(monster.TemplateFromParts(refs, []monster.NestRow{{SpawnPoint: monster.SpawnPoint{RefObjID: 1, RegionID: 0x62aa, X: 1910, Y: 20, Z: 100}}}))
	s.SetTimeSource(func() time.Time { return time.UnixMilli(now) })
	s.SetRandomSource(func() float64 { return 0 })
	s.StartDivision("summon")
	s.AdvancePopulation(s.CurrentTimeMillis())
	s.StartDivision("summon")
	s.AdvancePopulation(now)
	i := s.InstancesInRegions("summon", []uint16{0x62aa})[0]
	hit, _ := s.ApplyDamage("summon", i.Gid, 100)
	wave := monster.SummonSkill{Present: true, HPPercent: 80, Entries: [9]monster.SummonEntry{{RefObjID: 2, Grade: 6, Minimum: 2, Maximum: 2}}}
	return s, hit.Instance, wave, map[uint32]float64{1: 100, 2: 10}, &now
}

func TestSummonTransactionNoDuplicateAndNoIndependentRespawn(t *testing.T) {
	s, parent, wave, ranges, now := summonFixture(t)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := s.CommitSummon("summon", parent, wave, *now, *now, ranges); ok {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("accepted waves = %d", accepted.Load())
	}
	all := s.MaterializedInstances("summon")
	if len(all) != 3 {
		t.Fatalf("population=%d", len(all))
	}
	for _, child := range all {
		if child.Gid == parent.Gid {
			continue
		}
		if child.SummonerGID != parent.Gid || child.CurrentHP != 200 || child.Rarity() != 6 || child.Spawn.RegionID == parent.Spawn.RegionID {
			t.Fatalf("grade, HP or sector normalization: %+v", child)
		}
		if !s.Defeat("summon", child.Gid, time.UnixMilli(*now)) {
			t.Fatal("summoned child cannot retire")
		}
	}
	*now += 100000
	s.StartDivision("summon")
	s.AdvancePopulation(s.CurrentTimeMillis())
	if got := len(s.InstancesInRegions("summon", []uint16{0x62aa, 0x62ab})); got != 1 {
		t.Fatalf("summons respawned: %d", got)
	}
	if _, ok := s.CommitSummon("summon", parent, wave, *now, *now, ranges); ok {
		t.Fatal("stale damage snapshot reused")
	}
}

func TestSummonCommandResetPreservesDamageDuringCastingAndAfterDeadline(t *testing.T) {
	s, parent, wave, ranges, now := summonFixture(t)
	if _, ok := s.CommitSummon("summon", parent, wave, *now, *now+1000, ranges); !ok {
		t.Fatal("refused")
	}
	started, _ := s.Get("summon", parent.Gid)
	if started.DamageSinceSummon != 0 || started.LastSummonCommandMs != uint32(*now) {
		t.Fatal("selector completion was deferred until action recovery", started)
	}
	*now += 500
	s.ApplyDamage("summon", parent.Gid, 100)
	i, _ := s.Get("summon", parent.Gid)
	if monster.SummonDue(i) {
		t.Fatal("summoned during active action")
	}
	*now += 600
	s.ApplyDamageBatch("summon", []MonsterDamagePlan{{GID: parent.Gid, ExpectedHP: 800, Damage: 20}})
	i, _ = s.Get("summon", parent.Gid)
	if i.DamageSinceSummon != 120 || i.SummonActionUntilMs != 0 {
		t.Fatalf("deadline erased later hit: %+v", i)
	}
}

func TestRejectedSummonCompletesSelectorWithoutCreatingWaveOrDroppingTarget(t *testing.T) {
	s, parent, _, _, now := summonFixture(t)
	target := PlayerObjectID(7)
	if !s.ArmRetaliation("summon", parent.Gid, target) {
		t.Fatal("no retaliation")
	}
	before, _ := s.Mover("summon", parent.Gid)
	s.RejectSummonCommand("summon", parent.Gid, *now)
	after, _ := s.Mover("summon", parent.Gid)
	live, _ := s.Get("summon", parent.Gid)
	if before != after || live.DamageSinceSummon != 0 || live.LastSummonCommandMs != uint32(*now) ||
		live.SummonActionUntilMs != 0 || len(s.MaterializedInstances("summon")) != 1 {
		t.Fatal("command error changed movement, deferred completion, or created a wave", live)
	}
	*now += 10
	s.ApplyDamage("summon", parent.Gid, 20)
	live, _ = s.Get("summon", parent.Gid)
	if live.DamageSinceSummon != 20 {
		t.Fatal("refusal erased a later hit")
	}
}

func TestSummonInterruptedAfterCommandCompletionPreservesNewDamage(t *testing.T) {
	s, parent, wave, ranges, now := summonFixture(t)
	if _, ok := s.BeginSummon("summon", parent, wave, *now, *now+100, *now+1000, ranges); !ok {
		t.Fatal("refused")
	}
	*now += 50
	mover, _ := s.Mover("summon", parent.Gid)
	hits := s.ApplyDamageSequence("summon", parent.Gid, parent.CurrentHP, []MonsterDamagePlan{{GID: parent.Gid, Damage: 20,
		Knockback: &MonsterKnockdownPlan{Pose: mover.Pose, UntilMs: *now + 500}}})
	if len(hits) != 1 {
		t.Fatal("impact refused")
	}
	s.AdvanceSummons(*now + 1000)
	live, _ := s.Get("summon", parent.Gid)
	if live.DamageSinceSummon != 20 || live.SummonActionUntilMs != 0 || len(s.MaterializedInstances("summon")) != 1 {
		t.Fatal("cancelled cast spawned or erased its interrupting hit", live)
	}
}

func TestSummonRefusalPlacementFailureZeroCountsAndNativeCap(t *testing.T) {
	s, parent, wave, ranges, now := summonFixture(t)
	bad := wave
	bad.Entries[1] = monster.SummonEntry{RefObjID: 999, Minimum: 1, Maximum: 1}
	if _, ok := s.CommitSummon("summon", parent, bad, *now, *now, ranges); ok {
		t.Fatal("missing reference accepted")
	}
	if len(s.MaterializedInstances("summon")) != 1 {
		t.Fatal("partial malformed wave escaped")
	}
	s.SetSpawnGroundResolver(func(uint16, float64, float64, float64) (float64, bool) { return 0, false })
	created, ok := s.CommitSummon("summon", parent, wave, *now, *now, ranges)
	if !ok || len(created) != 0 {
		t.Fatal("terrain rejection produced entities")
	}
	s, parent, wave, ranges, now = summonFixture(t)
	wave.Entries[0].Minimum = 100
	wave.Entries[0].Maximum = 100
	created, ok = s.CommitSummon("summon", parent, wave, *now, *now, ranges)
	if !ok || len(created) != 50 {
		t.Fatalf("native per-tuple cap: %d/%v", len(created), ok)
	}
	s, parent, wave, ranges, now = summonFixture(t)
	wave.Entries[0].Minimum = 0
	wave.Entries[0].Maximum = 0
	created, ok = s.CommitSummon("summon", parent, wave, *now, *now, ranges)
	if !ok || len(created) != 0 {
		t.Fatal("authored zero count did not remain empty")
	}
}

func TestSummonCastingReservationAndCancellation(t *testing.T) {
	for _, dead := range []bool{false, true} {
		s, parent, wave, ranges, now := summonFixture(t)
		if _, ok := s.BeginSummon("summon", parent, wave, *now, *now+100, *now+1000, ranges); !ok {
			t.Fatal("reservation refused")
		}
		s.AdvanceSummons(*now + 99)
		if len(s.MaterializedInstances("summon")) != 1 {
			t.Fatal("early wave")
		}
		if dead {
			s.ApplyDamage("summon", parent.Gid, parent.CurrentHP)
		}
		s.AdvanceSummons(*now + 100)
		want := 3
		if dead {
			want = 1
		}
		if len(s.MaterializedInstances("summon")) != want {
			t.Fatalf("dead=%v wrong release population", dead)
		}
		s.AdvanceSummons(*now + 101)
		if len(s.MaterializedInstances("summon")) != want {
			t.Fatal("wave repeated")
		}
	}
	s, parent, wave, ranges, _ := summonFixture(t)
	if _, ok := s.CommitSummon("summon", parent, wave, 0, 0, ranges); !ok {
		t.Fatal("instant cast refused")
	}
	parent, _ = s.Get("summon", parent.Gid)
	if parent.DamageSinceSummon != 0 {
		t.Fatal("zero-time instant action did not consume accumulator")
	}
}

func TestSummonChildAcquisitionAndLeaderLoss(t *testing.T) {
	s, parent, wave, ranges, now := summonFixture(t)
	children, ok := s.CommitSummon("summon", parent, wave, *now, *now, ranges)
	if !ok {
		t.Fatal("wave refused")
	}
	child := children[0]
	ops := &MonsterMoverOps{Monsters: s, Rand: func() float64 { return 0 }, TacticsFor: fixedTactics(passiveTactics())}
	leaderTarget := PlayerObjectID(1)
	if !s.ArmRetaliation("summon", parent.Gid, leaderTarget) {
		t.Fatal("leader target refused")
	}
	mover, _ := s.Mover("summon", child.Gid)
	local := playerPose{Gid: PlayerObjectID(2), Pose: poseToSpawn(mover.Pose)}
	distant := playerPose{Gid: leaderTarget, Pose: Spawn{RegionID: parent.Spawn.RegionID, X: 1000, Z: 100}}
	target, found := ops.summonedTarget("summon", child, mover, []playerPose{distant, local}, *now)
	if !found || target.Gid != local.Gid {
		t.Fatal("local target must precede inherited enemy")
	}
	target, found = ops.summonedTarget("summon", child, mover, []playerPose{distant}, *now)
	if !found || target.Gid != leaderTarget {
		t.Fatal("summon did not inherit leader enemy")
	}
	leader, _ := s.Mover("summon", parent.Gid)
	leader.Pose.X = 900
	s.CommitMover("summon", parent.Gid, leader)
	if frames, following := ops.followSummoner("summon", child, mover, *now); !following || len(frames) != 0 {
		t.Fatal("distant leader did not enter FOLLOW without premature movement")
	}
	s.ApplyDamage("summon", parent.Gid, parent.CurrentHP)
	if _, found := ops.summonedTarget("summon", child, mover, []playerPose{distant}, *now); found {
		t.Fatal("dead leader supplied a target")
	}
	if frames, _ := ops.followSummoner("summon", child, mover, *now); len(frames) != 0 {
		t.Fatal("stale idle snapshot published follow movement after leader death")
	}
	if _, exists := s.Get("summon", child.Gid); !exists {
		t.Fatal("parent death invented a child despawn")
	}
}

func TestSummonRecoveryRetainsMovementOwnership(t *testing.T) {
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	instance.SummonActionUntilMs = 2000
	if frames, targeted := ops.advanceInstance(monsterTestDivision, instance, []playerPose{{Gid: PlayerObjectID(1), Pose: Spawn{RegionID: monsterTestRegion, X: 1050, Z: 1000}}}, 1999); len(frames) != 0 || targeted != nil {
		t.Fatal("cast recovery allowed movement/combat")
	}
}

func TestSummonVisibilityCreatesBeforeChildMovement(t *testing.T) {
	s, parent, wave, ranges, now := summonFixture(t)
	ops := &MonsterMoverOps{Monsters: s, Rand: func() float64 { return .5 }, TacticsFor: fixedTactics(passiveTactics())}
	// The summoner stands at x=1910 beside the east seam; this viewer's
	// interest (blocks 4..6 by -1..1) covers it and the seam neighbour where
	// children may land.
	session := SessionSnapshot{SessionID: "viewer", DivisionID: "summon", CharacterID: 1, WorldInstance: 0x10001, Population: instance.Lease{ID: instance.Pack(1, 1), Generation: 1}, World: WorldState{Spawn: Spawn{RegionID: parent.Spawn.RegionID, X: 1750, Z: 100}, SpawnSet: true}, BodyRadius: 4}
	push := &fakePusher{}
	ops.RunMonsterLeg(*now, []SessionSnapshot{session}, push)
	children, ok := s.CommitSummon("summon", parent, wave, *now, *now, ranges)
	if !ok {
		t.Fatal("wave refused")
	}
	push = &fakePusher{}
	*now += 100
	ops.RunMonsterLeg(*now, []SessionSnapshot{session}, push)
	childIDs := map[uint32]bool{}
	for _, child := range children {
		childIDs[child.Gid] = true
	}
	creates := 0
	for _, frame := range sessionFrames(push, "viewer") {
		if frame.Opcode == wire.OpSingleObjectSpawn {
			creates++
			continue
		}
		if (frame.Opcode == OpMovementAck || frame.Opcode == wire.OpObjectStateRefresh) && len(frame.Payload) >= 4 && childIDs[binary.LittleEndian.Uint32(frame.Payload)] && creates != len(children) {
			t.Fatal("child acted before reference/create admission")
		}
	}
	if creates != len(children) {
		t.Fatalf("created %d/%d visible children", creates, len(children))
	}
}

func TestDelayedSummonReleasePreservesPostRecoveryDamage(t *testing.T) {
	s, parent, wave, ranges, now := summonFixture(t)
	if _, ok := s.BeginSummon("summon", parent, wave, *now, *now+100, *now+200, ranges); !ok {
		t.Fatal("refused")
	}
	*now += 300
	s.ApplyDamage("summon", parent.Gid, 10)
	s.AdvanceSummons(*now)
	parent, _ = s.Get("summon", parent.Gid)
	if parent.DamageSinceSummon != 10 {
		t.Fatal("late simulation release erased newer damage")
	}
}

func TestSummonUnresolvedLocalSightStillInheritsLeader(t *testing.T) {
	s, parent, wave, ranges, now := summonFixture(t)
	children, _ := s.CommitSummon("summon", parent, wave, *now, *now, ranges)
	child := children[0]
	child.SummonSightRange = 0
	ops := &MonsterMoverOps{Monsters: s}
	mover, _ := s.Mover("summon", child.Gid)
	target := playerPose{Gid: PlayerObjectID(1), Pose: poseToSpawn(mover.Pose)}
	if _, ok := ops.summonedTarget("summon", child, mover, []playerPose{target}, *now); ok {
		t.Fatal("unknown tactics invented local hostility")
	}
	s.ArmRetaliation("summon", parent.Gid, target.Gid)
	if got, ok := ops.summonedTarget("summon", child, mover, []playerPose{target}, *now); !ok || got.Gid != target.Gid {
		t.Fatal("missing local metadata disabled leader assistance")
	}
}

func TestSummonInheritsTwoRememberedOpponents(t *testing.T) {
	s, parent, wave, ranges, now := summonFixture(t)
	children, _ := s.CommitSummon("summon", parent, wave, *now, *now, ranges)
	child := children[0]
	child.SummonSightRange = 0
	mover, _ := s.Mover("summon", child.Gid)
	first := playerPose{Gid: PlayerObjectID(1), Pose: poseToSpawn(mover.Pose)}
	second := playerPose{Gid: PlayerObjectID(2), Pose: first.Pose}
	third := playerPose{Gid: PlayerObjectID(3), Pose: first.Pose}
	first.Pose.X += 40
	second.Pose.X += 20
	for _, gid := range []uint32{first.Gid, second.Gid, first.Gid, third.Gid} {
		if !s.ArmRetaliation("summon", parent.Gid, gid) {
			t.Fatal("retaliation refused")
		}
	}
	parent, _ = s.Get("summon", parent.Gid)
	if parent.RememberedOpponents() != [2]uint32{first.Gid, second.Gid} {
		t.Fatalf("history evicted: %v", parent.RememberedOpponents())
	}
	ops := &MonsterMoverOps{Monsters: s}
	check := func(players []playerPose, want uint32) {
		t.Helper()
		got, found := ops.summonedTarget("summon", child, mover, players, *now)
		if found != (want != 0) || (found && got.Gid != want) {
			t.Fatalf("target=%d/%v want=%d", got.Gid, found, want)
		}
	}
	check([]playerPose{first, second, third}, second.Gid)
	// Native distance includes height; a horizontally nearer enemy can lose.
	second.Pose.Y += 100
	check([]playerPose{first, second, third}, first.Gid)
	second.Pose = first.Pose
	check([]playerPose{second, first, third}, first.Gid)
	// A removed/dead player is absent from the live player snapshot.
	check([]playerPose{second, third}, second.Gid)
	check([]playerPose{first, third}, first.Gid)
	check([]playerPose{third}, 0)
	stale, _ := s.Mover("summon", parent.Gid)
	s.ArmRetaliation("summon", parent.Gid, first.Gid)
	mustMoverTransition(&stale, monster.MoverEventTargetLost, 0)
	s.CommitMover("summon", parent.Gid, stale)
	check([]playerPose{first, second}, first.Gid)
	current, _ := s.Mover("summon", parent.Gid)
	mustMoverTransition(&current, monster.MoverEventTargetLost, 0)
	s.CommitMover("summon", parent.Gid, current)
	check([]playerPose{first, second}, 0)
}
