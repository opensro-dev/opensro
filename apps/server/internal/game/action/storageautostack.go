/*
===========================================================================

storageautostack.go - the storage quick move's auto-stack rule

Port-only, not native (owner decision 2026-10-11). The original's
right-click deposit takes the first empty slot of the open page (567290
-> 5B0BA0) and never looks for a stack. With SRO_STORAGE_AUTO_STACK on,
the server publishes the rule at world entry (enterworld Deps
StorageAutoStack) and the client's quick move aims a deposit or withdrawal
at a matching stack with room instead. That aimed move is the native drag
onto a stack (TransferWholeTo, 756A60), so the server enforces nothing new.
Off, the default here, is native; the GameWorld's Nomad job turns it on.

===========================================================================
*/
package action

import (
	"os"
	"strings"
)

// EnvStorageAutoStack turns the auto-stack rule on ("on", "1", "true").
const EnvStorageAutoStack = "SRO_STORAGE_AUTO_STACK"

/*
================
StorageAutoStackFromEnv
================
*/
func StorageAutoStackFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvStorageAutoStack))) {
	case "on", "1", "true":
		return true
	}
	return false
}
