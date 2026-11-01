/*
===========================================================================

reward_party_native_test.go - party EXP/SP shares at the native values

Players compared party and level-gap results with a later-version guide
(#863). These pin the reference server's rules the port follows:
its party kill experience distribution for the party bonus and level
shares, and its party level-difference formula for the
relative-level bonus with its party-only additions.

===========================================================================
*/
package action

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
)

// rewardFactorTolerance absorbs float32 spills; every expected value is
// written as the formula's decimal result.
const rewardFactorTolerance = 1e-6

/*
================
partyMember
================
*/
func partyMember(level int64, european bool) rewardActor {
	c := &enterworld.Character{Level: &level, ModelCodename: "CHAR_CH_MAN_ADVENTURER"}
	if european {
		c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	}
	return rewardActor{character: c}
}

/*
================
partyMonster

A party-grade monster (rarity high nibble 1) of the given level.
================
*/
func partyMonster(level uint8) monster.Instance {
	instance := monster.Instance{Ref: monster.MonsterRef{Level: level}}
	instance.Nest.HasRarityOverride = true
	instance.Nest.RarityOverride = 0x10
	return instance
}

/*
================
assertFactors
================
*/
func assertFactors(t *testing.T, got []float32, want ...float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("factors %v, want %v", got, want)
	}
	for i := range want {
		if math.Abs(float64(got[i])-want[i]) > rewardFactorTolerance {
			t.Fatalf("factor %d is %v, want %v (all %v)", i, got[i], want[i], got)
		}
	}
}

/*
================
TestPartyBonusAndSharesFollowTheNativeDistributor

Natively an all-Chinese party gets +0.1 per extra member. Any European
switches to +0.2 per extra European plus 0.1 per Chinese. Each member's
share is its level over the party's level sum, and a member above 0.7 of
the sum in a party of three or more with someone at level 21+ gets half
the bonus.
================
*/
func TestPartyBonusAndSharesFollowTheNativeDistributor(t *testing.T) {
	ordinary := monster.Instance{Ref: monster.MonsterRef{Level: 30}}
	assertFactors(t, partyRewardFactors([]rewardActor{partyMember(10, false), partyMember(10, false)}, ordinary, false),
		.55, .55)
	assertFactors(t, partyRewardFactors([]rewardActor{partyMember(30, true), partyMember(30, false), partyMember(30, false)}, ordinary, false),
		1.2/3, 1.2/3, 1.2/3)
	assertFactors(t, partyRewardFactors([]rewardActor{partyMember(80, false), partyMember(10, false), partyMember(10, false)}, ordinary, false),
		.8*1.1, .1*1.2, .1*1.2)
	// Two members never halve the bonus, however uneven the levels.
	assertFactors(t, partyRewardFactors([]rewardActor{partyMember(90, false), partyMember(10, false)}, ordinary, false),
		.9*1.1, .1*1.1)
}

/*
================
TestPartyMonsterBelowThePartyAverageHalvesTheShares

Natively a party monster below the party's average level gives
half; at or above the average it gives the full share.
================
*/
func TestPartyMonsterBelowThePartyAverageHalvesTheShares(t *testing.T) {
	members := []rewardActor{partyMember(30, false), partyMember(30, false)}
	assertFactors(t, partyRewardFactors(members, partyMonster(20), false), .275, .275)
	assertFactors(t, partyRewardFactors(members, partyMonster(30), false), .55, .55)
}

/*
================
TestPartyRelativeLevelBonusDoublesForEuropeans

Natively, with EXP share on, a monster 5/7/9+ levels above adds 0.15/0.30/
0.45 (0.30/0.60/0.90 for a European) to the 0.03-per-step relative bonus,
clamped to 1..4. A monster three or more levels below gives exactly 1.
================
*/
func TestPartyRelativeLevelBonusDoublesForEuropeans(t *testing.T) {
	cases := []struct {
		player, monster int64
		european        bool
		want            float64
	}{
		{10, 19, false, 1.45 + 12*.03},
		{10, 19, true, 1.90 + 12*.03},
		{10, 17, false, 1.30 + 10*.03},
		{10, 15, true, 1.30 + 8*.03},
		{10, 13, false, 1 + 6*.03},
		{10, 7, true, 1},
		{10, 40, true, 1.90 + 13*.03},
	}
	for _, c := range cases {
		got := monsterPartyRelativeLevelBonus(c.player, c.monster, c.european)
		if math.Abs(float64(got)-c.want) > rewardFactorTolerance {
			t.Errorf("player %d monster %d european=%v: %v, want %v", c.player, c.monster, c.european, got, c.want)
		}
	}
	// The solo arm has no party additions and stops at 13 steps.
	if got := monsterRelativeLevelBonus(10, 30); math.Abs(float64(got)-1.39) > rewardFactorTolerance {
		t.Errorf("solo bonus %v, want 1.39", got)
	}
}
