/*
===========================================================================
petcrow.go - port-only Mana Spring recovery after one committed bird attack.

Actual monster HP damage funds MP; one attack shortens skill reuse once,
regardless of impact or victim count. Items and action/animation gates remain native.
===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
crowManaSpring
================
*/
func (rt *Runtime) crowManaSpring(step petCombatStep, damage uint64) {
	if step.trained == nil || !rt.petExpansionAllowed(step.snapshot, step.pet) || companion.PetKind(step.pet.Codename) != "Crow" {
		return
	}
	skill, level, ok := companion.Passive(step.pet, "Mana Spring", companion.ModifiersFor(step.pet))
	if !ok {
		return
	}
	if damage == 0 {
		return
	}
	mp := int64(float64(damage) * skill.ValueAt(level) / 100)
	for gid, member := range rt.crowSupportRecipients(step, skill.Option >= 2, false) {
		var frames []wire.Frame
		var petFrames []wire.Frame
		recovered := false
		if !rt.deps.UpdateDeferred(member, "crow-mana-spring", func() bool {
			if !enterworld.CharacterAlive(member) {
				return false
			}
			if mp > 0 {
				if frame, ok := rt.applySkillRecovery(step.key.division, member, 0, mp); ok && frame.Opcode != 0 {
					frames = append(frames, frame)
				}
			}
			if skill.Option == 3 {
				recovered = crowRecoverCooldowns(member, step.nowMs)
				for _, pet := range member.Companions() {
					if !pet.Summoned || pet.CurrentHP == 0 {
						continue
					}
					if companion.RecoverPetCooldowns(pet, step.nowMs, 500) {
						petFrames = append(petFrames, rt.petTrainingStateFrames(pet, step.nowMs)...)
					}
				}
			}
			return true
		}) {
			continue
		}
		if len(frames) > 0 || recovered || len(petFrames) > 0 {
			rt.crowRecipientVisual(step, member, crowManaVisual, uint8(skill.Option))
		}
		if len(frames) > 0 {
			rt.queueMonsterLegFrames([]simulation.DivisionFrames{{DivisionID: step.key.division, SourceGID: gid, Frames: simFrames(frames)}})
		}
		if recovered {
			frame := wire.Frame{Opcode: 0x3fb8, Payload: wire.NewWriter(2).U16(500).Payload()}
			petFrames = append(petFrames, frame)
		}
		if len(petFrames) > 0 {
			rt.queueMonsterLegRecipients(step.key.division, []RecipientFrames{{CharacterID: member.ID, Frames: petFrames}})
		}
	}
}

/*
================
crowRecoverCooldowns

Both admission maps represent the same reuse; shorten each without double payment.
================
*/
func crowRecoverCooldowns(c *enterworld.Character, now int64) bool {
	changed := false
	for group, until := range c.OffensiveSkillCooldowns {
		if until > now {
			c.OffensiveSkillCooldowns[group] = max(now, until-500)
			changed = true
		}
	}
	for group, until := range c.SharedSkillCooldowns {
		if until > now {
			c.SharedSkillCooldowns[group] = max(now, until-500)
			changed = true
		}
	}
	return changed
}
