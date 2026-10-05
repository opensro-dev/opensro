/*
===========================================================================

compositeitem.go - composite scrolls and the premium packages

A composite (TID 3/3/13/14) lists what it does in its parameter
descriptions: each of Param2..Param4 that is 1 marks its description as a
list of "[TAG:value, args...]" entries (every shipped composite; the EXP
scrolls use Param2 only, the Gold Time packages Param2..Param4).
CGItemExpendable_ApplyParamJobs (SR_GameServer 49F590) installs every entry
or none:

	UIU1:<item>                   an internal item: a param item (TID 3/3/3/10,
	                              CTJ_CharParamKeeper) or a premium ticket
	                              (3/3/13/4 or 5, CTJ_PremiumKeeper; 0x1894
	                              while one runs)
	USU1:<skill>                  a persistent item-program buff (the APRU
	                              skills: luck, alchemy luck, drop, STR/INT)
	UIL1:<item>, A, period, n     CUsedItemLimit: the item's effect n times
	                              each period, until the package ends
	UQL1:<quest>, A, period, n    CUsedQuestLimit: the premium quest n times
	                              each period (quest/premiumlimit.go)
	BFI1:<item>, A, seconds       CBuffItem: a stall booth decoration

The package's Param1 is its period in seconds; UIL1 and UQL1 end with it,
BFI1 runs its own seconds. Pet composites (PSU1, BCS1) are pet-owned and
never reach this owner.

INFERENCE: the 'A' field is read into the work but changes nothing a v1.150
client can see; every shipped entry authors it.

===========================================================================
*/

package action

import (
	"strconv"
	"strings"
	"sync/atomic"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
)

// Composite entry tags (49F590's FourCC switch).
const (
	compositeUseInternalItem = "UIU1"
	compositeUseSkill        = "USU1"
)

/*
================
compositeListParam

The param whose value 1 marks ParamDescriptions[index] (1..3) as an entry
list.
================
*/
func compositeListParam(index int) string {
	switch index {
	case 1:
		return "itemParam2_2a0"
	case 2:
		return "itemParam3_2a4"
	case 3:
		return "itemParam4_2a8"
	}
	return ""
}

/*
================
compositeEntry
================
*/
type compositeEntry struct {
	tag  string
	args []string
}

/*
================
compositeEntries

Every entry of a composite's marked descriptions. An entry is
"[TAG:value]" or "[TAG:value, arg, ...]"; entries are separated by commas.
================
*/
func compositeEntries(ref *enterworld.ItemRef) ([]compositeEntry, bool) {
	var out []compositeEntry
	for index := 1; index <= 3; index++ {
		if flag, _ := ref.NativeFields.Lookup(compositeListParam(index)); flag != 1 {
			continue
		}
		text := strings.TrimSpace(ref.ParamDescriptions[index])
		for text != "" {
			if text[0] != '[' {
				return nil, false
			}
			end := strings.IndexByte(text, ']')
			if end < 0 {
				return nil, false
			}
			tag, value, found := strings.Cut(text[1:end], ":")
			if !found || strings.TrimSpace(tag) == "" {
				return nil, false
			}
			args := strings.Split(value, ",")
			for i := range args {
				args[i] = strings.TrimSpace(args[i])
			}
			if args[0] == "" {
				return nil, false
			}
			out = append(out, compositeEntry{tag: strings.TrimSpace(tag), args: args})
			text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text[end+1:]), ","))
		}
	}
	return out, len(out) > 0
}

/*
================
compositePlan

Everything one composite installs, built before any state changes.
================
*/
type compositePlan struct {
	paramJobs []domain.ParamJob
	clock     *domain.PremiumClock
	skills    []enterworld.SkillRow
	works     []domain.CompositeJob
	// refs reach the browser with the use: internal and limited items are
	// never carried, so their references may be unknown to it.
	refs []inventory.Item
}

/*
================
compositeWorkArgs

A limit entry's period and count ([value, A, period, n]) or a booth's
seconds ([value, A, seconds]).
================
*/
func compositeWorkArgs(entry compositeEntry, counted bool) (int64, uint8, bool) {
	want := 3
	if counted {
		want = 4
	}
	if len(entry.args) != want {
		return 0, 0, false
	}
	seconds, err := strconv.ParseInt(entry.args[2], 10, 64)
	if err != nil || seconds <= 0 || seconds > 1<<31 {
		return 0, 0, false
	}
	if !counted {
		return seconds, 0, true
	}
	count, err := strconv.ParseUint(entry.args[3], 10, 8)
	if err != nil || count == 0 {
		return 0, 0, false
	}
	return seconds, uint8(count), true
}

