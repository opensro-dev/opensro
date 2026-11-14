/*
===========================================================================
petgearproc_test.go - striking/struck proc isolation and credited spell damage
===========================================================================
*/
package action

import (
	"encoding/binary"
	"errors"
	"fmt"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"strings"
	"testing"
	"time"
)

/*
================
TestPetGearDamageRollFailureDoesNotSpendProc
================
*/
func TestPetGearDamageRollFailureDoesNotSpendProc(t *testing.T) {
	rt, clock, c, mob := wolfFieldFixture(t, 1)
	wearProc(t, c, "ProcFire", 9)
	calls := 0
	rt.CombatRoll = func() (uint32, error) {
		calls++
		if calls > 1 {
			return 0, errors.New("damage RNG unavailable")
		}
		return 0, nil
	}
	rt.petGearProcs(testDivision, c, c.ActiveCOS.GID, mob.Gid, simulation.Spawn{RegionID: mob.Spawn.RegionID, X: mob.Spawn.X, Y: mob.Spawn.Y, Z: mob.Spawn.Z}, false, clock.NowMs())
	after, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if after.CurrentHP != mob.CurrentHP || rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID).trained.procReadyAt != 0 {
		t.Fatal("failed damage roll dealt damage or spent cooldown")
	}
}

/*
================
TestPetGearProcRangeEndpoints
================
*/
func TestPetGearProcRangeEndpoints(t *testing.T) {
	for kind, group := range []string{"ProcChain", "ProcBlizzard", "ProcFire"} {
		for _, degree := range []int{1, 9} {
			for _, taken := range []bool{false, true} {
				for end := range 2 {
					t.Run(fmt.Sprint(group, degree, taken, end), func(t *testing.T) {
						rt, clock, c, mob := wolfFieldFixture(t, 1)
						name := group
						if taken {
							name += "Taken"
						}
						wearProc(t, c, name, degree)
						span := companion.D.Gear[degree-1].ProcRanges[kind]
						draw := uint32(0)
						if end == 1 {
							draw = span[1] - span[0]
						}
						rt.CombatRoll = func() (uint32, error) { return draw, nil }
						now := clock.NowMs()
						rt.petGearProcs(testDivision, c, c.ActiveCOS.GID, mob.Gid, simulation.Spawn{RegionID: mob.Spawn.RegionID, X: mob.Spawn.X, Y: mob.Spawn.Y, Z: mob.Spawn.Z}, taken, now)
						for i := int64(1); i <= 3; i++ {
							rt.advancePetGearFields(petOwnerKey{testDivision, strings.ToLower(c.Name), c.ActiveCOS.GID}, now+i*1000)
						}
						after, _ := rt.Monsters.Get(testDivision, mob.Gid)
						if got := mob.CurrentHP - after.CurrentHP; got != span[end] {
							t.Fatalf("damage=%d want=%d", got, span[end])
						}
					})
				}
			}
		}
	}
}

/*
================
TestPetGearFireBurnAndBlizzardControl
================
*/
func TestPetGearFireBurnAndBlizzardControl(t *testing.T) {
	for _, kind := range []int{companion.ProcFire, companion.ProcBlizzard} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			rt, clock, c, mob := wolfFieldFixture(t, 1)
			rt.Monsters.SetAbnormalContext(monsterAbnormalContext{rt})
			rt.CombatRoll = func() (uint32, error) { return 0, nil }
			step, _ := rt.wolfCompanion(testDivision, c, c.ActiveCOS.GID, clock.NowMs())
			at := simulation.Spawn{RegionID: mob.Spawn.RegionID, X: mob.Spawn.X, Y: mob.Spawn.Y, Z: mob.Spawn.Z}
			rt.petGearArea(step, at, 1000, 50, kind, false)
			live, _ := rt.Monsters.Get(testDivision, mob.Gid)
			if live.Abnormal == nil {
				t.Fatal("gear proc installed no native status")
			}
			if kind == companion.ProcFire {
				if !live.Abnormal.Has(abnormal.Burn) {
					t.Fatal("fire did not burn")
				}
				clock.Advance(2100 * time.Millisecond)
				rt.advanceMonsterAbnormals(clock.NowMs())
				after, _ := rt.Monsters.Get(testDivision, mob.Gid)
				if after.CurrentHP >= live.CurrentHP {
					t.Fatal("burn status did not deal periodic damage")
				}
			} else {
				if !live.Abnormal.Has(abnormal.Frostbite) || live.Abnormal.Has(abnormal.Freeze) {
					t.Fatal("early Blizzard pulse did not slow exclusively")
				}
				rt.petGearArea(step, at, 1000, 50, kind, true)
				after, _ := rt.Monsters.Get(testDivision, mob.Gid)
				if !after.Abnormal.Has(abnormal.Freeze) {
					t.Fatal("final Blizzard pulse could not freeze")
				}
			}
		})
	}
}

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
				want := uint32(1000)
				if group == "ProcChain" {
					want = 1500
				}
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
				} else if mob.CurrentHP-after.CurrentHP != want {
					t.Fatalf("total damage=%d, want%d", mob.CurrentHP-after.CurrentHP, want)
				}
				var shown uint32
				var visuals int
				for _, batch := range rt.drainMonsterLegRecipients() {
					for _, frame := range batch.Frames {
						if frame.Opcode == abnormalDamageCreditOpcode {
							if batch.OnlyCharacterID != c.ID {
								t.Fatal("proc number leaked to wrong recipient")
							}
							shown += binary.LittleEndian.Uint32(frame.Payload[4:])
						}
						if frame.Opcode == 0x324b {
							visuals++
						}
					}
				}
				if group != "ProcMend" && shown != want {
					t.Fatal("missing visible proc damage", group, shown)
				}
				if visuals == 0 {
					t.Fatal("missing proc animation", group)
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
	t.Run("five-percent-charm", func(t *testing.T) {
		rt, clock, c, mob := wolfFieldFixture(t, 1)
		charm := companion.D.Gear[0].Items[2]
		c.ActiveCOS.Training.Gear[2] = domain.PetGear{RefObjID: charm.ID}
		c.ActiveCOS.Training.Gear[2].Options[0] = companion.EncodeAffix(84, 5)
		if companion.ModifiersFor(c.ActiveCOS).Procs[companion.ProcChain][0].Chance != 5 {
			t.Fatal("charm lost 5 percent roll")
		}
		rt.CombatRoll = func() (uint32, error) { return 499, nil }
		if !rt.petChance(5) {
			t.Fatal("5 percent excluded roll499")
		}
		rt.CombatRoll = func() (uint32, error) { return 500, nil }
		if rt.petChance(5) {
			t.Fatal("5 percent included roll500")
		}
		rt.CombatRoll = func() (uint32, error) { return 0, nil }
		state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
		attackUntil(t, rt, clock, c, mob, func() bool { return state.trained.procReadyAt > 0 })
		if state.trained.procReadyAt == 0 {
			t.Fatal("5 percent charm never entered committed proc hook")
		}
	})
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
			at := state.mover.AnyFollower().Position(clock.NowMs())
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
	rt.petGearArea(step, simulation.Spawn{RegionID: mob.Spawn.RegionID, X: mob.Spawn.X, Y: mob.Spawn.Y, Z: mob.Spawn.Z}, 1000, 50, companion.ProcFire, false)
	after, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if before.CurrentHP != after.CurrentHP {
		t.Fatal("proc bypassed line of sight")
	}
}
