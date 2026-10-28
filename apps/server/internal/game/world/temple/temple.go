/*
===========================================================================

temple.go - the temple band worlds, port-only identity table.

Port-only, not native; the worlds exist only while SRO_CAP110_TEMPLE is
on (#1800 step 3). The source database tags the temple's regions with
its own game world ids (63-84, one per level band); the shipped world
catalogue has none of them, so the port registers exactly these 22 as
instanced worlds while the switch is on. Unlike the Pharaoh Tombs'
permanent worlds, each band is per-party layered (the owner settlement
on #1800), which the instance definitions in world/instance carry.

This leaf owns only the identity table. The monster catalogue asks it
which world a temple row joins, and the world catalogue asks it what
to register, so the two can never disagree. It imports nothing.

===========================================================================
*/
package temple

import "fmt"

// The temple worlds' shipped ids: the first and last of the 22 bands.
const (
	FirstWorldID = 63
	LastWorldID  = 84
)

// WorldCount is the number of temple band worlds (63-84).
const WorldCount = LastWorldID - FirstWorldID + 1

/*
================
World

One temple world: its shipped id and the codename its population joins.
================
*/
type World struct {
	ID       int
	CodeName string
}

var worlds []World

func init() {
	worlds = make([]World, 0, WorldCount)
	for id := FirstWorldID; id <= LastWorldID; id++ {
		worlds = append(worlds, World{ID: id, CodeName: fmt.Sprintf("INS_TEMPLE_%d", id)})
	}
}

/*
================
Worlds

Every temple world, in id order.
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

The codename a temple row joins, from its source world id. A source
world that is not a temple band reports false.
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
