/*
===========================================================================
petcounter_test.go - bear retaliation hit doors, tier ownership and area rolls
===========================================================================
*/
package action

import (
	"encoding/binary"
	"fmt"
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// All attackers charge one owner counter; the tenth can be a first-time hitter.
func TestPetCounterGroupedOwner(t *testing.T) {
	for _, draw := range []uint32{4999, 5000, 0} {
		t.Run(fmt.Sprint(draw), func(t *testing.T) {
			rt, clock, c, original := newPetCombatRuntime(t, 100000, attackPetBand)
			makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
			rt.AttackPetExpansion = true
			ref := original.Ref
			ref.Level = 90
			rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{ref.RefObjID: ref}, nil))
			rt.Monsters.SetAbnormalContext(monsterAbnormalContext{rt})
			rt.Monsters.SetTimeSource(clock.Now)
			rt.Monsters.StartDivision(testDivision)
			first := roarField(t, rt, c, clock)
			learnPetBook(t, c, 30)
			at := rt.LiveSpawnFor(testDivision, c)
			lease, _ := rt.casterPopulation(testDivision, c)
			gids := []uint32{first.Gid}
			for _, distance := range []float64{40, 50, 60, 70, 81} {
				point := at
				point.X += distance
				rt.Monsters.SpawnGMMonsters(simulation.GMMonsterSpawn{Division: testDivision, Population: lease, RefObjID: ref.RefObjID, Count: 1, Position: point, NowMs: clock.NowMs()})
				rows := rt.Monsters.CombatCandidatesInPopulation(testDivision, lease, point, 1, clock.NowMs(), true)
				if len(rows) != 1 {
					t.Fatal("missing group fixture")
				}
				gids = append(gids, rows[0].Gid)
			}
			state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
			bearAt := at
			bearAt.X += 300
			state.mover.FollowFrom(c.ActiveCOS.GID, bearAt)
			c.MissionInventory = []enterworld.InventoryRow{{Slot: 13, Summon: c.ActiveCOS}}
			c.ActiveCOS = nil
			rolls := 0
			rt.CombatRoll = func() (uint32, error) {
				rolls++
				if draw == 0 && rolls%2 == 0 {
					return 9999, nil
				}
				return draw, nil
			}
			hit := petCounterHit{division: testDivision, owner: c, victim: enterworld.ObjectIDForCharacter(c), formulas: []combat.Result{{Damage: 1}}, now: clock.NowMs()}
			for i := 0; i < 9; i++ {
				hit.attacker = gids[i%4]
				rt.petCounterStun(hit)
			}
			for _, gid := range gids {
				mob, _ := rt.Monsters.Get(testDivision, gid)
				if mob.Abnormal != nil && mob.Abnormal.Slots[abnormal.Stun].Active {
					t.Fatal("stunned before ten combined hits")
				}
			}
			hit.attacker = gids[4]
			pet := c.MissionInventory[0].Summon
			petRef, _ := rt.cosReference(pet)
			step := petCombatStep{key: petOwnerKey{division: testDivision}, snapshot: c, pet: pet, ref: petRef, state: state, nowMs: clock.NowMs()}
			if _, ok := rt.resolvePetCombatTarget(step, hit.attacker); !ok {
				t.Fatal("cannot resolve grouped attacker")
			}
			frames := rt.petCounterStun(hit)
			pulses := 0
			for _, frame := range frames {
				if frame.Opcode == petCounterPulseOpcode {
					pulses++
					if len(frame.Payload) != 4 || binary.LittleEndian.Uint32(frame.Payload) != hit.victim {
						t.Fatal("pulse not centered on hit recipient")
					}
				}
			}
			if pulses != 1 {
				t.Fatalf("got %d pulse cues, want one", pulses)
			}
			for i, gid := range gids {
				mob, _ := rt.Monsters.Get(testDivision, gid)
				stunned := mob.Abnormal != nil && mob.Abnormal.Slots[abnormal.Stun].Active
				want := i == 4 || i < 4 && (draw == 4999 || draw == 0 && i%2 == 0)
				if stunned != want {
					t.Fatalf("monster%d draw%d stunned=%v want=%v petLevel=%d mobLevel=%d", i, draw, stunned, want, c.MissionInventory[0].Summon.Level, mob.Ref.Level)
				}
			}
			if rolls != 4 {
				t.Fatalf("aura made %d rolls, want one per nearby extra", rolls)
			}
			if state.trained.counterHits != [2]int{} {
				t.Fatal("counter did not reset")
			}
		})
	}
}

