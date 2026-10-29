/*
===========================================================================

population_test.go - the published monsters are the shard's monsters

The site's monster pages come from shardPopulation, which must append
every family the GameWorld's bootstrap appends under the same switches
at their deployment defaults. The shipped Qin-Shi tomb was missing, so
its species (remapped into 14791..14851 and 22638..22655) had no page
(#2025).

===========================================================================
*/
package main

import (
	"testing"

	"opensro.online/server/internal/config/shardsettings"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

// qinShiTombSpecies is one tomb species from the reported 404
// (/monsters/14833): an ID in the tomb's remapped gap that retail
// v1.150 textdata does not define.
const qinShiTombSpecies = 14833

/*
================
TestCataloguePopulationCarriesTheShippedTomb

Unset, SRO_QINSHI_TOMB takes its deployment default (on) and the
catalogue's population holds the tomb species; an explicit "off" still
wins and leaves it out, as on a shard that turned the tomb off.
================
*/
func TestCataloguePopulationCarriesTheShippedTomb(t *testing.T) {
	tier.Long(t, "the catalogue population is the shard's population")
	dir := gamedatatest.TextdataDir(t)
	for _, c := range []struct {
		setting string
		want    bool
	}{{"", true}, {"off", false}} {
		// resolveShardSettings writes the process environment, so every
		// setting it may fill is scoped to the test first; otherwise the
		// deployment defaults leak into the package's later tests.
		for _, s := range shardsettings.Settings {
			if s.Catalogue {
				t.Setenv(s.Env, "")
			}
		}
		t.Setenv(monster.EnvQinShiTomb, c.setting)
		if err := resolveShardSettings(); err != nil {
			t.Fatal(err)
		}
		template, err := shardPopulation(dir, monster.Cap110Enabled())
		if err != nil {
			t.Fatalf("setting %q: %v", c.setting, err)
		}
		if _, got := template.Refs[qinShiTombSpecies]; got != c.want {
			t.Fatalf("setting %q: tomb species %d in the catalogue population = %v, want %v",
				c.setting, qinShiTombSpecies, got, c.want)
		}
	}
}
