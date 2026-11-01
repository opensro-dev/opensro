/*
===========================================================================

temple.go - the temple band worlds, port-only identity table.

Port-only, not native; the worlds exist only while SRO_CAP110_TEMPLE is
on (#1800 step 3). The source database's own world table carries the 22
temple worlds (ids 63-84) as instanced worlds, each named for its level
band (INS_GOD_TOGUI_35_50_LEVEL_1 and kin), and its world-group config
carries the band itself (ENTER_LIMIT_CONDITION_LEVEL_MINIMUM_NUM /
MAXIMUM_NUM). The shipped v1.150 catalogue has none of them, so the
port registers exactly these while the switch is on. Unlike the
Pharaoh Tombs' permanent worlds, each band is per-party layered (the
owner settlement on #1800), which the instance definitions in
world/instance carry.

This leaf owns only the identity table. The monster catalogue asks it
which world a temple row joins, and the world catalogue asks it what
to register, so the two can never disagree. It imports nothing.

===========================================================================
*/
package temple

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

One temple world: its shipped id, the codename its population joins,
the level band its group config admits (enter limits, inclusive), and
the residents a layer holds. Bands repeat: the LEVEL_1/_2 worlds of a
band take four residents a layer, the LEVEL_3/_4 worlds eight.

Note the top band: the codename says 100_110 but the group's enter
minimum is 101, not 100. The config is the gate, so the band is
101-110.
================
*/
type World struct {
	ID          int
	CodeName    string
	MinLevel    int
	MaxLevel    int
	PlayerLimit int
}

var worlds = []World{
	{63, "INS_GOD_TOGUI_35_50_LEVEL_1", 35, 50, 4},
	{64, "INS_GOD_TOGUI_35_50_LEVEL_2", 35, 50, 4},
	{65, "INS_GOD_TOGUI_51_60_LEVEL_1", 51, 60, 4},
	{66, "INS_GOD_TOGUI_51_60_LEVEL_2", 51, 60, 4},
	{67, "INS_GOD_TOGUI_61_70_LEVEL_1", 61, 70, 4},
	{68, "INS_GOD_TOGUI_61_70_LEVEL_2", 61, 70, 4},
	{69, "INS_GOD_FLAME_71_80_LEVEL_1", 71, 80, 4},
	{70, "INS_GOD_FLAME_71_80_LEVEL_2", 71, 80, 4},
	{71, "INS_GOD_FLAME_71_80_LEVEL_3", 71, 80, 8},
	{72, "INS_GOD_FLAME_71_80_LEVEL_4", 71, 80, 8},
	{73, "INS_GOD_FLAME_81_90_LEVEL_1", 81, 90, 4},
	{74, "INS_GOD_FLAME_81_90_LEVEL_2", 81, 90, 4},
	{75, "INS_GOD_FLAME_81_90_LEVEL_3", 81, 90, 8},
	{76, "INS_GOD_FLAME_81_90_LEVEL_4", 81, 90, 8},
	{77, "INS_GOD_WRECK_IN_91_100_LEVEL_1", 91, 100, 4},
	{78, "INS_GOD_WRECK_IN_91_100_LEVEL_2", 91, 100, 4},
	{79, "INS_GOD_WRECK_OUT_91_100_LEVEL_3", 91, 100, 8},
	{80, "INS_GOD_WRECK_OUT_91_100_LEVEL_4", 91, 100, 8},
	{81, "INS_GOD_WRECK_IN_100_110_LEVEL_1", 101, 110, 4},
	{82, "INS_GOD_WRECK_IN_100_110_LEVEL_2", 101, 110, 4},
	{83, "INS_GOD_WRECK_OUT_100_110_LEVEL_3", 101, 110, 8},
	{84, "INS_GOD_WRECK_OUT_100_110_LEVEL_4", 101, 110, 8},
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

/*
================
Band

The level band (inclusive) a temple world admits, from its source
world id. A source world that is not a temple band reports false.
================
*/
func Band(worldID int) (minLevel, maxLevel int, ok bool) {
	for _, world := range worlds {
		if world.ID == worldID {
			return world.MinLevel, world.MaxLevel, true
		}
	}
	return 0, 0, false
}