/*
================
planComposite

Resolves every entry of a composite. A refusal code is a category-1 notice;
an empty diagnostic with no code is malformed reference data.
================
*/
func (rt *Runtime) planComposite(character *enterworld.Character, ref *enterworld.ItemRef, nowMs int64) (compositePlan, uint8, string) {
	var plan compositePlan
	entries, ok := compositeEntries(ref)
	if !ok {
		return plan, 0, "malformed composite list " + ref.Codename
	}
	packageSeconds, _ := ref.NativeFields.Lookup("itemParam1_29c")
	packageEnd := nowMs + int64(packageSeconds)*1000
	items := rt.deps.ItemReferences()
	for _, entry := range entries {
		switch entry.tag {
		case compositeUseInternalItem:
			internal, found := items.ItemRefByCodename(entry.args[0])
			if !found || internal == nil {
				return plan, 0, "composite internal item " + entry.args[0] + " is unknown"
			}
			plan.refs = append(plan.refs, inventory.Item{RefObjID: internal.RefObjID, Codename: internal.Codename, TypeFlags: internal.TypeFlags()})
			if job, valid := paramJobFromItem(internal, nowMs); valid {
				plan.paramJobs = append(plan.paramJobs, job)
				continue
			}
			family := admittedItemUseFamily(internal)
			if family != itemUsePremiumTicket && family != itemUseSkillTimeTicket {
				return plan, 0, "composite internal item " + entry.args[0] + " is neither a param item nor a ticket"
			}
			if premiumRunning(character, nowMs) || plan.clock != nil {
				return plan, errCodePremiumActive, ""
			}
			jobs, clock, valid := premiumTicketPlan(internal, nowMs, family == itemUseSkillTimeTicket)
			if !valid {
				return plan, 0, "composite ticket " + entry.args[0] + " is malformed"
			}
			plan.paramJobs = append(plan.paramJobs, jobs...)
			plan.clock = clock
		case compositeUseSkill:
			skill, found := rt.compositeSkill(entry.args[0])
			if !found {
				return plan, 0, "composite skill " + entry.args[0] + " has no item program"
			}
			plan.skills = append(plan.skills, skill)
		case domain.CompositeUsedItemLimit, domain.CompositeUsedQuestLimit:
			period, count, valid := compositeWorkArgs(entry, true)
			if !valid || packageSeconds <= 0 {
				return plan, 0, "composite limit " + entry.args[0] + " is malformed"
			}
			work := domain.CompositeJob{Kind: entry.tag, PackageRefObjID: ref.RefObjID, Uses: count,
				MaxUses: count, PeriodSeconds: period, NextRefillUnixMs: nowMs + period*1000, EndUnixMs: packageEnd}
			if entry.tag == domain.CompositeUsedQuestLimit {
				work.QuestCodename = entry.args[0]
			} else {
				limited, found := items.ItemRefByCodename(entry.args[0])
				if !found || limited == nil || limitedUseKindOf(limited) == limitedUseNone {
					return plan, 0, "composite limited item " + entry.args[0] + " has no limited use"
				}
				work.Target, work.TargetCodename = limited.RefObjID, limited.Codename
				plan.refs = append(plan.refs, inventory.Item{RefObjID: limited.RefObjID, Codename: limited.Codename, TypeFlags: limited.TypeFlags()})
			}
			plan.works = append(plan.works, work)
		case domain.CompositeBuffItem:
			seconds, _, valid := compositeWorkArgs(entry, false)
			booth, found := items.ItemRefByCodename(entry.args[0])
			if !valid || !found || booth == nil {
				return plan, 0, "composite booth " + entry.args[0] + " is malformed"
			}
			plan.works = append(plan.works, domain.CompositeJob{Kind: entry.tag, PackageRefObjID: ref.RefObjID,
				Target: booth.RefObjID, TargetCodename: booth.Codename, EndUnixMs: nowMs + seconds*1000})
		default:
			return plan, 0, "composite entry " + entry.tag + " has no owner (" + ref.Codename + ")"
		}
	}
	return plan, 0, ""
}

