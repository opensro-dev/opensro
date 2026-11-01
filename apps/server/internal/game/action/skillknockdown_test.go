package action

import (
	"encoding/binary"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
)

func TestKnockdownReleaseOwnsDamageDisplacementAndRecovery(t *testing.T) {
	for _, branch := range []string{"proc", "immune", "already-down", "fatal", "cancel"} {
		t.Run(branch, func(t *testing.T) {
			rt, clock, c, target := newCombatTestRuntime(t, 100000)
			target.Ref.Knockdown = 1
			target.Ref.KORecoverMs = 1000
			if branch == "immune" {
				target.Ref.Knockdown = 0
			}
			if branch == "fatal" {
				target.Ref.MaxHP = 1
			}
			// The spawned copy carries its rolled grade; a template nest carries none.
			nest := target.Nest
			nest.HasRarityOverride, nest.RarityOverride = false, 0
			rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{target.Ref.RefObjID: target.Ref}, []monster.NestRow{nest}))
			rt.Monsters.SetTimeSource(clock.Now)
			rt.Monsters.StartDivision(testDivision)
			rt.Monsters.AdvancePopulation(rt.Monsters.CurrentTimeMillis())
			target = rt.Monsters.InstancesInRegions(testDivision, []uint16{target.Spawn.RegionID})[0]
			skill := shippedOffense(t, "SKILL_CH_SWORD_KNOCKDOWN_A_01")
			if !skill.DirectOffensePinned || !skill.Knockdown.Present {
				t.Fatalf("missing complete authored program: %+v", skill)
			}
			rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
			c.Skills = append(c.Skills, skill.ID)
			c.CurrentMP = testInt64(100)
			rt.CombatRoll = func() (uint32, error) { return 0, nil }
			now := clock.NowMs()
			if branch == "already-down" {
				mover, _ := rt.Monsters.Mover(testDivision, target.Gid)
				rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, target.CurrentHP, []simulation.MonsterDamagePlan{{GID: target.Gid, Knockdown: &simulation.MonsterKnockdownPlan{Pose: mover.Pose, UntilMs: now + 10000}}})
			}
			cast := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}
			start, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), cast, now, rt.handlerDoors())
			if decision != skillCastAccepted || len(start.Frames) != 1 || len(start.Frames[0].Payload) != 19 {
				t.Fatalf("preparation: %+v", start)
			}
			token := binary.LittleEndian.Uint32(start.Frames[0].Payload[10:])
			if enterworld.CurrentMP(c) != 100 {
				t.Fatal("premature cost")
			}
			if branch == "cancel" {
				rt.cancelPreparingProjectile(testDivision, c.Name)
				if len(rt.advanceProjectileCasts(now+10000)) != 0 {
					t.Fatal("cancelled action released")
				}
				after, _ := rt.Monsters.Get(testDivision, target.Gid)
				if after.CurrentHP != target.CurrentHP || after.Motion.StateAt(now) != 0 || enterworld.CurrentMP(c) != 100 {
					t.Fatal("cancel changed authority")
				}
				return
			}
			at := now + int64(skill.ActionCastingTimeMs) + 1
			if len(rt.advanceProjectileCasts(at-1)) != 0 {
				t.Fatal("early release")
			}
			frames := rt.advanceProjectileCasts(at)
			if len(frames) == 0 {
				t.Fatal("no release")
			}
			p := frames[0].Frames[0].Payload
			if p[0] != 1 || binary.LittleEndian.Uint32(p[1:]) != token {
				t.Fatalf("release token: %x", p)
			}
			after, _ := rt.Monsters.Get(testDivision, target.Gid)
			if enterworld.CurrentMP(c) != 21 || after.CurrentHP >= target.CurrentHP {
				t.Fatal("release did not commit damage and MP")
			}
			if branch == "proc" {
				if p[16] != 4 || len(p) != 33 {
					t.Fatalf("knockdown wire: %x", p)
				}
				mover, _ := rt.Monsters.Mover(testDivision, target.Gid)
				if mover.Pose.X != target.Spawn.X+20 || after.Motion.StateAt(at) != 8 {
					t.Fatalf("consequence: %+v %+v", mover.Pose, after.Motion)
				}
				if after.Motion.StateAt(after.Motion.UntilMs-1) != 8 || after.Motion.StateAt(after.Motion.UntilMs) != 0 {
					t.Fatal("recovery boundary")
				}
			} else if p[16]&0x7f != 0 {
				t.Fatalf("ineligible KO: %x", p)
			}
		})
	}
}

func TestKnockdownInterruptRetiresEveryMonsterActionOnce(t *testing.T) {
	rt, _, c, target := newCombatTestRuntime(t, 100000)
	rt.pendingMonsterCasts = []pendingMonsterCast{{division: testDivision, instance: target, token: 11}}
	rt.queueSkillFinalize(testDivision, monsterCastOwner(target.Gid), target.Gid, 500, wire.SkillCastReleaseFrame(12, 0))
	rt.queueSkillFinalize(testDivision, monsterCastOwner(target.Gid), target.Gid, 900, wire.SkillCastFinalizeFrame(12))
	rt.queueSkillFinalize(testDivision, c.Name, enterworld.ObjectIDForCharacter(c), 900, wire.SkillCastFinalizeFrame(13))
	frames := rt.interruptMonsterCast(testDivision, target.Gid)
	if len(frames) != 2 || len(rt.interruptMonsterCast(testDivision, target.Gid)) != 0 {
		t.Fatalf("duplicate/missing closes: %+v", frames)
	}
	remaining := rt.drainSkillFinalizes(1000)
	if len(remaining) != 1 || len(remaining[0].Frames) != 1 || binary.LittleEndian.Uint32(remaining[0].Frames[0].Payload[2:]) != 13 {
		t.Fatal("interruption affected another owner")
	}
}

func TestKnockdownDisplacementCrossesSectorAndHandlesCoincidentPoses(t *testing.T) {
	to := monster.Pose{RegionID: 0x6280, X: 1915, Y: 20, Z: 4}
	plan := knockdownConsequence(simulation.Spawn{RegionID: to.RegionID, X: 1900, Y: 999, Z: 4}, to, 1000, 1107, 0)
	if plan.Pose.RegionID != 0x6281 || plan.Pose.X != 15 || plan.Pose.Y != 20 || plan.Pose.Z != 4 {
		t.Fatalf("sector displacement: %+v", plan)
	}
	same := knockdownConsequence(simulation.Spawn{RegionID: to.RegionID, X: to.X, Z: to.Z}, to, 1000, 1107, 0)
	if same.Pose != to {
		t.Fatalf("coincident displacement: %+v", same)
	}
}
