/*
===========================================================================
petwolf_test.go - Execute tier gates, seven-action counters and real HP settlement.
===========================================================================
*/
package action

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
wolfFieldFixture
================
*/
func wolfFieldFixture(t *testing.T, tier uint8) (*Runtime, *fakeClock, *enterworld.Character, monster.Instance) {
	t.Helper()
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_WOLF_040", 90)
	rt.AttackPetExpansion = true
	mob := roarField(t, rt, c, clock)
	learnPetBook(t, c, 6+tier)
	return rt, clock, c, mob
}

/*
================
wolfWeaken
================
*/
func wolfWeaken(t *testing.T, rt *Runtime, mob monster.Instance, hp uint32) {
	t.Helper()
	if len(rt.Monsters.ApplyDamageSequence(testDivision, mob.Gid, mob.CurrentHP, []simulation.MonsterDamagePlan{{GID: mob.Gid, ExpectedHP: mob.CurrentHP, Damage: mob.CurrentHP - hp}})) != 1 {
		t.Fatal("could not prepare HP")
	}
}

/*
================
wolfSpawnAt
================
*/
func wolfSpawnAt(t *testing.T, rt *Runtime, c *enterworld.Character, at simulation.Spawn, now int64) monster.Instance {
	t.Helper()
	before := make(map[uint32]bool)
	for _, m := range rt.Monsters.InstancesInRegions(testDivision, []uint16{at.RegionID}) {
		before[m.Gid] = true
	}
	lease, _ := rt.casterPopulation(testDivision, c)
	if rt.Monsters.SpawnGMMonsters(simulation.GMMonsterSpawn{Division: testDivision, Population: lease, RefObjID: 1933, Count: 1, Position: at, NowMs: now}) != 1 {
		t.Fatal("spawn refused")
	}
	for _, m := range rt.Monsters.InstancesInRegions(testDivision, []uint16{at.RegionID}) {
		if !before[m.Gid] {
			return m
		}
	}
	t.Fatal("spawn not found")
	return monster.Instance{}
}

/*
================
TestWolfOwnerExecuteTierAndSettlement
================
*/
func TestWolfOwnerExecuteTierAndSettlement(t *testing.T) {
	for tier := uint8(1); tier <= 3; tier++ {
		t.Run(fmt.Sprint(tier), func(t *testing.T) {
			rt, clock, c, mob := wolfFieldFixture(t, tier)
			wolfWeaken(t, rt, mob, 1)
			credits := 0
			rt.UpdateExperience = func(_ *enterworld.Character, _, _ int64, _ uint32) ([]wire.Frame, bool) { credits++; return nil, true }
			rt.wolfExecuteAttack(testDivision, c, 0, 1, []uint32{mob.Gid}, clock.NowMs())
			after, _ := rt.Monsters.Get(testDivision, mob.Gid)
			if (after.CurrentHP == 0) != (tier >= 2) {
				t.Fatal("owner tier gate")
			}
			if tier >= 2 {
				rt.wolfExecuteAttack(testDivision, c, 0, 1, []uint32{mob.Gid}, clock.NowMs())
				if credits != 1 {
					t.Fatalf("reward settlements %d", credits)
				}
				emotes := 0
				for _, batch := range rt.drainMonsterLegRecipients() {
					for _, f := range batch.Frames {
						if f.Opcode == wire.OpSkillCastResult {
							t.Fatal("execution introduced a cast")
						}
						if f.Opcode == wire.OpActionEmote {
							// One port-only signal on the victim draws the execution.
							if string(f.Payload) != string(wolfEmoteFrame(mob.Gid, wolfEmoteExecuted).Payload) || batch.SourceGID != mob.Gid {
								t.Fatal("execution signal", f.Payload)
							}
							emotes++
						}
					}
				}
				if emotes != 1 {
					t.Fatalf("execution signals %d", emotes)
				}
			}
		})
	}
}

