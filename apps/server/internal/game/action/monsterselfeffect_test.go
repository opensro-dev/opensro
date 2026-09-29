/*
===========================================================================

monsterselfeffect_test.go - tests for monsterselfeffect.go

===========================================================================
*/

package action

import (
	"encoding/binary"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"strconv"
	"testing"
)

func childBuffFixture(t *testing.T, id uint32, skills *enterworld.TextdataSkills) (*Runtime, *fakeClock, *enterworld.Character, monster.Instance, enterworld.SkillRow) {
	t.Helper()
	rt, clock, c, m := newCombatTestRuntime(t, 100)
	skill, ok := skills.SkillByID(id)
	if !ok || !skill.MonsterSelfEffect.Pinned {
		t.Fatal("missing primary program", id)
	}
	rt.deps.SkillData().(staticSkillSource)[id] = skill
	m.Ref.DefaultSkillIDs = [10]uint32{id}
	m.Nest.HasRarityOverride = false
	m.Nest.ConditionalSkills[0] = monster.ConditionalSkill{SkillID: id, Data: 60}
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{m.Ref.RefObjID: m.Ref}, []monster.NestRow{m.Nest}))
	rt.Monsters.SetTimeSource(clock.Now)
	rt.Monsters.StartDivision(testDivision)
	rt.Monsters.AdvancePopulation(clock.NowMs())
	actors := rt.Monsters.InstancesInRegions(testDivision, []uint16{m.Spawn.RegionID})
	if len(actors) != 1 {
		t.Fatal("fixture actor count", len(actors))
	}
	m = actors[0]
	rt.Monsters.ApplyDamage(testDivision, m.Gid, 40)
	if selected, ok := rt.Monsters.SelectConditionalSkill(testDivision, m.Gid); !ok || selected != id {
		t.Fatal("conditional gate", selected, ok)
	}
	m, _ = rt.Monsters.Get(testDivision, m.Gid)
	return rt, clock, c, m, skill
}

