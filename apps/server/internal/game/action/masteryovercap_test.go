/*
===========================================================================

masteryovercap_test.go - a beta character over the native total trains again

Issue #3551. Characters that trained past the native total mastery budget
under the beta allowance (SRO_BETA_MASTERY, 5000) refuse every raise once
the native budget is back (Chinese 300, European min(2 x level, 240)). The
owner's way out (2026-11-15) is the native one: the Item Mall's skill
restoration potion lowers a mastery one level per potion with all its SP
returned, and once the total is under the budget the main tree trains.
The test drives the real withdrawal and training handlers through the
inventory owner, for both races.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
)

const (
	overCapMallPotionID uint32 = 3828
	overCapMallPotion          = "ITEM_MALL_SKILL_RESTORATION_POTION"
	overCapSkillPoints  int64  = 1000
	overCapLevelCost    int64  = 10
	overCapExcess       int64  = 3
	overCapMainLevel    int64  = 50
	overCapMainSkill    uint32 = 103
)

/*
================
overCapLevels

Every mastery level costs the same SP, so the refund of each withdrawn
level and the price of the next raise are both overCapLevelCost.
================
*/
type overCapLevels struct{}

/*
================
SkillPointCost
================
*/
func (overCapLevels) SkillPointCost(level int64) (int64, bool) { return overCapLevelCost, level > 0 }

/*
================
ExpRequired
================
*/
func (overCapLevels) ExpRequired(int64) (int64, bool) { return 0, false }

/*
================
MonsterExpBasis
================
*/
func (overCapLevels) MonsterExpBasis(int64) (int64, bool) { return 0, false }

/*
================
overCapCharacter

A character of the named race over its native budget by overCapExcess:
the main mastery sits at overCapMainLevel, below the character level, and
is held by a learned skill at that level, so only
the other masteries can be withdrawn, as a player keeping their main tree
would. Answers the character and its native budget.
================
*/
func overCapCharacter(t *testing.T, codename string, race, level int64) (*enterworld.Character, int64) {
	t.Helper()
	c := testCharacter()
	c.ModelCodename = codename
	c.RaceIndex = &race
	c.Level, c.MaxLevel = &level, &level
	sp := overCapSkillPoints
	c.SkillPoints = &sp
	c.Masteries = enterworld.DefaultMasteries(enterworld.ResolveCharacterRaceKey(c))
	budget := progression.TotalMasteryCapChina
	if enterworld.NativeCountryByte9C(c) != 0 {
		budget = min(progression.TotalMasteryEuropePerLevel*level, progression.TotalMasteryCapEuropeCeiling)
	}
	c.Masteries[0].Level = overCapMainLevel
	remaining := budget + overCapExcess - overCapMainLevel
	for i := 1; i < len(c.Masteries); i++ {
		share := min(remaining, level)
		c.Masteries[i].Level = share
		remaining -= share
	}
	if remaining != 0 || c.Masteries[1].Level <= overCapExcess+1 {
		t.Fatalf("%s: cannot place %d mastery levels under level %d", codename, budget+overCapExcess, level)
	}
	c.Skills = []uint32{overCapMainSkill}
	return c, budget
}

/*
================
overCapMasterySum
================
*/
func overCapMasterySum(c *enterworld.Character) int64 {
	sum := int64(0)
	for _, mastery := range c.Masteries {
		sum += mastery.Level
	}
	return sum
}

/*
================
overCapRaise

Trains a mastery one level and answers the response's result and error.
================
*/
func overCapRaise(t *testing.T, rt *progression.Runtime, c *enterworld.Character, id uint32) (uint8, uint8) {
	t.Helper()
	result := rt.HandleMasteryLevelUp(testDivision, c, wire.NewWriter(5).U32(id).U8(1).Payload())
	if len(result.Frames) == 0 || len(result.Frames[0].Payload) < 2 {
		t.Fatalf("mastery raise answered %+v", result)
	}
	return result.Frames[0].Payload[0], result.Frames[0].Payload[1]
}

