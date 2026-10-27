/*
===========================================================================

monster_world.go - which monster a character's world holds

The lookup every strike lane asks before it may touch a monster: the
instance registered for the character's world (its population lease when
one was admitted, else the world instance the record stands in).

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
characterMonster
================
*/
func (rt *Runtime) characterMonster(division string, character *enterworld.Character, gid uint32) (monster.Instance, bool) {
	if character == nil {
		return monster.Instance{}, false
	}
	return rt.characterMonsterFor(division, character.Name, domain.CharacterWorldInstance(character), gid)
}

/*
================
characterMonsterFor

characterMonster on values a caller already read under the authority
read door (#1820 part C): the registry is consulted on the name and
world instance, so no detached record is built for the lookup.
================
*/
func (rt *Runtime) characterMonsterFor(division string, name string, worldInstance uint32, gid uint32) (monster.Instance, bool) {
	if rt.Monsters == nil {
		return monster.Instance{}, false
	}
	if value, exists := rt.characterAdmissions.Load(simulation.WorldKey(division, name)); exists {
		owner := value.(populationAdmission)
		if uint32(owner.lease.ID) != worldInstance {
			return monster.Instance{}, false
		}
		return rt.Monsters.GetInPopulation(division, owner.lease, gid)
	}
	return rt.Monsters.GetInWorld(division, instance.ID(worldInstance), gid)
}
