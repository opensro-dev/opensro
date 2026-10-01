/*
===========================================================================

paramjob.go - composite scrolls and their timed ParamKeeper jobs

A composite scroll (TID 3/3/13/14, e.g. ITEM_ETC_100EXP_BASIC) lists what it
does in its second description: "[UIU1:<internal item>],...". The native
processor (49F590) turns each UIU1 entry into a CTJ_CharParamKeeper job
(654F30): the internal item (TID 3/3/3/10) names a ParamKeeper FourCC in
Param2, the value in Param3 and the duration in seconds in Param1. The
FourCC table is 6552D0. The kill-reward distributor (4EA6A0) adds the EXP
and skill-EXP percentages on top of each award.

The scroll's own COOLTIME group (Param6 milliseconds, Desc6
"COOLTIME:0x..") gates reuse, and equals the job length in every shipped
scroll, so two jobs of one scroll never overlap.

USU1, BFI1 and UQL1 entries belong to the premium item-mall tickets; this
owner refuses them until the item-mall premium owner exists.

===========================================================================
*/
package action

import (
	"math"
	"strconv"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

// ParamKeeper parameters a param job writes (6552D0).
const (
	paramExpRate      uint16 = 0x100
	paramSkillExpRate uint16 = 0x102
	paramHPRate       uint16 = 0x3c
	// paramPremiumExpRate / paramPremiumSkillExpRate are the second terms the
	// distributor sums (4EA6A0: 0x100 + 0xBA, 0x102 + 0xCA). No v1.150 param
	// item writes them; they stay in the sum for the native shape.
	paramPremiumExpRate      uint16 = 0xba
	paramPremiumSkillExpRate uint16 = 0xca
)

// paramJobFourCC maps the internal item's Param2 to a ParamKeeper id.
var paramJobFourCC = map[uint32]uint16{
	0x706874:   paramHPRate,       // "tph"
	0x70657275: paramExpRate,      // "urep"
	0x70737275: paramSkillExpRate, // "ursp"
	0x73657275: paramSkillExpRate, // "urse"
}

// compositeUseInternalItem is the 49F590 entry tag that creates a param job.
const compositeUseInternalItem = "UIU1"

// paramJobCapacity bounds one character's live jobs.
const paramJobCapacity = 8

// itemCooltimePrefix opens the COOLTIME description (Desc6).
const itemCooltimePrefix = "COOLTIME:"

/*
================
compositeEntries

The "[TAG:VALUE],[TAG:VALUE]" list of a composite scroll's Desc2. Any
malformed entry rejects the whole list.
================
*/
func compositeEntries(ref *enterworld.ItemRef) ([][2]string, bool) {
	text := strings.TrimSpace(ref.ParamDescriptions[1])
	if text == "" {
		return nil, false
	}
	var out [][2]string
	for _, part := range strings.Split(text, ",") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "[") || !strings.HasSuffix(part, "]") {
			return nil, false
		}
		tag, value, found := strings.Cut(part[1:len(part)-1], ":")
		if !found || tag == "" || value == "" {
			return nil, false
		}
		out = append(out, [2]string{strings.TrimSpace(tag), strings.TrimSpace(value)})
	}
	return out, len(out) > 0
}

/*
================
itemCooltime

The COOLTIME group and length of a scroll, or false when it has none.
================
*/
func itemCooltime(ref *enterworld.ItemRef) (uint32, int64, bool) {
	text := strings.TrimSpace(ref.ParamDescriptions[5])
	if !strings.HasPrefix(text, itemCooltimePrefix) {
		return 0, 0, false
	}
	group, err := strconv.ParseUint(strings.TrimPrefix(text, itemCooltimePrefix), 0, 32)
	ms, ok := ref.NativeFields.Lookup("itemParam6_2b0")
	if err != nil || !ok || ms <= 0 || math.Trunc(ms) != ms || ms > float64(math.MaxInt32) {
		return 0, 0, false
	}
	return uint32(group), int64(ms), true
}

