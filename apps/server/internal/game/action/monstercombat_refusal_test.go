package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// Exercise the actual action adapter, division transaction, simulation tick,
// pursuit and damage owner. Only geometry is the fixture's straight-line seam.
func TestMonsterMovingTargetRacePursuesAndEventuallyDealsDamage(t *testing.T) {
	rt, clock, c, original := newCombatTestRuntime(t, 100)
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent = 1, 1, 100
	skills[2] = skill
	ref := original.Ref
	ref.DefaultSkillIDs[0], ref.RunSpeed, ref.WalkSpeed, ref.ScaleDenom = 2, 22, 8, 100
	state := simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{ref.RefObjID: ref}, nil))
	state.SetTimeSource(clock.Now)
	state.SetRandomSource(func() float64 { return 0 })
	rt.Monsters = state
	actor, err := state.DevelopmentCreateLeader(testDivision, ref.RefObjID, monster.Pose{
		RegionID: original.Spawn.RegionID, X: original.Spawn.X, Y: original.Spawn.Y, Z: original.Spawn.Z,
	}, clock.NowMs()+100000)
	if err != nil {
		t.Fatal(err)
	}
	lease, ok := state.ObjectPopulation(testDivision, actor.Gid)
	if !ok {
		t.Fatal("missing population")
	}
	viewer := simulation.SessionSnapshot{SessionID: "range-race", DivisionID: testDivision, CharacterID: c.ID,
		Population: lease, WorldInstance: uint32(lease.ID), BodyRadius: 4, CombatEligible: true,
		World: simulation.SeedWorldState(c)}
	// AI snapshot is already detached when the character's position changes.
	*c.World.Spawn.X = original.Spawn.X + 100
	beforeHP := enterworld.CurrentHP(c)
	if !state.ArmRetaliation(testDivision, actor.Gid, enterworld.ObjectIDForCharacter(c)) {
		t.Fatal("no retaliation")
	}
	refusals := 0
	ops := &simulation.MonsterMoverOps{Monsters: state, Rand: func() float64 { return 0 }, AttackPlan: rt.MonsterAttackPlan,
		RunAction: func(division string, run func(simulation.MonsterAttackOperation)) {
			rt.RunMonsterAction(division, func(attack simulation.MonsterAttackOperation) {
				run(func(d string, m monster.Instance, target, skill uint32, now int64) simulation.MonsterAttackResult {
					result := attack(d, m, target, skill, now)
					if result.Refusal == simulation.MonsterAttackApproachRequired {
						refusals++
					}
					return result
				})
			})
		}}
	ops.RunMonsterLeg(clock.NowMs(), []simulation.SessionSnapshot{viewer}, &summonTickPusher{})
	mover, _ := state.Mover(testDivision, actor.Gid)
	if refusals != 1 || mover.TargetGID() != enterworld.ObjectIDForCharacter(c) || mover.Mode() != monster.MoverChasing ||
		mover.AttackSkillID != 2 || enterworld.CurrentHP(c) != beforeHP || rt.castTokenCounter != 0 {
		t.Fatalf("stale snapshot dropped aggro or emitted damage: refusal=%d mover=%+v", refusals, mover)
	}
	viewer.World = simulation.SeedWorldState(c)
	for i := 0; i < 100 && enterworld.CurrentHP(c) == beforeHP; i++ {
		clock.now = clock.now.Add(100 * time.Millisecond)
		ops.RunMonsterLeg(clock.NowMs(), []simulation.SessionSnapshot{viewer}, &summonTickPusher{})
	}
	mover, _ = state.Mover(testDivision, actor.Gid)
	if enterworld.CurrentHP(c) >= beforeHP || rt.castTokenCounter != 1 || mover.Mode() != monster.MoverAttacking ||
		mover.TargetGID() != enterworld.ObjectIDForCharacter(c) || refusals != 1 {
		t.Fatalf("monster failed to pursue and attack: hp=%d/%d tokens=%d refusal=%d mover=%+v", enterworld.CurrentHP(c), beforeHP, rt.castTokenCounter, refusals, mover)
	}
}

