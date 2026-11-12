/*
===========================================================================

muhan.go - the Muhan wave worlds, port-only identity table.

Port-only, not native; the worlds exist only while SRO_MUHAN is on
(#3273). The later server's world table carries twelve MUHAN worlds,
all native type 0; four (99-102) bind no region and author no waves -
unbuilt shells that stay unregistered - and the eight banded worlds
below each run twenty authored waves (scripts/muhan_import.py). A
world's band rides its codename: GROUP_MUHAN_SOLO_75 spawns kinds at
levels 72-80 (the wave rows' offsets around 75). Whether a world is
solo or party also rides the codename alone until the entry slice
gives it meaning.

This leaf owns only the identity table. The wave director asks it
which worlds run waves and at what band, and the world catalogue
asks it what to register, so the two can never disagree. It imports
nothing.

===========================================================================
*/
package muhan

/*
================
World

One Muhan wave world: its shipped id, the codename its population
joins, the band level its waves offset from, and whether its name
calls it a solo world.
================
*/
type World struct {
	ID       int
	CodeName string
	Band     int
	Solo     bool
}

var worlds = []World{
	{103, "GROUP_MUHAN_SOLO_75", 75, true},
	{104, "GROUP_MUHAN_SOLO_85", 85, true},
	{105, "GROUP_MUHAN_PARTY_75", 75, false},
	{106, "GROUP_MUHAN_PARTY_85", 85, false},
	{108, "GROUP_MUHAN_SOLO_95", 95, true},
	{109, "GROUP_MUHAN_SOLO_105", 105, true},
	{110, "GROUP_MUHAN_PARTY_95", 95, false},
	{111, "GROUP_MUHAN_PARTY_105", 105, false},
}

/*
================
Worlds

Every Muhan wave world, in id order. The unbanded shells (99-102)
are not here: nothing registers them.
================
*/
func Worlds() []World {
	out := make([]World, len(worlds))
	copy(out, worlds)
	return out
}

/*
================
Lookup

The wave world with this shipped id. A source world that is not a
Muhan wave world (the shells included) reports false.
================
*/
func Lookup(worldID int) (World, bool) {
	for _, world := range worlds {
		if world.ID == worldID {
			return world, true
		}
	}
	return World{}, false
}

/*
================
CodeName

The codename a Muhan row joins, from its source world id.
================
*/
func CodeName(worldID int) (string, bool) {
	world, ok := Lookup(worldID)
	if !ok {
		return "", false
	}
	return world.CodeName, true
}
