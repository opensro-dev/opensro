/*
===========================================================================

onboarding.go - the switch for the first-login tour of the port's additions

The browser client walks a new player through the controls this port adds
to the original interface (onboarding.ts). It is on unless the operator
sets SRO_ONBOARDING to "off", "0" or "false"; the Agent publishes the
choice at GET /title/onboarding, read once per page.

===========================================================================
*/
package onboarding

import "strings"

// Env names the deployer's switch; any value other than off/0/false, or
// none, leaves the tour on.
const Env = "SRO_ONBOARDING"

/*
================
FromEnv
================
*/
func FromEnv(getenv func(string) string) bool {
	switch strings.ToLower(strings.TrimSpace(getenv(Env))) {
	case "off", "0", "false":
		return false
	}
	return true
}
