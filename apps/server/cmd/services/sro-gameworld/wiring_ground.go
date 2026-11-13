/*
===========================================================================
wiring_ground.go - append runtime ground and skill objects to world entry.

Keeps the existing NPC bootstrap producer and visibility decisions intact.
===========================================================================
*/
package main

import (
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
appendGroundObjectRows

Adds live ground objects after authored NPC rows while retaining the existing
bootstrap producer and its visibility decisions.
================
*/
func appendGroundObjectRows(deps *enterworld.Deps, items *action.Runtime) {
	npcRows := deps.ObjectListRows
	deps.ObjectListRows = func(
		divisionID string,
		character *enterworld.Character,
		entry *enterworld.LocalPlayerEntry,
	) []enterworld.Packet {
		var rows []enterworld.Packet
		if npcRows != nil {
			rows = npcRows(divisionID, character, entry)
		}
		rows = append(rows, items.GroundObjectRows(divisionID, character, entry)...)
		return append(rows, items.SkillObjectRows(divisionID, character, entry)...)
	}
}
