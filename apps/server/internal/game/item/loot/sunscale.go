/*
===========================================================================

sunscale.go - the beta's wider degree-9 Sun class (port-only, not native)

Port-only, not native, beta balance (owner 2026-10-20; progression/
sunrate.go). Inside the rare class table's rows for levels 76-90, the
degree-9 Seal of Sun class (the *_09_C_RARE bucket's class) is mass
multiplier times as wide; every other class keeps its native width and
only shifts by the Sun class's growth, so Star and Moon attempts are
bit-identical. The classes are derived from the catalogue's own bucket
references, never named here. A row whose scaled end would leave the
million-roll is returned native instead.

Owner decision (#1605 P1, 2026-10-25): the widened Sun class is never
wider than the degree-9 Moon class (the *_09_B_RARE bucket's class) in
the same row, so with the keep factors (Star x3, Moon x2, Sun x1 over
the widening 3) a Sun expects about twice a Moon's kills at every
level. The coupling is deliberate: widen the Moon class later and the
Sun class's ceiling moves with it. A row that rolls no Moon class of
its own (levels 86-90 without the seal band) caps at the Moon class's
last native width; a catalogue with no derivable Moon class gets no
widening at all, since the ladder could not be proven there.

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

// moonClassGroup is the rare table's degree-9 Moon class, derived at
// build: the ceiling the widened Sun class answers to (#1605 P1).
var moonClassGroup = deriveMoonClassGroup(equipment)

// moonCapFallback is the Moon class's width in the last native row
// that rolls it: the ceiling for rows with no Moon class of their own.
var moonCapFallback = deriveMoonCapFallback(equipment)

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
	return deriveDegree9Class(c, "_09_C_RARE")
}

/*
================
deriveMoonClassGroup

The equipment class of the *_09_B_RARE items, derived the same way.
================
*/
func deriveMoonClassGroup(c equipmentCatalog) int {
	return deriveDegree9Class(c, "_09_B_RARE")
}

/*
================
deriveDegree9Class

The one class every degree-9 bucket reference with this suffix (both
countries) agrees on, or -1 when the catalogue holds none or they
disagree.
================
*/
func deriveDegree9Class(c equipmentCatalog, suffix string) int {
	group := -1
	for key, bucket := range c.buckets {
		if !key.rare {
			continue
		}
		for _, ref := range bucket.refs {
			if !strings.HasSuffix(ref.Codename, suffix) {
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
deriveMoonCapFallback

The Moon class's width in the last native rare row that rolls it
(level 85 in the shipped catalogue): the Sun cap for rows that roll
no Moon class of their own. 0 when the catalogue holds no Moon class.
================
*/
func deriveMoonCapFallback(c equipmentCatalog) uint32 {
	if moonClassGroup < 0 || len(c.classes) < 2 {
		return 0
	}
	for level := len(c.classes[1]); level >= 1; level-- {
		if width := classWidth(c.classes[1][level-1], moonClassGroup); width > 0 {
			return width
		}
	}
	return 0
}

/*
================
classWidth

A class's total width in a row (its entries' threshold steps).
================
*/
func classWidth(row []classThreshold, group int) uint32 {
	var below, width uint32
	for _, class := range row {
		if class.group == group {
			width += class.threshold - below
		}
		below = class.threshold
	}
	return width
}

/*
================
moonCap

The widest the Sun class may grow in row: the Moon class's own width,
or its last native width when the row rolls none. False when no cap
is derivable, in which case the row stays native.
================
*/
func moonCap(row []classThreshold) (uint32, bool) {
	if moonClassGroup < 0 {
		return 0, false
	}
	if width := classWidth(row, moonClassGroup); width > 0 {
		return width, true
	}
	return moonCapFallback, moonCapFallback > 0
}

/*
================
scaleSunClass

row with its Sun class mass times as wide, but never wider than the
Moon class (moonCap, #1605 P1). A multiplier of 1, a level outside
the scaling scope, a catalogue without a Sun class or one whose Moon
cap cannot be derived returns the row untouched (shared, not copied),
so the native path is exact.
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
	capMass, capped := moonCap(row)
	if !capped {
		return row
	}
	sunWidth := classWidth(row, sunClassGroup)
	target := sunWidth * uint32(multiplier)
	if target > capMass {
		target = capMass
	}
	growth := target - sunWidth
	if growth == 0 {
		return row
	}
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
