/*
===========================================================================

quickfuse.go - Fuse sends at once and never waits out the effects

Port-only, not native (owner decision 2026-11-21, after a player asked why
fusing had become slower). Natively the alchemy window's Fuse press
(v1.150 client 62AFA0) sets the window busy (+0x378), plays the
preparation effect on a 50 ms timer (0xB) and sends the request only once
that effect has played (v1.150 client: 62C350 calls 62B5E0,
about 0.8 s); Fuse stays ignored until the result effect ends (timer 0xA,
625260). With
SRO_QUICK_FUSE on, the GameWorld publishes the rule at world entry and the
client sends on the press and takes the next press as soon as the answer
arrives; the effects still play. The server's reinforcement is unchanged:
it never saw the client's wait. Off, the default here, is native.

===========================================================================
*/
package action

import (
	"os"
	"strings"
)

// EnvQuickFuse turns the rule on ("on", "1", "true").
const EnvQuickFuse = "SRO_QUICK_FUSE"

/*
================
QuickFuseFromEnv
================
*/
func QuickFuseFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvQuickFuse))) {
	case "on", "1", "true":
		return true
	}
	return false
}
