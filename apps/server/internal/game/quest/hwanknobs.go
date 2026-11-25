/*
===========================================================================

hwanknobs.go - the operator knobs of the Blue Berserk quest chains

Port-only, not native: three environment variables let an operator change
how generous the cap-110 Blue Berserk quests are. Each one's unset (or empty)
value is the original behaviour, byte for byte: the original rolls use
rand() mod 101, which no whole-percent knob can express, so an unset knob
keeps the 101-roll thresholds and a set knob replaces the roll with one over
100 outcomes. A malformed value refuses the boot (never rounded, truncated
or silently ignored), like SRO_LEVEL_CAP.

	SRO_HWAN_NIYA_CAPTURE_RATES=<nothing>,<flutter>,<normal>
	    the trap quest's capture roll as three whole percentages that sum
	    to 100 (original: 70,28,2 of 101).
	SRO_HWAN_SUN_RELIC=<percent>
	    the Sun God relic dig's chance as a whole percentage 0 to 100
	    (original: 11 of 101).
	SRO_HWAN_3_FAST_DROP=on|off
	    the Alexandria ostracon steps (QNO_SD_HW_002 to 008) drop ten quest
	    items per kill instead of one (original: one).

===========================================================================
*/
package quest

import (
	"fmt"
	"strconv"
	"strings"
)

// The knob variables.
const (
	EnvHwanNiyaCaptureRates = "SRO_HWAN_NIYA_CAPTURE_RATES"
	EnvHwanSunRelic         = "SRO_HWAN_SUN_RELIC"
	EnvHwan3FastDrop        = "SRO_HWAN_3_FAST_DROP"
)

// The original rolls, in outcomes of rand() mod 101.
const (
	// nativeRollSpan is the modulus of the original quest rolls.
	nativeRollSpan = 101
	// nativeNiyaNormalBelow and nativeNiyaFlutterBelow are the capture
	// roll's boundaries: below 2 a Normal Spirit, below 30 a Flutter Spirit.
	nativeNiyaNormalBelow  = 2
	nativeNiyaFlutterBelow = 30
	// nativeSunRelicAtMost is the dig roll's last winning value.
	nativeSunRelicAtMost = 10
	// knobRollSpan is the outcome count of a knob-driven roll.
	knobRollSpan = 100
	// FastDropCount is the quest items one kill drops while
	// SRO_HWAN_3_FAST_DROP is on.
	FastDropCount = 10
)

// NiyaOutcome is what a closed Niya trap yields.
type NiyaOutcome int

const (
	NiyaNothing NiyaOutcome = iota
	NiyaFlutterSpirit
	NiyaNormalSpirit
)

/*
================
HwanKnobs

The parsed knobs. The zero value is the original behaviour.
================
*/
type HwanKnobs struct {
	// NiyaRates is {nothing, flutter, normal} in whole percent when the knob
	// is set.
	NiyaRates    [3]uint8
	NiyaRatesSet bool
	// SunRelicPercent is the dig's chance in whole percent when the knob is set.
	SunRelicPercent uint8
	SunRelicSet     bool
	// FastDrop makes the ostracon steps drop FastDropCount items per kill.
	FastDrop bool
}

/*
================
HwanKnobsFromEnv

Parses the three knobs from the given lookup (os.Getenv in production). Empty
or whitespace-only is unset.
================
*/
func HwanKnobsFromEnv(getenv func(string) string) (HwanKnobs, error) {
	var knobs HwanKnobs
	if raw := strings.TrimSpace(getenv(EnvHwanNiyaCaptureRates)); raw != "" {
		parts := strings.Split(raw, ",")
		if len(parts) != 3 {
			return HwanKnobs{}, fmt.Errorf("%s must be three whole percentages nothing,flutter,normal (got %q)", EnvHwanNiyaCaptureRates, raw)
		}
		sum := 0
		for i, part := range parts {
			value, err := wholePercent(part, 100)
			if err != nil {
				return HwanKnobs{}, fmt.Errorf("%s: %w", EnvHwanNiyaCaptureRates, err)
			}
			knobs.NiyaRates[i] = uint8(value)
			sum += value
		}
		if sum != 100 {
			return HwanKnobs{}, fmt.Errorf("%s must sum to 100 (got %d)", EnvHwanNiyaCaptureRates, sum)
		}
		knobs.NiyaRatesSet = true
	}
	if raw := strings.TrimSpace(getenv(EnvHwanSunRelic)); raw != "" {
		value, err := wholePercent(raw, 100)
		if err != nil {
			return HwanKnobs{}, fmt.Errorf("%s: %w", EnvHwanSunRelic, err)
		}
		knobs.SunRelicPercent, knobs.SunRelicSet = uint8(value), true
	}
	switch strings.ToLower(strings.TrimSpace(getenv(EnvHwan3FastDrop))) {
	case "", "off", "0", "false":
	case "on", "1", "true":
		knobs.FastDrop = true
	default:
		return HwanKnobs{}, fmt.Errorf("%s must be on or off", EnvHwan3FastDrop)
	}
	return knobs, nil
}

/*
================
wholePercent

A non-negative decimal integer of digits only, at most max. No sign, no
fraction, no unit, no hex or exponent form: nothing is rounded.
================
*/
func wholePercent(raw string, max int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("a whole percentage is required")
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("%q is not a whole percentage", raw)
		}
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value > max {
		return 0, fmt.Errorf("%q is not a whole percentage from 0 to %d", raw, max)
	}
	return value, nil
}

/*
================
NiyaCapture

The outcome of a trap closing on a Niya General for one raw roll. Unset, the
original rand() mod 101 thresholds; set, the roll is taken mod 100 against the
operator's split (nothing first, then flutter, then normal).
================
*/
func (k HwanKnobs) NiyaCapture(roll uint32) NiyaOutcome {
	if !k.NiyaRatesSet {
		switch r := roll % nativeRollSpan; {
		case r < nativeNiyaNormalBelow:
			return NiyaNormalSpirit
		case r < nativeNiyaFlutterBelow:
			return NiyaFlutterSpirit
		}
		return NiyaNothing
	}
	r := roll % knobRollSpan
	switch {
	case r < uint32(k.NiyaRates[2]):
		return NiyaNormalSpirit
	case r < uint32(k.NiyaRates[2])+uint32(k.NiyaRates[1]):
		return NiyaFlutterSpirit
	}
	return NiyaNothing
}

/*
================
SunRelicFound

Whether one dig yields the Relic of the Sun God. Unset, the original
rand() mod 101 <= 10; set, a roll over 100 outcomes below the percentage.
================
*/
func (k HwanKnobs) SunRelicFound(roll uint32) bool {
	if !k.SunRelicSet {
		return roll%nativeRollSpan <= nativeSunRelicAtMost
	}
	return roll%knobRollSpan < uint32(k.SunRelicPercent)
}

/*
================
OstraconDropCount

How many quest items one kill of an Alexandria ostracon monster leaves: one,
or FastDropCount with the knob on.
================
*/
func (k HwanKnobs) OstraconDropCount() uint16 {
	if k.FastDrop {
		return FastDropCount
	}
	return 1
}
