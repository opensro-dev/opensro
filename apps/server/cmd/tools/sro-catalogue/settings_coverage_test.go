/*
===========================================================================

settings_coverage_test.go - every shard setting is modelled or reasoned

===========================================================================
*/

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"opensro.online/server/internal/config/shardsettings"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
TestEverySettingIsModelledOrReasoned

The coverage table is the contract (#2542): each registry setting is
modelled with a probe, or unmodelled with a reason - never neither,
never both, never a name the registry does not hold. Which list a
setting belongs on is decided by whether a catalogue build reads it;
the probe test below proves the modelled half.
================
*/
func TestEverySettingIsModelledOrReasoned(t *testing.T) {
	registry := map[string]bool{}
	for _, s := range shardsettings.Settings {
		registry[s.Env] = true
	}
	modelled := map[string]modelledSetting{}
	for _, m := range modelledSettings {
		if !registry[m.env] {
			t.Errorf("modelled %s is not a registry setting", m.env)
		}
		if _, dup := modelled[m.env]; dup {
			t.Errorf("modelled %s is listed twice", m.env)
		}
		modelled[m.env] = m
		if _, both := unmodelledSettings[m.env]; both {
			t.Errorf("%s is modelled and unmodelled", m.env)
		}
		if m.variantOnly == "" && m.probe == "" {
			t.Errorf("modelled %s has neither a probe nor a variant reason", m.env)
		}
		if m.variantOnly != "" && m.probe != "" {
			t.Errorf("modelled %s has a probe and a variant reason", m.env)
		}
		for companion := range m.companions {
			if !registry[companion] {
				t.Errorf("modelled %s names companion %s, not a registry setting", m.env, companion)
			}
			if companion == m.env {
				t.Errorf("modelled %s is its own companion", m.env)
			}
		}
	}
	for env, reason := range unmodelledSettings {
		if !registry[env] {
			t.Errorf("unmodelled %s is not a registry setting", env)
		}
		if reason == "" {
			t.Errorf("unmodelled %s has no reason", env)
		}
	}
	for _, s := range shardsettings.Settings {
		_, isModelled := modelled[s.Env]
		_, isUnmodelled := unmodelledSettings[s.Env]
		if !isModelled && !isUnmodelled {
			t.Errorf("%s is in neither list: model it with a probe or record why the catalogue does not", s.Env)
		}
		if ModelledSetting(s) != isModelled {
			t.Errorf("ModelledSetting disagrees with the table for %s", s.Env)
		}
	}
}

/*
================
setCatalogueEnv

Points every registry setting at the overlay (absent means blank)
before a probe build. The caller scoped the test with t.Setenv, so
the pre-test environment comes back when the test ends.
================
*/
func setCatalogueEnv(overlay map[string]string) {
	for _, s := range shardsettings.Settings {
		if value, ok := overlay[s.Env]; ok {
			os.Setenv(s.Env, value)
		} else {
			os.Setenv(s.Env, "")
		}
	}
}

/*
================
buildCatalogue

One catalogue build with the overlay as its whole settings
environment, read back as normalized JSON: the generated-at stamp is
deleted wherever it appears, so two builds compare on content.
================
*/
func buildCatalogue(t *testing.T, dir string, overlay map[string]string) (map[string]any, error) {
	t.Helper()
	setCatalogueEnv(overlay)
	out := t.TempDir()
	if err := run(out, "coverage", dir); err != nil {
		return nil, err
	}
	files := map[string]any{}
	err := filepath.WalkDir(out, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".json") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		rel, err := filepath.Rel(out, path)
		if err != nil {
			return err
		}
		files[rel] = dropGeneratedAt(value)
		return nil
	})
	if err != nil {
		t.Fatalf("read build: %v", err)
	}
	return files, nil
}

/*
================
dropGeneratedAt
================
*/
func dropGeneratedAt(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, inner := range v {
			if key == "generatedAt" {
				continue
			}
			out[key] = dropGeneratedAt(inner)
		}
		return out
	case []any:
		for i, inner := range v {
			v[i] = dropGeneratedAt(inner)
		}
		return v
	default:
		return value
	}
}

/*
================
TestModelledSettingsChangeTheCatalogue

For each modelled setting, a build with its probe value must differ
from a build without it (companions held constant on both, so the
difference is the probed setting's alone) - or refuse outright, for
the population families the shipped data cannot build. This is what
keeps the modelled list honest: a reader removed from the build, or a
setting renamed at its consumer, fails here instead of drifting.
================
*/
func TestModelledSettingsChangeTheCatalogue(t *testing.T) {
	tier.Long(t, "every modelled setting changes a catalogue build at its probe value")
	dir := gamedatatest.TextdataDir(t)
	for _, s := range shardsettings.Settings {
		t.Setenv(s.Env, "")
	}
	baseline, err := buildCatalogue(t, dir, nil)
	if err != nil {
		t.Fatalf("baseline build: %v", err)
	}
	again, err := buildCatalogue(t, dir, nil)
	if err != nil {
		t.Fatalf("baseline rebuild: %v", err)
	}
	if !reflect.DeepEqual(baseline, again) {
		t.Fatal("two builds with identical settings differ; probe comparisons would mean nothing")
	}
	companionBuilds := map[string]map[string]any{}
	for _, m := range modelledSettings {
		t.Run(m.env, func(t *testing.T) {
			if m.variantOnly != "" {
				t.Skip(m.variantOnly)
			}
			overlay := map[string]string{}
			for name, value := range m.companions {
				overlay[name] = value
			}
			overlay[m.env] = m.probe
			got, err := buildCatalogue(t, dir, overlay)
			if m.refuses {
				if err == nil {
					t.Fatalf("build with %s=%q succeeded, want refusal", m.env, m.probe)
				}
				return
			}
			if err != nil {
				t.Fatalf("build with %s=%q: %v", m.env, m.probe, err)
			}
			against := baseline
			if len(m.companions) > 0 {
				key := overlayKey(m.companions)
				shared, ok := companionBuilds[key]
				if !ok {
					shared, err = buildCatalogue(t, dir, m.companions)
					if err != nil {
						t.Fatalf("companion build for %s: %v", m.env, err)
					}
					companionBuilds[key] = shared
				}
				against = shared
			}
			if reflect.DeepEqual(against, got) {
				t.Fatalf("build with %s=%q equals the build without it; the catalogue no longer reads it, or the probe value changes nothing", m.env, m.probe)
			}
		})
	}
}

/*
================
overlayKey
================
*/
func overlayKey(overlay map[string]string) string {
	parts := make([]string, 0, len(overlay))
	for _, s := range shardsettings.Settings {
		if value, ok := overlay[s.Env]; ok {
			parts = append(parts, s.Env+"="+value)
		}
	}
	return strings.Join(parts, "\x00")
}
