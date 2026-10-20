/*
===========================================================================

sunscale.go - the beta's wider degree-9 Sun class (port-only, not native)

Port-only, not native, beta balance (owner 2026-10-20; progression/
sunrate.go). Inside the rare class table's rows for levels 76-90, the
degree-9 Seal of Sun class (the *_09_C_RARE bucket's class) is mass
multiplier times as wide; every other class keeps its native width and
only shifts by the Sun class's growth, so Star and Moon attempts are
bit-identical. The class is derived from the catalogue's own *_09_C_RARE
bucket references, never named here. A row whose scaled end would leave
the million-roll is returned native instead.

The roll path composes this after the seal-band row (sealband.go): the
band first, this scaling second, so levels 86-90 roll band-extended Star
and Moon alongside the widened Sun.

===========================================================================
*/
package loot

import "strings"

// The Sun class scaling covers rare rows of these monster levels.
const (
	sunScaleMinLevel = 76
	sunScaleMaxLevel = 90
)

// sunClassGroup is the rare table's degree-9 Sun class, derived at build.
var sunClassGroup = deriveSunClassGroup(equipment)

/*
================
SunScaleCovers

Whether the Sun scaling changes this level's rare row at all.
================
*/
func SunScaleCovers(level uint8) bool {
	return sunClassGroup >= 0 && level >= sunScaleMinLevel && level <= sunScaleMaxLevel
}

/*
================
deriveSunClassGroup

The equipment class of the *_09_C_RARE items: the one class every Sun
bucket reference (both countries) agrees on, or -1 when the catalogue
holds none or they disagree.
================
*/
func deriveSunClassGroup(c equipmentCatalog) int {
	group := -1
	for key, bucket := range c.buckets {
		if !key.rare {
			continue
		}
		for _, ref := range bucket.refs {
			if !strings.HasSuffix(ref.Codename, "_09_C_RARE") {
				continue
			}
			if group >= 0 && group != key.group {
				return -1
			}
			group = key.group
		}
	}
	return group
}

/*
================
scaleSunClass

row with its Sun class mass times as wide. A multiplier of 1, a level
outside the scaling scope or a catalogue without a Sun class returns
the row untouched (shared, not copied), so the native path is exact.
================
*/
func scaleSunClass(row []classThreshold, level uint8, multiplier int) []classThreshold {
	if multiplier <= 1 || sunClassGroup < 0 ||
		level < sunScaleMinLevel || level > sunScaleMaxLevel {
		return row
	}
	at := -1
	for i, class := range row {
		if class.group == sunClassGroup {
			at = i
			break
		}
	}
	if at < 0 {
		return row
	}
	var below uint32
	if at > 0 {
		below = row[at-1].threshold
	}
	growth := (row[at].threshold - below) * uint32(multiplier-1)
	if len(row) > 0 && row[len(row)-1].threshold+growth > 1_000_000 {
		return row
	}
	out := make([]classThreshold, len(row))
	for i, class := range row {
		if i >= at {
			class.threshold += growth
		}
		out[i] = class
	}
	return out
}

/*
================
composedRareRow

The rare row a roll or an odds publication reads: the seal band's row
while the band is in force, the catalogue's row otherwise, with the Sun
class scaled on top of either.
================
*/
func composedRareRow(level uint8, band bool, sunMultiplier int) ([]classThreshold, bool) {
	var row []classThreshold
	if band {
		banded, ok := sealBandRow(level)
		if !ok {
			return nil, false
		}
		row = banded
	} else {
		if level == 0 || int(level) > len(equipment.classes[1]) {
			return nil, false
		}
		row = equipment.classes[1][level-1]
	}
	return scaleSunClass(row, level, sunMultiplier), true
}

/*
================
ComposedRareGroup

EquipmentGroup's rare roll over the composed row (same lower_bound as
SealBandRareGroup).
================
*/
func ComposedRareGroup(level uint8, roll uint32, band bool, sunMultiplier int) (int, bool) {
	row, ok := composedRareRow(level, band, sunMultiplier)
	if !ok || roll >= 1_000_000 {
		return 0, false
	}
	return groupAt(row, roll)
}

/*
================
ComposedRareChances

EquipmentAttemptChances' rare attempt over the composed row, for the
website's tables (catalogue/droptables.go).
================
*/
func ComposedRareChances(level, country uint8, band bool, sunMultiplier int) []Chance {
	row, ok := composedRareRow(level, band, sunMultiplier)
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
