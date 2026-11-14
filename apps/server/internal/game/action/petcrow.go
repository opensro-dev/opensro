/*
===========================================================================
petcrow.go - port-only Mana Spring recovery after one committed bird attack.

Actual monster HP damage funds MP; only mana actually restored funds the
third book's true-damage burst. Items and action/animation gates remain native.
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
	var restored uint64
	for gid, member := range rt.crowSupportRecipients(step, skill.Option >= 2, false) {
		var frames []wire.Frame
		if !rt.deps.UpdateDeferred(member, "crow-mana-spring", func() bool {
			if !enterworld.CharacterAlive(member) {
				return false
			}
			if mp > 0 {
				before := int64(0)
				if member.CurrentMP != nil {
					before = *member.CurrentMP
				}
				if frame, ok := rt.applySkillRecovery(step.key.division, member, 0, mp); ok && frame.Opcode != 0 {
					frames = append(frames, frame)
					if member.CurrentMP != nil {
						restored += uint64(max(int64(0), *member.CurrentMP-before))
					}
				}
			}
			return true
		}) {
			continue
		}
		if len(frames) > 0 {
			rt.crowRecipientVisual(step, member, crowManaVisual, uint8(skill.Option))
		}
		if len(frames) > 0 {
			rt.queueMonsterLegFrames([]simulation.DivisionFrames{{DivisionID: step.key.division, SourceGID: gid, Frames: simFrames(frames)}})
		}
	}
	if skill.Option == 3 {
		step.state.crowManaRestored += restored
		bursts := step.state.crowManaRestored / 10000
		step.state.crowManaRestored %= 10000
		// The burst lands where the crow stands: its pose comes from the
		// companion's one pose owner (simulation.CompanionMover, #3464).
		if at, ok := step.state.mover.Pose(step.pet.GID, step.nowMs); bursts > 0 && ok {
			// One aggregated hit bounds work when a single attack restores several thresholds.
			rt.petGearArea(step, at, uint32(min(uint64(^uint32(0)), bursts*5000)), 50, -1, false)
		}
	}
}
