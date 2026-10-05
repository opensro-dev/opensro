/*
===========================================================================

premiumticket.go - the premium time tickets

The gold, silver and EXP-help tickets (3/3/13/4) and the skill time
service tickets (3/3/13/5) are 49C2B0 cases 3 and 4: each refuses while a
premium job already runs (0x1894) and otherwise starts an owner timed job
(type 3, or 9 for the skill tickets) for Param1 seconds. The tickets carry
Param4 (EXP +%) and Param5 (skill EXP +%); the kill-reward distributor
(4EA6A0) adds ParamKeeper 0xBA and 0xCA to every award, so the port raises
those two keepers as param jobs (paramjob.go), shown on the client's
param-job board like any other.

Param3 is the daily allotment in milliseconds: the keepers apply only
while the day's allotment lasts (premiumclock.go). Param2 is the EXP a
death keeps
(CTJ_PremiumKeeper's ParamKeeper 0x101, read by the death penalty
4E6B74); every shipped ticket authors zero, and a nonzero one raises it as
a third param job.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// errCodePremiumActive is the low byte of 49C65E's 0x1894.
const errCodePremiumActive uint8 = 0x94

/*
================
premiumTicketPlan

The keepers and the daily clock a ticket starts (49C2B0 cases 3 and 4,
or a premium package's UIU1 entry). skillOnly is case 4: the skill time
service raises only the skill-EXP keeper.
================
*/
func premiumTicketPlan(ref *enterworld.ItemRef, nowMs int64, skillOnly bool) ([]domain.ParamJob, *domain.PremiumClock, bool) {
	seconds, _ := ref.NativeFields.Lookup("itemParam1_29c")
	expPercent, _ := ref.NativeFields.Lookup("itemParam4_2a8")
	skillPercent, _ := ref.NativeFields.Lookup("itemParam5_2ac")
	keepPercent, _ := ref.NativeFields.Lookup("itemParam2_2a0")
	dailyMs, _ := ref.NativeFields.Lookup("itemParam3_2a4")
	if seconds <= 0 || dailyMs <= 0 {
		return nil, nil, false
	}
	end := nowMs + int64(seconds)*1000
	var jobs []domain.ParamJob
	if !skillOnly && expPercent > 0 {
		jobs = append(jobs, domain.ParamJob{ItemRefObjID: ref.RefObjID, Codename: ref.Codename,
			Param: paramPremiumExpRate, Value: int64(expPercent), EndUnixMs: end})
	}
	if skillPercent > 0 {
		jobs = append(jobs, domain.ParamJob{ItemRefObjID: ref.RefObjID, Codename: ref.Codename,
			Param: paramPremiumSkillExpRate, Value: int64(skillPercent), EndUnixMs: end})
	}
	if !skillOnly && keepPercent > 0 {
		jobs = append(jobs, domain.ParamJob{ItemRefObjID: ref.RefObjID, Codename: ref.Codename,
			Param: paramDeathExpKept, Value: int64(keepPercent), EndUnixMs: end})
	}
	if len(jobs) == 0 {
		return nil, nil, false
	}
	return jobs, newPremiumClock(ref.RefObjID, nowMs, end, int64(dailyMs)), true
}

/*
================
premiumRunning

49C65E's 0x1894 test: a premium job already runs.
================
*/
func premiumRunning(c *enterworld.Character, nowMs int64) bool {
	for _, job := range c.ParamJobs {
		if (job.Param == paramPremiumExpRate || job.Param == paramPremiumSkillExpRate) && job.EndUnixMs > nowMs {
			return true
		}
	}
	return false
}

/*
================
installParamJobs

Places a plan's jobs on the character; false when one does not fit.
================
*/
func installParamJobs(c *enterworld.Character, jobs []domain.ParamJob) bool {
	next := append([]domain.ParamJob(nil), c.ParamJobs...)
	for _, job := range jobs {
		var placed bool
		if next, placed = upsertParamJob(next, job); !placed {
			return false
		}
	}
	c.ParamJobs = next
	return true
}

/*
================
usePremiumTicket

Runs inside the item use's character Update.
================
*/
func (rt *Runtime) usePremiumTicket(use skillItemUse, c *enterworld.Character, tail []byte, skillOnly bool, result *OpResult) bool {
	if len(tail) != 0 {
		return false
	}
	if premiumRunning(c, use.nowMs) {
		*result = itemUseFailure(errCodePremiumActive)
		return false
	}
	jobs, clock, ok := premiumTicketPlan(use.ref, use.nowMs, skillOnly)
	if !ok || !installParamJobs(c, jobs) {
		return false
	}
	c.PremiumClock = clock
	remaining := rt.consumeItemUseRow(c, use.row)
	// One board row per ticket: every keeper shares its reference.
	*result = OpResult{Frames: []wire.Frame{
		{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)},
		{Opcode: wire.OpParamJobStart, Payload: wire.EncodeParamJobRow(enterworld.ObjectIDForCharacter(c), paramJobRemaining(jobs[0], use.nowMs), use.ref.RefObjID)},
	}}
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	rt.paramJobOwners.track(use.division, c.Name)
	return true
}