/*
================
paramJobFromItem

The job an internal param item (TID 3/3/3/10) describes.
================
*/
func paramJobFromItem(ref *enterworld.ItemRef, nowMs int64) (domain.ParamJob, bool) {
	if ref == nil || ref.TypeIDs != [4]int64{3, 3, 3, 10} {
		return domain.ParamJob{}, false
	}
	seconds, okSeconds := ref.NativeFields.Lookup("itemParam1_29c")
	fourCC, okFourCC := ref.NativeFields.Lookup("itemParam2_2a0")
	value, okValue := ref.NativeFields.Lookup("itemParam3_2a4")
	if !okSeconds || !okFourCC || !okValue || seconds <= 0 || seconds > 1<<24 || math.Trunc(seconds) != seconds ||
		fourCC <= 0 || fourCC > math.MaxUint32 || math.Trunc(fourCC) != fourCC || math.Trunc(value) != value {
		return domain.ParamJob{}, false
	}
	param, known := paramJobFourCC[uint32(fourCC)]
	if !known {
		return domain.ParamJob{}, false
	}
	return domain.ParamJob{
		ItemRefObjID: ref.RefObjID,
		Codename:     ref.Codename,
		Param:        param,
		Value:        int64(value),
		EndUnixMs:    nowMs + int64(seconds)*1000,
	}, true
}

/*
================
upsertParamJob

The same internal item restarts its own row (the board keys by item id).
================
*/
func upsertParamJob(jobs []domain.ParamJob, job domain.ParamJob) ([]domain.ParamJob, bool) {
	for i := range jobs {
		if jobs[i].ItemRefObjID == job.ItemRefObjID {
			jobs[i] = job
			return jobs, true
		}
	}
	if len(jobs) >= paramJobCapacity {
		return jobs, false
	}
	return append(jobs, job), true
}

/*
================
paramJobRemaining

Whole seconds left, rounded up, as the board counts down.
================
*/
func paramJobRemaining(job domain.ParamJob, nowMs int64) uint32 {
	return enterworld.PetSkillWindowRemaining(job.EndUnixMs, nowMs)
}

/*
================
paramJobPercent

The live value written to one parameter.
================
*/
func paramJobPercent(character *enterworld.Character, param uint16, nowMs int64) int64 {
	var total int64
	for _, job := range character.ParamJobs {
		if job.Param == param && job.EndUnixMs > nowMs {
			total += job.Value
		}
	}
	return total
}

/*
================
paramJobRewardBonus

4EA6A0 (arg6 bit 0): bonus EXP = (0x100 + 0xBA) / 100 * EXP and bonus
skill EXP = (0x102 + 0xCA) / 100 * SEXP, each converted after the recipient
share and floored at zero, then added to the award.
================
*/
func paramJobRewardBonus(character *enterworld.Character, exp, skillExp int64, nowMs int64) (int64, int64) {
	expPercent := paramJobPercent(character, paramExpRate, nowMs) + paramJobPercent(character, paramPremiumExpRate, nowMs)
	skillPercent := paramJobPercent(character, paramSkillExpRate, nowMs) + paramJobPercent(character, paramPremiumSkillExpRate, nowMs)
	bonusExp, bonusSkill := int64(0), int64(0)
	if expPercent > 0 && exp > 0 {
		bonusExp = max(0, int64(float64(expPercent)/100*float64(exp)))
	}
	if skillPercent > 0 && skillExp > 0 {
		bonusSkill = max(0, int64(nativeRewardDword(float64(skillPercent)/100*float64(skillExp))))
	}
	return exp + bonusExp, skillExp + bonusSkill
}

