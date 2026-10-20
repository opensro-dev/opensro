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

	"opensro.online/server/internal/domain"
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
	if state == nil || state.follower == nil || !found {
		return petTrainingReceipt("The pet is not ready.")
	}
	switch skill.EffectName() {
	case "Challenging Roar":
		level := companion.EffectiveLevel(pet, skill, companion.ModifiersFor(pet))
		if rt.inTown(division, snapshot) || townRegion(state.follower.Position(now).RegionID) {
			return petTrainingReceipt("Challenging Roar cannot be cast in town.")
		}
		empowered := false
		// updated is captured inside the roar door; the pet is never
		// re-read unlocked after it closes (checklist item 2).
		var updated *domain.CharacterCOS
		if !rt.deps.Update(owner, "pet-manual-roar", func() bool {
			live := owner.CompanionByGID(gid)
			if live == nil || !companion.RowReady(&live.Training, skill.Row, now) {
				return false
			}
			companion.StartRow(&live.Training, skill.Row, skill, level, now, companion.ModifiersFor(live))
			live.Training.Queued = 0
			empowered = rt.spendPetSpirit(owner, live)
			copy := *live
			updated = &copy
			return true
		}) {
			return petTrainingReceipt("The roar could not be applied.")
		}
		state.roar = &petRoarCast{skill: skill, level: level, releaseAtMs: now + petRoarCastMs, empowered: empowered}
		var public []wire.Frame
		for _, frame := range state.follower.Stop(now) {
			public = append(public, wire.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
		}
		if empowered {
			public = append(public, petSpiritFrame(updated, now, now), wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.HPRefreshPayload(gid, 0, updated.CurrentHP)})
		}
		public = append(public, wire.Frame{Opcode: wire.OpActionEmote,
			Payload: wire.ActionEmotePush{Gid: gid, Action: skill.ID}.Encode()})
		return OpResult{
			Frames:    append(append([]wire.Frame{}, public...), rt.petTrainingStateFrames(updated, now)...),
			Broadcast: public,
		}
	case "Iron Hide":
		return rt.castPetShield(division, owner, snapshot, pet, skill, now)
	case "Frenzy", "Mend", "Retribution":
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

// Port-only: match the one-second visual charge before applying aggression.
const petRoarCastMs int64 = 1000

/*
================
petRoarCast
================
*/
type petRoarCast struct {
	empowered   bool
	skill       companion.SkillDef
	level       uint8
	releaseAtMs int64
}

/*
================
advancePetRoar

The existing pet tick owns the windup; no background timer or delayed write.
================
*/
func (rt *Runtime) advancePetRoar(step petCombatStep) bool {
	cast := step.state.roar
	if cast == nil {
		return false
	}
	if rt.inTown(step.key.division, step.snapshot) || townRegion(step.state.follower.Position(step.nowMs).RegionID) {
		step.state.roar = nil
		return false
	}
	if step.nowMs < cast.releaseAtMs {
		return true
	}
	step.state.roar = nil
	if !petAttackBodyAllowed(step.pet, combatTarget{}) || rt.Monsters == nil {
		return true
	}
	lease, ok := rt.casterPopulation(step.key.division, step.snapshot)
	if !ok {
		return true
	}
	at := step.state.follower.Position(step.nowMs)
	limit := 3
	if cast.skill.Option == 3 {
		limit = 6
	}
	radius := cast.skill.RoarRadiusMeters(cast.level) * petAreaPerMeter
	if cast.empowered {
		radius *= 1.5
		limit = limit * 3 / 2
	}
	count := 0
	for _, mob := range rt.Monsters.CombatCandidatesInPopulation(step.key.division, lease, at, radius, step.nowMs, false) {
		if mob.Rarity()&0x0f == 3 || mob.Rarity()&0x0f == 8 || cast.skill.Option == 1 && mob.Rarity()&0x10 != 0 {
			continue
		}
		if target, valid := rt.resolvePetCombatTarget(step, mob.Gid); valid && target.monster != nil {
			rt.commitAggression(step.key.division, mob.Gid, simulation.HostilityEvent{Attacker: step.pet.GID, Aggression: 1000000}, step.nowMs)
			count++
			if count == limit {
				break
			}
		}
	}
	return true
}
