/*
===========================================================================
petwolf.go - port-only wolf/owner Execute and independent seventh-attack sweeps.

Only normal committed attack actions enter here. Executions use the existing
credited monster HP/reward door and publish death, not another cast or animation.
===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
	"slices"
	"strings"
)

/*
================
wolfSource

The division lock owns transient counters. The character read door supplies
one canonical snapshot; another player's wolf never grants owner bonuses.
================
*/
func (rt *Runtime) wolfSource(division string, owner *enterworld.Character, gid uint32, now int64) (petCombatStep, companion.SkillDef, uint8, bool) {
	step, ok := rt.wolfCompanion(division, owner, gid, now)
	if !ok {
		return step, companion.SkillDef{}, 0, false
	}
	skill, level, ok := companion.Passive(step.pet, "Execute", companion.ModifiersFor(step.pet))
	return step, skill, level, ok
}

/*
================
wolfCompanion

Common eligibility for every wolf bonus; no learned Execute prerequisite.
The wolf lanes read nothing of the owner past its worn equipment, so
their copy is the narrow one (#1943).
================
*/
func (rt *Runtime) wolfCompanion(division string, owner *enterworld.Character, gid uint32, now int64) (petCombatStep, bool) {
	return rt.elementalCompanionEligibilityWith(division, owner, gid, "Wolf", now, false, rt.characterSnapshotEquipmentAdmitted)
}

/*
================
elementalCompanion

Shared port-only PvE eligibility for the wolf and frost caster.
================
*/
func (rt *Runtime) elementalCompanion(division string, owner *enterworld.Character, gid uint32, kind string, now int64) (petCombatStep, bool) {
	return rt.elementalCompanionEligibility(division, owner, gid, kind, now, false)
}

/*
================
hasElementalCandidate

The record half of elementalCompanionEligibility's pet filter: plain
field reads, safe inside the read door. Every pet it rejects the loop
below rejects too, so admission never changes the outcome.
================
*/
func hasElementalCandidate(c *enterworld.Character, gid uint32, kind string) bool {
	for _, pet := range c.Companions() {
		if pet.BroodStage == 0 && (gid == 0 || pet.GID == gid) && pet.Summoned && pet.CurrentHP != 0 &&
			companion.PetKind(pet.Codename) == kind {
			return true
		}
	}
	return false
}

/*
================
elementalCompanionEligibility

Only travel support may opt into safe zones; combat callers keep the town gate.
================
*/
func (rt *Runtime) elementalCompanionEligibility(division string, owner *enterworld.Character, gid uint32, kind string, now int64, allowTown bool) (petCombatStep, bool) {
	return rt.elementalCompanionEligibilityWith(division, owner, gid, kind, now, allowTown, rt.characterSnapshotAdmitted)
}

/*
================
elementalCompanionEligibilityWith

The one eligibility walk. snapshotFor supplies the owner copy: the
admitted full copy for the combat and travel lanes, the admitted
equipment copy for the wolf lanes, whose readers stay inside the worn
equipment (#1943).
================
*/
func (rt *Runtime) elementalCompanionEligibilityWith(division string, owner *enterworld.Character, gid uint32, kind string, now int64, allowTown bool, snapshotFor func(string, *enterworld.Character, func(*enterworld.Character) bool) *enterworld.Character) (petCombatStep, bool) {
	var step petCombatStep
	if !rt.AttackPetExpansion || rt.Monsters == nil {
		return step, false
	}
	// Runs per pet per tick: copy the owner only when a pet could pass the
	// record filters below (#1943; the admitted snapshotFor door).
	snapshot := snapshotFor(division, owner, func(c *enterworld.Character) bool {
		return !c.DeletePending && enterworld.CharacterAlive(c) && hasElementalCandidate(c, gid, kind)
	})
	if snapshot == nil || snapshot.DeletePending || !enterworld.CharacterAlive(snapshot) || !allowTown && rt.inTown(division, snapshot) {
		return step, false
	}
	for _, pet := range snapshot.Companions() {
		if pet.BroodStage != 0 {
			continue
		}
		if gid != 0 && pet.GID != gid || companion.PetKind(pet.Codename) != kind || !pet.Summoned || pet.CurrentHP == 0 || !rt.petExpansionAllowed(snapshot, pet) {
			continue
		}
		state := rt.petSessionFor(division, snapshot.Name, pet.GID)
		if state == nil || !state.ready || state.follower == nil {
			continue
		}
		from := state.follower.Position(now)
		at := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
		if !samePlaneAdjacent(from, at) || areaDistance(from, at) > 20*petAreaPerMeter || !allowTown && townRegion(from.RegionID) {
			continue
		}
		ref, found := rt.cosReference(pet)
		if !found {
			continue
		}
		step = petCombatStep{key: petOwnerKey{division: division, name: strings.ToLower(snapshot.Name), gid: pet.GID}, state: state, snapshot: snapshot, pet: pet, ref: ref, nowMs: now}
		return step, true
	}
	return step, false
}

