/*
===========================================================================

storageautostack.go - the deployment's storage auto-stack setting

Port-only, not native. SRO_STORAGE_AUTO_STACK reaches the GameWorld job as
storage_auto_stack; the GameWorld parses it and its own unset value is
native (action/storageautostack.go). The deployment default is on, as
the owner decided on 2026-10-11.

===========================================================================
*/
package main

import (
	"os"
	"strings"
)

// storageAutoStackDefault is the owner-approved deployment default.
const storageAutoStackDefault = "on"

/*
================
storageAutoStackSetting

The deployer's value as written, or the default when unset.
================
*/
func storageAutoStackSetting() string {
	if value := strings.TrimSpace(os.Getenv("SRO_STORAGE_AUTO_STACK")); value != "" {
		return value
	}
	return storageAutoStackDefault
}
