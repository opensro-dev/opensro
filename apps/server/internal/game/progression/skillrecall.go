/*
===========================================================================

skillrecall.go - the skill reset scroll: every skill and mastery point back

Port-only, not native (owner, 2026-11-21). ITEM_MALL_SKILL_POINT_RECALL_A
(v1.696, type 3/3/13/13, Param1 5) is sold behind SRO_SKILL_RESET_SCROLL
(commerce/resetscrolls.go). Neither binary the port reads serves it: the
v1.150 client has no such item and the v1.188 server's special
consumable switch does not handle Param1 5.

INFERENCE from the item's purpose and the native withdrawal (the
herbalist's one-rank restoration, withdrawalplan.go): the scroll is a
full restoration without gold or potions. Every learned skill with a
paid rank is forgotten and every mastery goes back to 0; the SP comes
back exactly as the withdrawal refunds it, rank by rank (skillRankRefund,
masteryRankRefund). A skill whose ranks are all free and need no mastery
or prerequisite (the basic skills every character holds) stays. Quick
slots on a forgotten skill are cleared in the same record. A reset with
nothing to return, or a record it cannot represent, changes nothing and
keeps the scroll.

The browser learns the result from the port-only control SkillRecallControl
(release protocol 20): the native withdrawal acknowledgements answer an
herbalist request in flight and cannot carry a whole build.

===========================================================================
*/
package progression

import (
	"encoding/json"
	"math"
	"slices"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// SkillRecallControl is the port-only world control that carries a skill
// reset's kept skills and masteries to the browser (client
// commerce-controls.ts SKILL_RECALL_CONTROL), beside the commerce controls
// 11-20.
const SkillRecallControl uint16 = 21

// skillRecallVersion is the control body's version.
const skillRecallVersion = 1

/*
================
SkillRecall

What a skill reset sent and forgot: the frames for the browser and the
forgotten skills, whose resident effects the caller retires.
================
*/
type SkillRecall struct {
	Frames  []wire.Frame
	Removed []uint32
}

/*
================
skillRecallPlan
================
*/
type skillRecallPlan struct {
	Kept      []uint32
	Removed   []uint32
	Masteries []enterworld.CharacterMastery
	Refund    int64
}

/*
================
SkillRecallUpdater

The door-free reset for the item-use owner, which calls it inside the
character transaction that consumes the scroll. It reports false,
changing nothing, when nothing comes back.
================
*/
func (rt *Runtime) SkillRecallUpdater() func(*enterworld.Character) (SkillRecall, bool) {
	return rt.recallSkills
}

/*
================
freeSkill

A skill the reset keeps: no rank costs SP or asks for a mastery level or
another skill.
================
*/
func freeSkill(data withdrawalCatalog, row enterworld.SkillRow) bool {
	for rank := row.Level; rank > 0; rank-- {
		ranked, found := data.SkillByGroupLevel(row.Group, rank)
		if !found || ranked.SPCost != 0 {
			return false
		}
		for _, requirement := range ranked.Masteries {
			if requirement.ID != 0 && requirement.Level > 0 {
				return false
			}
		}
		for _, requirement := range ranked.Prerequisites {
			if requirement.ID != 0 {
				return false
			}
		}
	}
	return true
}

/*
================
planSkillRecall

Pure: the kept and forgotten skills, every mastery at 0 and the refund.
False on a skill the catalogue does not know, a missing rank or a refund
the SP word cannot hold.
================
*/
func planSkillRecall(c *enterworld.Character, data withdrawalCatalog, levels enterworld.LevelDataSource) (skillRecallPlan, bool) {
	var plan skillRecallPlan
	if c == nil || data == nil || levels == nil {
		return plan, false
	}
	plan.Kept = []uint32{}
	for _, id := range c.Skills {
		row, found := data.SkillByID(id)
		if !found || row.ChainSub || row.Level <= 0 {
			return skillRecallPlan{}, false
		}
		if freeSkill(data, row) {
			plan.Kept = append(plan.Kept, id)
			continue
		}
		refund, _, ok := skillRankRefund(data, row, 0)
		if !ok || refund > math.MaxUint32-plan.Refund {
			return skillRecallPlan{}, false
		}
		plan.Refund += refund
		plan.Removed = append(plan.Removed, id)
	}
	plan.Masteries = slices.Clone(c.Masteries)
	trained := false
	for i := range plan.Masteries {
		if plan.Masteries[i].Level <= 0 {
			continue
		}
		trained = true
		refund, ok := masteryRankRefund(levels, plan.Masteries[i].Level, 0)
		if !ok || refund > math.MaxUint32-plan.Refund {
			return skillRecallPlan{}, false
		}
		plan.Refund += refund
		plan.Masteries[i].Level = 0
	}
	if len(plan.Removed) == 0 && !trained {
		return skillRecallPlan{}, false
	}
	return plan, true
}

/*
================
recallSkills
================
*/
func (rt *Runtime) recallSkills(c *enterworld.Character) (SkillRecall, bool) {
	catalog, supported := rt.deps.SkillData().(withdrawalCatalog)
	if c == nil || !supported {
		return SkillRecall{}, false
	}
	plan, ok := planSkillRecall(c, catalog, rt.deps.LevelData())
	if !ok {
		return SkillRecall{}, false
	}
	available := coercePoints(c.SkillPoints)
	if available > math.MaxUint32-plan.Refund {
		return SkillRecall{}, false
	}
	available += plan.Refund
	next := c.Snapshot()
	next.Skills, next.Masteries, next.SkillPoints = plan.Kept, plan.Masteries, &available
	for i := range next.QuickSlots {
		binding := &next.QuickSlots[i]
		if binding.Kind == withdrawalSkillBindingKind && slices.Contains(plan.Removed, binding.Payload) {
			binding.Kind, binding.Payload = 0, 0
		}
	}
	stats, err := rt.playerBaseStats(next)
	if err != nil {
		return SkillRecall{}, false
	}
	body, err := json.Marshal(struct {
		Version   int                           `json:"version"`
		Skills    []uint32                      `json:"skills"`
		Masteries []enterworld.CharacterMastery `json:"masteries"`
	}{skillRecallVersion, plan.Kept, plan.Masteries})
	if err != nil {
		return SkillRecall{}, false
	}
	c.Skills, c.Masteries, c.SkillPoints, c.QuickSlots = next.Skills, next.Masteries, next.SkillPoints, next.QuickSlots
	return SkillRecall{Removed: plan.Removed, Frames: []wire.Frame{
		{Opcode: wire.OpPointsUpdate, Payload: wire.EncodePointsSkillUpdate(uint32(available), false)},
		{Opcode: wire.OpBaseStats, Payload: enterworld.BuildLoginStatBlock(next, stats)},
		{Opcode: SkillRecallControl, Payload: body},
	}}, true
}
