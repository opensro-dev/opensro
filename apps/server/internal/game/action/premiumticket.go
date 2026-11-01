/*
===========================================================================

premiumticket.go - the premium time tickets

The gold, silver and EXP-help tickets (3/3/13/4) and the skill time
service tickets (3/3/13/5) are native special-consumable cases 3 and 4: each refuses while a
premium job already runs (0x1894) and otherwise starts an owner timed job
(type 3, or 9 for the skill tickets) for Param1 seconds. The tickets carry
Param4 (EXP +%) and Param5 (skill EXP +%); the kill-reward distributor
adds ParamKeeper 0xBA and 0xCA to every award, so the port raises
those two keepers as param jobs (paramjob.go), shown on the client's
param-job board like any other.

Param3 is the daily allotment in milliseconds: the keepers apply only
while the day's allotment lasts (premiumclock.go). Param2 is the EXP a
death keeps
(the premium keeper's ParamKeeper 0x101, read by the death penalty);
every shipped ticket authors zero, and a nonzero one raises it as
a third param job.

===========================================================================
*/

package action

import (
	"os"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// EnvPremiumUpgrade turns the port-only premium upgrade on ("on", "1",
// "true"): using a strictly higher-grade premium of the same family
// replaces the running lower one instead of the native 0x1894 refusal.
// Default off is native (#1624, owner-requested gameplay tuning).
const EnvPremiumUpgrade = "SRO_PREMIUM_UPGRADE"

/*
================
PremiumUpgradeFromEnv
================
*/
func PremiumUpgradeFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvPremiumUpgrade))) {
	case "on", "1", "true":
		return true
	}
	return false
}

// errCodePremiumActive is the low byte of the native 0x1894.
const errCodePremiumActive uint8 = 0x94

/*
================
premiumTicketPlan

The keepers and the daily clock a ticket starts (native cases 3 and 4,
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

The native 0x1894 test: a premium job already runs.
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
premiumGrade

A premium ticket's grade (port-only upgrade, SRO_PREMIUM_UPGRADE):
every shipped ticket authors the same 100% rates, so the authored
quality axes are the daily allotment (Param3), then the duration
(Param1). A bigger allotment outranks any duration; between equals the
longer course wins (#1624, from the shipped rows: Gold Time 1D/4W/8W/
12W/16W at 3 h a day, the Plus ticket at 5 h a day for 4 weeks, the
skill tickets at 3 h for 1 day and 4 weeks).
================
*/
type premiumGrade struct {
	dailyMs, seconds int64
}

/*
================
premiumGradeOf
================
*/
func premiumGradeOf(ref *enterworld.ItemRef) (premiumGrade, bool) {
	seconds, okSeconds := ref.NativeFields.Lookup("itemParam1_29c")
	dailyMs, okDaily := ref.NativeFields.Lookup("itemParam3_2a4")
	if !okSeconds || !okDaily || seconds <= 0 || dailyMs <= 0 {
		return premiumGrade{}, false
	}
	return premiumGrade{dailyMs: int64(dailyMs), seconds: int64(seconds)}, true
}

/*
================
premiumGradeHigher

Strictly higher within one family; equal grades never replace.
================
*/
func premiumGradeHigher(next, running premiumGrade) bool {
	if next.dailyMs != running.dailyMs {
		return next.dailyMs > running.dailyMs
	}
	return next.seconds > running.seconds
}

/*
================
runningPremium

The premium now running, if any: its ticket reference and whether it is
the skill-only family (no EXP keeper among its live jobs).
================
*/
func (rt *Runtime) runningPremium(c *enterworld.Character, nowMs int64) (*enterworld.ItemRef, bool, bool) {
	var ref *enterworld.ItemRef
	skillOnly := true
	for _, job := range c.ParamJobs {
		if job.EndUnixMs <= nowMs {
			continue
		}
		switch job.Param {
		case paramPremiumExpRate:
			skillOnly = false
		case paramPremiumSkillExpRate:
		default:
			continue
		}
		if ref == nil {
			found, ok := rt.deps.ItemReferences().ItemRefByCodename(job.Codename)
			if !ok {
				return nil, false, false
			}
			ref = found
		}
	}
	return ref, skillOnly, ref != nil
}

