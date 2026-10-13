/*
===========================================================================

portonly.go - the GameWorld job's shard settings

Each setting the deployment chooses reaches the GameWorld job as one Nomad
variable. The table that names them and holds the owner-approved
deployment defaults is shardsettings.Settings (internal/config/
shardsettings): the operator's value wins, otherwise the table's default.
The GameWorld parses what arrives; its own unset value is always native.

Settings the deployment computes itself (the beta mastery policy file,
the party masteries switch, the parsed stack sizes) are filled in
deployment.go; the table still holds their defaults for the job file and
the catalogue build.

===========================================================================
*/
package main

import (
	"os"

	"opensro.online/server/internal/config/shardsettings"
)

/*
================
portOnlySettings

The table-filled settings' values, keyed by Nomad variable.
================
*/
type portOnlySettings map[string]any

/*
================
portOnlySettingsFromEnv

The deployer's values as written, each unset one at its deployment
default.
================
*/
func portOnlySettingsFromEnv() portOnlySettings {
	return portOnlySettings(shardsettings.JobVariables(os.Getenv, map[string]any{}))
}

/*
================
jobVariables

Adds the settings to the GameWorld job's variables.
================
*/
func (settings portOnlySettings) jobVariables(variables map[string]any) map[string]any {
	for name, value := range settings {
		variables[name] = value
	}
	return variables
}
