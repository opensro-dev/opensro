/*
===========================================================================
petcrow_test.go - Mana Spring tiers, actual damage, party range and reuse.
===========================================================================
*/
package action

import (
	"fmt"
	"opensro.online/server/internal/domain"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestCrowManaSpringBooks
================
*/
func TestCrowManaSpringBooks(t *testing.T) {
	for tier := uint8(1); tier <= 3; tier++ {
		t.Run(fmt.Sprint(tier), func(t *testing.T) {
			rt, clock, c, _, key := tailwindFixture(t, 1)
			skill := learnPetBook(t, c, 51+tier)
			for rank := uint8(1); rank <= skill.MaxLevel; rank++ {
				if got := skill.ValueAt(rank); got != 10 {
					t.Fatalf("rank %d restores %v%%, want 10%%", rank, got)
				}
			}
			c.ActiveCOS.Training.Rows[1] = domain.PetRowCooldown{ReadyAtMs: clock.NowMs() + 1000}
			member := nearbyCharacter(rt, c, 21, "spring-party", 50)
			far := nearbyCharacter(rt, c, 22, "spring-far", 201)
			for _, player := range []*enterworld.Character{c, member, far} {
				player.MissionInventory = nil
				mp := int64(1)
				player.CurrentMP = &mp
				player.OffensiveSkillCooldowns = map[uint32]int64{1: clock.NowMs() + 1000, 2: clock.NowMs() + 100}
				player.SharedSkillCooldowns = map[uint8]int64{1: clock.NowMs() + 1000}
				rt.petSessions[petOwnerKey{division: testDivision, name: player.Name}] = &petSession{character: player, ready: true}
			}
			member.ActiveCOS = nil
			far.ActiveCOS = nil
			gid := enterworld.ObjectIDForCharacter(member)
			rt.RewardParties = func(string) []RewardParty {
				return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), gid, enterworld.ObjectIDForCharacter(far)}}}
			}
			step, ok := rt.elementalCompanion(testDivision, c, key.gid, "Crow", clock.NowMs())
			if !ok {
				t.Fatal("crow unavailable")
			}
			step.trained = &petTrainedStrike{}
			rt.crowManaSpring(step, 200)
			if *c.CurrentMP != 21 {
				t.Fatal("actual damage MP", *c.CurrentMP)
			}
			want := int64(1)
			if tier >= 2 {
				want = 21
			}
			if *member.CurrentMP != want || *far.CurrentMP != 1 {
				t.Fatal("party MP/range", *member.CurrentMP, *far.CurrentMP)
			}
			petRemaining := c.ActiveCOS.Training.Rows[1].ReadyAtMs - clock.NowMs()
			expected := int64(1000)
			if tier == 3 {
				expected = 500
			}
			if petRemaining != expected {
				t.Fatal("pet reuse tier", petRemaining)
			}
			remaining := c.OffensiveSkillCooldowns[1] - clock.NowMs()
			want = 1000
			if tier == 3 {
				want = 500
			}
			if remaining != want || c.SharedSkillCooldowns[1]-clock.NowMs() != want {
				t.Fatal("reuse paid per impact", remaining)
			}
			if tier == 3 && (member.OffensiveSkillCooldowns[1]-clock.NowMs() != 500 || c.OffensiveSkillCooldowns[2] != clock.NowMs()) {
				t.Fatal("party reuse or zero clamp")
			}
			rt.crowManaSpring(step, 0)
			if *c.CurrentMP != 21 || c.OffensiveSkillCooldowns[1]-clock.NowMs() != want {
				t.Fatal("blocked attack granted recovery")
			}
			rt.AttackPetExpansion = false
			rt.crowManaSpring(step, 200)
			if *c.CurrentMP != 21 {
				t.Fatal("flag off granted recovery")
			}
		})
	}
}

/*
================
TestCrowManaSpringActualKillDebit
================
*/
func TestCrowManaSpringActualKillDebit(t *testing.T) {
	rt, clock, c, mob := newPetCombatRuntime(t, 100, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
	rt.AttackPetExpansion = true
	learnPetBook(t, c, 52)
	c.MissionInventory = nil
	mp := int64(1)
	c.CurrentMP = &mp
	rt.HandleCosCommand(testDivision, c, petAttackOrder(c.ActiveCOS.GID, mob.Gid))
	tickPetCombat(t, rt, clock, 100, func() bool {
		live, ok := rt.Monsters.Get(testDivision, mob.Gid)
		return !ok || live.CurrentHP == 0
	})
	if *c.CurrentMP <= 1 || *c.CurrentMP > 11 {
		t.Fatal("recovery must use at most the monster's 100 HP, not overkill", *c.CurrentMP)
	}
}
