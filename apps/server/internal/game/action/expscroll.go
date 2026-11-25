/*
===========================================================================

expscroll.go - the later EXP and skill-EXP increase scrolls

The six v1.696 mall scrolls (3/3/13/21, ITEM_MALL_*_EXP_SCROLL_*TIME)
author Param1 as a duration in seconds, Param2 as the reward kind and
Param3 as a percent: kind 1 raises the EXP keeper for that long, kind 2
the skill-EXP keeper. The port keeps both as param jobs (paramjob.go),
the same CTJ_CharParamKeeper rows the native premium tickets raise on
their own keepers: the kill-reward distributor sums the param-job
keeper with the premium keeper, so a scroll and a premium stack. A
second scroll of the same item and kind restarts the job, as any
param-job upsert does.

The scrolls are later-native (#4240 part 3): no v1.150 row carries the
type, and the build stages the rows from the v1.696 mall data
(scripts/later_mall_items.py). Kind 3 (both keepers at once) occurs in
no staged row and stays refused rather than guessed.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// The reward kinds the staged rows author in Param2.
const (
	expScrollKindEXP      = 1
	expScrollKindSkillEXP = 2
)

/*
================
expScrollJob

The param job a scroll use starts: Param1 seconds at Param3 percent on
the keeper Param2 names. A row missing any of the three, or naming an
unstaged kind, starts nothing.
================
*/
func expScrollJob(ref *enterworld.ItemRef, nowMs int64) (domain.ParamJob, bool) {
	seconds, okSeconds := ref.NativeFields.Lookup("itemParam1_29c")
	kind, okKind := ref.NativeFields.Lookup("itemParam2_2a0")
	percent, okPercent := ref.NativeFields.Lookup("itemParam3_2a4")
	if !okSeconds || !okKind || !okPercent || seconds <= 0 || percent <= 0 {
		return domain.ParamJob{}, false
	}
	job := domain.ParamJob{ItemRefObjID: ref.RefObjID, Codename: ref.Codename,
		Value: int64(percent), EndUnixMs: nowMs + int64(seconds)*1000}
	switch kind {
	case expScrollKindEXP:
		job.Param = paramExpRate
	case expScrollKindSkillEXP:
		job.Param = paramSkillExpRate
	default:
		return domain.ParamJob{}, false
	}
	return job, true
}

/*
================
useExpScroll

Runs inside the item use's character Update.
================
*/
func (rt *Runtime) useExpScroll(use skillItemUse, character *enterworld.Character, tail []byte, result *OpResult) bool {
	if len(tail) != 0 {
		return false
	}
	job, ok := expScrollJob(use.ref, use.nowMs)
	if !ok {
		return false
	}
	jobs, placed := upsertParamJob(character.ParamJobs, job)
	if !placed {
		return false
	}
	character.ParamJobs = jobs
	remaining := rt.consumeItemUseRow(character, use.row)
	*result = OpResult{Frames: []wire.Frame{
		{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)},
		{Opcode: wire.OpParamJobStart,
			Payload: wire.EncodeParamJobRow(enterworld.ObjectIDForCharacter(character), paramJobRemaining(job, use.nowMs), use.ref.RefObjID)},
	}}
	result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
	rt.paramJobOwners.track(use.division, character.Name)
	return true
}
