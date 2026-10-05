/*
===========================================================================

countjob.go - a premium package's limited uses (CUsedItemLimit)

A Gold Time package's UIL1 entries (compositeitem.go) let its owner use an
item's effect a few times each period without holding the item: instant
return and reverse return three times a day, resurrection once. The chat
commands /Return, /Reverse Return and /Resurrection send 0x76FD for the
board row (item/wire/countjob.go); CUsedItemLimit (SR_GameServer vtable
B05164) runs the item's own rule:

  - CUsedItemLimit_vf1C 654080 dispatches by the item's type: the return
    scroll (CheckReturnScrollLocationValidity), the reverse return
    (UseReverseReturnScroll, the client's chosen point) and the
    resurrection scroll (CheckPlayerReturnCondition);
  - a success spends one use (CCompositeItemWork_LogEvent 6538D0);
  - CUsedObjectLimit_Tick 653C30 refills the uses to the maximum when the
    period passes (CUsedObjectLimit_Refill 653A40: the next refill moves on
    by whole periods);
  - the work ends with its package (the keeper's end time).

A refusal answers [2][code] with the item rule's own category-1 code; an
exhausted work 0xC6 (UIIT_MSG_PREMIUM_USE_OVER) and a missing one 0xCC
(UIIT_MSG_PREMIUM_NOT_USE), the codes the client's own pre-checks raise.

INFERENCE: the limited use publishes no item-use visual (0x3449); the
item rules it shares publish theirs from the bag use only.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// errCodePremiumUseOver / errCodePremiumNotUse are category-1 notices
	// 454 and 460.
	errCodePremiumUseOver uint8 = 0xc6
	errCodePremiumNotUse  uint8 = 0xcc
)

// limitedUseKind is the item rule a UIL1 work runs (654080's type switch).
type limitedUseKind uint8

const (
	limitedUseNone limitedUseKind = iota
	limitedUseReturn
	limitedUseReverseReturn
	limitedUseResurrection
)

/*
================
limitedUseKindOf
================
*/
func limitedUseKindOf(ref *enterworld.ItemRef) limitedUseKind {
	switch {
	case ref.TypeIDs == [4]int64{3, 3, 3, 1} && ref.ReturnDestination == "RESURRECT":
		return limitedUseReturn
	case ref.TypeIDs == [4]int64{3, 3, 3, 3}:
		return limitedUseReverseReturn
	case ref.TypeIDs == [4]int64{3, 3, 13, 6}:
		return limitedUseResurrection
	}
	return limitedUseNone
}

/*
================
countJobStartFrame
================
*/
func countJobStartFrame(job domain.CompositeJob, nowMs int64) wire.Frame {
	return wire.Frame{Opcode: wire.OpCountJobStart, Payload: wire.EncodeCountJobStart(job.PackageRefObjID,
		enterworld.PetSkillWindowRemaining(job.EndUnixMs, nowMs), job.Target, job.Uses)}
}

/*
================
refillCompositeJob

CUsedObjectLimit_Refill: once the refill time passes, the uses return to
the maximum and the next refill moves on by whole periods.
================
*/
func refillCompositeJob(job *domain.CompositeJob, nowMs int64) bool {
	if job.MaxUses == 0 || job.PeriodSeconds <= 0 || nowMs < job.NextRefillUnixMs {
		return false
	}
	period := job.PeriodSeconds * 1000
	job.NextRefillUnixMs += ((nowMs-job.NextRefillUnixMs)/period + 1) * period
	job.Uses = job.MaxUses
	return true
}

