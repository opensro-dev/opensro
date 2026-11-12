/*
===========================================================================

arabia.go - the Arabia dungeon world, port-only identity table.

Port-only, not native; the world exists only while SRO_ARABIA is on
(#3268). The later client's continent stands on the persistent world
(Baghdad town and the three fields are ordinary sectors), but the
Baghdad dungeon is a world of its own in the source (GROUP_ARABIA_
DUNGEON, id 112, native type 0, twenty regions); the shipped v1.150
catalogue has no such world, so the port registers exactly this one
while the switch is on. The boss-lair, Petra and Garden worlds
(113-115) author no nests in the source and stay unregistered; their
regions' terrain merges with the continent regardless.

This leaf owns only the identity table. The monster catalogue asks it
which world an Arabia dungeon row joins, and the world catalogue asks
it what to register, so the two can never disagree. It imports
nothing.

===========================================================================
*/
package arabia

// The Baghdad dungeon world's shipped id.
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

var dungeon = World{DungeonWorldID, "GROUP_ARABIA_DUNGEON"}

/*
================
DungeonWorld

The Baghdad dungeon world: what the switch registers.
================
*/
func DungeonWorld() World {
	return dungeon
}

/*
================
Lookup

The Arabia world with this id, if the leaf carries it.
================
*/
func Lookup(id int) (World, bool) {
	if id == dungeon.ID {
		return dungeon, true
	}
	return World{}, false
}

/*
================
CodeName

The codename of the Arabia world with this id, if the leaf carries it.
================
*/
func CodeName(id int) (string, bool) {
	world, ok := Lookup(id)
	if !ok {
		return "", false
	}
	return world.CodeName, true
}
