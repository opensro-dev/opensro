/*
===========================================================================

moonlitthiefsuit.go - the Moonlit Ash look for the level-20 thief suits

Port-only, not native (owner decision 2026-10-15, the look the balance
advisor picked on 2026-10-14). With SRO_MOONLIT_THIEF_SUIT on, the
GameWorld publishes the rule at world entry and the client draws the
level-20 thief suits the thief NPC sells as the higher suit's retail mesh
with the Moonlit Ash recolour, built from the retail texture by the asset
build (scripts/build/char/moonlitThiefSuit.mjs). The server keeps no
appearance; the rule is presentation only. Off, the default here, is
native.

===========================================================================
*/
package action

import (
	"os"
	"strings"
)

// EnvMoonlitThiefSuit turns the look on ("on", "1", "true").
const EnvMoonlitThiefSuit = "SRO_MOONLIT_THIEF_SUIT"

/*
================
MoonlitThiefSuitFromEnv
================
*/
func MoonlitThiefSuitFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvMoonlitThiefSuit))) {
	case "on", "1", "true":
		return true
	}
	return false
}
