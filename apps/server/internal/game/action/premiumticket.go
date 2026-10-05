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

INFERENCE: the tickets' Param3 is a daily allotment (milliseconds a day)
kept by a v1.188 timed-job clock this port does not carry; the bonus runs
for the ticket's whole period. Param2 (EXP recovered on death) is zero in
every shipped ticket.

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
usePremiumTicket

Runs inside the item use's character Update. skillOnly is case 4: the
skill time service raises only the skill-EXP keeper.
================
*/
func (rt *Runtime) usePremiumTicket(use skillItemUse, c *enterworld.Character, tail []byte, skillOnly bool, result *OpResult) bool {
	if len(tail) != 0 {
		return false
	}
	seconds, _ := use.ref.NativeFields.Lookup("itemParam1_29c")
	expPercent, _ := use.ref.NativeFields.Lookup("itemParam4_2a8")
	skillPercent, _ := use.ref.NativeFields.Lookup("itemParam5_2ac")
	if seconds <= 0 {
		return false
	}
	for _, job := range c.ParamJobs {
		if (job.Param == paramPremiumExpRate || job.Param == paramPremiumSkillExpRate) && job.EndUnixMs > use.nowMs {
			*result = itemUseFailure(errCodePremiumActive)
			return false
		}
	}
	end := use.nowMs + int64(seconds)*1000
	var jobs []domain.ParamJob
	if !skillOnly && expPercent > 0 {
		jobs = append(jobs, domain.ParamJob{ItemRefObjID: use.ref.RefObjID, Codename: use.ref.Codename,
			Param: paramPremiumExpRate, Value: int64(expPercent), EndUnixMs: end})
	}
	if skillPercent > 0 {
		jobs = append(jobs, domain.ParamJob{ItemRefObjID: use.ref.RefObjID, Codename: use.ref.Codename,
			Param: paramPremiumSkillExpRate, Value: int64(skillPercent), EndUnixMs: end})
	}
	if len(jobs) == 0 {
		return false
	}
	next := append([]domain.ParamJob(nil), c.ParamJobs...)
	for _, job := range jobs {
		var placed bool
		if next, placed = upsertParamJob(next, job); !placed {
			return false
		}
	}
	c.ParamJobs = next
	remaining := rt.consumeItemUseRow(c, use.row)
	// One board row per ticket: both keepers share its reference.
	*result = OpResult{Frames: []wire.Frame{
		{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)},
		{Opcode: wire.OpParamJobStart, Payload: wire.EncodeParamJobRow(enterworld.ObjectIDForCharacter(c), paramJobRemaining(jobs[0], use.nowMs), use.ref.RefObjID)},
	}}
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	rt.paramJobOwners.track(use.division, c.Name)
	return true
}
