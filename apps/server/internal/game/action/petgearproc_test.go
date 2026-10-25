/*
===========================================================================
petgearproc_test.go - striking/struck proc isolation and credited spell damage
===========================================================================
*/
package action

import (
	"fmt"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"strings"
	"testing"
)

/*
================
wearProc
================
*/
func wearProc(t *testing.T, c *enterworld.Character, group string, degree int) {
	t.Helper()
	for _, a := range companion.D.Affixes {
		if a.Group != group || a.Status != "Implemented" {
			continue
		}
		for slot, g := range companion.D.Gear[degree-1].Items {
			for _, allowed := range a.Slots {
				if g.Slot == allowed {
					c.ActiveCOS.Training.Gear[slot] = domain.PetGear{RefObjID: g.ID}
					c.ActiveCOS.Training.Gear[slot].Options[0] = companion.EncodeAffix(a.Code, 100)
					return
				}
			}
		}
	}
	t.Fatal("missing implemented proc", group)
}

/*
================
TestPetGearProcDamageAndTriggerIsolation
================
*/
func TestPetGearProcDamageAndTriggerIsolation(t *testing.T) {
	for _, group := range []string{"ProcChain", "ProcBlizzard", "ProcFire", "ProcPoison", "ProcMend"} {
		for _, taken := range []bool{false, true} {
			t.Run(fmt.Sprint(group, taken), func(t *testing.T) {
				rt, clock, c, mob := wolfFieldFixture(t, 1)
				name := group
				if taken {
					name += "Taken"
				}
				wearProc(t, c, name, 9)
				c.MissionInventory = nil
				*c.CurrentHP = 1
				state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
				now := clock.NowMs()
				rt.petGearProcs(testDivision, c, c.ActiveCOS.GID, mob.Gid, simulation.Spawn{RegionID: mob.Spawn.RegionID, X: mob.Spawn.X, Y: mob.Spawn.Y, Z: mob.Spawn.Z}, !taken, now)
				if state.trained.procReadyAt != 0 {
					t.Fatal("wrong damage direction triggered")
				}
				rt.petGearProcs(testDivision, c, c.ActiveCOS.GID, mob.Gid, simulation.Spawn{RegionID: mob.Spawn.RegionID, X: mob.Spawn.X, Y: mob.Spawn.Y, Z: mob.Spawn.Z}, taken, now)
				if state.trained.procReadyAt != now+companion.D.Rules.ProcInternalCooldownMs {
					t.Fatal("shared cooldown not reserved")
				}
				before, _ := rt.Monsters.Get(testDivision, mob.Gid)
				rt.petGearProcs(testDivision, c, c.ActiveCOS.GID, mob.Gid, simulation.Spawn{RegionID: mob.Spawn.RegionID, X: mob.Spawn.X, Y: mob.Spawn.Y, Z: mob.Spawn.Z}, taken, now)
				same, _ := rt.Monsters.Get(testDivision, mob.Gid)
				if same.CurrentHP != before.CurrentHP {
					t.Fatal("cooldown allowed repeated damage")
				}
				for i := int64(1); i <= 3; i++ {
					rt.advancePetGearFields(petOwnerKey{division: testDivision, name: strings.ToLower(c.Name), gid: c.ActiveCOS.GID}, now+i*1000)
				}
				after, _ := rt.Monsters.Get(testDivision, mob.Gid)
				if group == "ProcMend" {
					if *c.CurrentHP <= 1 || after.CurrentHP != mob.CurrentHP {
						t.Fatal("Mend failed to heal owner exclusively")
					}
				} else if mob.CurrentHP-after.CurrentHP != 1000 {
					t.Fatalf("total damage=%d, want1000", mob.CurrentHP-after.CurrentHP)
				}
				if len(state.trained.gearFields) != 0 {
					t.Fatal("field outlived four pulses")
				}
				if state.trained.attacks != 0 {
					t.Fatal("proc generated attack counters")
				}
			})
		}
	}
}

/*
================
TestPetGearProcDegreeAndLifecycle
================
*/
func TestPetGearProcDegreeAndLifecycle(t *testing.T) {
	for _, degree := range []int{3, 9} {
		rt, clock, c, mob := wolfFieldFixture(t, 1)
		wearProc(t, c, "ProcFire", degree)
		rt.petGearProcs(testDivision, c, c.ActiveCOS.GID, mob.Gid, simulation.Spawn{RegionID: mob.Spawn.RegionID, X: mob.Spawn.X, Y: mob.Spawn.Y, Z: mob.Spawn.Z}, false, clock.NowMs())
		after, _ := rt.Monsters.Get(testDivision, mob.Gid)
		if mob.CurrentHP-after.CurrentHP != companion.D.Gear[degree-1].ProcDamage {
			t.Fatal("degree did not set proc damage")
		}
	}
	for _, reason := range []string{"dead", "unsummoned", "disabled", "town"} {
		t.Run(reason, func(t *testing.T) {
			rt, clock, c, mob := wolfFieldFixture(t, 1)
			wearProc(t, c, "ProcBlizzard", 9)
			state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
			rt.petGearProcs(testDivision, c, c.ActiveCOS.GID, mob.Gid, simulation.Spawn{RegionID: mob.Spawn.RegionID, X: mob.Spawn.X, Y: mob.Spawn.Y, Z: mob.Spawn.Z}, false, clock.NowMs())
			before, _ := rt.Monsters.Get(testDivision, mob.Gid)
			switch reason {
			case "dead":
				c.ActiveCOS.CurrentHP = 0
			case "unsummoned":
				c.ActiveCOS.Summoned = false
			case "disabled":
				rt.AttackPetExpansion = false
			case "town":
				c.World.Spawn.RegionID = testInt64(0x5f98)
				rt.Worlds.Forget(simulation.WorldKey(testDivision, c.Name))
			}
			rt.advancePetGearFields(petOwnerKey{division: testDivision, name: strings.ToLower(c.Name), gid: c.ActiveCOS.GID}, clock.NowMs()+1000)
			after, _ := rt.Monsters.Get(testDivision, mob.Gid)
			if after.CurrentHP != before.CurrentHP || len(state.trained.gearFields) != 0 {
				t.Fatal("ineligible source retained field")
			}
		})
	}
}