/*
================
premiumUpgradePlan

With the port-only upgrade on, whether using next retires the running
premium: the running ticket it replaces, or nil when the native refusal
stands (flag off, another family, or no strictly higher grade).
================
*/
func (rt *Runtime) premiumUpgradePlan(c *enterworld.Character, nowMs int64, next *enterworld.ItemRef, nextSkillOnly bool) *enterworld.ItemRef {
	if !rt.PremiumUpgrade {
		return nil
	}
	runningRef, runningSkillOnly, running := rt.runningPremium(c, nowMs)
	if !running || runningSkillOnly != nextSkillOnly {
		return nil
	}
	nextGrade, okNext := premiumGradeOf(next)
	runningGrade, okRunning := premiumGradeOf(runningRef)
	if !okNext || !okRunning || !premiumGradeHigher(nextGrade, runningGrade) {
		return nil
	}
	return runningRef
}

/*
================
premiumRemainingMs

The time left on the premium being replaced: its keepers share one
end, so the latest end among them minus now (never negative).
================
*/
func premiumRemainingMs(c *enterworld.Character, nowMs int64, replaced *enterworld.ItemRef) int64 {
	var end int64
	for _, job := range c.ParamJobs {
		if job.ItemRefObjID != replaced.RefObjID {
			continue
		}
		switch job.Param {
		case paramPremiumExpRate, paramPremiumSkillExpRate, paramDeathExpKept:
			if job.EndUnixMs > end {
				end = job.EndUnixMs
			}
		}
	}
	if end <= nowMs {
		return 0
	}
	return end - nowMs
}

/*
================
creditPremiumPlan

Port-only (#1624, owner decision): the replaced ticket's unused time
moves the new ticket's end later - every keeper and the daily clock
end creditMs after the fresh plan's own end. The clock otherwise
restarts exactly as a fresh ticket's (full grant, no carry).
================
*/
func creditPremiumPlan(jobs []domain.ParamJob, clock *domain.PremiumClock, creditMs int64) {
	if creditMs <= 0 {
		return
	}
	for i := range jobs {
		jobs[i].EndUnixMs += creditMs
	}
	if clock != nil {
		clock.EndUnixMs += creditMs
	}
}

/*
================
withoutPremiumJobs
================
*/
func withoutPremiumJobs(jobs []domain.ParamJob, ticket *enterworld.ItemRef) []domain.ParamJob {
	kept := make([]domain.ParamJob, 0, len(jobs))
	for _, job := range jobs {
		if job.ItemRefObjID == ticket.RefObjID {
			switch job.Param {
			case paramPremiumExpRate, paramPremiumSkillExpRate, paramDeathExpKept:
				continue
			}
		}
		kept = append(kept, job)
	}
	return kept
}

/*
================
installPremiumSwap

Retires the replaced ticket and installs the new plan's jobs as one
step: a plan that does not fit changes nothing.
================
*/
func installPremiumSwap(c *enterworld.Character, replaced *enterworld.ItemRef, jobs []domain.ParamJob) bool {
	next := append([]domain.ParamJob(nil), c.ParamJobs...)
	if replaced != nil {
		next = withoutPremiumJobs(next, replaced)
	}
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
	// Port-only, not native (SRO_PREMIUM_UPGRADE): a strictly higher
	// grade of the same family replaces the running premium and the
	// replaced ticket's remaining time moves the new end later
	// (#1624, owner decision). Off: any running premium refuses
	// (0x1894).
	var replaced *enterworld.ItemRef
	if premiumRunning(c, use.nowMs) {
		replaced = rt.premiumUpgradePlan(c, use.nowMs, use.ref, skillOnly)
		if replaced == nil {
			*result = itemUseFailure(errCodePremiumActive)
			return false
		}
	}
	jobs, clock, ok := premiumTicketPlan(use.ref, use.nowMs, skillOnly)
	if !ok {
		return false
	}
	if replaced != nil {
		creditPremiumPlan(jobs, clock, premiumRemainingMs(c, use.nowMs, replaced))
	}
	if !installPremiumSwap(c, replaced, jobs) {
		return false
	}
	c.PremiumClock = clock
	remaining := rt.consumeItemUseRow(c, use.row)
	// One board row per ticket: every keeper shares its reference.
	frames := []wire.Frame{
		{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)},
	}
	if replaced != nil {
		frames = append(frames, wire.Frame{Opcode: wire.OpParamJobEnd,
			Payload: wire.EncodeParamJobEnd(enterworld.ObjectIDForCharacter(c), replaced.RefObjID)})
	}
	frames = append(frames, wire.Frame{Opcode: wire.OpParamJobStart,
		Payload: wire.EncodeParamJobRow(enterworld.ObjectIDForCharacter(c), paramJobRemaining(jobs[0], use.nowMs), use.ref.RefObjID)})
	*result = OpResult{Frames: frames}
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	rt.paramJobOwners.track(use.division, c.Name)
	return true
}
