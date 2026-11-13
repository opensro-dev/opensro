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
	kind      int
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
	step, ok := rt.elementalCompanionMatching(division, owner, elementalQuery{gid: gid, kind: companion.PetKind(pet.Codename), gear: true}, now)
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
		damage := p.Damage
		if p.MaxDamage > damage {
			draw, err := rt.CombatRoll()
			if err != nil {
				continue
			}
			damage += draw % (p.MaxDamage - damage + 1)
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
			rt.wolfEmote(division, enterworld.ObjectIDForCharacter(owner), 0xda)
		} else if kind == companion.ProcBlizzard || kind == companion.ProcPoison {
			if len(step.state.trained.gearFields) >= 4 {
				continue
			}
			field := petGearField{center: at, damage: damage, kind: kind, next: now + 1000, end: now + 4000, left: 3}
			rt.petGearArea(step, at, damage/4, 50, kind, false)
			step.state.trained.gearFields = append(step.state.trained.gearFields, field)
			if kind == companion.ProcBlizzard {
				rt.wolfEmote(division, target, 0xd3)
			} else {
				rt.wolfEmote(division, target, 0xd9)
			}
		} else if kind == companion.ProcChain {
			rt.petGearChain(step, target, at, damage)
		} else {
			rt.petGearArea(step, at, damage, 30, kind, false)
			rt.wolfEmote(division, target, 0xd8)
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
func (rt *Runtime) petGearArea(step petCombatStep, at simulation.Spawn, damage uint32, radius float64, kind int, final bool) {
	lease, ok := rt.casterPopulation(step.key.division, step.snapshot)
	if !ok {
		return
	}
	count := 0
	status := enterworld.SkillRow{}
	switch kind {
	case companion.ProcFire:
		// Port-only design inputs, not a native audit: a level-eight burn of up to
		// six seconds through the native abnormal engine; damage follows the pet level.
		setPetStatus(&status, tagBurn, [6]uint32{8, 25, uint32(step.pet.Level)})
	case companion.ProcBlizzard:
		// Port-only design inputs: frostbite of up to three seconds; freeze is tried only on the final pulse.
		setPetStatus(&status, tagFrostbite, [6]uint32{12, 100})
		if final {
			setPetStatus(&status, tagFreeze, [6]uint32{20, 10})
		}
	}
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
		if kind == companion.ProcFire || kind == companion.ProcBlizzard {
			if after, ok := rt.Monsters.Get(step.key.division, target.gid); ok && after.CurrentHP > 0 && after.CurrentHP < target.monster.CurrentHP {
				frames := rt.applyPetStatus(step, status, target.gid)
				if len(frames) > 0 {
					rt.queueMonsterLegFrames(rt.sealTickPrivate([]simulation.DivisionFrames{{DivisionID: step.key.division, SourceGID: target.gid, Frames: simFrames(frames)}}))
				}
			}
		}
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
			rt.wolfEmote(step.key.division, gid, 0xd7)
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
	rt.advancePetGearFieldsSession(key, rt.petSessionFor(key.division, key.name, key.gid), now)
}

/*
================
advancePetGearFieldsSession
================
*/
func (rt *Runtime) advancePetGearFieldsSession(key petOwnerKey, state *petSession, now int64) {
	if state == nil || len(state.trained.gearFields) == 0 {
		return
	}
	pet := state.character.CompanionByGID(key.gid)
	if pet == nil {
		state.trained.gearFields = nil
		return
	}
	step, ok := rt.elementalCompanionMatching(key.division, state.character, elementalQuery{gid: key.gid, kind: companion.PetKind(pet.Codename), gear: true}, now)
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
			rt.petGearArea(step, field.center, damage, 50, field.kind, field.left == 1)
			field.left--
			field.next = now + 1000
		}
		if field.left > 0 {
			keep = append(keep, field)
		}
	}
	state.trained.gearFields = keep
}