/*
================
TestPetGearProcsUseCommittedCombatHooks
================
*/
func TestPetGearProcsUseCommittedCombatHooks(t *testing.T) {
	t.Run("attack", func(t *testing.T) {
		rt, clock, c, mob := wolfFieldFixture(t, 1)
		wearProc(t, c, "ProcFire", 9)
		state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
		attackUntil(t, rt, clock, c, mob, func() bool { return state.trained.procReadyAt > 0 })
		if state.trained.procReadyAt == 0 {
			t.Fatal("normal attack never triggered its blue")
		}
	})
	for _, kind := range []string{"physical", "magic", "shielded"} {
		t.Run("received-"+kind, func(t *testing.T) {
			shielded := kind == "shielded"
			rt, clock, c, mob := wolfFieldFixture(t, 1)
			wearProc(t, c, "ProcFireTaken", 9)
			state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
			ref, _ := rt.cosReference(c.ActiveCOS)
			at := state.follower.Position(clock.NowMs())
			skill := rt.deps.SkillData().(staticSkillSource)[2]
			if kind == "magic" {
				skill.Attack.Flags = 8
			}
			if shielded {
				rt.petShields.rows = map[petShieldKey]petShield{{testDivision, c.ActiveCOS.GID}: {key: petShieldKey{testDivision, c.ActiveCOS.GID}, amount: 100000, until: clock.NowMs() + 5000}}
			}
			hit := rt.monsterStrikeCOS(monsterStrikeInput{division: testDivision, instance: mob, skill: skill, now: clock.NowMs(), from: monster.Pose{RegionID: at.RegionID, X: at.X, Y: at.Y, Z: at.Z}, percent: fullAreaPercent, attacker: combat.Stats{Level: 1, HitRate: 10000, PhysicalAttackMin: 1, PhysicalAttackMax: 1, MagicalAttackMin: 1, MagicalAttackMax: 1}}, c, c.ActiveCOS, ref, at)
			if !hit.committed || hit.strike.fatal {
				t.Fatal("test hit did not commit nonfatally")
			}
			if (state.trained.procReadyAt > 0) == shielded {
				t.Fatalf("incoming proc shielded=%v ready=%d", shielded, state.trained.procReadyAt)
			}
		})
	}
}

/*
================
TestPetGearChainDistinctHopsAndSight
================
*/
func TestPetGearChainDistinctHopsAndSight(t *testing.T) {
	rt, clock, c, mob := wolfFieldFixture(t, 1)
	wearProc(t, c, "ProcChain", 9)
	step, _ := rt.wolfCompanion(testDivision, c, c.ActiveCOS.GID, clock.NowMs())
	at := simulation.Spawn{RegionID: mob.Spawn.RegionID, X: mob.Spawn.X, Y: mob.Spawn.Y, Z: mob.Spawn.Z}
	mobs := []monster.Instance{mob}
	for range 5 {
		at.X += 70
		mobs = append(mobs, wolfSpawnAt(t, rt, c, at, clock.NowMs()))
	}
	rt.petGearChain(step, mob.Gid, simulation.Spawn{RegionID: mob.Spawn.RegionID, X: mob.Spawn.X, Y: mob.Spawn.Y, Z: mob.Spawn.Z}, 1000)
	for i, want := range []uint32{1000, 800, 640, 512, 409, 0} {
		after, _ := rt.Monsters.Get(testDivision, mobs[i].Gid)
		if mobs[i].CurrentHP-after.CurrentHP != want {
			t.Fatalf("hop%d damage=%d want%d", i, mobs[i].CurrentHP-after.CurrentHP, want)
		}
	}
	rt.LineOfSight = func(simulation.Spawn, simulation.NavOwner, simulation.Spawn) bool { return false }
	before, _ := rt.Monsters.Get(testDivision, mob.Gid)
	rt.petGearArea(step, simulation.Spawn{RegionID: mob.Spawn.RegionID, X: mob.Spawn.X, Y: mob.Spawn.Y, Z: mob.Spawn.Z}, 1000, 50)
	after, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if before.CurrentHP != after.CurrentHP {
		t.Fatal("proc bypassed line of sight")
	}
}