func TestMonsterActionRefusalClassifiesApproachWithoutWeakeningGuards(t *testing.T) {
	for _, cause := range []string{"range", "status", "dead", "unknown-skill", "missing-stats"} {
		t.Run(cause, func(t *testing.T) {
			rt, clock, c, m := newCombatTestRuntime(t, 100)
			m.Ref.DefaultSkillIDs[0] = 2
			skillID, want := uint32(2), simulation.MonsterAttackUnavailable
			switch cause {
			case "range":
				*c.World.Spawn.X = 1500
				want = simulation.MonsterAttackApproachRequired
			case "status":
				c.NativeBodyStatus = 7
				want = simulation.MonsterAttackCommandRejected
			case "dead":
				c.CurrentHP = testInt64(0)
			case "unknown-skill":
				skillID = 999
			case "missing-stats":
				m.Ref.CombatPinned = false
			}
			before := enterworld.CurrentHP(c)
			result := rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), skillID, clock.NowMs())
			if result.Accepted || result.Refusal != want || enterworld.CurrentHP(c) != before || len(result.Frames) != 0 || rt.castTokenCounter != 0 {
				t.Fatalf("wrong refusal or leaked damage/cast: %+v want=%v", result, want)
			}
		})
	}
}

func TestMonsterCastLongerThanSelectorIntervalKeepsTargetAndReleasesOnce(t *testing.T) {
	testMonsterCastCadence(t, false)
}

func TestMonsterLateTickSettlesAcceptedCastBeforeNextAICommand(t *testing.T) {
	testMonsterCastCadence(t, true)
}

func TestRejectedUniqueSummonAppliesNativeCompletionWithoutWave(t *testing.T) {
	rt, clock, character, _ := newCombatTestRuntime(t, 1000)
	skills := rt.deps.SkillData().(staticSkillSource)
	row := skills[2]
	row.Summon = monster.SummonSkill{Present: true, HPPercent: 80}
	skills[2] = row
	ref := monster.MonsterRef{RefObjID: 1, Codename: "MOB_CH_TIGERWOMAN", TidWord: 0xc6, MaxHP: 1000}
	ref.DefaultSkillIDs[0] = 2
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{1: ref},
		[]monster.NestRow{{SpawnPoint: monster.SpawnPoint{RefObjID: 1, RegionID: 0x62a8}}}))
	rt.Monsters.SetTimeSource(clock.Now)
	rt.Monsters.StartDivision(testDivision)
	rt.Monsters.AdvancePopulation(clock.NowMs())
	parent := rt.Monsters.InstancesInRegions(testDivision, []uint16{0x62a8})[0]
	hit, _ := rt.Monsters.ApplyDamage(testDivision, parent.Gid, 100)
	target := enterworld.ObjectIDForCharacter(character)
	if !rt.Monsters.ArmRetaliation(testDivision, parent.Gid, target) {
		t.Fatal("no retaliation")
	}
	character.NativeBodyStatus = 7
	result := rt.MonsterBasicAttack(testDivision, hit.Instance, target, 2, clock.NowMs())
	live, _ := rt.Monsters.Get(testDivision, parent.Gid)
	mover, _ := rt.Monsters.Mover(testDivision, parent.Gid)
	if result.Accepted || result.Refusal != simulation.MonsterAttackCommandRejected || len(result.Frames) != 0 ||
		live.DamageSinceSummon != 0 || live.LastSummonCommandMs != uint32(clock.NowMs()) ||
		mover.TargetGID() != target || len(rt.Monsters.MaterializedInstances(testDivision)) != 1 || rt.castTokenCounter != 0 {
		t.Fatal("unique refusal lost completion, target, or spawned a wave", result, live, mover)
	}
}

