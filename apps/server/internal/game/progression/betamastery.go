/*
===========================================================================

betamastery.go - reversible beta total-mastery allowance

Only the total budget changes. Individual mastery levels, character levels,
SP costs and prerequisites remain native. Existing training is never erased
when the operator returns to native limits.

===========================================================================
*/
package progression

import (
	"fmt"
	"os"
	"strings"

	"opensro.online/server/internal/game/enterworld"
)

const (
	EnvBetaMastery            = "SRO_BETA_MASTERY"
	BetaTotalMasteryCap int64 = 5000
)

/*
================
BetaMasteryFromEnv

Composition reads this once and shares the value with bootstrap publication.
An invalid switch refuses startup instead of silently changing training rules.
================
*/
func BetaMasteryFromEnv() (int64, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvBetaMastery))) {
	case "", "off", "0", "false":
		return 0, nil
	case "on", "1", "true":
		return BetaTotalMasteryCap, nil
	default:
		return 0, fmt.Errorf("%s must be on or off", EnvBetaMastery)
	}
}

/*
================
masteryAllowance

Zero preserves the original race-specific rule. The override is immutable
runtime configuration, never a character field or a client-supplied value.
================
*/
func (rt *Runtime) masteryAllowance(character *enterworld.Character) int64 {
	if rt.MasteryTotalOverride > 0 {
		return rt.MasteryTotalOverride
	}
	return totalMasteryCap(character)
}