/*
================
TestWolfSeventhAttackRadiusAndIndependentCounters
================
*/
func TestWolfSeventhAttackRadiusAndIndependentCounters(t *testing.T) {
	rt, clock, c, trigger := wolfFieldFixture(t, 3)
	at := rt.LiveSpawnFor(testDivision, c)
	at.X += 100 // Exactly10m from the wolf/owner; target body radius must not extend it.
	near := wolfSpawnAt(t, rt, c, at, clock.NowMs())
	wolfWeaken(t, rt, near, 1)
	at.X++
	far := wolfSpawnAt(t, rt, c, at, clock.NowMs())
	wolfWeaken(t, rt, far, 1)
	at.X -= 20
	at.Z += 20
	equal := wolfSpawnAt(t, rt, c, at, clock.NowMs())
	wolfWeaken(t, rt, equal, 19000)
	for range 6 {
		rt.wolfExecuteAttack(testDivision, c, c.ActiveCOS.GID, 0, []uint32{trigger.Gid}, clock.NowMs())
	}
	for range 5 {
		rt.wolfExecuteAttack(testDivision, c, 0, 1, []uint32{trigger.Gid}, clock.NowMs())
	}
	state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
	if state.wolfHits != [2]uint8{6, 5} {
		t.Fatal("counters combined", state.wolfHits)
	}
	still, _ := rt.Monsters.Get(testDivision, near.Gid)
	if still.CurrentHP == 0 {
		t.Fatal("early sweep")
	}
	rt.wolfExecuteAttack(testDivision, c, c.ActiveCOS.GID, 0, []uint32{trigger.Gid, trigger.Gid}, clock.NowMs())
	for _, check := range []struct {
		gid  uint32
		dead bool
	}{{near.Gid, true}, {far.Gid, false}, {equal.Gid, false}} {
		m, _ := rt.Monsters.Get(testDivision, check.gid)
		if (m.CurrentHP == 0) != check.dead {
			t.Fatalf("radius/threshold gid%d hp%d", check.gid, m.CurrentHP)
		}
	}
	if state.wolfHits != [2]uint8{0, 5} {
		t.Fatal("sweep victims counted as attacks", state.wolfHits)
	}
	rt.wolfExecuteAttack(testDivision, c, 0, 1, nil, clock.NowMs())
	if state.wolfHits != [2]uint8{0, 5} {
		t.Fatal("blocked/no-damage action charged")
	}
	at = rt.LiveSpawnFor(testDivision, c)
	at.X += 90
	ownerVictim := wolfSpawnAt(t, rt, c, at, clock.NowMs())
	wolfWeaken(t, rt, ownerVictim, 1)
	for range 2 {
		rt.wolfExecuteAttack(testDivision, c, 0, 1, []uint32{trigger.Gid}, clock.NowMs())
	}
	dead, _ := rt.Monsters.Get(testDivision, ownerVictim.Gid)
	if dead.CurrentHP != 0 || state.wolfHits != [2]uint8{} {
		t.Fatal("seventh owner attack did not sweep independently", dead.CurrentHP, state.wolfHits)
	}
}

/*
================
TestWolfEligibilityAndProgressReset
================
*/
func TestWolfEligibilityAndProgressReset(t *testing.T) {
	for _, mode := range []string{"off", "dead", "unsummoned", "far", "town", "book"} {
		t.Run(mode, func(t *testing.T) {
			rt, clock, c, mob := wolfFieldFixture(t, 3)
			rt.wolfExecuteAttack(testDivision, c, 0, 1, []uint32{mob.Gid}, clock.NowMs())
			state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
			switch mode {
			case "off":
				rt.AttackPetExpansion = false
			case "dead":
				c.ActiveCOS.CurrentHP = 0
			case "unsummoned":
				c.ActiveCOS.Summoned = false
			case "far":
				at := rt.LiveSpawnFor(testDivision, c)
				at.X += 201
				state.follower = simulation.NewPetFollower(c.ActiveCOS.GID, at)
			case "town":
				c.World.Spawn.RegionID = testInt64(0x62a8)
				rt.Worlds.Forget(simulation.WorldKey(testDivision, c.Name))
			case "book":
				c.ActiveCOS.Training.Slotted[0] = 2
			}
			rt.resetWolfExecution(petOwnerKey{testDivision, c.Name, c.ActiveCOS.GID}, clock.NowMs())
			if state.wolfHits != [2]uint8{} {
				t.Fatal("progress survived eligibility loss", state.wolfHits)
			}
		})
	}
}

