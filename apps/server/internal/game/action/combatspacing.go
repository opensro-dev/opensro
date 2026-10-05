/*
===========================================================================

combat_spacing.go - authoritative combat geometry

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
playerToMonsterCombatSpacing

Build one immutable snapshot from the player and monster authority records.
Browser mesh bounds and movement goals do not enter this calculation.
================
*/
func (rt *Runtime) playerToMonsterCombatSpacing(
	character *enterworld.Character,
	target monster.Instance,
	reach simulation.ActionReach,
) (simulation.CombatSpacing, bool) {
	var spacing simulation.CombatSpacing

	playerRadius, ok := rt.deps.CharacterBodyRadius(character)
	if !ok {
		return spacing, false
	}

	spacing.ActorBodyRadius = simulation.BodyRadius(playerRadius)
	spacing.TargetBodyRadius = simulation.BodyRadius(target.BodyRadius())
	spacing.ActionReach = reach

	return spacing, spacing.Valid()
}

/*
================
monsterToPlayerCombatSpacing

Build the reverse-directed snapshot for the monster damage commit gate.
Keep actor and target explicit so asymmetric modifiers stay visible here.
================
*/
func (rt *Runtime) monsterToPlayerCombatSpacing(
	attacker monster.Instance,
	character *enterworld.Character,
	reach simulation.ActionReach,
) (simulation.CombatSpacing, bool) {
	var spacing simulation.CombatSpacing

	playerRadius, ok := rt.deps.CharacterBodyRadius(character)
	if !ok {
		return spacing, false
	}

	spacing.ActorBodyRadius = simulation.BodyRadius(attacker.BodyRadius())
	spacing.TargetBodyRadius = simulation.BodyRadius(playerRadius)
	spacing.ActionReach = reach

	return spacing, spacing.Valid()
}

/*
================
playerToPlayerCombatSpacing

Build one geometry snapshot from two characters.
Native combat spacing is both body radii plus the action reach.
================
*/
func (rt *Runtime) playerToPlayerCombatSpacing(
	character, target *enterworld.Character,
	reach simulation.ActionReach,
) (simulation.CombatSpacing, bool) {
	var spacing simulation.CombatSpacing

	playerRadius, ok := rt.deps.CharacterBodyRadius(character)
	if !ok {
		return spacing, false
	}

	targetRadius, ok := rt.deps.CharacterBodyRadius(target)
	if !ok {
		return spacing, false
	}

	spacing.ActorBodyRadius = simulation.BodyRadius(playerRadius)
	spacing.TargetBodyRadius = simulation.BodyRadius(targetRadius)
	spacing.ActionReach = reach

	return spacing, spacing.Valid()
}
