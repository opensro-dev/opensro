/*
===========================================================================

sunrate.go - the beta's degree-9 Sun drop rate (port-only, not native)

Port-only, not native, beta balance (owner 2026-10-20). SRO_SUN_DROP_RATE
names an integer R; with R >= 1 the degree-9 Seal of Sun class of the rare
equipment table (the *_09_C_RARE class) is SunMassPerRate times R as wide
on every rare roll of a level 76-90 monster, so a Sun pick is that much
more likely while every other class keeps its exact native width. Unset,
zero or unreadable is off: no scaled rows are built and every roll draws
exactly the native table.

The widening is capped (owner decision #1605 P1, 2026-10-25): the Sun
class never grows wider than the degree-9 Moon class in the same row
(loot/sunscale.go), so a Sun expects about twice a Moon's kills at every
level. The rate names the nominal widening; the cap is what binds at the
shipped rates. The coupling is deliberate: widen the Moon class later
and the Sun class's ceiling moves with it.

The rate composes with the rest of the loot rules, it does not replace
them: the rare window (25/1000), the seal-band row and the keep-seal
multipliers all run as before (sealrates.go, item/loot), and the
SRO_SUN_EVENT_ONLY exclusion, when an operator still turns it on, still
wins over the rate - a Sun pick then drops nothing and this widening is
silent. Sun gear never returns to NPC shops either way: that withholding
is permanent (action/commerce.go), not part of this rate.

===========================================================================
*/
package progression

import (
	"os"
	"strconv"
	"strings"
)

// EnvSunDropRate is the integer degree-9 Sun class rate (unset/0: native).
const EnvSunDropRate = "SRO_SUN_DROP_RATE"

// SunMassPerRate is the Sun class mass each unit of the rate buys.
const SunMassPerRate = 30

// MaxSunDropRate caps the rate. Past it the Sun class would swallow the
// whole rare table and a typo'd rate (GH #1269) only buys certainty; the
// cap keeps a mis-typed extra zero from silently becoming that.
const MaxSunDropRate = 1_000_000

/*
================
SunRate

The parsed rate. The zero value is off: native drops.
================
*/
type SunRate struct {
	rate int
}

/*
================
SunRateFromEnv
================
*/
func SunRateFromEnv() SunRate {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(EnvSunDropRate)))
	if err != nil || value < 1 {
		return SunRate{}
	}
	if value > MaxSunDropRate {
		value = MaxSunDropRate
	}
	return SunRate{rate: value}
}

/*
================
SunRate.Active

Whether the rate widens the Sun class at all.
================
*/
func (s SunRate) Active() bool { return s.rate >= 1 }

/*
================
SunRate.Rate

The rate itself, 0 when off.
================
*/
func (s SunRate) Rate() int { return s.rate }

/*
================
SunRate.MassMultiplier

The nominal widening the rate buys, before the Moon class cap
(loot/sunscale.go) binds; 1 when off.
================
*/
func (s SunRate) MassMultiplier() int {
	if !s.Active() {
		return 1
	}
	return SunMassPerRate * s.rate
}
