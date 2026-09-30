/*
===========================================================================

skillknockback_test.go - knockback skills

Bash release and area variants own their damage, displacement, recovery and
cost; every authored knockback family is complete.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"fmt"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestBashReleaseOwnsDamageDisplacementAndRecovery
================
*/
func TestBashReleaseOwnsDamageDisplacementAndRecovery(t *testing.T) {
	for _, branch := range []string{"proc", "immune", "fatal", "cancel"} {
		t.Run(branch, func(t *testing.T) {
			rt, clock, c, target := newCombatTestRuntime(t, 100000)
			target.Ref.Knockdown = 2
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
			skill := shippedOffense(t, "SKILL_EU_WARRIOR_TWOHANDA_DASH_A_01")
			if !skill.DirectOffensePinned || !skill.Knockback.Present {
				t.Fatalf("missing complete authored program: %+v", skill)
			}
			weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
			weapon.TypeIDs[3] = 8
			c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
			rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
			c.Skills = append(c.Skills, skill.ID)
			c.CurrentMP = testInt64(100)
			rt.CombatRoll = func() (uint32, error) { return 0, nil }
			now := clock.NowMs()
			cast := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}
			start, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), cast, now)
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
			if enterworld.CurrentMP(c) != 70 || after.CurrentHP >= target.CurrentHP {
				t.Fatal("release did not commit damage and MP")
			}
			if branch == "proc" {
				if p[16] != 5 || len(p) != 33 {
					t.Fatalf("knockback wire: %x", p)
				}
				mover, _ := rt.Monsters.Mover(testDivision, target.Gid)
				if mover.Pose.X != target.Spawn.X+50 || after.Motion.StateAt(at) != 16 {
					t.Fatalf("consequence: %+v %+v", mover.Pose, after.Motion)
				}
				if after.Motion.UntilMs != at+2000 {
					t.Fatalf("hold: %+v", after.Motion)
				}
				if after.Motion.StateAt(after.Motion.UntilMs-1) != 16 || after.Motion.StateAt(after.Motion.UntilMs) != 0 {
					t.Fatal("recovery boundary")
				}
			} else if p[16]&0x7f != 0 {
				t.Fatalf("ineligible KB: %x", p)
			}
		})
	}
}

/*
================
TestBashAreaCommitsPerVictimKnockbackAndOneCost
================
*/
func TestBashAreaCommitsPerVictimKnockbackAndOneCost(t *testing.T) {
	rt, targets := areaFixture(t, 100000)
	c := rt.findCharacter(testDivision, "asd2")
	var nests []monster.NestRow
	ref := targets[0].Ref
	ref.Knockdown = 2
	for _, target := range targets {
		nest := target.Nest
		nest.HasRarityOverride = false
		nest.RarityOverride = 0
		nests = append(nests, nest)
	}
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{ref.RefObjID: ref}, nests))
	rt.Monsters.SetTimeSource(rt.Now)
	rt.Monsters.StartDivision(testDivision)
	rt.Monsters.AdvancePopulation(rt.Monsters.CurrentTimeMillis())
	targets = rt.Monsters.InstancesInRegions(testDivision, []uint16{targets[0].Spawn.RegionID})
	skill := shippedOffense(t, "SKILL_EU_WARRIOR_TWOHANDA_DASH_A_01")
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 8
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentMP = testInt64(100)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	now := rt.Now().UnixMilli()
	start, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: targets[0].Gid}, now)
	if decision != skillCastAccepted {
		t.Fatalf("refused: %+v", start)
	}
	token := binary.LittleEndian.Uint32(start.Frames[0].Payload[10:])
	at := now + int64(skill.ActionCastingTimeMs) + 1
	result := rt.advanceProjectileCasts(at)
	if len(result) == 0 {
		t.Fatal("no release")
	}
	p := result[0].Frames[0].Payload
	if len(p) != 67 || p[10] != 1 || p[11] != 3 || binary.LittleEndian.Uint32(p[1:]) != token {
		t.Fatalf("area result: %x", p)
	}
	offset := 12
	previousDamage := uint32(0xffffffff)
	for i, target := range targets {
		after, _ := rt.Monsters.Get(testDivision, target.Gid)
		if i >= 3 {
			if after.CurrentHP != target.CurrentHP || after.Motion.StateAt(at) != 0 {
				t.Fatal("outside victim changed")
			}
			continue
		}
		off := offset
		damage := binary.LittleEndian.Uint32(p[off+5:]) >> 8
		kind := []byte{5, 0, 5}[i] // Caster-owned probability history spans all victims.
		if binary.LittleEndian.Uint32(p[off:]) != target.Gid || p[off+4] != kind || damage == 0 || damage >= previousDamage || target.CurrentHP-after.CurrentHP != damage {
			t.Fatalf("victim %d: %x", i, p)
		}
		mover, _ := rt.Monsters.Mover(testDivision, target.Gid)
		distance, motion := float64(0), byte(0)
		if kind == 5 {
			distance, motion = 50, 16
		}
		if mover.Pose.X != target.Spawn.X+distance || after.Motion.StateAt(at) != motion {
			t.Fatalf("victim %d displacement: %+v", i, mover.Pose)
		}
		previousDamage = damage
		offset += 13
		if kind == 5 {
			offset += 8
		}
	}
	if enterworld.CurrentMP(c) != 70 || rt.castTokenCounter != 1 {
		t.Fatal("cost or token repeated")
	}
	if len(rt.advanceProjectileCasts(at)) != 0 {
		t.Fatal("release replayed")
	}
}

/*
================
TestKnockbackCompleteAuthoredFamilies
================
*/
func TestKnockbackCompleteAuthoredFamilies(t *testing.T) {
	licensed.RequireGameData(t)
	source := enterworld.NewTextdataSkills(licensed.RetailTextdataDir(t))
	for family, count := range map[string]int{"SKILL_EU_WARRIOR_TWOHANDA_DASH_A": 22, "SKILL_CH_SPEAR_ROUNDAREA_B": 9, "SKILL_CH_SPEAR_ROUNDAREA_C": 9, "SKILL_CH_SPEAR_ROUNDAREA_D": 3} {
		for rank := 1; rank <= count; rank++ {
			code := fmt.Sprintf("%s_%02d", family, rank)
			row, ok := source.SkillByCodename(code)
			if !ok || !row.DirectOffensePinned || !row.Knockback.Present || row.Attack.ImpactCount != 1 {
				t.Fatalf("incomplete %s: %+v", code, row)
			}
		}
	}
}
