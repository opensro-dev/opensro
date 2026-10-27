/*
===========================================================================

pharaohtomb.go - the three Pharaoh Tomb worlds, port-only identity table.

Port-only, not native; the worlds exist only while SRO_PHARAOH_TOMB is on
(#1750, owner 2026-10-27). The source database tags each tomb's regions
with its own game world id (57, 58, 59); the shipped world catalogue has
no 57-59, so the port registers exactly these three as permanent worlds.

This leaf owns only the identity table. The monster catalogue asks it
which world a tomb row joins, and the world catalogue asks it what to
register, so the two can never disagree. It imports nothing.

===========================================================================
*/
package pharaohtomb

// The tomb worlds' shipped ids.
const (
	World1ID = 57
	World2ID = 58
	World3ID = 59
)

/*
================
World

One tomb world: its shipped id and the codename its population joins.
================
*/
type World struct {
	ID       int
	CodeName string
}

var worlds = []World{
	{World1ID, "INS_PHARAOH_TOMB_1"},
	{World2ID, "INS_PHARAOH_TOMB_2"},
	{World3ID, "INS_PHARAOH_TOMB_3"},
}

/*
================
Worlds

Every tomb world, in id order.
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

The codename a tomb row joins, from its source world id. A source world
that is not a tomb reports false.
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
