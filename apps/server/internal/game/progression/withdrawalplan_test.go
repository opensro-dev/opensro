/*
===========================================================================

withdrawalplan_test.go - restoration preserves dependencies and exact prices

Multi-rank removal must reverse the actual authored costs, not multiply the
last rank's price. Refusals must leave every persisted slice unchanged.

===========================================================================
*/
package progression

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestResuscitationPricesEachRankAndDeductsLossAfterSummation
================
*/
func TestResuscitationPricesEachRankAndDeductsLossAfterSummation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dg.txt"), []byte("1\t28\t42\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	levels := enterworld.NewTextdataLevels(dir)
	gold := int64(10000)
	c := &enterworld.Character{Gold: &gold}
	plan := withdrawalPlan{Refund: 22, GoldRanks: []int64{2, 1, 0}}
	if code := priceResuscitation(&plan, c, levels); code != 0 || plan.Gold != 9240 || plan.Refund != 18 || gold != 10000 {
		t.Fatal(plan, code, gold)
	}
	gold = 9239
	plan = withdrawalPlan{Refund: 22, GoldRanks: []int64{2, 1, 0}}
	if code := priceResuscitation(&plan, c, levels); code != withdrawalGold || gold != 9239 {
		t.Fatal(plan, code, gold)
	}
	gold = 10000
	for refund := int64(0); refund <= 6; refund++ {
		plan = withdrawalPlan{Refund: refund, GoldRanks: []int64{0}}
		if code := priceResuscitation(&plan, c, levels); code != 0 || plan.Refund != refund-refund/5 || plan.Gold != 2800 {
			t.Fatal(refund, plan, code)
		}
	}
	plan = withdrawalPlan{Refund: 22, GoldRanks: []int64{2}}
	if code := priceResuscitation(&plan, c, testLevels()); code != withdrawalUnavailable {
		t.Fatal("missing price authority accepted", code)
	}
}

/*
================
withdrawalTestCatalog
================
*/
type withdrawalTestCatalog struct{ staticSkills }

/*
================
SkillByGroupLevel
================
*/
func (s withdrawalTestCatalog) SkillByGroupLevel(group uint32, rank int64) (enterworld.SkillRow, bool) {
	for _, row := range s.staticSkills {
		if row.Group == group && row.Level == rank && !row.ChainSub {
			return row, true
		}
	}
	return enterworld.SkillRow{}, false
}

/*
================
TestSkillWithdrawalPlansEveryRankAndPreservesDependents
================
*/
func TestSkillWithdrawalPlansEveryRankAndPreservesDependents(t *testing.T) {
	data := withdrawalTestCatalog{staticSkills{
		10: {ID: 10, Group: 100, Level: 1, SPCost: 2},
		11: {ID: 11, Group: 100, Level: 2, SPCost: 7},
		12: {ID: 12, Group: 100, Level: 3, SPCost: 13},
		20: {ID: 20, Group: 200, Level: 1, Prerequisites: [3]enterworld.SkillRequirement{{ID: 100, Level: 2}}},
	}}
	c := &enterworld.Character{Skills: []uint32{12, 20}}
	before := append([]uint32(nil), c.Skills...)
	request := wire.WithdrawalRequest{LearnedID: 12, Rank: 1}
	if _, code := planSkillWithdrawal(c, data, request); code != withdrawalDependency {
		t.Fatal(code)
	}
	request.Rank = 2
	plan, code := planSkillWithdrawal(c, data, request)
	if code != 0 || plan.Refund != 13 || plan.PotionCount != 1 || plan.ReceiptID != 11 || !reflect.DeepEqual(plan.Skills, []uint32{11, 20}) {
		t.Fatal(plan, code)
	}
	if !reflect.DeepEqual(c.Skills, before) {
		t.Fatal("planning mutated persisted character")
	}
	c.Skills = []uint32{12}
	request.Rank = 0
	plan, code = planSkillWithdrawal(c, data, request)
	if code != 0 || plan.Refund != 22 || plan.PotionCount != 3 || plan.ReceiptID != 12 || len(plan.Skills) != 0 {
		t.Fatal(plan, code)
	}
	delete(data.staticSkills, 11)
	if _, code = planSkillWithdrawal(c, data, request); code != withdrawalUnavailable {
		t.Fatal("missing intermediate rank accepted", code)
	}
}

/*
================
TestMasteryWithdrawalReversesTrainingAndChecksBothRequirements
================
*/
func TestMasteryWithdrawalReversesTrainingAndChecksBothRequirements(t *testing.T) {
	data := staticSkills{30: {ID: 30, Masteries: [2]enterworld.SkillRequirement{{ID: 999, Level: 1}, {ID: 257, Level: 3}}}}
	c := &enterworld.Character{Skills: []uint32{30}, Masteries: []enterworld.CharacterMastery{{ID: 257, Level: 6}}}
	request := wire.WithdrawalRequest{LearnedID: 257, Rank: 2}
	if _, code := planMasteryWithdrawal(c, data, testLevels(), request); code != withdrawalDependency {
		t.Fatal(code)
	}
	request.Rank = 3
	plan, code := planMasteryWithdrawal(c, data, testLevels(), request)
	if code != 0 || plan.Refund != 5 || plan.PotionCount != 3 || plan.Masteries[0].Level != 3 || c.Masteries[0].Level != 6 {
		t.Fatal(plan, code, c.Masteries)
	}
	c.Skills = nil
	request.Rank = 0
	plan, code = planMasteryWithdrawal(c, data, testLevels(), request)
	if code != 0 || plan.Refund != 7 || plan.PotionCount != 6 || plan.Masteries[0].Level != 0 {
		t.Fatal(plan, code)
	}
	c.Masteries[0].Level = 1
	plan, code = planMasteryWithdrawal(c, data, testLevels(), request)
	if code != 0 || plan.Refund != 0 || plan.PotionCount != 1 {
		t.Fatal("free initial mastery rank", plan, code)
	}
}
