/*
===========================================================================

shardsettings_test.go - the table, the job file and the resolution rules

===========================================================================
*/
package shardsettings

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// gameWorldJob is the GameWorld job file, relative to this package.
var gameWorldJob = filepath.Join("..", "..", "..", "ops", "nomad", "jobs", "gameworld.nomad.hcl")

/*
================
TestTableMatchesTheJobFile

Every table row has a job variable whose default is the row's, and the
job passes it to the GameWorld as the row's environment variable: the job
file, sro-nomad and the catalogue build cannot drift apart.
================
*/
func TestTableMatchesTheJobFile(t *testing.T) {
	data, err := os.ReadFile(gameWorldJob)
	if err != nil {
		t.Fatal(err)
	}
	job := string(data)
	seenEnv := map[string]bool{}
	for _, s := range Settings {
		if seenEnv[s.Env] {
			t.Fatalf("%s is in the table twice", s.Env)
		}
		seenEnv[s.Env] = true
		block := regexp.MustCompile(`(?s)variable "` + regexp.QuoteMeta(s.NomadVar) + `" \{(.*?)\n\}`).FindStringSubmatch(job)
		if block == nil {
			t.Fatalf("the job file has no variable %q", s.NomadVar)
		}
		def := regexp.MustCompile(`default\s*=\s*"([^"]*)"`).FindStringSubmatch(block[1])
		if def == nil || def[1] != s.Default {
			t.Fatalf("variable %q default %v, table %q", s.NomadVar, def, s.Default)
		}
		if !regexp.MustCompile(`\n\s*` + regexp.QuoteMeta(s.Env) + `\s*=\s*var\.` + regexp.QuoteMeta(s.NomadVar) + `\s*\n`).MatchString(job) {
			t.Fatalf("the job does not pass var.%s as %s", s.NomadVar, s.Env)
		}
	}
}

/*
================
TestEveryJobSettingIsListed

The reverse direction: every SRO_ variable the GameWorld task sets from a
job variable is a table row or a named NotSettings entry, and every
NotSettings entry is still in the job. A new job setting cannot slip past
the table.
================
*/
func TestEveryJobSettingIsListed(t *testing.T) {
	data, err := os.ReadFile(gameWorldJob)
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, s := range Settings {
		listed[s.Env] = true
	}
	inJob := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s*(SRO_\w+)\s*=\s*[^\n]*\bvar\.`).FindAllStringSubmatch(string(data), -1) {
		inJob[m[1]] = true
		if !listed[m[1]] && NotSettings[m[1]] == "" {
			t.Fatalf("the job sets %s from a variable, but it is neither a shard setting nor in NotSettings", m[1])
		}
	}
	for env := range NotSettings {
		if !inJob[env] {
			t.Fatalf("NotSettings lists %s, which the job no longer sets", env)
		}
		if listed[env] {
			t.Fatalf("%s is both a setting and NotSettings", env)
		}
	}
}

/*
================
TestValuesPreferTheOperator

An operator's value wins, "off" included; unset or blank is the default.
================
*/
func TestValuesPreferTheOperator(t *testing.T) {
	s := Setting{Env: "X", NomadVar: "x", Default: "on"}
	for env, want := range map[string]string{"": "on", "  ": "on", "off": "off", "7": "7"} {
		if got := s.Value(func(string) string { return env }); got != want {
			t.Fatalf("env %q: %q, want %q", env, got, want)
		}
	}
	vars := JobVariables(func(string) string { return "" }, map[string]any{})
	for _, s := range Settings {
		_, present := vars[s.NomadVar]
		if present != (s.Source == FromTable) {
			t.Fatalf("%s present=%v, source %v", s.NomadVar, present, s.Source)
		}
	}
}

/*
================
TestCatalogueDefaultsFillOnlyUnsetCatalogueSettings
================
*/
func TestCatalogueDefaultsFillOnlyUnsetCatalogueSettings(t *testing.T) {
	env := map[string]string{"SRO_ALCHEMY_RATES": "off"}
	filled, err := ApplyCatalogueDefaults(func(k string) string { return env[k] }, func(k, v string) error {
		env[k] = v
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if env["SRO_ALCHEMY_RATES"] != "off" {
		t.Fatal("an explicit off was replaced")
	}
	for _, s := range Settings {
		_, set := env[s.Env]
		wantSet := s.Catalogue && s.Default != "" || s.Env == "SRO_ALCHEMY_RATES"
		if set != wantSet {
			t.Fatalf("%s set=%v, catalogue=%v", s.Env, set, s.Catalogue)
		}
	}
	if strings.Contains(strings.Join(filled, ","), "SRO_ALCHEMY_RATES") {
		t.Fatal("filled reports an operator value")
	}
}
