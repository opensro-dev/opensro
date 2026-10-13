/*
===========================================================================

sealband.go - the beta's degree-9 Star and Moon band (port-only, not native)

Port-only, not native, beta balance (owner 2026-10-13). The rare class
table rolls degree-9 Seal of Star and Moon only from monsters of level
76-85, while degree-9 Sun runs to 94. With the beta seal rates on, levels
86-94 also roll Star and Moon, at the share level 85 gives them, so the
band ends without a step.

The rule, from the catalogue's own rows: the classes level 85's rare row
rolls and level 86's does not form a prefix of the level-85 row (degree 9
Star and Moon, up to threshold 320 of a million). Each row of levels 86-94
keeps its own entries untouched and gets that band appended after its last
threshold, the same width. Every class the row had (degree-9 Sun among
them) keeps its exact chance; Star and Moon get level 85's, up to the
combined roll's modulo bias between the two positions (under 0.5%).

===========================================================================
*/

package loot

// The band's edge, the last level whose rare row rolls degree-9 Star and
// Moon natively, and the last level the beta extends them to (degree-9
// Sun's own last level).
const (
	sealBandEdge = 85
	sealBandEnd  = 94
)

// sealBandRows are the extended rare rows of levels sealBandEdge+1 through
// sealBandEnd; nil when the catalogue's rows do not have the prefix shape.
var sealBandRows = buildSealBandRows(equipment)

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
	edge, next := rare[sealBandEdge-1], rare[sealBandEdge]
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
	rows := make([][]classThreshold, 0, sealBandEnd-sealBandEdge)
	for level := sealBandEdge + 1; level <= sealBandEnd; level++ {
		row := append([]classThreshold(nil), rare[level-1]...)
		base := uint32(0)
		if len(row) > 0 {
			base = row[len(row)-1].threshold
		}
		for _, class := range prefix {
			row = append(row, classThreshold{group: class.group, threshold: base + class.threshold})
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
		return sealBandRows[int(level)-sealBandEdge-1], true
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
	return sealBandRows != nil && level > sealBandEdge && level <= sealBandEnd
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