/*
================
TestPetCounterRealHits

Use both actual monster hit doors. Full absorption keeps the fixture alive
and proves that counting does not depend on HP loss or the damage school.
================
*/
func TestPetCounterRealHits(t *testing.T) {
	for _, tier := range []uint8{1, 2, 3} {
		for _, ownerTarget := range []bool{false, true} {
			for _, flags := range []uint32{5, 9} {
				t.Run(fmt.Sprintf("tier%d/owner%v/flags%d", tier, ownerTarget, flags), func(t *testing.T) {
					rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
					makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
					c.MissionInventory = nil
					rt.AttackPetExpansion = true
					rt.CombatRoll = func() (uint32, error) { return 100, nil }
					mob := roarField(t, rt, c, clock)
					learnPetBook(t, c, 27+tier)
					skills := rt.deps.SkillData().(staticSkillSource)
					row := skills[2]
					row.ActionCastingTimeMs = 0
					row.ActionRange = 100
					row.Attack.ImpactCount = 1
					row.Attack.Min, row.Attack.Max, row.Attack.Percent, row.Attack.Flags = 300, 300, 100, flags
					skills[2] = row
					mob.Ref.DefaultSkillIDs[0] = 2
					if _, _, err := rt.playerCombatStats(testDivision, c); err != nil {
						t.Fatal(err)
					}
					victim := c.ActiveCOS.GID
					if ownerTarget {
						victim = enterworld.ObjectIDForCharacter(c)
					}
					key := petShieldKey{testDivision, victim}
					rt.petShields.rows = map[petShieldKey]petShield{key: {key: key, amount: 1000000, until: clock.NowMs() + 5000}}
					// Production summons belong to their bag item, not the legacy transport slot.
					c.MissionInventory = []enterworld.InventoryRow{{Slot: 13, Summon: c.ActiveCOS}}
					c.ActiveCOS = nil
					for hit := 1; hit <= petCounterHits; hit++ {
						result := rt.MonsterBasicAttack(testDivision, mob, victim, 2, clock.NowMs())
						if !result.Accepted {
							t.Fatalf("hit %d refused: %+v", hit, result)
						}
						live, _ := rt.Monsters.Get(testDivision, mob.Gid)
						stunned := live.Abnormal != nil && live.Abnormal.Slots[abnormal.Stun].Active
						want := hit == petCounterHits && (!ownerTarget || tier >= 2)
						if stunned != want {
							t.Fatalf("hit %d: stunned=%v want=%v", hit, stunned, want)
						}
						if live.CurrentHP != mob.CurrentHP {
							t.Fatal("retaliation dealt damage")
						}
					}
				})
			}
		}
	}
}

/*
================
TestPetCounterAreaAndAdmission

Deterministic draws pin the 50% boundary; an 8.1m monster remains untouched.
Counter state is transient and separate for owner and bear.
================
*/
func TestPetCounterAreaAndAdmission(t *testing.T) {
	for _, draw := range []uint32{4999, 5000} {
		t.Run(fmt.Sprint(draw), func(t *testing.T) {
			rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
			makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
			rt.AttackPetExpansion = true
			attacker := roarField(t, rt, c, clock)
			learnPetBook(t, c, 30)
			at := rt.LiveSpawnFor(testDivision, c)
			lease, _ := rt.casterPopulation(testDivision, c)
			var extras []uint32
			for _, distance := range []float64{79, 81} {
				point := at
				point.X += distance
				rt.Monsters.SpawnGMMonsters(simulation.GMMonsterSpawn{Division: testDivision, Population: lease, RefObjID: 1933, Count: 1, Position: point, NowMs: clock.NowMs()})
				rows := rt.Monsters.CombatCandidatesInPopulation(testDivision, lease, point, 1, clock.NowMs(), true)
				if len(rows) != 1 {
					t.Fatal("missing range fixture")
				}
				extras = append(extras, rows[0].Gid)
			}
			state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
			hit := petCounterHit{division: testDivision, owner: c, victim: c.ActiveCOS.GID, attacker: attacker.Gid,
				formulas: []combat.Result{{Damage: 1, Blocked: true}, {}}, now: clock.NowMs()}
			rt.petCounterStun(hit)
			if state.trained.counterHits != [2]int{} {
				t.Fatal("block/miss counted")
			}
			hit.formulas = []combat.Result{{Damage: 1}}
			for range 9 {
				rt.petCounterStun(hit)
			}
			hit.victim = enterworld.ObjectIDForCharacter(c)
			rt.petCounterStun(hit)
			if state.trained.counterHits != [2]int{9, 1} {
				t.Fatal("owner and bear counters combined")
			}
			hit.victim = c.ActiveCOS.GID
			// Native guaranteed stun uses no chance draw; only the extra aura does.
			rt.CombatRoll = func() (uint32, error) { return draw, nil }
			rt.petCounterStun(hit)
			for i, gid := range extras {
				mob, _ := rt.Monsters.Get(testDivision, gid)
				stunned := mob.Abnormal != nil && mob.Abnormal.Slots[abnormal.Stun].Active
				if stunned != (i == 0 && draw == 4999) {
					t.Fatalf("extra%d draw%d stunned=%v", i, draw, stunned)
				}
			}
			rt.AttackPetExpansion = false
			before := state.trained.counterHits
			rt.petCounterStun(hit)
			if state.trained.counterHits != before {
				t.Fatal("native-off counted hits")
			}
		})
	}
}
