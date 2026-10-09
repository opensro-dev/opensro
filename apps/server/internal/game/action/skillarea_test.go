/*
===========================================================================

skillarea_test.go - authoritative combat behavior and state transitions

Exercise the production combat lane, including its committed HP and wire results.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"errors"
	"opensro.online/server/internal/game/enterworld"
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
areaFixture
================
*/
func areaFixture(t *testing.T, hp uint32) (*Runtime, []monster.Instance) {
	t.Helper()
	rt, clock, _, primary := newCombatTestRuntime(t, hp)
	var nests []monster.NestRow
	for i := 0; i < 5; i++ {
		spawn := primary.Spawn
		spawn.X += float64(i)
		if i == 3 {
			spawn.Y += 1000
		}
		if i == 4 {
			spawn.X += 1000
		}
		nests = append(nests, monster.NestRow{SpawnPoint: spawn, RetailEvidence: true, MaxCount: 1})
	}
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{primary.Ref.RefObjID: primary.Ref}, nests))
	rt.Monsters.SetTimeSource(clock.Now)
	rt.Monsters.StartDivision(testDivision)
	rt.Monsters.AdvancePopulation(rt.Monsters.CurrentTimeMillis())
	return rt, rt.Monsters.InstancesInRegions(testDivision, []uint16{primary.Spawn.RegionID})
}

/*
================
TestAreaFalloffAndFatalRewardOwnership
================
*/
func TestAreaFalloffAndFatalRewardOwnership(t *testing.T) {
	for _, fatal := range []bool{false, true} {
		hp := uint32(100000)
		if fatal {
			hp = 1
		}
		rt, targets := areaFixture(t, hp)
		c := rt.findCharacter(testDivision, "asd2")
		skill := shippedOffense(t, "SKILL_CH_LIGHTNING_CHUNDUNG_A_01")
		// Exercise the authored EFR reduction parameter independently of which
		// weapon family happens to carry 35 percent in the current catalog.
		skill.OffensiveArea.ReductionPercent = 35
		rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
		c.Skills = append(c.Skills, skill.ID)
		c.Intellect = testInt64(200)
		c.CurrentMP = testInt64(1000)
		kills := 0
		rt.UpdateQuestKill = func(*enterworld.Character, string, uint8) ([]wire.Frame, bool) { kills++; return nil, true }
		request := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: targets[0].Gid}.Encode()
		result := rt.HandleTargetInteract(testDivision, c, request)
		if len(result.Frames) == 0 {
			t.Fatal("area refused")
		}
		payload := result.Frames[0].Payload
		first := binary.LittleEndian.Uint32(payload[26:30]) >> 8
		for i, percent := range []uint32{100, 65, 42} {
			damage := binary.LittleEndian.Uint32(payload[26+i*13:30+i*13]) >> 8
			if fatal {
				if damage <= hp || payload[25+i*13] != 128 {
					t.Fatal("fatal hit lost its full damage or fatal flag")
				}
			}
			if damage != uint32(uint64(first)*uint64(percent)/100) {
				t.Fatalf("falloff %d: %d first %d", i, damage, first)
			}
		}
		wantKills := 0
		if fatal {
			wantKills = 3
		}
		if kills != wantKills {
			t.Fatalf("kill credits %d", kills)
		}
		// 4A9C80: every killed victim publishes LIFE dead, or the client keeps
		// moving its corpse through the death animation.
		dead := map[uint32]bool{}
		for _, frame := range result.Frames {
			if frame.Opcode == wire.OpObjectStateRefresh && len(frame.Payload) == 6 && frame.Payload[4] == wire.StateChannelLife &&
				frame.Payload[5] == wire.LifeStateDead {
				dead[binary.LittleEndian.Uint32(frame.Payload)] = true
			}
		}
		if len(dead) != wantKills {
			t.Fatalf("LIFE dead published for %d victims, want %d", len(dead), wantKills)
		}
		rt.HandleTargetInteract(testDivision, c, request)
		if kills != wantKills || rt.castTokenCounter != 1 {
			t.Fatal("replay duplicated area rewards")
		}
	}
}

/*
================
TestAreaCastCommitsThreeVictimsWithOneCostAndToken
================
*/
func TestAreaCastCommitsThreeVictimsWithOneCostAndToken(t *testing.T) {
	rt, targets := areaFixture(t, 100000)
	c := rt.findCharacter(testDivision, "asd2")
	skill := shippedOffense(t, "SKILL_CH_LIGHTNING_CHUNDUNG_A_01")
	if skill.OffensiveArea.MaxTargets != 3 || !skill.DirectOffensePinned {
		t.Fatalf("not admitted: %+v", skill)
	}
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.Intellect = testInt64(200)
	c.CurrentMP = testInt64(1000)
	result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: targets[0].Gid}.Encode())
	if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpSkillCastResult {
		t.Fatalf("area refused %+v", result)
	}
	p := result.Frames[0].Payload
	if len(p) != 60 || p[19] != 1 || p[20] != 3 {
		t.Fatalf("area matrix %x", p)
	}
	for i, target := range targets {
		after, _ := rt.Monsters.Get(testDivision, target.Gid)
		if i < 3 {
			if after.CurrentHP >= target.CurrentHP || binary.LittleEndian.Uint32(p[21+i*13:]) != target.Gid {
				t.Fatal("victim missing/damaged wrong gid")
			}
		} else if after.CurrentHP != target.CurrentHP {
			t.Fatal("out-of-volume target damaged")
		}
	}
	if *c.CurrentMP != 883 || rt.castTokenCounter != 1 {
		t.Fatalf("area cost/token MP=%d token=%d authored=%d", *c.CurrentMP, rt.castTokenCounter, skill.Consumption.MP)
	}
}