/*
================
resetWolfExecution

Book changes, separation, death, unsummon and eligibility loss clear progress.
================
*/
func (rt *Runtime) resetWolfExecution(key petOwnerKey, now int64) {
	state := rt.petSessionFor(key.division, key.name, key.gid)
	if state == nil {
		return
	}
	step, skill, _, ok := rt.wolfSource(key.division, state.character, key.gid, now)
	if !ok || skill.Option != 3 || state.wolfBook != skill.ID {
		state.wolfHits = [2]uint8{}
		state.wolfBook = 0
		if ok && skill.Option == 3 {
			step.state.wolfBook = skill.ID
		}
	}
}

/*
================
petStrikeDamaged
================
*/
func petStrikeDamaged(hit monsterStrike) bool {
	for _, impact := range hit.impacts {
		if !impact.Blocked && impact.Damage > 0 {
			return true
		}
	}
	return false
}

/*
================
wolfExecuteAttack

actor0 is the wolf,1 the owner. Nonempty targets are committed damaging monster
hits, collected once per action after all primary/area victims have settled.
Player victims (an owner's PK hit) are dropped: the kit is PvE only.
================
*/
func (rt *Runtime) wolfExecuteAttack(division string, owner *enterworld.Character, gid uint32, actor int, targets []uint32, now int64) {
	if rt.Monsters == nil {
		return
	}
	targets = slices.DeleteFunc(slices.Clone(targets), func(target uint32) bool {
		_, monster := rt.Monsters.Get(division, target)
		return !monster
	})
	if len(targets) == 0 {
		return
	}
	if step, ok := rt.wolfCompanion(division, owner, gid, now); ok {
		rt.wolfKitAttack(step, actor, targets)
	}
	step, skill, level, ok := rt.wolfSource(division, owner, gid, now)
	if !ok || actor == 1 && skill.Option < 2 {
		return
	}
	if actor == 1 {
		for _, target := range targets {
			rt.wolfExecuteVictim(step, skill, level, target)
		}
	}
	if skill.Option != 3 {
		return
	}
	if step.state.wolfBook != skill.ID {
		step.state.wolfBook, step.state.wolfHits = skill.ID, [2]uint8{}
	}
	step.state.wolfHits[actor] = (step.state.wolfHits[actor] + 1) % 7
	if step.state.wolfHits[actor] != 0 {
		return
	}
	from := step.state.follower.Position(now)
	if actor == 1 {
		from = rt.liveSpawn(simulation.WorldKey(division, step.snapshot.Name), step.snapshot, now)
	}
	lease, found := rt.casterPopulation(division, step.snapshot)
	if !found {
		return
	}
	if actor == 1 {
		rt.wolfEmote(division, enterworld.ObjectIDForCharacter(step.snapshot), wolfEmoteSweep)
	} else {
		rt.wolfEmote(division, step.pet.GID, wolfEmoteSweep)
	}
	_, nav := rt.liveNav(simulation.WorldKey(division, step.snapshot.Name), step.snapshot, now)
	if actor == 0 {
		world, _ := step.state.follower.Presentation()
		nav = world.LiveOwnerAt(now)
	}
	for _, mob := range rt.Monsters.CombatCandidatesInPopulation(division, lease, from, 10*petAreaPerMeter, now, false) {
		target, found := rt.resolvePetCombatTarget(step, mob.Gid)
		if !found || target.monster == nil || !samePlaneAdjacent(from, target.at) || areaDistance(from, target.at) > 10*petAreaPerMeter {
			continue
		}
		if rt.LineOfSight != nil && !rt.LineOfSight(from, nav, target.at) {
			continue
		}
		rt.wolfExecuteVictim(step, skill, level, mob.Gid)
	}
}

/*
================
wolfExecuteVictim

Fresh HP and expected-HP commit prevent double kills/rewards. No call back into
wolfExecuteAttack: sweep victims never generate attacks or recursive sweeps.
================
*/
func (rt *Runtime) wolfExecuteVictim(step petCombatStep, skill companion.SkillDef, level uint8, gid uint32) {
	target, found := rt.resolvePetCombatTarget(step, gid)
	if !found || target.monster == nil {
		return
	}
	mob := *target.monster
	unique := mob.Rarity()&0x0f == 3 || mob.Rarity()&0x0f == 8
	threshold := companion.ExecutionThreshold(skill, level, petTargetBleeds(mob))
	if unique || mob.CurrentHP == 0 || float64(mob.CurrentHP)*100 >= float64(mob.Ref.MaxHP)*threshold {
		return
	}
	rt.wolfBonusHit(step, mob, mob.CurrentHP, "wolf-execute")
}