/*
================
HandleCountJobUse

0x76FD: one limited use. The item rule runs inside the character door with
no bag row; the work spends a use only when it succeeds.
================
*/
func (rt *Runtime) HandleCountJobUse(division string, c *enterworld.Character, payload []byte) OpResult {
	refuse := func(code uint8) OpResult {
		return OpResult{Frames: []wire.Frame{{Opcode: wire.OpCountJobAnswer, Payload: wire.EncodeCountJobRefusal(code)}}}
	}
	if c == nil {
		return OpResult{}
	}
	pack, item, choice, err := wire.DecodeCountJobUse(payload)
	if err != nil {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	now := rt.Now().UnixMilli()
	var effect OpResult
	var revived *revivedWhereDead
	code := errCodePremiumNotUse
	committed := rt.deps.Update(c, "count-job-use", func() bool {
		index := -1
		for i, job := range c.CompositeJobs {
			if job.PackageRefObjID == pack && job.Target == item && job.Kind == domain.CompositeUsedItemLimit &&
				job.EndUnixMs > now {
				index = i
				break
			}
		}
		if index < 0 || c.DeletePending {
			return false
		}
		job := c.CompositeJobs[index]
		refillCompositeJob(&job, now)
		if job.Uses == 0 {
			code = errCodePremiumUseOver
			return false
		}
		ref, found := rt.deps.ItemReferences().ItemRefByCodename(job.TargetCodename)
		if !found || ref == nil {
			return false
		}
		effect = itemUseFailure(wire.ErrCodeInvalidRequest)
		if !rt.runLimitedUse(division, c, ref, choice, now, &effect, &revived) {
			code = itemUseFailureCode(effect)
			return false
		}
		job.Uses--
		jobs := append([]domain.CompositeJob(nil), c.CompositeJobs...)
		jobs[index] = job
		c.CompositeJobs = jobs
		return true
	})
	if !committed {
		return refuse(code)
	}
	result := OpResult{Frames: []wire.Frame{{Opcode: wire.OpCountJobAnswer, Payload: wire.EncodeCountJobSpent(pack, item)}}}
	result.Frames = append(result.Frames, effect.Frames...)
	result.Broadcast = effect.Broadcast
	if revived != nil {
		actor, peers := rt.revivalFrames(division, c, *revived, now)
		result.Frames = append(result.Frames, actor...)
		result.Broadcast = append(result.Broadcast, peers...)
	}
	return result
}

/*
================
itemUseFailureCode

The notice code of an item rule's [2][code] refusal.
================
*/
func itemUseFailureCode(result OpResult) uint8 {
	for _, frame := range result.Frames {
		if frame.Opcode == wire.OpItemUseResponse && len(frame.Payload) == 2 && frame.Payload[0] == wire.ResultError {
			return frame.Payload[1]
		}
	}
	return wire.ErrCodeInvalidRequest
}

/*
================
runLimitedUse

654080's type switch over the item rules, with no bag row. Runs inside the
character door; a resurrection leaves its revival for the caller to
publish after the commit.
================
*/
func (rt *Runtime) runLimitedUse(division string, c *enterworld.Character, ref *enterworld.ItemRef, choice uint8, now int64, result *OpResult, revived **revivedWhereDead) bool {
	switch limitedUseKindOf(ref) {
	case limitedUseReturn:
		duration, mode, ok := returnScrollTiming(ref)
		if !ok || mode != teleportModeBlocking || !rt.returnScrollAdmission(division, c, result) {
			return false
		}
		at := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now)
		if !rt.startReturnCast(returnCast{division: division, character: c, row: -1, duration: duration, now: now, mode: mode}, result) {
			return false
		}
		c.World.LastRecallPoint = worldSpawnFromMission(at)
		return true
	case limitedUseReverseReturn:
		if choice != reverseReturnLastRecall && choice != reverseReturnLastDeath {
			return false
		}
		duration, ok := returnScrollDuration(ref)
		if !ok || !rt.returnScrollAdmission(division, c, result) {
			return false
		}
		destination, refusal := reverseReturnPoint(c, choice)
		if refusal != 0 {
			*result = itemUseFailure(refusal)
			return false
		}
		return rt.startReturnCast(returnCast{division: division, character: c, row: -1, duration: duration,
			destination: &destination, now: now}, result)
	case limitedUseResurrection:
		done, ok := rt.resurrectWithScroll(division, c, ref, now, result)
		if ok {
			*revived = &done
		}
		return ok
	}
	return false
}

/*
================
advanceCompositeJobs

One tick of an online owner's composite works: refilled limits are
re-raised on the board (end, then start: the client's count map keeps
the first insert of a key), ended works leave it.
================
*/
func (rt *Runtime) advanceCompositeJobs(character *enterworld.Character, nowMs int64) []wire.Frame {
	due := false
	for _, job := range character.CompositeJobs {
		if job.EndUnixMs <= nowMs || job.MaxUses != 0 && job.Uses < job.MaxUses && nowMs >= job.NextRefillUnixMs {
			due = true
			break
		}
	}
	if !due {
		return nil
	}
	var frames []wire.Frame
	rt.deps.Update(character, "composite-job-tick", func() bool {
		frames = nil
		kept := make([]domain.CompositeJob, 0, len(character.CompositeJobs))
		for _, job := range character.CompositeJobs {
			board := job.Kind == domain.CompositeUsedItemLimit
			if job.EndUnixMs <= nowMs {
				if board {
					frames = append(frames, wire.Frame{Opcode: wire.OpCountJobEnd, Payload: wire.EncodeCountJobEnd(job.PackageRefObjID, job.Target)})
				}
				continue
			}
			if refillCompositeJob(&job, nowMs) && board {
				frames = append(frames, wire.Frame{Opcode: wire.OpCountJobEnd, Payload: wire.EncodeCountJobEnd(job.PackageRefObjID, job.Target)},
					countJobStartFrame(job, nowMs))
			}
			kept = append(kept, job)
		}
		character.CompositeJobs = kept
		return true
	})
	return frames
}
