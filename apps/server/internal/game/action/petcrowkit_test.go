/*
===========================================================================
petcrowkit_test.go - independent Precision, consumption and expiry.
===========================================================================
*/
package action

import (
	"fmt"
	"testing"
	"time"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestCrowPrecisionBooks
================
*/
func TestCrowPrecisionBooks(t *testing.T) {
	for tier := uint8(1); tier <= 3; tier++ {
		t.Run(fmt.Sprint(tier), func(t *testing.T) {
			rt, clock, c, mob, key := tailwindFixture(t, 1)
			learnPetBook(t, c, 48+tier)
			learnPetBook(t, c, 57+tier)
			member := nearbyCharacter(rt, c, 21, "feather-party", 50)
			member.MissionInventory = nil
			rt.petSessions[petOwnerKey{division: testDivision, name: member.Name}] = &petSession{character: member, ready: true}
			memberGID := enterworld.ObjectIDForCharacter(member)
			rt.RewardParties = func(string) []RewardParty {
				return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), memberGID}}}
			}
			step, ok := rt.elementalCompanion(testDivision, c, key.gid, "Crow", clock.NowMs())
			if !ok {
				t.Fatal("missing crow")
			}
			step.trained = &petTrainedStrike{}
			for range 8 {
				rt.crowAfterAttack(step, []uint32{mob.Gid})
			}
			_, hit := rt.crowPlayerBonuses(testDivision, c, mob.Gid, enterworld.SkillRow{}, clock.NowMs())
			if hit != 10 {
				t.Fatal("precision cap", hit)
			}
			_, partyHit := rt.crowPlayerBonuses(testDivision, member, mob.Gid, enterworld.SkillRow{}, clock.NowMs())
			if (partyHit > 0) != (tier == 3) {
				t.Fatal("precision party tier", partyHit)
			}
			spell := enterworld.SkillRow{Codename: "SKILL_CH_SWORD_TEST", Attack: enterworld.SkillAttack{Present: true}}
			// A blocked action has no committed targets and must retain all stacks.
			rt.crowPlayerAttack(testDivision, c, spell, nil, clock.NowMs())
			if step.state.crow.precision[enterworld.ObjectIDForCharacter(c)].stacks != 5 {
				t.Fatal("blocked action spent precision")
			}
			hits := []combat.Result{{Damage: 1000}}
			rt.crowPlayerDamage(testDivision, c, mob, spell, hits, clock.NowMs())
			if (hits[0].Damage > 1000) != (tier >= 2) {
				t.Fatal("precision damage tier mismatch")
			}
			basic := enterworld.SkillRow{Codename: enterworld.DefaultSkillCodenames(enterworld.ResolveCharacterRaceKey(c))[0], Attack: enterworld.SkillAttack{Present: true}}
			rt.crowPlayerAttack(testDivision, c, basic, []uint32{mob.Gid}, clock.NowMs())
			if step.state.crow.precision[enterworld.ObjectIDForCharacter(c)].stacks != 5 {
				t.Fatal("basic attack spent precision")
			}
			before, _ := rt.crowPlayerBonuses(testDivision, c, mob.Gid, spell, clock.NowMs())
			if tier >= 2 && before < 20 {
				t.Fatal("empowerment missing", before)
			}
			rt.crowPlayerAttack(testDivision, c, spell, []uint32{mob.Gid}, clock.NowMs())
			_, hit = rt.crowPlayerBonuses(testDivision, c, mob.Gid, spell, clock.NowMs())
			if tier >= 2 && hit != 0 || tier == 1 && hit != 10 {
				t.Fatal("wrong consumption", hit)
			}
			if tier == 3 && step.state.crow.precision[memberGID].stacks != 5 {
				t.Fatal("owner consumed party charges")
			}
			clock.now = clock.now.Add(11 * time.Second)
			rt.advanceCrowKit(key, clock.NowMs())
			bonus, hit := rt.crowPlayerBonuses(testDivision, c, mob.Gid, spell, clock.NowMs())
			if bonus != 0 || hit != 0 {
				t.Fatal("expired grant retained", bonus, hit)
			}
			rt.AttackPetExpansion = false
			rt.advanceCrowKit(key, clock.NowMs())
			if len(step.state.crow.precision) != 0 {
				t.Fatal("flag off retained state")
			}
		})
	}
}
