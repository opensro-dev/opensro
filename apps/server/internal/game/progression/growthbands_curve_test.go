/*
===========================================================================

growthbands_curve_test.go - the shipped default growth bands keep a smooth
level curve on the retail level table

The default SRO_BETA_GROWTH_BANDS (shardsettings) is port-only tuning. What
it must never do is what the first bands did: make one level cost 84 times
the level before it (59 -> 60) or put 90 -> 110 thousands of hours away.
The cost of a level is its at-level kills under the bands: the native kill
count levelKills over the band's multiplier, exactly as applyExperience
scales a gain.

===========================================================================
*/
package progression

import (
	"testing"

	"opensro.online/server/internal/config/shardsettings"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/licensed"
)

// maxAdjacentLevelStep bounds one level's cost over the level before it.
const maxAdjacentLevelStep = 2.0

// measuredKillsPerHour is #1606's live measurement: online players' levels
// gained per online hour times the level-1 pace (4.92 at-level kills a
// level), rounded.
const measuredKillsPerHour = 50

// Hours of play for 90 -> 110 at measuredKillsPerHour (#1604 P2).
const (
	minHours90To110 = 60
	maxHours90To110 = 100
)

/*
================
bandedLevelCost

At-level kills for level -> level+1 under bands.
================
*/
func bandedLevelCost(t *testing.T, levels enterworld.LevelDataSource, bands GrowthBands, level int64) float64 {
	t.Helper()
	kills, ok := levelKills(levels, level)
	if !ok {
		t.Fatalf("level %d has no level-table row", level)
	}
	return kills / bands.multiplier(levels, level)
}

/*
================
TestDefaultGrowthBandsKeepASmoothCurve

Every adjacent pair 1..110 stays within maxAdjacentLevelStep, which covers
every band edge, and 90 -> 110 lands inside P2's hours at the measured
rate.
================
*/
func TestDefaultGrowthBandsKeepASmoothCurve(t *testing.T) {
	levels := enterworld.NewTextdataLevels(licensed.RetailTextdataDir(t))
	bands, err := ParseGrowthBands(shardsettings.Lookup(EnvBetaGrowthBands).Default)
	if err != nil {
		t.Fatalf("default bands: %v", err)
	}
	for level := int64(1); level < ExpansionLevelCap; level++ {
		before := bandedLevelCost(t, levels, bands, level)
		after := bandedLevelCost(t, levels, bands, level+1)
		if step := after / before; step > maxAdjacentLevelStep {
			t.Errorf("level %d costs %.1f kills, %.2fx level %d's %.1f (max %.0fx)",
				level+1, after, step, level, before, maxAdjacentLevelStep)
		}
	}
	var kills float64
	for level := int64(90); level < ExpansionLevelCap; level++ {
		kills += bandedLevelCost(t, levels, bands, level)
	}
	hours := kills / measuredKillsPerHour
	if hours < minHours90To110 || hours > maxHours90To110 {
		t.Errorf("90 -> 110 costs %.0f at-level kills, %.0f h at %d an hour; want %d-%d h",
			kills, hours, measuredKillsPerHour, minHours90To110, maxHours90To110)
	}
}