/*
================
TestAreaPlanningFailureDoesNotPartiallyCommit
================
*/
func TestAreaPlanningFailureDoesNotPartiallyCommit(t *testing.T) {
	rt, targets := areaFixture(t, 100000)
	c := rt.findCharacter(testDivision, "asd2")
	skill := shippedOffense(t, "SKILL_CH_LIGHTNING_CHUNDUNG_A_01")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.Intellect = testInt64(200)
	c.CurrentMP = testInt64(1000)
	rolls := 0
	rt.CombatRoll = func() (uint32, error) {
		rolls++
		if rolls > 1 {
			return 0, errors.New("rng unavailable")
		}
		return 0, nil
	}
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: targets[0].Gid}.Encode())
	for _, target := range targets {
		after, _ := rt.Monsters.Get(testDivision, target.Gid)
		if after.CurrentHP != target.CurrentHP {
			t.Fatal("partial damage")
		}
	}
	if *c.CurrentMP != 1000 || rt.castTokenCounter != 0 {
		t.Fatal("failed plan spent resources")
	}
}

/*
================
TestAreaBatchRefusesStaleAndDuplicateVictimsBeforeMutation
================
*/
func TestAreaBatchRefusesStaleAndDuplicateVictimsBeforeMutation(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		rt, targets := areaFixture(t, 100)
		plans := []simulation.MonsterDamagePlan{{GID: targets[0].Gid, ExpectedHP: 100, Damage: 200}, {GID: targets[1].Gid, ExpectedHP: 99, Damage: 200}}
		if duplicate {
			plans[1] = plans[0]
		}
		if _, ok := rt.Monsters.ApplyDamageBatch(testDivision, plans); ok {
			t.Fatal("invalid batch accepted")
		}
		for _, target := range targets {
			after, _ := rt.Monsters.Get(testDivision, target.Gid)
			if after.CurrentHP != 100 {
				t.Fatal("invalid batch partially committed")
			}
		}
	}
}

/*
================
TestAreaVictimDropsFollowTheirOwnDeath

The client knows a drop by the death it follows (it holds both until the
killing hit plays), so each victim's LIFE dead frame is followed directly by
its own drops, as the native death credit publishes them (4C42F0). Each
victim's gold lands at its own pose, which names the dropper.
================
*/
func TestAreaVictimDropsFollowTheirOwnDeath(t *testing.T) {
	rt, targets := areaFixture(t, 1)
	installSmallGoldRef(rt)
	one := len(assignedDropMisses()) + 5 + 16 + 4
	// Each victim draws one complete gold-only sequence.
	draws := 0
	var roll func() (uint32, error)
	rt.DropRoll = func() (uint32, error) {
		if draws%one == 0 {
			roll = goldOnlyMonsterDropRoll(0, 0)
		}
		draws++
		return roll()
	}
	c := rt.findCharacter(testDivision, "asd2")
	skill := shippedOffense(t, "SKILL_CH_LIGHTNING_CHUNDUNG_A_01")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.Intellect = testInt64(200)
	c.CurrentMP = testInt64(1000)
	result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: targets[0].Gid}.Encode())
	poses := map[float32]uint32{}
	for _, target := range targets {
		poses[float32(target.Spawn.X+8)] = target.Gid
	}
	var dying uint32
	drops := 0
	for _, frame := range result.Frames {
		switch {
		case frame.Opcode == wire.OpObjectStateRefresh && len(frame.Payload) == 6 && frame.Payload[4] == wire.StateChannelLife &&
			frame.Payload[5] == wire.LifeStateDead:
			dying = binary.LittleEndian.Uint32(frame.Payload)
		case frame.Opcode == wire.OpSingleObjectSpawn:
			row, err := wire.DecodeGroundItemRow(frame.Payload, wire.PackTypeFlags(3, 3, 5, 1), true)
			if err != nil {
				t.Fatalf("drop row: %v", err)
			}
			if dying == 0 || poses[row.X] != dying {
				t.Fatalf("drop at x=%v follows the death of %d, want its own victim %d", row.X, dying, poses[row.X])
			}
			drops++
		default:
			dying = 0
		}
	}
	if drops != 3 {
		t.Fatalf("published %d victim drops, want 3", drops)
	}
}
