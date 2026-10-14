/*
===========================================================================

masteries.go - the party roster's mastery pair, switched by the operator

The quick party board shows each member's two main mastery trees. The wire
carries them in the member row's 0x08 pair (+0x50/+0x54 of 75DB30). The pair
is port-only, not native: off (unset, the GameWorld's default) every roster
row is the plain full-info subset; the deployment default (shardsettings)
turns it on.

===========================================================================
*/
package party

import (
	"os"
	"strings"
)

// EnvPartyMasteries turns the mastery pair on ("on", "1", "true"). Any
// other value, or none, is native: no pair.
const EnvPartyMasteries = "SRO_PARTY_MASTERIES"

/*
================
MasteriesFromEnv
================
*/
func MasteriesFromEnv() bool {
	return masteriesEnabled(os.Getenv(EnvPartyMasteries))
}

/*
================
masteriesEnabled
================
*/
func masteriesEnabled(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
UseMasteries

Chooses whether member rows carry the primary/secondary mastery pair.
================
*/
func (r *Runtime) UseMasteries(enabled bool) {
	r.masteries = enabled
}
