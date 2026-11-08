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

	"opensro.online/server/internal/catalogue"
	"opensro.online/server/internal/config/shardsettings"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/loot"
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

// The eight tomb uniques the TombChain spawns at chain-owned nests
// (Floor 5's guardians and Flame Master, Floor 6's anchors): the
// catalogue lost them when the chain pinned their nests to zero
// (#2856). 14838 is Flame Master Sinmu, the /uniques board's face.
var qinShiTombChainUniques = []uint32{14824, 14833, 14834, 14835, 14836, 14837, 14838, 14839}

/*
================
TestCatalogueMonstersKeepTheChainUniques

The published monster rows are built from the shard population at its
deployment defaults (the tomb on): a monster the server's spawn data
still puts into the world - a chain-owned nest included - keeps its
catalogue row. The count pins the published set the 2026-11-01
release shipped (#2856 done-when 1).
================
*/
func TestCatalogueMonstersKeepTheChainUniques(t *testing.T) {
	tier.Long(t, "the catalogue build keeps the TombChain uniques")
	dir := gamedatatest.TextdataDir(t)
	for _, s := range shardsettings.Settings {
		if s.Catalogue {
			t.Setenv(s.Env, "")
		}
	}
	if err := resolveShardSettings(); err != nil {
		t.Fatal(err)
	}
	if err := loot.InitCap110(dir, monster.Cap110Enabled()); err != nil {
		t.Fatal(err)
	}
	template, err := shardPopulation(dir, monster.Cap110Enabled())
	if err != nil {
		t.Fatal(err)
	}
	itemSource, err := shardItems(dir)
	if err != nil {
		t.Fatal(err)
	}
	itemID := func(codename string) uint32 {
		if ref, ok := itemSource.ItemRefByCodename(codename); ok {
			return ref.RefObjID
		}
		return 0
	}
	monsters, err := catalogue.BuildMonsters(catalogue.MonsterSource{Template: template, ItemID: itemID,
		Skills: enterworld.NewTextdataSkills(dir)})
	if err != nil {
		t.Fatal(err)
	}
	ids := make(map[uint32]bool, len(monsters))
	for _, m := range monsters {
		ids[m.ID] = true
	}
	for _, id := range qinShiTombChainUniques {
		if !ids[id] {
			t.Fatalf("chain unique %d lost its catalogue row; the shard still spawns it", id)
		}
	}
	if len(monsters) != 230 {
		t.Fatalf("catalogue monsters = %d, want 230 (the 2026-11-01 published set)", len(monsters))
	}
}
