/*
===========================================================================
petplume.go - port-only Rejuvenating Plume: one heal followed by five pulses.

The pet session owns this short-lived cast. Recipients are fixed at casting;
each pulse rechecks life, party membership, range and source eligibility.
===========================================================================
*/
package action

import (
	"sort"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
crowPlumeCast
================
*/
type crowPlumeCast struct {
	book       uint8
	percent    float64
	recipients map[uint32]float64
	next       int64
	left       uint8
}

/*
================
castCrowPlume
================
*/
func (rt *Runtime) castCrowPlume(division string, owner *enterworld.Character, gid uint32, skill companion.SkillDef, now int64) OpResult {
	step, ok := rt.elementalCompanion(division, owner, gid, "Crow", now)
	if !ok {
		return petTrainingReceipt("Rejuvenating Plume requires a living nearby crow outside town.")
	}
	level := companion.EffectiveLevel(step.pet, skill, companion.ModifiersFor(step.pet))
	if level == 0 {
		return petTrainingReceipt("Learn this ability first.")
	}
	var frames []wire.Frame
	if !rt.deps.Update(owner, "crow-plume-cast", func() bool {
		live := owner.CompanionByGID(gid)
		if live == nil || !companion.RowReady(&live.Training, skill.Row, now) {
			return false
		}
		companion.StartRow(&live.Training, skill.Row, skill, level, now, companion.ModifiersFor(live))
		live.Training.Queued = 0
		frames = rt.petTrainingStateFrames(live, now)
		return true
	}) {
		return petTrainingReceipt("The ability could not be applied.")
	}
	frames = append(frames, crowVisualFrame(crowVisual{gid: gid, kind: crowPlumeVisual, book: uint8(skill.Option), at: step.state.follower.Position(now)}))
	members := rt.crowSupportRecipients(step, skill.Option == 3, false)
	ownerGID := enterworld.ObjectIDForCharacter(step.snapshot)
	var ordered []uint32
	for member := range members {
		if member != ownerGID {
			ordered = append(ordered, member)
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	ordered = append([]uint32{ownerGID}, ordered...)
	cast := &crowPlumeCast{book: skill.ID, percent: skill.ValueAt(level), recipients: make(map[uint32]float64), next: now + 1000, left: 5}
	scale := 1.0
	for _, member := range ordered {
		cast.recipients[member] = scale
		scale *= 0.8
	}
	step.state.plume = cast
	rt.healCrowPlume(step, cast, 0.5, skill.Option >= 2)
	return OpResult{Frames: frames}
}

/*
================
advanceCrowPlume
================
*/
func (rt *Runtime) advanceCrowPlume(key petOwnerKey, now int64) {
	state := rt.petSessionFor(key.division, key.name, key.gid)
	if state == nil || state.plume == nil {
		return
	}
	step, ok := rt.elementalCompanion(key.division, state.character, key.gid, "Crow", now)
	if !ok {
		state.plume = nil
		return
	}
	cast := state.plume
	for cast.left > 0 && cast.next <= now {
		rt.healCrowPlume(step, cast, 0.1, false)
		cast.left--
		cast.next += 1000
	}
	if cast.left == 0 {
		state.plume = nil
	}
}

/*
================
healCrowPlume
================
*/
func (rt *Runtime) healCrowPlume(step petCombatStep, cast *crowPlumeCast, share float64, cleanse bool) {
	skill, _ := companion.PetSkill(step.pet, cast.book)
	for gid, member := range rt.crowSupportRecipients(step, skill.Option == 3, false) {
		scale, selected := cast.recipients[gid]
		if !selected {
			continue
		}
		var public, private []wire.Frame
		rt.deps.Update(member, "crow-plume-heal", func() bool {
			if !enterworld.CharacterAlive(member) {
				delete(cast.recipients, gid)
				return false
			}
			if frame, ok := rt.petOwnerHeal(step.key.division, member, cast.percent*share*scale); ok {
				public = append(public, frame)
			}
			if member.ID != step.state.character.ID {
				return true
			}
			if cleanse {
				owner := rt.newPlayerAbnormalOwner(step.key.division, member, step.nowMs)
				for _, status := range []abnormal.Status{abnormal.Poison, abnormal.Burn} {
					if owner.block.Clear(owner, status) {
						owner.changed = true
						break
					}
				}
				owner.commit()
				cured := rt.playerAbnormalPublication(step.key.division, member, owner)
				public, private = append(public, cured.public...), append(private, cured.actor...)
			}
			if skill.Option >= 2 {
				pet := member.CompanionByGID(step.pet.GID)
				if pet != nil && pet.Summoned && pet.CurrentHP > 0 {
					amount := uint32(float64(step.ref.MaxHP) * cast.percent * share / 100)
					pet.CurrentHP = uint32(min(uint64(step.ref.MaxHP), uint64(pet.CurrentHP)+uint64(amount)))
					public = append(public, wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.HPRefreshPayload(pet.GID, 0, pet.CurrentHP)})
				}
			}
			return true
		})
		if share == 0.5 || len(public) > 0 {
			rt.crowRecipientVisual(step, member, crowPlumeVisual, uint8(skill.Option))
		}
		if len(public) > 0 {
			rt.queueMonsterLegFrames([]simulation.DivisionFrames{{DivisionID: step.key.division, SourceGID: gid, Frames: simFrames(public)}})
		}
		if len(private) > 0 {
			rt.queueMonsterLegRecipients(step.key.division, []RecipientFrames{{CharacterID: member.ID, Frames: private}})
		}
	}
}