func testMonsterCastCadence(t *testing.T, delayed bool) {
	t.Helper()
	rt, clock, c, original := newCombatTestRuntime(t, 100)
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent = 1, 1, 100
	skill.ActionKind, skill.ActionCastingTimeMs, skill.ActionDurationMs, skill.CoolTimeMs = 2, 2000, 300, 100
	skills[2] = skill
	ref := original.Ref
	ref.DefaultSkillIDs[0], ref.RunSpeed, ref.WalkSpeed, ref.ScaleDenom = 2, 22, 8, 100
	state := simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{ref.RefObjID: ref}, nil))
	state.SetTimeSource(clock.Now)
	state.SetRandomSource(func() float64 { return 0 })
	rt.Monsters = state
	actor, err := state.DevelopmentCreateLeader(testDivision, ref.RefObjID, monster.Pose{
		RegionID: original.Spawn.RegionID, X: original.Spawn.X, Y: original.Spawn.Y, Z: original.Spawn.Z,
	}, clock.NowMs()+100000)
	if err != nil {
		t.Fatal(err)
	}
	lease, _ := state.ObjectPopulation(testDivision, actor.Gid)
	viewer := simulation.SessionSnapshot{SessionID: "pending-cast", DivisionID: testDivision, CharacterID: c.ID,
		Population: lease, WorldInstance: uint32(lease.ID), BodyRadius: 4, CombatEligible: true,
		World: simulation.SeedWorldState(c)}
	target := enterworld.ObjectIDForCharacter(c)
	if !state.ArmRetaliation(testDivision, actor.Gid, target) {
		t.Fatal("no retaliation")
	}
	plans := 0
	ops := &simulation.MonsterMoverOps{Monsters: state, Rand: func() float64 { return 0 },
		AttackPlan: func(i monster.Instance, requested uint32, pick simulation.AttackPick) (simulation.MonsterAttackPlan, bool) {
			plans++
			return rt.MonsterAttackPlan(i, requested, pick)
		}, RunAction: rt.RunMonsterAction}
	tick := func() { ops.RunMonsterLeg(clock.NowMs(), []simulation.SessionSnapshot{viewer}, &summonTickPusher{}) }
	start, hp := clock.Now(), enterworld.CurrentHP(c)
	tick()
	if rt.castTokenCounter != 1 || len(rt.pendingMonsterCasts) != 1 || enterworld.CurrentHP(c) != hp {
		t.Fatal("cast did not prepare without damage", rt.castTokenCounter, len(rt.pendingMonsterCasts))
	}
	if delayed {
		// Skip beyond BOTH action deadlines, reproducing the real coordinator
		// ordering bug. Production installs this same hook before division AI.
		clock.now = start.Add(5 * time.Second)
		frames := rt.MonsterActionTickHook()(clock.NowMs())
		if len(frames) == 0 || enterworld.CurrentHP(c) >= hp || len(rt.pendingMonsterCasts) != 0 {
			t.Fatal("pre-AI phase failed to settle expired cast")
		}
		tick()
		mover, _ := state.Mover(testDivision, actor.Gid)
		if plans != 2 || rt.castTokenCounter != 2 || len(rt.pendingMonsterCasts) != 1 || mover.TargetGID() != target || mover.Mode() != monster.MoverAttacking {
			t.Fatal("late tick collided with old cast or dropped target", plans, rt.castTokenCounter, mover)
		}
		after := enterworld.CurrentHP(c)
		rt.TickHook()(clock.NowMs())
		if enterworld.CurrentHP(c) != after || len(rt.pendingMonsterCasts) != 1 {
			t.Fatal("post-AI phase repeated or consumed new cast")
		}
		return
	}
	for _, elapsed := range []int64{100, 500, 1000, 1999, 2000} {
		clock.now = start.Add(time.Duration(elapsed) * time.Millisecond)
		rt.advanceMonsterCasts(clock.NowMs())
		tick()
		mover, _ := state.Mover(testDivision, actor.Gid)
		if plans != 1 || rt.castTokenCounter != 1 || len(rt.pendingMonsterCasts) != 1 ||
			mover.TargetGID() != target || mover.Mode() != monster.MoverAttacking || enterworld.CurrentHP(c) != hp {
			t.Fatalf("elapsed=%d overlapped cast/dropped target: plans=%d pending=%d mover=%+v", elapsed, plans, len(rt.pendingMonsterCasts), mover)
		}
	}
	clock.now = start.Add(2001 * time.Millisecond)
	rt.advanceMonsterCasts(clock.NowMs())
	after := enterworld.CurrentHP(c)
	if after >= hp || len(rt.pendingMonsterCasts) != 0 {
		t.Fatal("owned cast failed to release", after, hp)
	}
	rt.advanceMonsterCasts(clock.NowMs())
	if enterworld.CurrentHP(c) != after {
		t.Fatal("cast released twice")
	}
	clock.now = start.Add(2299 * time.Millisecond)
	tick()
	if plans != 1 {
		t.Fatal("reselected during Timer-10 recovery")
	}
	clock.now = start.Add(2300 * time.Millisecond)
	tick()
	mover, _ := state.Mover(testDivision, actor.Gid)
	if plans != 2 || rt.castTokenCounter != 2 || len(rt.pendingMonsterCasts) != 1 || mover.TargetGID() != target {
		t.Fatalf("did not resume attacks at native duration boundary: plans=%d tokens=%d mover=%+v", plans, rt.castTokenCounter, mover)
	}
}
