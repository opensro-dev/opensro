/*
===========================================================================
petgearproc.go - credited PvE gear spells on damage dealt or received

Proc hits bypass the attack/counter pipeline, so they cannot trigger themselves.
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
petGearField
================
*/
type petGearField struct {
	center    simulation.Spawn
	damage    uint32
	next, end int64
	left      uint8
}

/*
================
petGearProcs

One proc per committed damaging action, sharing an internal recovery across
striking/struck effects. Blocked hits and shield-only impacts never enter here.
================
*/
func (rt *Runtime) petGearProcs(division string, owner *enterworld.Character, gid, target uint32, at simulation.Spawn, taken bool, now int64) {
	pet := owner.CompanionByGID(gid)
	if pet == nil {
		return
	}
	step, ok := rt.elementalCompanion(division, owner, gid, companion.PetKind(pet.Codename), now)
	if !ok || now < step.state.trained.procReadyAt {
		return
	}
	trigger := 0
	if taken {
		trigger = 1
	}
	mods := companion.ModifiersFor(step.pet)
	for kind, pair := range mods.Procs {
		p := pair[trigger]
		if p.Damage == 0 || !rt.petChance(p.Chance) {
			continue
		}
		if kind == companion.ProcMend {
			var heal wire.Frame
			healed := false
			if !rt.deps.UpdateDeferred(owner, "pet-gear-mend", func() bool {
				heal, healed = rt.petOwnerHeal(division, owner, 5)
				return healed
			}) {
				continue
			}
			rt.queueMonsterLegFrames(rt.sealTickPrivate([]simulation.DivisionFrames{{DivisionID: division, SourceGID: enterworld.ObjectIDForCharacter(owner), Frames: simFrames([]wire.Frame{heal})}}))
		} else if kind == companion.ProcBlizzard || kind == companion.ProcPoison {
			if len(step.state.trained.gearFields) >= 4 {
				continue
			}
			field := petGearField{center: at, damage: p.Damage, next: now + 1000, end: now + 4000, left: 3}
			rt.petGearArea(step, at, p.Damage/4, 50)
			step.state.trained.gearFields = append(step.state.trained.gearFields, field)
			if kind == companion.ProcBlizzard {
				rt.wolfEmote(division, target, 0xd3)
			}
		} else if kind == companion.ProcChain {
			rt.petGearChain(step, target, at, p.Damage)
		} else {
			rt.petGearArea(step, at, p.Damage, 30)
		}
		step.state.trained.procReadyAt = now + companion.D.Rules.ProcInternalCooldownMs
		return
	}
}

/*
================
petGearArea

Only the source's ordinary-world population, with explicit range, sight and
normal target eligibility. Eight victims bounds each pulse's work.
================
*/
func (rt *Runtime) petGearArea(step petCombatStep, at simulation.Spawn, damage uint32, radius float64) {
	lease, ok := rt.casterPopulation(step.key.division, step.snapshot)
	if !ok {
		return
	}
	count := 0
	for _, mob := range rt.Monsters.CombatCandidatesInPopulation(step.key.division, lease, at, radius, step.nowMs, false) {
		target, valid := rt.resolvePetCombatTarget(step, mob.Gid)
		if !valid || target.monster == nil || !samePlaneAdjacent(at, target.at) || areaDistance(at, target.at) > radius {
			continue
		}
		world, _ := step.state.follower.Presentation()
		if rt.LineOfSight != nil && !rt.LineOfSight(at, world.LiveOwnerAt(step.nowMs), target.at) {
			continue
		}
		rt.wolfBonusHit(step, *target.monster, damage, "pet-gear-proc")
		count++
		if count == 8 {
			break
		}
	}
}

/*
================
petGearChain

Five distinct victims, losing 20 percent damage on each eight-meter hop.
================
*/
func (rt *Runtime) petGearChain(step petCombatStep, first uint32, at simulation.Spawn, damage uint32) {
	lease, ok := rt.casterPopulation(step.key.division, step.snapshot)
	if !ok {
		return
	}
	seen := map[uint32]bool{}
	for hop := range 5 {
		var candidates []uint32
		if hop == 0 {
			candidates = append(candidates, first)
		}
		for _, mob := range rt.Monsters.CombatCandidatesInPopulation(step.key.division, lease, at, 80, step.nowMs, false) {
			candidates = append(candidates, mob.Gid)
		}
		found := false
		for _, gid := range candidates {
			victim, valid := rt.resolvePetCombatTarget(step, gid)
			if !valid || victim.monster == nil || seen[gid] || !samePlaneAdjacent(at, victim.at) || areaDistance(at, victim.at) > 80 {
				continue
			}
			world, _ := step.state.follower.Presentation()
			if rt.LineOfSight != nil && !rt.LineOfSight(at, world.LiveOwnerAt(step.nowMs), victim.at) {
				continue
			}
			seen[gid] = true
			rt.wolfBonusHit(step, *victim.monster, damage, "pet-gear-proc")
			at = victim.at
			damage = damage * 80 / 100
			found = true
			break
		}
		if !found {
			break
		}
	}
}

/*
================
advancePetGearFields

One pulse per second; late ticks never replay a backlog. Death, separation,
unsummon, town entry and logout discard pending fields.
================
*/
func (rt *Runtime) advancePetGearFields(key petOwnerKey, now int64) {
	state := rt.petSessionFor(key.division, key.name, key.gid)
	if state == nil || len(state.trained.gearFields) == 0 {
		return
	}
	pet := state.character.CompanionByGID(key.gid)
	if pet == nil {
		state.trained.gearFields = nil
		return
	}
	step, ok := rt.elementalCompanion(key.division, state.character, key.gid, companion.PetKind(pet.Codename), now)
	if !ok {
		state.trained.gearFields = nil
		return
	}
	keep := state.trained.gearFields[:0]
	for _, field := range state.trained.gearFields {
		if now >= field.end || field.left == 0 {
			continue
		}
		if now >= field.next {
			damage := field.damage / 4
			if field.left == 1 {
				damage += field.damage % 4
			}
			rt.petGearArea(step, field.center, damage, 50)
			field.left--
			field.next = now + 1000
		}
		if field.left > 0 {
			keep = append(keep, field)
		}
	}
	state.trained.gearFields = keep
}
