/*
===========================================================================
petplume_test.go - Plume timing, tier targets, cleansing and shared reuse.
===========================================================================
*/
package action

import (
	"fmt"
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestCrowPlumeBooks
================
*/
func TestCrowPlumeBooks(t *testing.T) {
	for tier := uint8(1); tier <= 3; tier++ {
		t.Run(fmt.Sprint(tier), func(t *testing.T) {
			rt, clock, c, mob, key := tailwindFixture(t, 1)
			skill := learnPetBook(t, c, 45+tier)
			member := nearbyCharacter(rt, c, 21, "plume-party", 50)
			far := nearbyCharacter(rt, c, 22, "plume-far", 201)
			for _, player := range []*enterworld.Character{c, member, far} {
				player.CurrentHP = testInt64(1)
				player.MissionInventory = nil
				rt.petSessions[petOwnerKey{division: testDivision, name: player.Name}] = &petSession{character: player, ready: true}
			}
			c.ActiveCOS.CurrentHP = 1
			gid := enterworld.ObjectIDForCharacter(member)
			rt.RewardParties = func(string) []RewardParty {
				return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), gid, enterworld.ObjectIDForCharacter(far)}}}
			}
			seedPlayerStatus(rt, c, abnormal.Poison, 10000, clock.NowMs(), mob.Gid)
			seedPlayerStatus(rt, c, abnormal.Burn, 10000, clock.NowMs(), mob.Gid)
			stats, _, err := rt.playerCombatStats(testDivision, c)
			if err != nil {
				t.Fatal(err)
			}
			maxHP, _, _, _ := keeperVitalsFrom(c, stats, nil)
			percent := skill.ValueAt(1)
			initial := int64(float64(maxHP) * percent * 0.5 / 100)
			pulse := int64(float64(maxHP) * percent * 0.1 / 100)
			rt.HandleCosCommand(testDivision, c, trainingCommand(c.ActiveCOS.GID, petTrainingUse, skill.ID))
			if *c.CurrentHP != 1+initial {
				t.Fatal("instant half", *c.CurrentHP, initial)
			}
			if companion.RowReady(&c.ActiveCOS.Training, skill.Row, clock.NowMs()) {
				t.Fatal("shared cooldown not started")
			}
			if (c.ActiveCOS.CurrentHP > 1) != (tier >= 2) {
				t.Fatal("crow healing tier")
			}
			block := rt.playerAbnormal(testDivision, c.Name)
			if block.Has(abnormal.Poison) != (tier == 1) || !block.Has(abnormal.Burn) {
				t.Fatal("must cure exactly one poison/burn")
			}
			if (*member.CurrentHP > 1) != (tier == 3) || *far.CurrentHP != 1 {
				t.Fatal("party tier/range")
			}
			if tier == 3 && *member.CurrentHP != 1+int64(float64(maxHP)*percent*0.5*0.8/100) {
				t.Fatal("party attenuation", *member.CurrentHP)
			}
			clock.now = clock.now.Add(999 * time.Millisecond)
			rt.advanceCrowPlume(key, clock.NowMs())
			if *c.CurrentHP != 1+initial {
				t.Fatal("early pulse")
			}
			clock.now = clock.now.Add(time.Millisecond)
			for i := 1; i <= 5; i++ {
				rt.advanceCrowPlume(key, clock.NowMs())
				if *c.CurrentHP != 1+initial+int64(i)*pulse {
					t.Fatal("pulse", i, *c.CurrentHP)
				}
				clock.now = clock.now.Add(time.Second)
			}
			state := rt.petSessionFor(key.division, key.name, key.gid)
			if state.plume != nil {
				t.Fatal("completed cast retained")
			}
			rt.advanceCrowPlume(key, clock.NowMs())
			if *c.CurrentHP != 1+initial+5*pulse {
				t.Fatal("extra pulse")
			}
		})
	}
}

/*
================
TestCrowPlumeReuseAndSourceRetirement
================
*/
func TestCrowPlumeReuseAndSourceRetirement(t *testing.T) {
	rt, clock, c, _, key := tailwindFixture(t, 1)
	learnPetBook(t, c, 46)
	c.MissionInventory = nil
	c.CurrentHP = testInt64(1)
	command := trainingCommand(c.ActiveCOS.GID, petTrainingUse, 46)
	rt.HandleCosCommand(testDivision, c, command)
	hp := *c.CurrentHP
	state := rt.petSessionFor(key.division, key.name, key.gid)
	cast := state.plume
	rt.HandleCosCommand(testDivision, c, command)
	if *c.CurrentHP != hp || state.plume != cast {
		t.Fatal("reuse bypassed shared cooldown")
	}
	c.ActiveCOS.Summoned = false
	clock.now = clock.now.Add(time.Second)
	rt.advanceCrowPlume(key, clock.NowMs())
	if state.plume != nil || *c.CurrentHP != hp {
		t.Fatal("unsummoned crow retained healing")
	}
}
