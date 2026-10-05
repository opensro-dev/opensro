package action

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
)

type combatRewardLevels struct {
	exp   map[int64]int64
	basis map[int64]int64
	gold  map[int64]int64
}

func (levels combatRewardLevels) SkillPointCost(int64) (int64, bool) { return 0, false }

func (levels combatRewardLevels) ExpRequired(level int64) (int64, bool) {
	value, ok := levels.exp[level]
	return value, ok
}

func (levels combatRewardLevels) MonsterExpBasis(level int64) (int64, bool) {
	value, ok := levels.basis[level]
	return value, ok
}

func (levels combatRewardLevels) WithdrawalGoldBasis(level int64) (int64, bool) {
	value, ok := levels.gold[level]
	return value, ok
}

func testCombatRewardLevels() combatRewardLevels {
	return combatRewardLevels{
		exp: map[int64]int64{1: 118, 4: 1880},
		basis: map[int64]int64{
			1: 24,
			4: 94,
		},
	}
}

func TestMonsterEXPDoesNotSaturateAtSignedDword(t *testing.T) {
	c := &enterworld.Character{Level: testInt64(4), Masteries: []enterworld.CharacterMastery{{ID: 257, Level: 4}}}
	target := rewardTestMangnyang()
	target.Ref.ExpToGive = 3_000_000_000
	// Three levels below: all factors are exactly one. Native 410352
	// returns EDX:EAX; 4EA7D9/4EA87C retain that 64-bit lane.
	exp, _ := monsterKillReward(c, target, testCombatRewardLevels())
	if exp != 3_000_000_000 {
		t.Fatalf("EXP narrowed before sharing: %d", exp)
	}
}

func TestMonsterContributionHasNativeMinimumFraction(t *testing.T) {
	c := &enterworld.Character{Level: testInt64(4), Masteries: []enterworld.CharacterMastery{{ID: 257, Level: 4}}}
	target := rewardTestMangnyang()
	target.Ref.MaxHP = 1_000_000_000
	target.Ref.ExpToGive = 1_000_000_000
	exp, _ := monsterContributionReward(c, target, testCombatRewardLevels(), 1, 1, false)
	// B45C84 is slightly below 1e-6; the unspilled EXP product truncates
	// to 999. Spilling the product to float32 here incorrectly yields 1000.
	if exp != 999 {
		t.Fatalf("fraction clamp or premature EXP spill: %d", exp)
	}
}

func rewardTestMangnyang() monster.Instance {
	return monster.Instance{Ref: monster.MonsterRef{
		Level:              1,
		MaxHP:              54,
		ExpToGive:          24,
		RewardActionPinned: true,
	}}
}

func TestMonsterKillRewardPortsTheTwoNativeFormulaLanes(t *testing.T) {
	levels := testCombatRewardLevels()
	tests := []struct {
		name      string
		race      int64
		model     string
		level     int64
		mastery   int64
		wantExp   int64
		wantSkill int64
	}{
		{
			name: "Chinese equal-level mastery",
			race: enterworld.RaceChina, model: "CHAR_CH_MAN_ADVENTURER",
			level: 1, mastery: 1, wantExp: 26, wantSkill: 109,
		},
		{
			name: "European equal-level mastery",
			race: enterworld.RaceEurope, model: "CHAR_EU_MAN_ADVENTURER",
			level: 1, mastery: 1, wantExp: 26, wantSkill: 109,
		},
		{
			name: "level-four three-level mastery gap",
			race: enterworld.RaceChina, model: "CHAR_CH_MAN_ADVENTURER",
			level: 4, mastery: 1, wantExp: 16, wantSkill: 33,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			character := &enterworld.Character{
				ModelCodename: testCase.model,
				RaceIndex:     testInt64(testCase.race),
				Level:         testInt64(testCase.level),
				Masteries: []enterworld.CharacterMastery{{
					ID:    257,
					Level: testCase.mastery,
				}},
			}
			exp, skillExp := monsterKillReward(character, rewardTestMangnyang(), levels)
			if exp != testCase.wantExp || skillExp != testCase.wantSkill {
				t.Fatalf("reward = %d/%d, want EXP/SEXP %d/%d", exp, skillExp, testCase.wantExp, testCase.wantSkill)
			}
		})
	}
}

func TestMonsterKillRewardFailsClosedWithoutTheSharedLevelBasis(t *testing.T) {
	character := &enterworld.Character{
		Level:     testInt64(1),
		Masteries: []enterworld.CharacterMastery{{ID: 257, Level: 1}},
	}
	for name, levels := range map[string]enterworld.LevelDataSource{
		"nil table":         nil,
		"missing basis row": combatRewardLevels{exp: map[int64]int64{1: 118}},
	} {
		t.Run(name, func(t *testing.T) {
			exp, skillExp := monsterKillReward(character, rewardTestMangnyang(), levels)
			if exp != 0 || skillExp != 0 {
				t.Fatalf("degraded reward = %d/%d, want atomic refusal", exp, skillExp)
			}
		})
	}
}

func TestMonsterRewardGradeMultiplierIsIndependentFromMaxHPScaling(t *testing.T) {
	tests := []struct {
		rarity uint8
		want   float32
	}{
		{0x00, 1},
		{0x01, 2},
		{0x04, 15},
		{0x05, 60},
		{0x06, 4},
		{0x07, 30},
		{0x10, 7.5},
		{0x11, 15},
		{0x14, 112.5},
		{0x15, 450},
		{0x16, 30},
		{0x17, 225},
	}
	for _, testCase := range tests {
		if got := monsterRewardGradeMultiplier(testCase.rarity); got != testCase.want {
			t.Errorf("rarity 0x%02X reward multiplier = %g, want %g", testCase.rarity, got, testCase.want)
		}
	}
}

func TestMonsterLevelGapRewardScalePortsTheDiscontinuousRetailBranches(t *testing.T) {
	tests := []struct {
		player, monster int64
		want            float32
	}{
		{10, 10, 1},
		{10, 7, 1},
		{10, 6, 0.9},
		{10, 4, 0.9},
		{10, 3, 0.4},
		{10, 2, 0.25},
		{10, 1, 0.1},
		{10, 20, 1},
		{10, 21, 0.98},
		{10, 100, 0.1},
	}
	for _, testCase := range tests {
		got := monsterLevelGapRewardScale(testCase.player, testCase.monster)
		if math.Abs(float64(got-testCase.want)) > 0.00001 {
			t.Errorf("level scale player=%d monster=%d = %g, want %g", testCase.player, testCase.monster, got, testCase.want)
		}
	}
}

func TestMasteryGapRatesClampBothPersistedDirections(t *testing.T) {
	for _, testCase := range []struct {
		level, mastery     int64
		wantExp, wantSkill float32
	}{
		{4, 1, 0.7, 1.3},
		{20, 0, 0.1, 1.9},
		{1, 20, 1.9, 0.1},
	} {
		expRate, skillRate := masteryGapProgressionRates(testCase.level, testCase.mastery)
		if math.Abs(float64(expRate-testCase.wantExp)) > 0.00001 ||
			math.Abs(float64(skillRate-testCase.wantSkill)) > 0.00001 {
			t.Errorf("rates level/mastery %d/%d = %g/%g, want %g/%g",
				testCase.level, testCase.mastery, expRate, skillRate, testCase.wantExp, testCase.wantSkill)
		}
	}
}
