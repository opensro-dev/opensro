/*
===========================================================================

maxplus.go - the shard's enhancement cap (port-only, not native)

Natively an item can be reinforced while its plus is below 250 (Reinforce
refuses 0x0F at 250, the server's own ceiling). The shard's success table
(rates.go) ends at +11, and the owner approved a cap at +11 as a server
setting (2026-10-13) so a lucky streak never carries an item past the
balanced range.

SRO_ALCHEMY_MAX_PLUS is "on" (+11), a number 1..249, or unset/"off"
(native). An item at or above the cap is refused with the same 0x0F the
native ceiling uses, so the client shows the message it already has for
an item that cannot be reinforced further; nothing is consumed.

===========================================================================
*/

package alchemy

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// EnvReinforceMaxPlus names the setting: "on", a plus value, or "off".
const EnvReinforceMaxPlus = "SRO_ALCHEMY_MAX_PLUS"

// requestedReinforceMaxPlus is the cap "on" selects (owner decision
// 2026-10-13): the last plus the shard's success table prices.
const requestedReinforceMaxPlus = 11

// nativeReinforceCeiling is the plus at which Reinforce refuses natively.
const nativeReinforceCeiling = 250

/*
================
ParseReinforceMaxPlus

"" and "off" are native (0, no cap below the native ceiling); "on" is +11;
anything else must be a plus in 1..nativeReinforceCeiling-1. A malformed
value is an error so a typo never ships the wrong cap silently.
================
*/
func ParseReinforceMaxPlus(text string) (uint8, error) {
	text = strings.TrimSpace(text)
	switch strings.ToLower(text) {
	case "", "off", "0", "false":
		return 0, nil
	case "on", "true":
		return requestedReinforceMaxPlus, nil
	}
	n, err := strconv.Atoi(text)
	if err != nil || n < 1 || n >= nativeReinforceCeiling {
		return 0, fmt.Errorf("%s: %q is not a plus between 1 and %d", EnvReinforceMaxPlus, text, nativeReinforceCeiling-1)
	}
	return uint8(n), nil
}

/*
================
ReinforceMaxPlusFromEnv
================
*/
func ReinforceMaxPlusFromEnv() (uint8, error) {
	return ParseReinforceMaxPlus(os.Getenv(EnvReinforceMaxPlus))
}
