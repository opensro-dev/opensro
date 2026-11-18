/*
===========================================================================

betamastery_early.go - the beta mastery allowance kept for early beta characters

Port-only, not native (owner, 2026-11-18, #3551). Until the 11-14 release
every character trained under the closed beta's flat 5,000 total
mastery-level budget (SRO_BETA_MASTERY). That release made the native
totals the default (#3403: Chinese 300, European min(2 x level, 240)), and
416 of 1,218 live characters were over their native total, most of them
fully trained (7 x 90 Chinese, 6 x 90 European). Every mastery raise checks
the total, so none of them could train again, and lowering 300-330 levels
one restoration potion at a time is no way out.

With SRO_BETA_MASTERY_EARLY on, a character created before the native
totals went live keeps the beta budget; every character created since
trains under the native totals. Off is native for everyone.
SRO_BETA_MASTERY (the flat budget for all) still wins when it is on.

This file owns the rule. progression enforces it on every raise and the
world-entry bootstrap publishes the same value to the client's skill
window, so the two cannot disagree.

===========================================================================
*/
package enterworld

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	EnvBetaMasteryEarly = "SRO_BETA_MASTERY_EARLY"
	// EarlyBetaMasteryCap is the closed beta's flat total
	// (progression.BetaTotalMasteryCap, which cannot be imported here).
	EarlyBetaMasteryCap int64 = 5000
)

// EarlyBetaCutoff is when the native totals went live: #3403 merged
// 2026-11-13 13:20Z and shipped with the release installed on 11-14 at
// 13:57Z. On the live store the newest character over its native total
// was created at 12:43:26Z that day, and none after (read 2026-11-18).
var EarlyBetaCutoff = time.Date(2026, time.November, 14, 13, 57, 0, 0, time.UTC)

/*
================
BetaMasteryEarlyFromEnv

Read once at composition. An invalid value refuses startup instead of
silently choosing a training rule.
================
*/
func BetaMasteryEarlyFromEnv() (bool, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvBetaMasteryEarly))) {
	case "", "off", "0", "false":
		return false, nil
	case "on", "1", "true":
		return true, nil
	default:
		return false, fmt.Errorf("%s must be on or off", EnvBetaMasteryEarly)
	}
}

/*
================
EarlyBetaCharacter

True for a character created before EarlyBetaCutoff. The creation time is
the server's own (agent api: createdAt at creation, RFC 3339 UTC); a
character without a readable one is not early: the rule never widens on a
guess.
================
*/
func EarlyBetaCharacter(character *Character) bool {
	if character == nil {
		return false
	}
	created, err := time.Parse(time.RFC3339Nano, character.CreatedAt)
	if err != nil {
		return false
	}
	return created.Before(EarlyBetaCutoff)
}

/*
================
MasteryAllowanceOverride

The total mastery budget that replaces the native race rule for this
character, or zero for native: the shard-wide beta budget when it is on,
else the beta budget for an early character when SRO_BETA_MASTERY_EARLY is
on.
================
*/
func MasteryAllowanceOverride(shardWide int64, early bool, character *Character) int64 {
	if shardWide > 0 {
		return shardWide
	}
	if early && EarlyBetaCharacter(character) {
		return EarlyBetaMasteryCap
	}
	return 0
}
