/*
===========================================================================

growthbands.go - the beta EXP and SP multiplier by level band (port-only,
not native)

Owner, 2026-10-24, with the 110 cap: levels 1-59 keep the level-1 pace
(levelPace, growth.go) and higher levels earn a fixed multiple of the
native gain. The default (shardsettings, #1606) derives each band's
multiple from one smooth target kills-per-level curve, so no level costs
more than twice the one before it (growthbands_curve_test.go); the first
bands, 60-89 at 20 and 90-110 at 10, made 59 -> 60 an 84x step and
90 -> 110 about 2,600 h of play. SRO_BETA_GROWTH_BANDS names the bands as a comma list of
"<first>-<last>:<rule>", the rule being "pace" or a whole multiplier, for
example "1-59:pace,60-89:20,90-110:10". Unset, every level keeps the pace,
which is the beta before this change. A level no band covers earns the
native gain. Skill EXP takes the same band multiplier before its flat
SkillExpRate, as it took the pace before.

The bands live in a fixed array so GrowthRates stays comparable and
switching back is still unsetting a variable and restarting the world.

===========================================================================
*/
package progression

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"opensro.online/server/internal/game/enterworld"
)

// EnvBetaGrowthBands overrides the per-level-band growth multipliers.
const EnvBetaGrowthBands = "SRO_BETA_GROWTH_BANDS"

// maxGrowthBands bounds the band list.
const maxGrowthBands = 8

// maxBandMultiplier bounds a fixed band multiplier.
const maxBandMultiplier = 10_000

// growthBandPace marks a band that keeps the level-1 pace.
const growthBandPace = 0

/*
================
GrowthBand

Levels First..Last earn Multiplier times the native gain, or the level-1
pace when Multiplier is growthBandPace.
================
*/
type GrowthBand struct {
	First, Last int64
	Multiplier  int64
}

/*
================
GrowthBands

The parsed bands in level order; Count 0 means every level keeps the pace.
================
*/
type GrowthBands struct {
	Count int
	Band  [maxGrowthBands]GrowthBand
}

/*
================
ParseGrowthBands

Strict: bands must be ascending, non-overlapping, within level 1..
ExpansionLevelCap, and each rule "pace" or a whole multiplier 1..
maxBandMultiplier. An empty text is the all-pace default.
================
*/
func ParseGrowthBands(text string) (GrowthBands, error) {
	var bands GrowthBands
	text = strings.TrimSpace(text)
	if text == "" {
		return bands, nil
	}
	last := int64(0)
	for _, part := range strings.Split(text, ",") {
		span, rule, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok {
			return GrowthBands{}, fmt.Errorf("%s: %q needs <first>-<last>:<rule>", EnvBetaGrowthBands, part)
		}
		lo, hi, ok := strings.Cut(span, "-")
		if !ok {
			return GrowthBands{}, fmt.Errorf("%s: %q needs a level range", EnvBetaGrowthBands, part)
		}
		first, err1 := strconv.ParseInt(strings.TrimSpace(lo), 10, 64)
		end, err2 := strconv.ParseInt(strings.TrimSpace(hi), 10, 64)
		if err1 != nil || err2 != nil || first < 1 || end < first || end > ExpansionLevelCap || first <= last {
			return GrowthBands{}, fmt.Errorf("%s: %q is not an ascending range within 1-%d", EnvBetaGrowthBands, part, ExpansionLevelCap)
		}
		multiplier := int64(growthBandPace)
		if rule = strings.ToLower(strings.TrimSpace(rule)); rule != "pace" {
			n, err := strconv.ParseInt(rule, 10, 64)
			if err != nil || n < 1 || n > maxBandMultiplier {
				return GrowthBands{}, fmt.Errorf("%s: %q rule must be pace or 1-%d", EnvBetaGrowthBands, part, maxBandMultiplier)
			}
			multiplier = n
		}
		if bands.Count == maxGrowthBands {
			return GrowthBands{}, fmt.Errorf("%s: at most %d bands", EnvBetaGrowthBands, maxGrowthBands)
		}
		bands.Band[bands.Count] = GrowthBand{First: first, Last: end, Multiplier: multiplier}
		bands.Count++
		last = end
	}
	return bands, nil
}

/*
================
GrowthBandsFromEnv
================
*/
func GrowthBandsFromEnv() (GrowthBands, error) {
	return ParseGrowthBands(os.Getenv(EnvBetaGrowthBands))
}

/*
================
GrowthBands.multiplier

The gain multiplier at this level: the band's fixed multiplier, the
level-1 pace for a pace band (and for every level when no band is set),
or 1 (native) for a level no band covers.
================
*/
func (b GrowthBands) multiplier(levels enterworld.LevelDataSource, level int64) float64 {
	if b.Count == 0 {
		return levelPace(levels, level)
	}
	for i := 0; i < b.Count; i++ {
		band := b.Band[i]
		if level < band.First || level > band.Last {
			continue
		}
		if band.Multiplier == growthBandPace {
			return levelPace(levels, level)
		}
		return float64(band.Multiplier)
	}
	return 1
}

/*
================
GrowthBands.Describe

The bands in plain words for the public rules page.
================
*/
func (b GrowthBands) Describe() string {
	if b.Count == 0 {
		return "Every level levels about as fast as level 1."
	}
	parts := make([]string, 0, b.Count)
	for i := 0; i < b.Count; i++ {
		band := b.Band[i]
		rule := fmt.Sprintf("%d times the retail rate", band.Multiplier)
		if band.Multiplier == growthBandPace {
			rule = "about as fast as level 1"
		}
		parts = append(parts, fmt.Sprintf("levels %d-%d: %s", band.First, band.Last, rule))
	}
	return "Experience and skill points by level band: " + strings.Join(parts, "; ") + "."
}
