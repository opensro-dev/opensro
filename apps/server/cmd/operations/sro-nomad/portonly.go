/*
===========================================================================

portonly.go - the GameWorld job's port-only switches

Each switch here turns on behaviour the original game does not have. The
deployer's value reaches the GameWorld job as written and the GameWorld
parses it; the GameWorld's own unset value is always native.

  - SRO_STORAGE_AUTO_STACK (storage_auto_stack): storage quick moves land
    on a matching stack (action/storageautostack.go). The deployment
    default is on, as the owner decided on 2026-10-11.
  - SRO_REVERSE_RETURN_MAP (reverse_return_map): the reverse return
    scroll's map destinations (action/reversemap.go). Off by default.

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
portOnlySettings
================
*/
type portOnlySettings struct {
	StorageAutoStack string
	ReverseReturnMap string
}

/*
================
portOnlySettingsFromEnv

The deployer's values as written; storage auto-stack takes its default
when unset.
================
*/
func portOnlySettingsFromEnv() portOnlySettings {
	settings := portOnlySettings{
		StorageAutoStack: strings.TrimSpace(os.Getenv("SRO_STORAGE_AUTO_STACK")),
		ReverseReturnMap: strings.TrimSpace(os.Getenv("SRO_REVERSE_RETURN_MAP")),
	}
	if settings.StorageAutoStack == "" {
		settings.StorageAutoStack = storageAutoStackDefault
	}
	return settings
}

/*
================
jobVariables

Adds the switches to the GameWorld job's variables.
================
*/
func (settings portOnlySettings) jobVariables(variables map[string]any) map[string]any {
	variables["storage_auto_stack"] = settings.StorageAutoStack
	variables["reverse_return_map"] = settings.ReverseReturnMap
	return variables
}
