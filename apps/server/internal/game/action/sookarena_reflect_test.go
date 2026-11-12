/*
===========================================================================
sookarena_reflect_test.go - paid hits, death, periodic damage and solo isolation.
===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
)

/*
================
TestSookReflectionPaidDamageAndIsolation
================
*/
func TestSookReflectionPaidDamageAndIsolation(t *testing.T) {
	for _, mode := range []string{"physical", "magic", "periodic", "lethal", "expired", "foreign", "zero"} {
		t.Run(mode, func(t *testing.T) {
			a, b := instanceWorldTestCharacter(11, "Mirror"), instanceWorldTestCharacter(12, "Foreign")
			rt, clock := sookFixture(t, a, b)
			rt.enterSookArena(testDivision, a)
			rt.enterSookArena(testDivision, b)
			run := rt.sookRun(testDivision, a)
			now := clock.Now().UnixMilli()
			run.program.DamageUntil, run.program.DamageMultiplier = now+60000, 10
			item := rt.addCharacterGround(testDivision, a, grounditem.Item{ArenaPowerup: sookReflect, RefObjID: 9272, Codename: sookEgg, DroppedAt: clock.Now()})
			rt.pickupSookPowerup(testDivision, simulation.WorldKey(testDivision, a.Name), a, item)
			if run.program.ReflectUntil != now+30000 || sookPowerDuration(sookReflect) != 30 {
				t.Fatal("reflection egg duration")
			}
			population := run
			if mode == "foreign" {
				population = rt.sookRun(testDivision, b)
			}
			ids := rt.Monsters.SpawnGMMonstersTracked(simulation.GMMonsterSpawn{Division: testDivision, Population: population.lease, RefObjID: 100, Count: 1, Position: population.center, NowMs: now, Arena: true})
			if len(ids) != 1 {
				t.Fatal("spawn")
			}
			target, _ := rt.Monsters.Get(testDivision, ids[0])
			hp := int64(100)
			a.CurrentHP = &hp
			damage := uint32(37)
			if mode == "lethal" {
				damage = 5000
			}
			if mode == "zero" {
				damage = 0
			}
			if mode == "expired" {
				now += 30000
			}
			owner := rt.newPlayerAbnormalOwner(testDivision, a, now)
			if mode == "periodic" {
				owner.Hit(target.Gid, true, damage, abnormalDamageOverTimeReason, abnormal.Poison)
				rt.playerAbnormalPublication(testDivision, a, owner)
			} else {
				formula := combat.Result{Damage: damage, ResultFlags: 1}
				if mode == "magic" {
					formula.MagicalDamage = damage
				}
				struck := rt.strikePlayerInDoor(playerStrike{division: testDivision, victim: a, owner: owner, now: now, killer: deathKiller{monster: &target}, formulas: []combat.Result{formula}})
				rt.playerStruckFrames(testDivision, a, struck, now)
			}
			paid := min(uint32(100), damage)
			if *a.CurrentHP != 100-int64(paid) {
				t.Fatalf("reflection prevented incoming damage: %d", *a.CurrentHP)
			}
			if mode == "foreign" || mode == "expired" {
				paid = 0
			}
			after, _ := rt.Monsters.Get(testDivision, target.Gid)
			if after.CurrentHP != target.CurrentHP-paid*5 {
				t.Fatalf("reflected %d, want %d", target.CurrentHP-after.CurrentHP, paid*5)
			}
			// A repeated publication cannot reflect the same paid debit twice.
			rt.playerAbnormalPublication(testDivision, a, owner)
			again, _ := rt.Monsters.Get(testDivision, target.Gid)
			if again.CurrentHP != after.CurrentHP {
				t.Fatal("duplicate reflection")
			}
		})
	}
}