/*
================
useCompositeScroll

TID 3/3/13/14 inside the item-use door: verify every entry before any
state changes, debit the cooldown, consume one scroll and raise each job.
================
*/
func (rt *Runtime) useCompositeScroll(divisionID string, character *enterworld.Character, ref *enterworld.ItemRef, rowIndex int, request wire.ItemUseRequest, nowMs int64, result *OpResult) bool {
	entries, ok := compositeEntries(ref)
	if !ok {
		result.DiagnosticRefusal = "item-use: malformed composite list " + ref.Codename
		return false
	}
	group, cooltimeMs, hasCooltime := itemCooltime(ref)
	if hasCooltime && character.ItemGroupCooldowns[group] > nowMs {
		*result = itemUseFailure(wire.ErrCodeItemReuseDelay)
		return false
	}
	var jobs []domain.ParamJob
	var internals []inventory.Item
	for _, entry := range entries {
		if entry[0] != compositeUseInternalItem {
			result.DiagnosticRefusal = "item-use: composite entry " + entry[0] + " needs the item-mall premium owner (" + ref.Codename + ")"
			return false
		}
		internal, found := rt.deps.ItemReferences().ItemRefByCodename(entry[1])
		job, valid := paramJobFromItem(internal, nowMs)
		if !found || !valid {
			result.DiagnosticRefusal = "item-use: composite internal item " + entry[1] + " is not a param item"
			return false
		}
		jobs = append(jobs, job)
		internals = append(internals, inventory.Item{RefObjID: internal.RefObjID, Codename: internal.Codename, TypeFlags: internal.TypeFlags()})
	}
	next := append([]domain.ParamJob(nil), character.ParamJobs...)
	for _, job := range jobs {
		var placed bool
		if next, placed = upsertParamJob(next, job); !placed {
			return false
		}
	}
	character.ParamJobs = next
	if hasCooltime {
		if character.ItemGroupCooldowns == nil {
			character.ItemGroupCooldowns = map[uint32]int64{}
		}
		character.ItemGroupCooldowns[group] = nowMs + cooltimeMs
	}
	remaining := rt.consumeItemUseRow(character, rowIndex)
	owner := enterworld.ObjectIDForCharacter(character)
	// The internal items are never carried, so their references (icon,
	// duration) reach the browser here, ahead of the rows that need them.
	frames := []wire.Frame{
		{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)},
		rt.commerceReferences(internals, nil),
	}
	for _, job := range jobs {
		frames = append(frames, wire.Frame{Opcode: wire.OpParamJobStart,
			Payload: wire.EncodeParamJobRow(owner, paramJobRemaining(job, nowMs), job.ItemRefObjID)})
	}
	*result = OpResult{Frames: append(frames, rt.updateQuestInventory(character)...)}
	rt.paramJobOwners.track(divisionID, character.Name)
	return true
}

/*
================
advanceParamJobs

655110 retires a job once its deadline passes; 655180 removes the modifier
and the client row (0x36D4). Retirement commits through the character door.
================
*/
func (rt *Runtime) advanceParamJobs(nowMs int64) {
	type retirement struct {
		key    petOwnerKey
		frames []wire.Frame
	}
	var due []retirement
	for _, key := range rt.paramJobOwners.keys() {
		character := rt.findCharacter(key.division, key.name)
		if character == nil {
			rt.paramJobOwners.forget(key)
			continue
		}
		var frames []wire.Frame
		empty := false
		rt.deps.Update(character, "param-job-expiry", func() bool {
			kept := make([]domain.ParamJob, 0, len(character.ParamJobs))
			owner := enterworld.ObjectIDForCharacter(character)
			for _, job := range character.ParamJobs {
				if job.EndUnixMs > nowMs {
					kept = append(kept, job)
					continue
				}
				frames = append(frames, wire.Frame{Opcode: wire.OpParamJobEnd, Payload: wire.EncodeParamJobEnd(owner, job.ItemRefObjID)})
			}
			empty = len(kept) == 0
			if len(frames) == 0 {
				return false
			}
			character.ParamJobs = kept
			return true
		})
		if empty {
			rt.paramJobOwners.forget(key)
		}
		if len(frames) > 0 {
			due = append(due, retirement{key: key, frames: frames})
		}
	}
	for _, row := range due {
		if rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(row.key.division, row.key.name, row.frames)
		}
	}
}
