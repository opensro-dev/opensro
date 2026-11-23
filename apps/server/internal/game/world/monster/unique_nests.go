/*
===========================================================================

unique_nests.go - which population nests spawn a unique

The one owner of the unique rule. The simulation's notices, kill record
and unique tracker ask it, and so do the action lane's unique rewards
and the world map's port-only nest table (sro-evidence unique-nests), so
nothing re-derives the rule from the textdata on its own.

A unique's rarity grade (the low nibble of Instance.Rarity) is 3 or 8:
grade 3 is every field unique's grade, and the tomb and temple data
authors its named uniques at grade 8 (the Tomb General family among
them). The drop prepass has always rolled unique drops for both grades;
the announcement and counting gates once read grade 3 only, so a
grade-8 kill was never announced, recorded or listed (#4235).

===========================================================================
*/
package monster

// uniqueRarityMask isolates the grade of a rarity byte; the high nibble
// is the party-monster flag, not part of the grade.
const uniqueRarityMask = 15

// The grades that name a unique.
const (
	uniqueGrade     = 3
	uniqueGradeLate = 8
)

/*
================
RarityIsUnique

Whether a rarity byte names a unique: its grade is 3 or 8.
================
*/
func RarityIsUnique(rarity uint8) bool {
	grade := rarity & uniqueRarityMask
	return grade == uniqueGrade || grade == uniqueGradeLate
}

/*
================
NestIsUnique

A nest spawns a unique when its rarity (the nest's override, else the
reference's monster type) names one, as Instance.Rarity reads it.
================
*/
func NestIsUnique(nest NestRow, ref MonsterRef) bool {
	if nest.HasRarityOverride {
		return RarityIsUnique(nest.RarityOverride)
	}
	return RarityIsUnique(ref.MonsterType)
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