/*
================
compositeSkill

A USU1 skill: a persistent, pinned item program (skillitemeffect.go).
================
*/
func (rt *Runtime) compositeSkill(codename string) (enterworld.SkillRow, bool) {
	source, ok := rt.deps.SkillData().(interface {
		SkillByCodename(string) (enterworld.SkillRow, bool)
	})
	if !ok {
		return enterworld.SkillRow{}, false
	}
	skill, found := source.SkillByCodename(codename)
	return skill, found && skill.TimedEffect.Pinned && skill.TimedEffect.ItemProgram && skill.EffectDurationMs > 0
}

/*
================
useCompositeScroll

TID 3/3/13/14 inside the item-use door: plan every entry before any state
changes, debit the cooldown, consume one composite and install the plan.
================
*/
func (rt *Runtime) useCompositeScroll(divisionID string, character *enterworld.Character, ref *enterworld.ItemRef, rowIndex int, request wire.ItemUseRequest, nowMs int64, result *OpResult) bool {
	plan, refusal, diagnostic := rt.planComposite(character, ref, nowMs)
	if refusal != 0 {
		*result = itemUseFailure(refusal)
		return false
	}
	if diagnostic != "" {
		result.DiagnosticRefusal = "item-use: " + diagnostic
		return false
	}
	group, cooltimeMs, hasCooltime := itemCooltime(ref)
	if hasCooltime && character.ItemGroupCooldowns[group] > nowMs {
		*result = itemUseFailure(wire.ErrCodeItemReuseDelay)
		return false
	}
	if len(plan.skills) > 0 {
		if _, err := rt.PlayerBaseStats(divisionID, character); err != nil {
			result.DiagnosticRefusal = "item-use: character stats unavailable"
			return false
		}
	}
	if !installParamJobs(character, plan.paramJobs) {
		return false
	}
	var effectFrames []wire.Frame
	for _, skill := range plan.skills {
		token := atomic.AddUint32(&rt.castTokenCounter, 1)
		if token == 0 {
			token = atomic.AddUint32(&rt.castTokenCounter, 1)
		}
		frames, applied := rt.commitCharacterEffect(divisionID, character, skill, token,
			statuseffect.StateActive, false, EffectPresentation{Phase: 2}, nowMs)
		if !applied {
			return false
		}
		effectFrames = append(effectFrames, frames...)
	}
	if plan.clock != nil {
		character.PremiumClock = plan.clock
	}
	character.CompositeJobs = append(append([]domain.CompositeJob(nil), character.CompositeJobs...), plan.works...)
	if hasCooltime {
		if character.ItemGroupCooldowns == nil {
			character.ItemGroupCooldowns = map[uint32]int64{}
		}
		character.ItemGroupCooldowns[group] = nowMs + cooltimeMs
	}
	remaining := rt.consumeItemUseRow(character, rowIndex)
	owner := enterworld.ObjectIDForCharacter(character)
	frames := []wire.Frame{
		{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)},
	}
	if len(plan.refs) > 0 {
		frames = append(frames, rt.commerceReferences(plan.refs, nil))
	}
	// One board row per internal item, whatever keepers it raised.
	shown := map[uint32]bool{}
	for _, job := range plan.paramJobs {
		if !shown[job.ItemRefObjID] {
			shown[job.ItemRefObjID] = true
			frames = append(frames, wire.Frame{Opcode: wire.OpParamJobStart,
				Payload: wire.EncodeParamJobRow(owner, paramJobRemaining(job, nowMs), job.ItemRefObjID)})
		}
	}
	for _, work := range plan.works {
		if work.Kind == domain.CompositeUsedItemLimit {
			frames = append(frames, countJobStartFrame(work, nowMs))
		}
	}
	frames = append(frames, effectFrames...)
	*result = OpResult{Frames: append(frames, rt.updateQuestInventory(character)...), Broadcast: effectFrames}
	if len(plan.skills) > 0 {
		result.Frames = append(result.Frames, rt.gaugeDropFrames(divisionID, character, true, true, true)...)
	}
	rt.paramJobOwners.track(divisionID, character.Name)
	return true
}