/*
================
TestOverCapCharacterWithdrawsWithMallPotionsAndTrainsAgain

The over-cap character refuses every raise with the total-limit notice,
and the main mastery cannot be withdrawn while its skill holds it. Spending
excess+1 mall potions on another mastery returns their SP and brings the
total one under the budget; the main mastery then trains exactly once
before the native budget binds again.
================
*/
func TestOverCapCharacterWithdrawsWithMallPotionsAndTrainsAgain(t *testing.T) {
	for _, tc := range []struct {
		name     string
		codename string
		race     int64
		level    int64
	}{
		{"china", "CHAR_CH_MAN_ADVENTURER", enterworld.RaceChina, 100},
		{"europe", "CHAR_EU_WOMAN_ADVENTURER", enterworld.RaceEurope, 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, budget := overCapCharacter(t, tc.codename, tc.race, tc.level)
			main, other := c.Masteries[0], c.Masteries[1]
			potions := overCapExcess + 1
			items := testItems()
			potion := &enterworld.ItemRef{RefObjID: overCapMallPotionID, Codename: overCapMallPotion, TypeIDs: [4]int64{3, 3, 13, 0}}
			items[potion.Codename] = potion
			c.MissionInventory = []enterworld.InventoryRow{{Slot: 13, RefObjID: potion.RefObjID,
				Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: potions}}
			action, _ := newTestRuntime(c, items)
			deps := action.deps.(*enterworld.Deps)
			deps.Levels = overCapLevels{}
			deps.Skills = withdrawalSkills{staticSkillSource{
				overCapMainSkill: {ID: overCapMainSkill, Group: 77, Level: 1, SPCost: 13,
					Masteries: [2]enterworld.SkillRequirement{{ID: main.ID, Level: main.Level}}},
			}}
			rt := progression.NewRuntime(deps)
			rt.Withdrawal = action.WithdrawalHooks()

			if result, code := overCapRaise(t, rt, c, main.ID); result != wire.ResultError || code != wire.ErrCodeMasteryTotalLimit {
				t.Fatalf("over-cap raise answered %d/%#x, want the total-limit refusal", result, code)
			}
			blocked := wire.NewWriter(9).U32(overCapMallPotionID).U32(main.ID).U8(uint8(main.Level - 1)).Payload()
			if result := rt.HandleMasteryWithdrawal(testDivision, c, blocked); len(result.Frames) != 1 || result.Frames[0].Payload[0] != 2 {
				t.Fatalf("main mastery withdrawn under its learned skill: %+v", result)
			}

			target := other.Level - potions
			request := wire.NewWriter(9).U32(overCapMallPotionID).U32(other.ID).U8(uint8(target)).Payload()
			result := rt.HandleMasteryWithdrawal(testDivision, c, request)
			receipt := false
			for _, frame := range result.Frames {
				if frame.Opcode == wire.OpMasteryWithdrawalResponse {
					receipt = len(frame.Payload) > 0 && frame.Payload[0] == 1
				}
			}
			if !receipt {
				t.Fatalf("mall potion withdrawal refused: %+v", result)
			}
			refund := potions * overCapLevelCost
			if level, _ := enterworld.MasteryLevel(c, other.ID); level != target ||
				overCapMasterySum(c) != budget-1 || *c.SkillPoints != overCapSkillPoints+refund ||
				len(c.MissionInventory) != 0 || *c.Gold != 5000 {
				t.Fatalf("withdrawal left level %d, sum %d (budget %d), SP %d, inventory %+v, gold %d",
					level, overCapMasterySum(c), budget, *c.SkillPoints, c.MissionInventory, *c.Gold)
			}

			if result, code := overCapRaise(t, rt, c, main.ID); result != wire.ResultSuccess {
				t.Fatalf("raise under the budget refused with %#x", code)
			}
			if level, _ := enterworld.MasteryLevel(c, main.ID); level != main.Level+1 || overCapMasterySum(c) != budget ||
				*c.SkillPoints != overCapSkillPoints+refund-overCapLevelCost {
				t.Fatalf("raise left main level %d, sum %d, SP %d", level, overCapMasterySum(c), *c.SkillPoints)
			}
			if result, code := overCapRaise(t, rt, c, main.ID); result != wire.ResultError || code != wire.ErrCodeMasteryTotalLimit {
				t.Fatalf("raise past the native budget answered %d/%#x", result, code)
			}
		})
	}
}
