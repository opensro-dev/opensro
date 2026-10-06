/*
===========================================================================

mercenary_target.go - guild soldiers acquire enemies through their owner

Selector 6 (546CA0) chooses the nearest eligible object inside the authored
sight range. Players must also pass their owner's world-context predicate.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	mercenarySightRange               = 130
	mercenaryBodyProtectedFirst uint8 = 2
	mercenaryBodyProtectedLast  uint8 = 4
	mercenaryBodyHiddenFirst    uint8 = 6
	mercenaryBodyHiddenLast     uint8 = 7
)

/*
================
mercenaryNormalEnemy

52B6D0 precedes the job/guild checks with both level floors. Cape colors
are absent here; deliberate player attack permission is a different query.
================
*/
func (rt *Runtime) mercenaryNormalEnemy(division string, owner, target *enterworld.Character) bool {
	if owner == nil || target == nil || owner.ID == target.ID || owner.Level == nil || target.Level == nil ||
		*owner.Level < playerCombatMinimumLevel || *target.Level < playerCombatMinimumLevel {
		return false
	}
	if len(target.Aggressions) != 0 || target.PVPState() == 2 {
		return true
	}
	return hostileJobs(enterworld.DressedJob(owner), enterworld.DressedJob(target)) || rt.guildsAtWar(division, owner, target)
}

/*
================
acquireMercenaryTarget

The native nearest accumulator is an integer: each accepted float distance
is truncated before the next comparison, rather than sorting float values.
================
*/
func (rt *Runtime) acquireMercenaryTarget(step petCombatStep) {
	if step.ref.TidWord>>11 != domain.MercenaryBand || step.state.combat != nil {
		return
	}
	at := step.state.follower.Position(step.nowMs)
	best, gid := uint32(mercenarySightRange), uint32(0)
	admit := func(target uint32, pose simulation.Spawn) {
		distance := areaDistance(at, pose)
		if distance >= float64(best) {
			return
		}
		best, gid = uint32(distance), target
	}
	if lease, ok := rt.casterPopulation(step.key.division, step.snapshot); ok && rt.Monsters != nil {
		for _, m := range rt.Monsters.CombatCandidatesInPopulation(step.key.division, lease, at, mercenarySightRange, step.nowMs, true) {
			admit(m.Gid, rt.monsterSpawn(step.key.division, m.Gid, step.nowMs))
		}
	}
	world := domain.CharacterWorldInstance(step.snapshot)
	for _, present := range rt.PopulationPlayers(step.key.division, step.nowMs) {
		if present.World != world || !samePlaneAdjacent(at, present.Spawn) {
			continue
		}
		target := rt.findCharacterByGid(step.key.division, present.GID)
		snapshot := rt.characterSnapshot(step.key.division, target)
		if snapshot == nil || snapshot.DeletePending ||
			!rt.mercenaryEnemy(step.key.division, step.snapshot, snapshot) {
			continue
		}
		if enterworld.CharacterAlive(snapshot) && mercenaryBodyVisible(snapshot.NativeBodyStatus) {
			admit(present.GID, present.Spawn)
		}
		for _, pet := range rt.companionTargets(step.key.division, present.GID, step.nowMs) {
			if pet.Band == 4 || !mercenaryBodyVisible(pet.NativeBodyStatus) {
				continue
			}
			admit(pet.Gid, pet.Pose)
		}
	}
	if gid != 0 {
		step.state.combat = &petCombatIntent{target: gid, nextAttackMs: step.nowMs}
	}
}

/*
================
mercenaryBodyVisible

5299E0: guild soldiers have no monster-only hidden-target tactics override.
================
*/
func mercenaryBodyVisible(body uint8) bool {
	return !(body >= mercenaryBodyProtectedFirst && body <= mercenaryBodyProtectedLast ||
		body >= mercenaryBodyHiddenFirst && body <= mercenaryBodyHiddenLast)
}

/*
================
mercenaryEnemy

52DA50 uses guild/union identity in fortress worlds, without the normal-world
level floor. The same guild and union authorities own these identities.
================
*/
func (rt *Runtime) mercenaryEnemy(division string, owner, target *enterworld.Character) bool {
	if owner == nil || target == nil || owner.ID == target.ID {
		return false
	}
	world, found := instance.Lookup(instance.ID(domain.CharacterWorldInstance(owner)).Definition())
	if !found || !world.Siege() {
		return rt.mercenaryNormalEnemy(division, owner, target)
	}
	if len(target.Aggressions) != 0 || target.PVPState() == 2 {
		return true
	}
	var a, b int64
	if owner.GuildID != nil {
		a = *owner.GuildID
	}
	if target.GuildID != nil {
		b = *target.GuildID
	}
	if a == b || rt.Unions.Allied(division, a, b) {
		return false
	}
	return rt.Fortresses != nil && rt.Fortresses.WarActive(division)
}
