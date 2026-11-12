/*
===========================================================================

jupiter.go - the Jupiter worlds, port-only identity table.

Port-only, not native; the worlds exist only while SRO_JUPITER is on
(#3267). The later client's world table carries the Jupiter field and
the two Mirror Dimension temple sets as ordinary outdoor worlds (ids
86-90 and 93-94, all native type 0); the shipped v1.150 catalogue has
none of them, so the port registers exactly these while the switches
are on. The temple sets' worlds register only while SRO_JUPITER_TEMPLE
is on beside the field.

This leaf owns only the identity table. The monster catalogue asks it
which world a Jupiter row joins, and the world catalogue asks it what
to register, so the two can never disagree. It imports nothing.

===========================================================================
*/
package jupiter

// The Jupiter worlds' shipped ids: the field and the first temple world.
const (
	FieldWorldID       = 90
	FirstTempleWorldID = 86
)

/*
================
World

One Jupiter world: its shipped id and the codename its population joins.
================
*/
type World struct {
	ID       int
	CodeName string
}

var worlds = []World{
	{86, "GROUP_JUPITER_A"},
	{87, "GROUP_JUPITER_B"},
	{88, "GROUP_JUPITER_A2"},
	{89, "GROUP_JUPITER_B2"},
	{FieldWorldID, "GROUP_JUPITER_FIELD"},
	{93, "GROUP_JUPITER_A3"},
	{94, "GROUP_JUPITER_B3"},
}

/*
================
Worlds

Every Jupiter world, in id order.
================
*/
func Worlds() []World {
	out := make([]World, len(worlds))
	copy(out, worlds)
	return out
}

/*
================
FieldWorlds

The field world alone: what the field switch registers.
================
*/
func FieldWorlds() []World {
	return []World{{FieldWorldID, "GROUP_JUPITER_FIELD"}}
}

/*
================
TempleWorlds

The six temple worlds of the two Mirror Dimension sets: what the
temple switch registers beside the field.
================
*/
func TempleWorlds() []World {
	out := make([]World, 0, len(worlds)-1)
	for _, world := range worlds {
		if world.ID != FieldWorldID {
			out = append(out, world)
		}
	}
	return out
}

/*
================
CodeName

The codename a Jupiter row joins, from its source world id. A source
world that is not a Jupiter world reports false.
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
