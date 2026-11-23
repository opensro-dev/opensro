/*
===========================================================================

arabia.go - the Arabia dungeon world, port-only identity table.

Port-only, not native; the world exists only while
SRO_BAGHDAD_DUNGEON is on (#4134; the continent rides SRO_ARABIA,
#3268). The source database tags the Baghdad dungeon's regions with
its own game world id (112); the shipped world catalogue has no 112,
so the port registers exactly this one as a permanent world. The
boss lair (113) carries 27 sourced nests in the later backup but no
gate in either source, so it stays unregistered until its admission
slice sources its entrance; Petra (114) and the garden (115) carry
no nests and no gates in either later source.

This leaf owns only the identity table. The monster catalogue asks it
which world a dungeon row joins, and the world catalogue asks it what
to register, so the two can never disagree. It imports nothing.

===========================================================================
*/
package arabia

// The dungeon world's shipped id.
const DungeonWorldID = 112

// The boss lair's shipped id (the Kalia Hideout). Its nests are sourced
// (#4134); it joins the table below only when its slice registers it.
const HideoutWorldID = 113

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
IsDungeonWorld

Whether a source world is one of the Baghdad dungeon's own: the
dungeon itself or its boss lair. Their population and gates ride
their own switch (SRO_BAGHDAD_DUNGEON, #4134), apart from the
continent's, so every consumer names them through here.
================
*/
func IsDungeonWorld(worldID int) bool {
	return worldID == DungeonWorldID || worldID == HideoutWorldID
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
