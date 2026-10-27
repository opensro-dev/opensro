/*
===========================================================================

sealpity.go - seal pity: luck-debt bad-luck protection (port-only, not native)

Port-only, not native (owner decision 2026-10-27, #1690, decided by
simulation on the issue). Every credited monster kill owes its loot
owner luck-debt equal to that kill's exact chance of each tracked seal
(degree, tier), times the kill's level-gap admission chance. When a
counter reaches its threshold T (in expected drops), the next eligible
kill forces a real drop of that tier. Only degree-9 Moon and Sun carry
a threshold by default; degree-9 Star is the calibration tier, counted
in the daily rollup without a counter.

Modes (SRO_SEAL_PITY): off is native - no counters, no debt, no forced
drops, and the loot planner's random sequence is untouched. Shadow runs
the counters, logs every drop it would have forced and resets as if it
had forced one, but never adds an item, so its loot output stays
byte-identical to off. On forces the drops.

The counters, the hook and the rollup live in action/sealpity.go; the
debt itself persists on the character (domain.SealPityDebt).

===========================================================================
*/
package progression

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// The seal pity switches (shardsettings.go carries the same names).
const (
	EnvSealPity           = "SRO_SEAL_PITY"
	EnvSealPityThresholds = "SRO_SEAL_PITY_THRESHOLDS"
	EnvSealPityDailyCap   = "SRO_SEAL_PITY_DAILY_CAP"
)

// DefaultSealPityThresholds enables degree-9 Moon and Sun at T = 1.5
// expected drops (#1690: the threshold caps the wait at 1.5 times the
// average for 29% more supply; T = 1.0 would be a disguised rate rise).
const DefaultSealPityThresholds = "9:moon:1.5,9:sun:1.5"

// DefaultSealPityDailyCap bounds forced drops per character per
// (degree, tier) per UTC day. It never binds at T = 1.5 (the fastest
// forced cycle is 62 farming hours); it exists so a mistyped T cannot
// mint items.
const DefaultSealPityDailyCap = 1

// maxSealPityThreshold caps one parsed threshold: past it a counter
// cannot fire in a shard's lifetime, so a larger value is a typo.
const maxSealPityThreshold = 1000.0

/*
================
SealPityMode

Off, shadow or on (SealPity). The zero value is off.
================
*/
type SealPityMode int

const (
	SealPityOff SealPityMode = iota
	SealPityShadow
	SealPityOn
)

/*
================
SealPityMode.Name

The mode as the switch spells it, for the boot log.
================
*/
func (m SealPityMode) Name() string {
	switch m {
	case SealPityShadow:
		return "shadow"
	case SealPityOn:
		return "on"
	}
	return "off"
}

/*
================
SealPity

The parsed rule: the mode, one threshold per (degree 1..9, Star/Moon/
Sun) in expected drops (0 tracks no counter) and the daily forced cap.
The zero value is off with no thresholds.
================
*/
type SealPity struct {
	mode       SealPityMode
	thresholds [maxSealDegree][sealClassesPerDegree]float64
	dailyCap   int
}

/*
================
SealPityFromEnv

Reads the three switches. A malformed value stops the boot (as a
malformed drop mix does): a pity rule that silently ran at other
numbers than the operator wrote would mint or withhold items. The
thresholds name degrees 1..9 and tiers star, moon or sun, e.g.
"9:moon:1.5,9:sun:1.5"; unset runs DefaultSealPityThresholds, set-but-
empty enables no tier.
================
*/
func SealPityFromEnv() (SealPity, error) {
	pity := SealPity{dailyCap: DefaultSealPityDailyCap}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvSealPity))) {
	case "", "off":
		pity.mode = SealPityOff
	case "shadow":
		pity.mode = SealPityShadow
	case "on":
		pity.mode = SealPityOn
	default:
		return SealPity{}, fmt.Errorf("seal pity: %s %q is not off, shadow or on",
			EnvSealPity, os.Getenv(EnvSealPity))
	}
	thresholds, present := os.LookupEnv(EnvSealPityThresholds)
	if !present {
		thresholds = DefaultSealPityThresholds
	}
	if err := pity.parseThresholds(thresholds); err != nil {
		return SealPity{}, err
	}
	if raw, present := os.LookupEnv(EnvSealPityDailyCap); present {
		cap, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || cap < 0 {
			return SealPity{}, fmt.Errorf("seal pity: %s %q is not a non-negative count",
				EnvSealPityDailyCap, raw)
		}
		pity.dailyCap = cap
	}
	return pity, nil
}

/*
================
SealPity.parseThresholds

Fills the threshold table from "degree:tier:T,..." entries. Caller
holds no state; a bad entry names itself in the error.
================
*/
func (p *SealPity) parseThresholds(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	seen := map[int]bool{}
	for _, entry := range strings.Split(raw, ",") {
		parts := strings.Split(strings.TrimSpace(entry), ":")
		if len(parts) != 3 {
			return fmt.Errorf("seal pity: threshold %q is not degree:tier:T", entry)
		}
		degree, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil || degree < 1 || degree > maxSealDegree {
			return fmt.Errorf("seal pity: threshold %q names no degree 1..%d", entry, maxSealDegree)
		}
		var tier int
		switch strings.ToLower(strings.TrimSpace(parts[1])) {
		case "star":
			tier = SealStar
		case "moon":
			tier = SealMoon
		case "sun":
			tier = SealSun
		default:
			return fmt.Errorf("seal pity: threshold %q names no tier (star, moon or sun)", entry)
		}
		threshold, err := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
		if err != nil || threshold <= 0 || threshold > maxSealPityThreshold {
			return fmt.Errorf("seal pity: threshold %q is not a T in (0, %v]", entry, maxSealPityThreshold)
		}
		itemClass := (degree-1)*sealClassesPerDegree + tier + 1
		if seen[itemClass] {
			return fmt.Errorf("seal pity: threshold %q repeats a (degree, tier)", entry)
		}
		seen[itemClass] = true
		p.thresholds[degree-1][tier] = threshold
	}
	return nil
}

/*
================
SealPity.Mode
================
*/
func (p SealPity) Mode() SealPityMode { return p.mode }

/*
================
SealPity.Enabled

Whether the counters run at all (shadow or on). Off is native: the
planner never looks at a counter.
================
*/
func (p SealPity) Enabled() bool { return p.mode != SealPityOff }

/*
================
SealPity.ForcesDrops

Whether a reached threshold adds an item (on). Shadow only counts.
================
*/
func (p SealPity) ForcesDrops() bool { return p.mode == SealPityOn }

/*
================
SealPity.Threshold

One (degree, tier) threshold in expected drops; 0 (no counter) for a
tier the table leaves off or an index outside the nine degrees.
================
*/
func (p SealPity) Threshold(degree, tier int) float64 {
	if degree < 1 || degree > maxSealDegree || tier < 0 || tier >= sealClassesPerDegree {
		return 0
	}
	return p.thresholds[degree-1][tier]
}

/*
================
SealPity.DailyCap

Forced drops allowed per character per (degree, tier) per UTC day.
================
*/
func (p SealPity) DailyCap() int { return p.dailyCap }
