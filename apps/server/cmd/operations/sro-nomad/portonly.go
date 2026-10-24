/*
===========================================================================

portonly.go - the GameWorld job's shard settings

Each setting the deployment chooses reaches the GameWorld job as one Nomad
variable. The table that names them and holds the owner-approved
deployment defaults is shardsettings.Settings (internal/config/
shardsettings): the operator's value wins, otherwise the table's default.
The deployment validates the captured beta settings before Nomad mutations.
The GameWorld keeps its existing parsers, including their legacy fallbacks.

Settings the deployment computes itself (the beta mastery policy file,
the parsed stack sizes) are filled in
deployment.go; the table still holds their defaults for the job file and
the catalogue build.

===========================================================================
*/
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"opensro.online/server/internal/config/shardsettings"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/progression"
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
validate

Checks the captured deployment values without reading the environment again.
The table chooses validation; gameplay owners keep their numeric limits and
compound syntax. Built settings retain their separate deployment policy.
================
*/
func (settings portOnlySettings) validate() error {
	var failures []error
	for _, setting := range shardsettings.Settings {
		if setting.Source != shardsettings.FromTable || setting.Validation == shardsettings.ValueUnchecked {
			continue
		}
		raw, ok := settings[setting.NomadVar].(string)
		if !ok {
			failures = append(failures, fmt.Errorf("%s: resolved Nomad variable %s must be a string",
				setting.Env, setting.NomadVar))
			continue
		}
		var err error
		switch setting.Validation {
		case shardsettings.ValueSwitch:
			switch strings.ToLower(strings.TrimSpace(raw)) {
			case "on", "off", "1", "0", "true", "false":
			default:
				err = fmt.Errorf("must be on, off, 1, 0, true or false")
			}
		case shardsettings.ValueBetaGrowth:
			_, err = progression.ParseBetaGrowthSetting(setting.Env, raw)
		case shardsettings.ValueBetaDropMix:
			_, err = progression.ParseDropMix(raw)
		case shardsettings.ValueBetaSilk:
			_, err = action.ParseBetaSilk(raw)
		default:
			err = fmt.Errorf("unknown validation kind %d", setting.Validation)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("%s=%q: %w", setting.Env, raw, err))
		}
	}
	return errors.Join(failures...)
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
