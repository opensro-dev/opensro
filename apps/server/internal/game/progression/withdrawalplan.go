/*
===========================================================================

withdrawalplan.go - validate restoration against the complete learned graph

The plan is pure: no inventory debit, rank change or refund escapes before
the caller can commit all of them. Native 59F410/59F610 check every learned
dependent; 410EA0/410F10 sum the authored costs of the ranks being removed.

===========================================================================
*/
package progression

import (
	"math"
	"slices"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const (
	withdrawalUnknown     uint8 = 1
	withdrawalRank        uint8 = 2
	withdrawalUnavailable uint8 = 3
	withdrawalGold        uint8 = 4
	withdrawalPotions     uint8 = 5
	withdrawalDependency  uint8 = 6
)

/*
================
withdrawalCatalog

An exact root-rank lookup prevents restoring a linked attack stage as a
learned skill. Both operations resolve the same authoritative skill rows.
================
*/
type withdrawalCatalog interface {
	enterworld.SkillDataSource
	SkillByGroupLevel(uint32, int64) (enterworld.SkillRow, bool)
}

/*
================
withdrawalPlan
================
*/
type withdrawalPlan struct {
	Skills        []uint32
	Masteries     []enterworld.CharacterMastery
	Refund        int64
	PotionCount   uint32
	PreviousSkill uint32
	ReceiptID     uint32
}

/*
================
planSkillWithdrawal

Zero removes the root entirely. A positive target must exist in the same
group. Basic skills with no paid ranks cannot produce a withdrawal refund.
================
*/
func planSkillWithdrawal(c *enterworld.Character, data withdrawalCatalog, request wire.WithdrawalRequest) (withdrawalPlan, uint8) {
	var plan withdrawalPlan
	index := slices.Index(c.Skills, request.LearnedID)
	if index < 0 || data == nil {
		return plan, withdrawalUnknown
	}
	row, ok := data.SkillByID(request.LearnedID)
	if !ok || row.ChainSub || row.Level <= 0 || row.Level > math.MaxUint8 {
		return plan, withdrawalUnknown
	}
	target := int64(request.Rank)
	if target >= row.Level {
		return plan, withdrawalRank
	}
	for _, id := range c.Skills {
		if id == row.ID {
			continue
		}
		other, found := data.SkillByID(id)
		if !found {
			return plan, withdrawalUnavailable
		}
		for _, requirement := range other.Prerequisites {
			if requirement.ID == row.Group && (target == 0 || requirement.Level > target) {
				return plan, withdrawalDependency
			}
		}
	}
	plan.Skills = slices.Clone(c.Skills)
	plan.PreviousSkill = row.ID
	plan.ReceiptID = row.ID
	plan.PotionCount = uint32(row.Level - target)
	for rank := row.Level; rank > target; rank-- {
		removed, found := data.SkillByGroupLevel(row.Group, rank)
		if !found || removed.SPCost < 0 || removed.SPCost > math.MaxUint32-plan.Refund {
			return withdrawalPlan{}, withdrawalUnavailable
		}
		plan.Refund += removed.SPCost
	}
	if plan.Refund == 0 {
		return withdrawalPlan{}, withdrawalUnavailable
	}
	if target == 0 {
		plan.Skills = slices.Delete(plan.Skills, index, index+1)
		return plan, 0
	}
	replacement, found := data.SkillByGroupLevel(row.Group, target)
	if !found {
		return withdrawalPlan{}, withdrawalUnavailable
	}
	plan.Skills[index] = replacement.ID
	plan.ReceiptID = replacement.ID
	return plan, 0
}

/*
================
planMasteryWithdrawal

The free initial mastery rank contributes no SP. Every higher rank refunds
the same leveldata row that training charged, including multi-rank requests.
================
*/
func planMasteryWithdrawal(c *enterworld.Character, data enterworld.SkillDataSource, levels enterworld.LevelDataSource, request wire.WithdrawalRequest) (withdrawalPlan, uint8) {
	var plan withdrawalPlan
	current, known := enterworld.MasteryLevel(c, request.LearnedID)
	if !known || current <= 0 || current > math.MaxUint8 || data == nil || levels == nil {
		return plan, withdrawalUnknown
	}
	target := int64(request.Rank)
	if target >= current {
		return plan, withdrawalRank
	}
	for _, id := range c.Skills {
		row, found := data.SkillByID(id)
		if !found {
			return plan, withdrawalUnavailable
		}
		for _, requirement := range row.Masteries {
			if requirement.ID == request.LearnedID && requirement.Level > target {
				return plan, withdrawalDependency
			}
		}
	}
	for rank := current; rank > target && rank > 1; rank-- {
		cost, found := levels.SkillPointCost(rank - 1)
		if !found || cost < 0 || cost > math.MaxUint32-plan.Refund {
			return withdrawalPlan{}, withdrawalUnavailable
		}
		plan.Refund += cost
	}
	plan.Masteries = slices.Clone(c.Masteries)
	for i := range plan.Masteries {
		if plan.Masteries[i].ID == request.LearnedID {
			plan.Masteries[i].Level = target
		}
	}
	plan.PotionCount = uint32(current - target)
	plan.ReceiptID = request.LearnedID
	return plan, 0
}
