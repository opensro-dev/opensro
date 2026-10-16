/*
===========================================================================

petmanual.go - owner-triggered pet abilities (port-only expansion)

Buffs and heals apply on command without a basic attack. Offensive abilities
start an explicit target order immediately and retain normal approach/body
validation. The legacy seven-byte queue command remains compatible.

===========================================================================
*/
package action

import (
	"strings"

	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
manualPetSkill
================
*/
func (rt *Runtime) manualPetSkill(division string, owner, snapshot *enterworld.Character, gid uint32, id uint8, target uint32, now int64) OpResult {
	pet := snapshot.CompanionByGID(gid)
	if pet == nil || !pet.Summoned || pet.Mounted || pet.CurrentHP == 0 || !enterworld.CharacterAlive(snapshot) || !petAttackBodyAllowed(pet, combatTarget{}) {
		return petTrainingReceipt("A living, available pet and owner are required.")
	}
	validation := *pet
	validation.Training.Queued = 0
	if refusal := companion.Queue(&validation, id, now); refusal != "" {
		return petTrainingReceipt(refusal)
	}
	skill, _ := companion.PetSkill(pet, id)
	state := rt.petSessionFor(division, owner.Name, gid)
	ref, found := rt.cosReference(pet)
	if state == nil || !found {
		return petTrainingReceipt("The pet is not ready.")
	}
	switch skill.EffectName() {
	case "Challenging Roar":
		level := companion.EffectiveLevel(pet, skill, companion.ModifiersFor(pet))
		at := state.follower.Position(now)
		// Tier II admits party spawns; Tier III raises the target count.
		radius := float64(8+(skill.Option-1)*4) + float64(level-1)*0.4
		limit := 3
		if skill.Option == 3 {
			limit = 6
		}
		var victims []uint32
		if lease, ok := rt.casterPopulation(division, snapshot); ok && rt.Monsters != nil {
			for _, mob := range rt.Monsters.CombatCandidatesInPopulation(division, lease, at, radius*petAreaPerMeter, now, false) {
				if mob.Rarity()&0x0f == 3 || mob.Rarity()&0x0f == 8 || skill.Option == 1 && mob.Rarity()&0x10 != 0 {
					continue
				}
				if target, valid := rt.resolvePetCombatTarget(petCombatStep{key: petOwnerKey{division: division}, snapshot: snapshot, pet: pet, ref: ref, nowMs: now}, mob.Gid); valid && target.monster != nil {
					victims = append(victims, mob.Gid)
					if len(victims) == limit {
						break
					}
				}
			}
		}
		if len(victims) == 0 {
			return petTrainingReceipt("No eligible monsters are in roar range.")
		}
		if !rt.deps.Update(owner, "pet-manual-roar", func() bool {
			live := owner.CompanionByGID(gid)
			if live == nil || !companion.RowReady(&live.Training, skill.Row, now) {
				return false
			}
			companion.StartRow(&live.Training, skill.Row, skill, level, now, companion.ModifiersFor(live))
			live.Training.Queued = 0
			return true
		}) {
			return petTrainingReceipt("The roar could not be applied.")
		}
		for _, victim := range victims {
			rt.commitAggression(division, victim, simulation.HostilityEvent{Attacker: gid, Aggression: 1000000}, now)
		}
		return OpResult{Frames: rt.petTrainingStateFrames(owner.CompanionByGID(gid), now)}
	case "Iron Hide", "Frenzy", "Mend", "Retribution":
		step := petCombatStep{key: petOwnerKey{division: division, name: strings.ToLower(owner.Name), gid: gid}, state: state, snapshot: snapshot, pet: pet, ref: ref, nowMs: now}
		strike := &petTrainedStrike{mods: companion.ModifiersFor(pet), passive: -1}
		level := companion.EffectiveLevel(pet, skill, strike.mods)
		before := state.trained
		var frames []wire.Frame
		if !rt.deps.Update(owner, "pet-manual-skill", func() bool {
			live := owner.CompanionByGID(gid)
			if live == nil || !companion.RowReady(&live.Training, skill.Row, now) ||
				!rt.petApplyAction(step, &enterworld.SkillRow{}, skill, level, strike) {
				return false
			}
			strike.uses = []petRowUse{{row: skill.Row, skill: skill, level: level}}
			var committed bool
			frames, committed = rt.petTrainedCommit(step, owner, live, strike, 0, false, false)
			return committed
		}) {
			state.trained = before
			return petTrainingReceipt("The ability could not be applied.")
		}
		return OpResult{Frames: append(frames, strike.ownerState...), Broadcast: frames}
	default:
		if target == 0 {
			return petTrainingReceipt("Select an enemy for this pet ability.")
		}
		order := rt.orderPetAttack(division, owner, snapshot, gid, target, now)
		if order.DiagnosticRefusal != "" {
			return petTrainingReceipt("That enemy cannot be attacked by your pet.")
		}
		if !rt.deps.Update(owner, "pet-manual-target", func() bool {
			live := owner.CompanionByGID(gid)
			return live != nil && companion.Queue(live, id, now) == ""
		}) {
			return petTrainingReceipt("The ability could not be applied.")
		}
		if state.combat != nil && state.combat.castToken == 0 {
			state.combat.nextAttackMs = now
		}
		order.Frames = append(order.Frames, rt.petTrainingStateFrames(owner.CompanionByGID(gid), now)...)
		return order
	}
}
