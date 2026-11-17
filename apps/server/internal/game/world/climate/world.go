/*
===========================================================================

world.go - every climate of the shard and the region lookup

One World runs the state machine of every generated climate row, and
answers which climate a region belongs to. The region lookup is a direct
index over all 65536 region ids built once at package load from the
generated ranges (the regioncombat.go pattern), so the per-crossing
lookup costs one array read. A region the reference data does not list
has no climate and no weather.

The World holds no lock: its owner (action/weather.go) serialises the
tick's Update against the readers.

===========================================================================
*/
package climate

import "strings"

// Count is the number of climates the reference data defines.
const Count = len(climateRows)

// Update reports changes as a uint32 bit set: the compiler refuses more
// than 32 climates here.
const _ = uint(32 - Count)

// noClimate marks a region id without a climate in regionClimateIndex.
const noClimate = 0

// Env switches the weather cycle (SRO_WEATHER, on or off). On, the
// shard's default, is native; off sends no weather at all and leaves
// every client clear.
const Env = "SRO_WEATHER"

/*
================
Enabled

Whether an SRO_WEATHER value leaves the cycle on: only an explicit off
(off, 0, false) stops it; absent, it runs, as natively.
================
*/
func Enabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "off":
		return false
	}
	return true
}

/*
================
regionClimateIndex

Every region id's climate, as its index in climateRows plus one (zero is
none), expanded from the generated ranges.
================
*/
var regionClimateIndex = buildRegionClimateIndex()

/*
================
buildRegionClimateIndex

A range naming a climate that has no row is a generator defect, caught by
the importer and again here.
================
*/
func buildRegionClimateIndex() [65536]uint8 {
	var index [65536]uint8
	for _, row := range regionClimateRanges {
		slot := noClimate
		for i, climate := range climateRows {
			if climate.ID == row.climate {
				slot = i + 1
				break
			}
		}
		if slot == noClimate {
			panic("climate: a region range names an unknown climate")
		}
		for id := int(row.first); id <= int(row.last); id++ {
			index[id] = uint8(slot)
		}
	}
	return index
}

/*
================
RegionClimate

The index (0..Count-1) of the climate a region belongs to.
================
*/
func RegionClimate(region uint16) (int, bool) {
	slot := regionClimateIndex[region]
	if slot == noClimate {
		return 0, false
	}
	return int(slot) - 1, true
}

/*
================
RowAt

The reference row of the climate at an index.
================
*/
func RowAt(index int) Row { return climateRows[index] }

/*
================
World
================
*/
type World struct {
	states [Count]State
}

/*
================
NewWorld

Every climate starts at nowMs; nil random is NativeRand.
================
*/
func NewWorld(nowMs int64, random Rand) *World {
	w := &World{}
	for i, row := range climateRows {
		w.states[i] = NewState(row, nowMs, random)
	}
	return w
}

/*
================
World.Update

One server tick for every climate. The result has bit i set when climate
i's current weather changed; it allocates nothing.
================
*/
func (w *World) Update(nowMs int64) uint32 {
	var changed uint32
	for i := range w.states {
		if w.states[i].Update(nowMs) {
			changed |= 1 << i
		}
	}
	return changed
}

/*
================
World.Current

The current weather of the climate at an index.
================
*/
func (w *World) Current(index int) Weather { return w.states[index].Current() }

/*
================
World.RegionWeather

The current weather a region's players see; false for a region without
a climate.
================
*/
func (w *World) RegionWeather(region uint16) (Weather, bool) {
	index, ok := RegionClimate(region)
	if !ok {
		return Weather{}, false
	}
	return w.states[index].Current(), true
}
