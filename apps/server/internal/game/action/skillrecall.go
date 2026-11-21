/*
===========================================================================

skillrecall.go - the skill reset scroll's use

Port-only, not native (commerce/resetscrolls.go, behind
SRO_SKILL_RESET_SCROLL; the admission checks the switch by codename).
The scroll is ITEM_MALL_SKILL_POINT_RECALL_A (v1.696, 3/3/13/13, Param1
5), used with no tail. Progression owns the rule (progression/
skillrecall.go): every paid skill forgotten, every mastery at 0, the SP
back. This file spends the scroll only when something came back, answers
the use, and retires the forgotten skills' resident effects through the
withdrawal's owner (finishSkillRemoval), as the herbalist's restoration
does for one skill.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
useSkillRecall
================
*/
func (rt *Runtime) useSkillRecall(use skillItemUse, c *enterworld.Character, tail []byte, result *OpResult) bool {
	if len(tail) != 0 || rt.RecallSkills == nil {
		return false
	}
	recall, ok := rt.RecallSkills(c)
	if !ok {
		result.DiagnosticRefusal = "item-use: no skill or mastery point to recall"
		return false
	}
	remaining := rt.consumeItemUseRow(c, use.row)
	frames := append([]wire.Frame{{Opcode: wire.OpItemUseResponse,
		Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)}}, recall.Frames...)
	*result = OpResult{Frames: append(frames, rt.finishSkillRemoval(use.division, c, recall.Removed)...)}
	return true
}
