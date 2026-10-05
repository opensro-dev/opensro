/*
===========================================================================

masteries.go - the party roster's mastery pair, switched by the operator

The quick party board shows each member's two main mastery trees. The wire
carries them in the member row's 0x08 pair (+0x50/+0x54 of 75DB30). The pair
is a port feature, so the operator can turn it off and every roster row goes
back to the plain full-info subset.

===========================================================================
*/
package party

import (
	"os"
	"strings"
)

// EnvPartyMasteries turns the mastery pair off ("off", "0", "false"). Any
// other value, or none, leaves it on.
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
	case "off", "0", "false":
		return false
	}
	return true
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