func TestConditionalChildBuffsCastInstallExpireAndScope(t *testing.T) {
	licensed.RequireGameData(t)
	skills := enterworld.NewTextdataSkills(gamedatatest.TextdataDir(t))
	for _, id := range []uint32{10495, 10498, 10507, 10510} {
		t.Run(strconv.Itoa(int(id)), func(t *testing.T) {
			rt, clock, c, m, skill := childBuffFixture(t, id, skills)
			before, err := combat.MonsterInstanceStats(m)
			if err != nil {
				t.Fatal(err)
			}
			hp := enterworld.CurrentHP(c)
			now := clock.NowMs()
			plan, ok := rt.MonsterAttackPlan(m, id, .9)
			if !ok || !plan.SelfEffect || plan.Summon || plan.Reach != 0 {
				t.Fatal("self plan", plan)
			}
			result := rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), id, now)
			if !result.Accepted {
				t.Fatalf("cast refused: %+v", result)
			}
			frames := result.Frames
			if skill.ActionCastingTimeMs > 0 {
				if len(frames) != 1 || frames[0].Opcode != 0xb245 {
					t.Fatal("prepare bracket", frames)
				}
				atDeadline := now + int64(skill.ActionCastingTimeMs)
				if got := rt.advanceMonsterCasts(atDeadline); len(got) != 0 {
					t.Fatal("early release", got)
				}
				// A self recipient survives enemy death and session teardown.
				c.CurrentHP = testInt64(0)
				rt.clearSkillFinalizes(testDivision, c.Name)
				now = atDeadline + 1
				got := rt.advanceMonsterCasts(now)
				if len(got) != 1 {
					t.Fatal("missing self release", got)
				}
				frames = got[0].Frames
				if enterworld.CurrentHP(c) != 0 {
					t.Fatal("self cast changed enemy")
				}
			} else if enterworld.CurrentHP(c) != hp {
				t.Fatal("buff damaged enemy")
			}
			if frames[len(frames)-1].Opcode != wire.OpAttachedEffect {
				t.Fatal("no effect attachment", frames)
			}
			payload := frames[len(frames)-1].Payload
			if binary.LittleEndian.Uint32(payload) != m.Gid || binary.LittleEndian.Uint32(payload[4:]) != id {
				t.Fatal("wrong recipient", payload)
			}
			live, _ := rt.Monsters.Get(testDivision, m.Gid)
			e := live.SelfEffects[0]
			if e.Token == 0 || e.SkillID != id || e.UntilMs != now+8000 {
				t.Fatal("lost effect", e)
			}
			after, err := combat.MonsterInstanceStats(live)
			if err != nil {
				t.Fatal(err)
			}
			switch id {
			case 10495:
				if after.PhysicalDefense != before.PhysicalDefense+16 || after.MagicalDefense != before.MagicalDefense {
					t.Fatal("defense", before, after)
				}
			case 10498, 10507:
				if after.PhysicalBasicRate != 20 || after.PhysicalSkillRate != 20 || after.PhysicalAttackMin != before.PhysicalAttackMin || after.MagicalBasicRate != 0 {
					t.Fatal("damage rate", after)
				}
			case 10510:
				if after.CriticalRate != before.CriticalRate+10 {
					t.Fatal("critical", after)
				}
			}
			def := simulation.MonsterWireDefFromInstance(live, e.UntilMs)
			if def.SelfEffects[0] != e {
				t.Fatal("deadline scope lost live effect")
			}
			row := simulation.BuildMonsterCreateRow(def, m.Gid, simulation.Spawn{RegionID: m.Spawn.RegionID, X: m.Spawn.X, Y: m.Spawn.Y, Z: m.Spawn.Z})
			// 32 fixed bytes + three float32 speeds, then count/skill/token.
			if row[44] != 1 || binary.LittleEndian.Uint32(row[45:]) != id || binary.LittleEndian.Uint32(row[49:]) != e.Token {
				t.Fatalf("scope effect bytes %x", row)
			}
			if got := rt.retireMonsterSelfEffects(e.UntilMs); len(got) != 0 {
				t.Fatal("expired at equality")
			}
			got := rt.retireMonsterSelfEffects(e.UntilMs + 1)
			if len(got) != 1 || got[0].SourceGID != m.Gid || got[0].Frames[0].Opcode != wire.OpEndedEffectInstances {
				t.Fatal("missing expiry", got)
			}
			ended, err := wire.DecodeEndedEffectInstances(got[0].Frames[0].Payload)
			if err != nil || len(ended.InstanceTokens) != 1 || ended.InstanceTokens[0] != e.Token {
				t.Fatal("expiry identity", ended, err)
			}
			live, _ = rt.Monsters.Get(testDivision, m.Gid)
			restored, _ := combat.MonsterInstanceStats(live)
			if restored.PhysicalDefense != before.PhysicalDefense || restored.CriticalRate != before.CriticalRate || restored.PhysicalBasicRate != 0 {
				t.Fatal("effect stats leaked", restored)
			}
			if _, selected := rt.Monsters.SelectConditionalSkill(testDivision, m.Gid); selected {
				t.Fatal("expiry re-enabled health condition")
			}
			if got := rt.retireMonsterSelfEffects(e.UntilMs + 2); len(got) != 0 {
				t.Fatal("duplicate retirement")
			}
		})
	}
}

func TestConditionalChildBuffCasterDeathCancels(t *testing.T) {
	licensed.RequireGameData(t)
	skills := enterworld.NewTextdataSkills(gamedatatest.TextdataDir(t))
	rt, clock, c, m, _ := childBuffFixture(t, 10510, skills)
	if result := rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), 10510, clock.NowMs()); !result.Accepted {
		t.Fatal("no preparation")
	}
	rt.Monsters.ApplyDamage(testDivision, m.Gid, 1000)
	frames := rt.advanceMonsterCasts(clock.NowMs() + 930)
	if len(frames) != 1 || len(frames[0].Frames) != 1 || frames[0].Frames[0].Payload[0] != 2 {
		t.Fatal("death did not cancel", frames)
	}
	live, _ := rt.Monsters.Get(testDivision, m.Gid)
	if live.SelfEffects != (monster.SelfEffects{}) {
		t.Fatal("dead caster received buff")
	}
}
