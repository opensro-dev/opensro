/*
===========================================================================

questselected.go - the monster a character has selected, for quest tools

Port-only reading of an existing plane: some quest items act on the monster
the player has selected (the Blue Berserk chain's Mountain Essence is used on
a Yeoha). The selection store and the live monster population already hold
what is needed; this answers it in one call so the quest owner stays free of
both.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
SelectedMonster

The codename and live position of the monster the character has selected, or
false when nothing is selected, the selection is not a monster of the
character's own population, or it is dead.
================
*/
func (rt *Runtime) SelectedMonster(division string, c *enterworld.Character) (string, simulation.Spawn, bool) {
	if rt == nil || c == nil || rt.Selected == nil || rt.Monsters == nil {
		return "", simulation.Spawn{}, false
	}
	gid, selected := rt.Selected.Get(division, c.Name)
	if !selected {
		return "", simulation.Spawn{}, false
	}
	lease, present := rt.EntryPopulationLease(division, c.Name)
	if !present {
		return "", simulation.Spawn{}, false
	}
	target, exists := rt.Monsters.GetInPopulation(division, lease, gid)
	if !exists || target.CurrentHP == 0 {
		return "", simulation.Spawn{}, false
	}
	return target.Ref.Codename, rt.monsterSpawn(division, gid, rt.Now().UnixMilli()), true
}
