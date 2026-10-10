package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

func TestAreaCenterUsesLiveCasterOrPrimary(t *testing.T) {
	rt, clock, c, primary := newCombatTestRuntime(t, 100000)
	origin := simulation.SeedWorldState(c).Spawn
	key := simulation.WorldKey(testDivision, c.Name)
	// Keep the persisted character position far away. Selection must use the
	// live world owner, just as an attack issued after approaching its target does.
	origin.X = primary.Spawn.X - 100
	origin.Y, origin.Z = primary.Spawn.Y, primary.Spawn.Z
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) { w.Spawn = origin })
	nests := []monster.NestRow{}
	for _, dx := range []float64{0, -100, 5, -100} {
		spawn := primary.Spawn
		spawn.X += dx
		if len(nests) == 3 {
			spawn.Y += 1000
		}
		nests = append(nests, monster.NestRow{SpawnPoint: spawn, RetailEvidence: true, MaxCount: 1})
	}
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{primary.Ref.RefObjID: primary.Ref}, nests))
	rt.Monsters.SetTimeSource(clock.Now)
	rt.Monsters.StartDivision(testDivision)
	rt.Monsters.AdvancePopulation(rt.Monsters.CurrentTimeMillis())
	targets := rt.Monsters.InstancesInRegions(testDivision, []uint16{primary.Spawn.RegionID})
	for _, tc := range []struct {
		shape     uint8
		secondary int
	}{{1, 1}, {2, 2}} {
		area := enterworld.SkillOffensiveArea{Shape: tc.shape, Radius: 10, MaxTargets: 5, Select: 24}
		got := areaTestVictims(rt, c, targets[0], area, 0, clock.Now().UnixMilli())
		if len(got) != 2 || got[0].Gid != targets[0].Gid || got[1].Gid != targets[tc.secondary].Gid {
			t.Fatalf("shape %d picked %+v", tc.shape, got)
		}
	}
	for _, shape := range []uint8{0, 5, 7, 255} {
		if got := areaTestVictims(rt, c, targets[0], enterworld.SkillOffensiveArea{Shape: shape, Radius: 1000, MaxTargets: 5, Select: 24}, 0, clock.Now().UnixMilli()); len(got) != 0 {
			t.Fatalf("unknown shape %d became sphere", shape)
		}
	}
}

func TestTargetedChainNearestPrimaryCenterAndNoBodyExpansion(t *testing.T) {
	rt, clock, c, primary := newCombatTestRuntime(t, 100000)
	var nests []monster.NestRow
	for _, dx := range []float64{0, 9, 2, 11, -2, 20, 10} {
		spawn := primary.Spawn
		spawn.X += dx
		nests = append(nests, monster.NestRow{SpawnPoint: spawn, RetailEvidence: true, MaxCount: 1})
	}
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{primary.Ref.RefObjID: primary.Ref}, nests))
	rt.Monsters.SetTimeSource(clock.Now)
	rt.Monsters.StartDivision(testDivision)
	rt.Monsters.AdvancePopulation(rt.Monsters.CurrentTimeMillis())
	targets := rt.Monsters.InstancesInRegions(testDivision, []uint16{primary.Spawn.RegionID})
	area := enterworld.SkillOffensiveArea{Shape: 6, Radius: 10, MaxTargets: 6, Select: 24}
	got := areaTestVictims(rt, c, targets[0], area, 0, clock.Now().UnixMilli())
	want := []int{0, 2, 4, 1, 6}
	if len(got) != len(want) {
		t.Fatalf("chain victims: %+v", got)
	}
	for i, index := range want {
		if got[i].Gid != targets[index].Gid {
			t.Fatalf("victim %d: %d, want %d", i, got[i].Gid, targets[index].Gid)
		}
	}
	area.MaxTargets = 2
	if got = areaTestVictims(rt, c, targets[0], area, 0, clock.Now().UnixMilli()); len(got) != 2 || got[1].Gid != targets[2].Gid {
		t.Fatal("chain cap did not include primary")
	}
}

func TestShippedNewAreaShapesExecuteThroughSkillAdmission(t *testing.T) {
	for _, name := range []string{"SKILL_CH_SPEAR_ROUNDAREA_A_02", "SKILL_CH_LIGHTNING_STORM_A_01"} {
		t.Run(name, func(t *testing.T) {
			rt, targets := areaFixture(t, 100000)
			c := rt.findCharacter(testDivision, "asd2")
			skill := shippedOffense(t, name)
			if !skill.DirectOffensePinned || (skill.OffensiveArea.Shape != 1 && skill.OffensiveArea.Shape != 6) {
				t.Fatalf("not admitted: %+v", skill)
			}
			rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
			c.Skills = append(c.Skills, skill.ID)
			c.Intellect = testInt64(2000)
			c.CurrentMP = testInt64(10000)
			if skill.RequiredWeaponKinds[0] == 4 {
				items := rt.deps.ItemReferences().(staticItemSource)
				weapon := *items[c.MissionInventory[0].Codename]
				weapon.Codename, weapon.RefObjID, weapon.TypeIDs[3] = "ITEM_CH_SPEAR_01_A", 73, 4
				items[weapon.Codename] = &weapon
				c.MissionInventory[0].Codename, c.MissionInventory[0].RefObjID, c.MissionInventory[0].TypeFlags = weapon.Codename, weapon.RefObjID, weapon.TypeFlags()
			}
			result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: targets[0].Gid}.Encode())
			if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpSkillCastResult {
				t.Fatalf("cast refused: %+v", result)
			}
			p := result.Frames[0].Payload
			matrix := 20
			if skill.ActionCastingTimeMs != 0 {
				result = releasePreparedSkillForTest(t, rt, rt.Now().UnixMilli()+int64(skill.ActionCastingTimeMs)+1)
				p = result.Frames[0].Payload
				matrix = 11
			}
			if len(p) != matrix+40 || p[matrix] != 3 {
				t.Fatalf("cast matrix: %x", p)
			}
			for i := 0; i < 3; i++ {
				if binary.LittleEndian.Uint32(p[matrix+1+i*13:]) != targets[i].Gid {
					t.Fatalf("wrong victim: %x", p)
				}
				after, _ := rt.Monsters.Get(testDivision, targets[i].Gid)
				if after.CurrentHP >= targets[i].CurrentHP {
					t.Fatal("cast did not damage victim")
				}
			}
			if *c.CurrentMP != 10000-int64(skill.Consumption.MP) || rt.castTokenCounter != 1 {
				t.Fatal("cost or cast duplicated")
			}
		})
	}
}

/*
================
areaTestVictims

areaVictims for a monster primary, as the monsters it selected (these
fixtures stand no players in range).
================
*/
func areaTestVictims(rt *Runtime, c *enterworld.Character, primary monster.Instance, area enterworld.SkillOffensiveArea, reach float32, now int64) []monster.Instance {
	target := combatTarget{gid: primary.Gid, monster: &primary, at: rt.monsterSpawn(testDivision, primary.Gid, now)}
	var out []monster.Instance
	for _, victim := range rt.areaVictims(testDivision, c, enterworld.SkillRow{}, target, area, reach, now) {
		if victim.monster != nil {
			out = append(out, *victim.monster)
		}
	}
	return out
}
