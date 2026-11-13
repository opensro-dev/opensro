/*
===========================================================================
petcrowkit_test.go - cumulative Refuge admission, threshold, healing and cooldown.
===========================================================================
*/
package action

import (
	"encoding/json"
	"fmt"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"testing"
)

/*
================
TestRavenRefugeBooks
================
*/
func TestRavenRefugeBooks(t *testing.T) {
	for tier := uint8(1); tier <= 3; tier++ {
		t.Run(fmt.Sprint(tier), func(t *testing.T) {
			rt, clock, c, _, _ := tailwindFixture(t, 1)
			learnPetBook(t, c, 57+tier)
			member := nearbyCharacter(rt, c, 21, "refuge-party", 50)
			member.MissionInventory = nil
			rt.petSessions[petOwnerKey{division: testDivision, name: member.Name}] = &petSession{character: member, ready: true}
			rt.RewardParties = func(string) []RewardParty {
				return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(member)}}}
			}
			grant := rt.planRavenRefuge(testDivision, c, clock.NowMs())
			if grant.book != 57+tier || ravenRefugeDamage(grant, 1, 1000, 100) != 85 {
				t.Fatal("protection missing", grant)
			}
			party := rt.planRavenRefuge(testDivision, member, clock.NowMs())
			if (party.book != 0) != (tier == 3) {
				t.Fatal("party admission", party)
			}
			maximum := int64(1000)
			before := int64(grant.threshold*10) + 1
			after := before - 20
			heal := ravenRefugeRecovery(grant, c, before, after, maximum, clock.NowMs())
			if (heal == 100) != (tier >= 2) {
				t.Fatal("heal tier", heal)
			}
			if tier >= 2 {
				deadline := c.RavenRefugeReadyAtMs
				if ravenRefugeRecovery(grant, c, before, after, maximum, deadline-1) != 0 {
					t.Fatal("early recovery")
				}
				if ravenRefugeRecovery(grant, c, before, after, maximum, deadline) != 100 {
					t.Fatal("deadline recovery")
				}
				if member.RavenRefugeReadyAtMs != 0 {
					t.Fatal("shared recipient cooldown")
				}
				raw, _ := json.Marshal(c)
				var copy enterworld.Character
				if err := json.Unmarshal(raw, &copy); err != nil || copy.RavenRefugeReadyAtMs != c.RavenRefugeReadyAtMs {
					t.Fatal("cooldown persistence", err)
				}
			}
			rt.AttackPetExpansion = false
			if rt.planRavenRefuge(testDivision, c, clock.NowMs()).book != 0 {
				t.Fatal("flag off changed native damage")
			}
		})
	}
}

/*
================
TestRavenRefugeBoundaryAndFatalHits
================
*/
func TestRavenRefugeBoundaryAndFatalHits(t *testing.T) {
	c := &enterworld.Character{}
	grant := ravenRefugeGrant{threshold: 40, cooldown: 120000, book: 60}
	if ravenRefugeDamage(grant, 400, 1000, 100) != 100 || ravenRefugeDamage(grant, 399, 1000, 100) != 85 {
		t.Fatal("strict threshold")
	}
	for _, pair := range [][2]int64{{410, 0}, {390, 300}, {410, 400}, {410, 410}} {
		if ravenRefugeRecovery(grant, c, pair[0], pair[1], 1000, 1) != 0 || c.RavenRefugeReadyAtMs != 0 {
			t.Fatal("invalid crossing spent recovery", pair)
		}
	}
	if ravenRefugeRecovery(grant, c, 410, 390, 1000, 1) != 100 || c.RavenRefugeReadyAtMs != 120001 {
		t.Fatal("survived crossing")
	}
}

/*
================
TestRavenRefugeCommitsDamageAndHealing
================
*/
func TestRavenRefugeCommitsDamageAndHealing(t *testing.T) {
	rt, clock, c, mob, _ := tailwindFixture(t, 1)
	learnPetBook(t, c, 60)
	maximum, _, _, _ := rt.playerKeeperVitals(testDivision, c)
	grant := rt.planRavenRefuge(testDivision, c, clock.NowMs())
	before := int64(float64(maximum)*grant.threshold/100) + 1
	damage := uint32(maximum / 20)
	c.CurrentHP = testInt64(before)
	strike := playerStrike{division: testDivision, victim: c, killer: deathKiller{monster: &mob}, now: clock.NowMs(), impacts: 1}
	resolve := func(*enterworld.SkillWall) (combat.WallOutcome, error) {
		return combat.WallOutcome{Defender: combat.Result{Damage: damage}}, nil
	}
	roll := func(*enterworld.SkillWall, combat.Result) ([]abnormal.Record, error) { return nil, nil }
	if !rt.planPlayerStrike(&strike, resolve, roll) {
		t.Fatal("strike rejected")
	}
	var out playerStruck
	rt.deps.UpdateDeferred(c, "refuge-test", func() bool { out = rt.strikePlayerInDoor(strike); return true })
	expected := before - int64(damage) + maximum/10
	if !out.refugeHealed || *c.CurrentHP != expected || c.RavenRefugeReadyAtMs != clock.NowMs()+grant.cooldown {
		t.Fatal("atomic heal", *c.CurrentHP, expected, c.RavenRefugeReadyAtMs)
	}
	// A later hit while already below the threshold gets protection, not another heal.
	c.CurrentHP = testInt64(maximum / 10)
	strike.formulas = []combat.Result{{Damage: damage}}
	out = rt.strikePlayerInDoor(strike)
	if out.impacts[0].Damage != uint32(uint64(damage)*85/100) || out.refugeHealed {
		t.Fatal("protection or reuse", out)
	}
}

/*
================
TestRavenRefugePeriodicMonsterDamage
================
*/
func TestRavenRefugePeriodicMonsterDamage(t *testing.T) {
	rt, clock, c, mob, _ := tailwindFixture(t, 1)
	learnPetBook(t, c, 60)
	grant := rt.planRavenRefuge(testDivision, c, clock.NowMs())
	maximum, _, _, _ := rt.playerKeeperVitals(testDivision, c)
	before := int64(float64(maximum)*grant.threshold/100) + 1
	damage := uint32(maximum / 20)
	c.CurrentHP = testInt64(before)
	o := rt.newPlayerAbnormalOwner(testDivision, c, clock.NowMs())
	o.refuge = grant
	o.sources = map[uint32]abnormalSourceState{mob.Gid: {killer: deathKiller{monster: &mob}}}
	o.Hit(mob.Gid, false, damage, 0, abnormal.Poison)
	if *c.CurrentHP != before-int64(damage)+maximum/10 || c.RavenRefugeReadyAtMs == 0 {
		t.Fatal("periodic crossing did not recover")
	}
	c.CurrentHP = testInt64(maximum / 10)
	o.Hit(mob.Gid, false, damage, 0, abnormal.Poison)
	if *c.CurrentHP != maximum/10-int64(uint64(damage)*85/100) {
		t.Fatal("periodic damage not reduced")
	}
}
