/*
===========================================================================

sealband.go - the beta's degree-9 Star and Moon band (port-only, not native)

Port-only, not native, beta balance (owner 2026-10-13; Star valley filled
2026-10-19). In the rare class table, degree-9 Seal of Star's direct class
(G24, 3554 of a million) stops after monster level 80, leaving levels 81-89
with only the small degree-10 step-3 fallback (G27, 269 at 81-84 and 538 at
85-89) before G27 rises to 2689 at levels 90-94. Degree-9 Seal of Moon's
direct class (G25, 320 of a million) stops after level 85. With the beta
seal rates on, levels 81-89 also roll level 80's degree-9 Star prefix and
levels 86-94 also roll level 85's degree-9 Moon prefix, so neither seal
drops into a step before the band ends at level 94.

The rule, from the catalogue's own rows: the classes level 80's rare row
rolls and level 81's does not form a prefix of the level-80 row (degree-9
Star, up to threshold 3554 of a million), and the classes level 85's rare
row rolls and level 86's does not form a prefix of the level-85 row
(degree-9 Moon, up to threshold 320 of a million). Each row in 81-94 keeps
its own entries untouched and gets the missing prefix(es) appended after its
last threshold, the same width. Every class the row had (degree-9 Sun and
the native step-3 fallbacks among them) keeps its exact chance; Star and
Moon gain their edge row's share, up to the combined roll's modulo bias
between the two positions (under 0.5%).

===========================================================================
*/

package loot

// The band's edges: level 80 is the last level whose rare row rolls degree-9
// Star (G24) directly, level 89 is the last level of the Star valley before
// G27 rises at level 90, level 85 is the last level whose rare row rolls
// degree-9 Moon (G25) directly, and level 94 is the band's end (degree-9
// Sun's last level).
const (
	sealStarEdge = 80
	sealStarEnd  = 89
	sealMoonEdge = 85
	sealBandEnd  = 94
)

// sealBandRows are the extended rare rows of levels sealStarEdge+1 through
// sealBandEnd; nil when the catalogue's rows do not have the prefix shape.
var sealBandRows = buildSealBandRows(equipment)

/*
================
missingPrefix

The leading classes of edge that next drops, or nil when edge does not split
into a dropped prefix followed by classes next keeps.
================
*/
func missingPrefix(edge, next []classThreshold) []classThreshold {
	kept := map[int]bool{}
	for _, class := range next {
		kept[class.group] = true
	}
	var prefix []classThreshold
	for _, class := range edge {
		if kept[class.group] {
			break
		}
		prefix = append(prefix, class)
	}
	if len(prefix) == 0 {
		return nil
	}
	for _, class := range edge[len(prefix):] {
		if !kept[class.group] {
			return nil
		}
	}
	return prefix
}

/*
================
appendPrefix

Appends prefix after row's last threshold, preserving each entry's width.
================
*/
func appendPrefix(row, prefix []classThreshold) []classThreshold {
	base := uint32(0)
	if len(row) > 0 {
		base = row[len(row)-1].threshold
	}
	for _, class := range prefix {
		row = append(row, classThreshold{group: class.group, threshold: base + class.threshold})
	}
	return row
}

/*
================
buildSealBandRows
================
*/
func buildSealBandRows(c equipmentCatalog) [][]classThreshold {
	rare := c.classes[1]
	if len(rare) < sealBandEnd {
		return nil
	}
	starPrefix := missingPrefix(rare[sealStarEdge-1], rare[sealStarEdge])
	moonPrefix := missingPrefix(rare[sealMoonEdge-1], rare[sealMoonEdge])
	if starPrefix == nil || moonPrefix == nil {
		return nil
	}
	rows := make([][]classThreshold, 0, sealBandEnd-sealStarEdge)
	for level := sealStarEdge + 1; level <= sealBandEnd; level++ {
		row := append([]classThreshold(nil), rare[level-1]...)
		if level <= sealStarEnd {
			row = appendPrefix(row, starPrefix)
		}
		if level > sealMoonEdge {
			row = appendPrefix(row, moonPrefix)
		}
		rows = append(rows, row)
	}
	return rows
}

/*
================
sealBandRow

The rare row the beta seal band rolls at this level: the extended row
inside the band, the catalogue's row everywhere else.
================
*/
func sealBandRow(level uint8) ([]classThreshold, bool) {
	if level == 0 || int(level) > len(equipment.classes[1]) {
		return nil, false
	}
	if InSealBand(level) {
		return sealBandRows[int(level)-sealStarEdge-1], true
	}
	return equipment.classes[1][level-1], true
}

/*
================
InSealBand

Whether the beta seal band changes this level's rare row.
================
*/
func InSealBand(level uint8) bool {
	return sealBandRows != nil && level > sealStarEdge && level <= sealBandEnd
}

/*
================
SealBandRareGroup

EquipmentGroup's rare roll with the beta seal band (same lower_bound).
================
*/
func SealBandRareGroup(level uint8, roll uint32) (int, bool) {
	row, ok := sealBandRow(level)
	if !ok || roll >= 1_000_000 {
		return 0, false
	}
	return groupAt(row, roll)
}

/*
================
SealBandRareChances

EquipmentAttemptChances' rare attempt with the beta seal band.
================
*/
func SealBandRareChances(level, country uint8) []Chance {
	row, ok := sealBandRow(level)
	if !ok {
		return nil
	}
	acc := chanceAccumulator{}
	for group, classMass := range classChances(row) {
		for dropCountry, countryMass := range equipmentCountries(country) {
			equipment.selectionChances(dropCountry, group, true, level, classMass*countryMass, acc.add)
		}
	}
	return acc.sorted()
}
