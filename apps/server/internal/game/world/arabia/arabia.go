/*
===========================================================================

arabia.go - the Arabia dungeon world, port-only identity table.

Port-only, not native; the worlds exist only while
SRO_BAGHDAD_DUNGEON is on (#4134; the continent rides SRO_ARABIA,
#3268). The source database tags the Baghdad dungeon's regions with
its own game world id (112) and the boss lair's (the Kalia Hideout)
with 113; the shipped world catalogue has neither, so the port
registers exactly these two as permanent worlds. The lair's chain is
the source's own (GATE_MONG_TO_BOSS at the field-02 mound,
GATE_BOSS_TO_MONG on the template's doorstep; the import takes the
pair only while the extraction carries the lair's nests,
scripts/arabia_gates.py), and its ground is the later client's own
dungeon row (the DOF the build admits by file existence).
Petra (114) and the garden (115) carry no nests and no gates in
either later source, so they stay unregistered.

This leaf owns only the identity table. The monster catalogue asks it
which world a dungeon row joins, and the world catalogue asks it what
to register, so the two can never disagree. It imports nothing.

===========================================================================
*/
package arabia

// The dungeon world's shipped id.
const DungeonWorldID = 112

// The boss lair's shipped id (the Kalia Hideout), registered beside
// the dungeon under the same switch (#4134).
const HideoutWorldID = 113

// dungeonRegionBit marks a region whose ground is a dungeon template,
// not a sector.
const dungeonRegionBit = 0x8000

// The Kalia Hideout's region (RN_ARABIA_FIELD_02_BOSS in the later
// client's regioncode): the one dungeon-bit region of the Arabia set,
// admitted below by id while every other Arabia region is low-bit.
const HideoutRegionID = 32793

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
	{HideoutWorldID, "GROUP_ARABIA_FIELD_02_BOSS"},
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

// The hideout chain's gate codes, the source's own rows: the field
// entry at the field-02 mound and the exit on the template's
// doorstep.
const (
	HideoutEntryGateCode = "GATE_MONG_TO_BOSS"
	HideoutExitGateCode  = "GATE_BOSS_TO_MONG"
)

/*
================
HidesGateCode

Whether a teleportdata row is one of the hideout's gates. While the
dungeon switch is off the whole lair is absent: the exit hides with
its world (IsDungeonWorld), and the entry - whose row names the
field's world, as the source authored it - hides by this code, so a
click on either standing building refuses like the dungeon's own
buildings' clicks do.
================
*/
func HidesGateCode(code string) bool {
	return code == HideoutEntryGateCode || code == HideoutExitGateCode
}

/*
================
IsRegion

Whether a region is one Arabia's population may stand in: every
low-bit region by its sector, and the hideout's dungeon-bit region
by id (its ground is the dungeon template the build admits, never a
sector).
================
*/
func IsRegion(region uint16) bool {
	return region&dungeonRegionBit == 0 || region == HideoutRegionID
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
