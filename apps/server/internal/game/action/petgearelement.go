/*
===========================================================================
petgearelement.go - visible elemental riders and four one-second poison ticks

Port-only gear damage uses the credited hit door and native yellow 3128 text.
It cannot recurse into proc rolls, counters, leech or attack animations.
===========================================================================
*/
package action

import "opensro.online/server/internal/game/companion"

/*
================
petGearPoison
================
*/
type petGearPoison struct {
	damage uint32
	left   uint8
	next   int64
}

/*
================
petGearElements

One rider per committed damaging action/victim. Poison refreshes its amount
and duration without postponing an already scheduled tick or stacking copies.
================
*/
func (rt *Runtime) petGearElements(step petCombatStep, gid uint32) {
	m := step.trained.mods
	for _, amount := range []float64{m.FireAdd, m.ColdAdd, m.ShockAdd} {
		if amount <= 0 {
			continue
		}
		if victim, ok := rt.resolvePetCombatTarget(step, gid); ok && victim.monster != nil {
			rt.wolfBonusHit(step, *victim.monster, uint32(amount), "pet-gear-element")
		}
	}
	if m.PoisonAdd <= 0 {
		return
	}
	run := &step.state.trained
	if run.poison == nil {
		run.poison = make(map[uint32]petGearPoison)
	}
	old, exists := run.poison[gid]
	if !exists && len(run.poison) >= 64 {
		return
	}
	if !exists {
		old.next = step.nowMs + 1000
	}
	old.damage, old.left = uint32(m.PoisonAdd), 4
	run.poison[gid] = old
}

/*
================
advancePetGearPoison

Death, unsummon, town entry and invalid targets discard pending damage.
Late ticks issue one pulse and schedule from now, never a damage backlog.
================
*/
func (rt *Runtime) advancePetGearPoison(key petOwnerKey, now int64) {
	state := rt.petSessionFor(key.division, key.name, key.gid)
	if state == nil || len(state.trained.poison) == 0 {
		return
	}
	pet := state.character.CompanionByGID(key.gid)
	if pet == nil {
		state.trained.poison = nil
		return
	}
	step, ok := rt.elementalCompanion(key.division, state.character, key.gid, companion.PetKind(pet.Codename), now)
	if !ok {
		state.trained.poison = nil
		return
	}
	for gid, poison := range state.trained.poison {
		victim, valid := rt.resolvePetCombatTarget(step, gid)
		if !valid || victim.monster == nil || poison.left == 0 {
			delete(state.trained.poison, gid)
			continue
		}
		if now < poison.next {
			continue
		}
		// Divide the remaining total so rounding never loses damage.
		damage := poison.damage / uint32(poison.left)
		rt.wolfBonusHit(step, *victim.monster, damage, "pet-gear-element")
		poison.damage -= damage
		poison.left--
		poison.next = now + 1000
		if poison.left == 0 {
			delete(state.trained.poison, gid)
		} else {
			state.trained.poison[gid] = poison
		}
	}
}