/*
================
TestWolfOwnerRealMultiImpactAndAreaCountOnce
================
*/
func TestWolfOwnerRealMultiImpactAndAreaCountOnce(t *testing.T) {
	for _, area := range []bool{false, true} {
		t.Run(fmt.Sprint(area), func(t *testing.T) {
			rt, clock, c, _ := wolfFieldFixture(t, 3)
			learnPetBook(t, c, 15)
			// The pet fixture replaces item references; restore the real owner weapon.
			armed, _, _, _ := newCombatTestRuntime(t, 100000)
			weapon, _ := armed.deps.ItemReferences().ItemRefByCodename("ITEM_CH_SWORD_01_A")
			rt.deps.ItemReferences().(cosTestItemSource).staticItemSource[weapon.Codename] = weapon
			at := rt.LiveSpawnFor(testDivision, c)
			at.X += 3
			mob := wolfSpawnAt(t, rt, c, at, clock.NowMs())
			skills := rt.deps.SkillData().(staticSkillSource)
			skill := skills[2]
			skill.Attack.ImpactCount = 3
			skill.Group, skill.Consumption.Pinned = 2, true
			if area {
				at := rt.LiveSpawnFor(testDivision, c)
				at.X += 4
				wolfSpawnAt(t, rt, c, at, clock.NowMs())
				skill.OffensiveArea = enterworld.SkillOffensiveArea{Shape: 2, Radius: 100, MaxTargets: 5, Select: petAreaHostile}
			}
			skills[2] = skill
			result := rt.HandleTargetInteract(testDivision, c, wire.BasicAttackEngage{TargetGid: mob.Gid}.Encode())
			tickPetCombat(t, rt, clock, 100, func() bool {
				after, _ := rt.Monsters.Get(testDivision, mob.Gid)
				return after.CurrentHP < mob.CurrentHP
			})
			if result.DiagnosticRefusal != "" {
				t.Fatal(result.DiagnosticRefusal)
			}
			state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
			if state.wolfHits[1] != 1 || state.wolf.huntHits != 1 {
				t.Fatalf("one action counted impacts/targets: %v", state.wolfHits)
			}
		})
	}
}

/*
================
TestWolfRealPetAttackChargesSweep
================
*/
func TestWolfRealPetAttackChargesSweep(t *testing.T) {
	rt, clock, c, trigger := wolfFieldFixture(t, 3)
	at := rt.LiveSpawnFor(testDivision, c)
	at.X += 80
	weak := wolfSpawnAt(t, rt, c, at, clock.NowMs())
	wolfWeaken(t, rt, weak, 1)
	rt.HandleCosCommand(testDivision, c, petAttackOrder(c.ActiveCOS.GID, trigger.Gid))
	tickPetCombat(t, rt, clock, 250, func() bool { mob, _ := rt.Monsters.Get(testDivision, weak.Gid); return mob.CurrentHP == 0 })
	state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
	if state.wolfHits != [2]uint8{} {
		t.Fatal("seventh pet attack did not consume its own counter", state.wolfHits)
	}
}

/*
================
TestWolfOwnerPlayerHitsBuildNoStacks

An owner's PK hit is not an attack on prey: the kit stays PvE only.
================
*/
func TestWolfOwnerPlayerHitsBuildNoStacks(t *testing.T) {
	rt, clock, c, mob := wolfFieldFixture(t, 1)
	learnPetBook(t, c, 11) // Bloodscent II grows the owner's own pool.
	player := enterworld.ObjectIDForCharacter(c) + 1
	rt.wolfExecuteAttack(testDivision, c, 0, 1, []uint32{player}, clock.NowMs())
	state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
	if state.wolf.blood[1] != 0 {
		t.Fatal("a player hit built owner Bloodscent")
	}
	rt.wolfExecuteAttack(testDivision, c, 0, 1, []uint32{player, mob.Gid}, clock.NowMs())
	if state.wolf.blood[1] != 1 {
		t.Fatal("the monster hit of a mixed action did not count once", state.wolf.blood)
	}
}
