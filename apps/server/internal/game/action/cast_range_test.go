package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// Use the shipped Logos Baroi rows: preparation, one/two impacts and projectile
// speed must not silently turn into a synthetic instant melee test.
func TestLogosBaroiReleaseRetainsMovingTarget(t *testing.T) {
	for _, code := range []string{"MSKILL_EU_BAROI_CLON_ATTACK01", "MSKILL_EU_BAROI_CLON_ATTACK02"} {
		t.Run(code, func(t *testing.T) {
			rt, clock, c, m := newCombatTestRuntime(t, 1000)
			skill := shippedOffense(t, code)
			if skill.ActionCastingTimeMs != 1053 || skill.ProjectileSpeed != 400 {
				t.Fatalf("unexpected authored timing: %+v", skill)
			}
			m.Ref.DefaultSkillIDs[0] = skill.ID
			rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
			c.CurrentHP = testInt64(200)
			key := simulation.WorldKey(testDivision, c.Name)
			pose := func(x float64) {
				rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) { w.Spawn.X = x })
			}
			spacing, _ := rt.monsterToPlayerCombatSpacing(m, c, simulation.ActionReach(skill.ActionRange))
			mv, _ := rt.Monsters.Mover(testDivision, m.Gid)
			edge := mv.Pose.X + spacing.AdmissionRadius()
			pose(edge + 1)
			if got := rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), skill.ID, clock.NowMs()); got.Accepted {
				t.Fatal("a new cast outside range was admitted")
			}
			pose(edge - 1)
			start := rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), skill.ID, clock.NowMs())
			if !start.Accepted || len(start.Frames) != 1 || enterworld.CurrentHP(c) != 200 {
				t.Fatalf("edge preparation did not retain an uncharged cast: %+v, hp=%d, mover=%+v world=%+v", start, enterworld.CurrentHP(c), mv.Pose, rt.liveSpawn(key, c, clock.NowMs()))
			}
			pose(edge + 100)
			if got := rt.advanceMonsterCasts(clock.NowMs() + 1053); len(got) != 0 {
				t.Fatal("cast released before strict expiry")
			}
			got := rt.advanceMonsterCasts(clock.NowMs() + 1054)
			if len(got) == 0 || len(got[0].Frames) == 0 || got[0].Frames[0].Opcode != wire.OpSkillEffectControl || got[0].Frames[0].Payload[0] != 1 || enterworld.CurrentHP(c) >= 200 {
				t.Fatalf("moving outside admission range cancelled an owned cast: %+v", got)
			}
			hp := enterworld.CurrentHP(c)
			pose(edge + 500)
			if len(rt.advanceMonsterCasts(clock.NowMs()+1500)) != 0 || enterworld.CurrentHP(c) != hp {
				t.Fatal("movement after release changed or replayed damage")
			}
		})
	}
}

func TestPlayerArrowReleaseRetainsMovingTarget(t *testing.T) {
	rt, c, gid, skill, now := arrowFixture(t)
	cast := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: gid}
	start, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), cast, now, rt.handlerDoors())
	if decision != skillCastAccepted || len(start.Frames) != 1 {
		t.Fatal("not preparing")
	}
	before, _ := rt.Monsters.Get(testDivision, gid)
	mover, _ := rt.Monsters.Mover(testDivision, gid)
	mover.Pose.X += 500
	if !rt.Monsters.CommitMover(testDivision, gid, mover) {
		t.Fatal("fixture movement rejected")
	}
	frames := rt.advanceProjectileCasts(now + int64(skill.ActionCastingTimeMs) + 1)
	after, _ := rt.Monsters.Get(testDivision, gid)
	if len(frames) == 0 || frames[0].Frames[0].Payload[0] != 1 || after.CurrentHP >= before.CurrentHP || c.MissionInventory[1].StackCount != 1 {
		t.Fatalf("target movement cancelled the player's owned arrow: %+v", frames)
	}
}
