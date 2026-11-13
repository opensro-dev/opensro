/*
===========================================================================

unique_nests.go - which population nests spawn a unique

The one owner of the unique rule. The simulation's unique tracker and
reverse return anchors ask it, and so does the world map's port-only nest
table (sro-evidence unique-nests), so the map never re-derives the rule
from the textdata on its own.

===========================================================================
*/
package monster

// uniqueRarity is the low nibble of a unique's rarity (Instance.Rarity).
const (
	uniqueRarityMask = 15
	uniqueRarity     = 3
)

/*
================
NestIsUnique

A nest spawns a unique when its rarity (the nest's override, else the
reference's monster type) has low nibble 3, as Instance.Rarity reads it.
================
*/
func NestIsUnique(nest NestRow, ref MonsterRef) bool {
	if nest.HasRarityOverride {
		return nest.RarityOverride&uniqueRarityMask == uniqueRarity
	}
	return ref.MonsterType&uniqueRarityMask == uniqueRarity
}

/*
================
UniqueNests

Every nest of the template whose reference spawns a unique, in template
order. A unique with several nests keeps them all: each is a candidate.
================
*/
func (t Template) UniqueNests() []NestRow {
	var nests []NestRow
	for _, nest := range t.Nests {
		ref, ok := t.Refs[nest.RefObjID]
		if ok && NestIsUnique(nest, ref) {
			nests = append(nests, nest)
		}
	}
	return nests
}
