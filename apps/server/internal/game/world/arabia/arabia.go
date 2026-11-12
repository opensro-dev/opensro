/*
===========================================================================

arabia.go - the Arabia dungeon world, port-only identity table.

Port-only, not native; the world exists only while SRO_ARABIA is on
(#3268). The source database tags the Baghdad dungeon's regions with
its own game world id (112); the shipped world catalogue has no 112,
so the port registers exactly this one as a permanent world. The
boss-lair (113), Petra (114) and garden (115) worlds of the source
carry no nests and no gates in either later source, so they stay
unregistered until a later admission slice sources their content.

This leaf owns only the identity table. The monster catalogue asks it
which world a dungeon row joins, and the world catalogue asks it what
to register, so the two can never disagree. It imports nothing.

===========================================================================
*/
package arabia

// The dungeon world's shipped id.
const DungeonWorldID = 112

/*
================
World

One Arabia world: its shipped id and the codename its population joins.
================
*/
type World struct {
	ID       int
	CodeName string
}

var worlds = []World{
	{DungeonWorldID, "GROUP_ARABIA_DUNGEON"},
}

/*
================
Worlds

Every Arabia world, in id order.
================
*/
func Worlds() []World {
	out := make([]World, len(worlds))
	copy(out, worlds)
	return out
}

/*
================
CodeName

The codename a dungeon row joins, from its source world id. A source
world that is not the dungeon reports false.
================
*/
func CodeName(worldID int) (string, bool) {
	for _, world := range worlds {
		if world.ID == worldID {
			return world.CodeName, true
		}
	}
	return "", false
}
