/*
===========================================================================

instantdelete.go - the switch that skips the deletion wait

Port-only, not native (owner, F10). Natively a character deleted at the
select screen waits seven days, restorable all the while, before the
reaper archives it (data/store/character_deletion.go). With
SRO_INSTANT_CHAR_DELETE on, the delete action archives it at once through
the same fail-closed transaction, so the name and the slot free up
immediately and there is no restore. Off, the default, is native.

===========================================================================
*/
package agentapi

import (
	"os"
	"strings"
)

// EnvInstantCharacterDelete turns the switch on ("on", "1", "true").
const EnvInstantCharacterDelete = "SRO_INSTANT_CHAR_DELETE"

/*
================
InstantCharacterDeleteFromEnv
================
*/
func InstantCharacterDeleteFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvInstantCharacterDelete))) {
	case "on", "1", "true":
		return true
	}
	return false
}
