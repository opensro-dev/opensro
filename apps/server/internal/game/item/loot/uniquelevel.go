/*
===========================================================================

uniquelevel.go - the level a top-band unique's equipment tables roll at

Port-only, not native (owner 2026-10-25, narrowed 2026-10-26). A unique's
reward tables are built for its own level, so a capped hunter looting
Tiger Girl (level 20) received degree-3 gear even once the level-gap
exemption admitted the kill. The owner's rule: a unique already in the
installed catalog's top loot band rolls its equipment at the highest
level the catalog fills; every other unique keeps its own level. Native
content ends at level 90 (degree 9); the optional cap-110 catalog
(cap110.go) lifts it to level 110 (degree 11), so a cap change moves the
drop level and the band with it and no degree is named here.

===========================================================================
*/
package loot

// expansionMonsterLevelCap is the highest monster level the optional
// cap-110 catalog adds; the level cap's own expansion level.
const expansionMonsterLevelCap uint8 = 110

// cap110Installed reports that InitCap110 published the expansion catalog.
// The catalog itself is the same kind of startup-owned table.
var cap110Installed bool

/*
================
UniqueDropLevel

The level a unique's equipment tables roll at: the native cap, raised to
the expansion cap while the cap-110 catalog is installed and its top class
row carries gear. A cap with no gear at the top keeps the native level, so
a misconfigured deployment still drops something rather than nothing.
================
*/
func UniqueDropLevel() uint8 {
	if cap110Installed {
		if _, ok := SpecialEquipmentGroup(expansionMonsterLevelCap); ok {
			return expansionMonsterLevelCap
		}
	}
	return nativeMonsterLevelCap
}

// topLootBandLevels is how far below the drop level a unique still counts
// as top-band for the max-level roll: its own level within this many
// levels of the top (own level >= drop level - topLootBandLevels).
const topLootBandLevels = 10

/*
================
UniqueInTopLootBand

Whether a unique of this level belongs to the installed catalog's top
loot band and so rolls at UniqueDropLevel under the max-level rule. Every
unique below the band keeps its own level's table. The band follows the
drop level, so installing the cap-110 catalog moves both together.
================
*/
func UniqueInTopLootBand(level uint8) bool {
	return int(level)+topLootBandLevels >= int(UniqueDropLevel())
}
