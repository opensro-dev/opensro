/*
===========================================================================

gate.go - the operator switch for the bot functions

Phase 0 of issue #74. The switch is off unless a deployment sets
SRO_BOT_PLANE to "1". No bot function starts while the switch is off.

The switch replaces the development loopback gate of issue #69. The operator
decided on 2026-10-09 that the bots run on the production shard Ares,
together with the players. The operator therefore controls the bots.

===========================================================================
*/
package bot

import (
	"errors"
	"os"
	"strings"
)

// EnvBotPlane is the operator switch for every bot function. A deployment
// enables the bots with SRO_BOT_PLANE=1. The default is off.
const EnvBotPlane = "SRO_BOT_PLANE"

// ErrDisabled is returned by every bot entry point while the operator switch
// is off.
var ErrDisabled = errors.New("bot: " + EnvBotPlane + " is off")

// Enabled reports whether the operator switch is on.
func Enabled() bool {
	return strings.TrimSpace(os.Getenv(EnvBotPlane)) == "1"
}

// RequireEnabled returns ErrDisabled unless the operator switch is on. Every
// bot entry point calls it before it touches the game.
func RequireEnabled() error {
	if !Enabled() {
		return ErrDisabled
	}
	return nil
}
