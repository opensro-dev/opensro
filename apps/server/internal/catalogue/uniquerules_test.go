/*
===========================================================================

uniquerules_test.go - the unique-reward rules the drop tables publish

===========================================================================
*/
package catalogue

import (
	"encoding/json"
	"strings"
	"testing"

	"opensro.online/server/internal/config/shardsettings"
	"opensro.online/server/internal/game/item/gacha"
	"opensro.online/server/internal/game/progression"
)

/*
================
TestDropTablesPublishUniqueRules

Every unique rule the shard runs reaches the site: the export is built
from the same UniqueRulesFromEnv the GameWorld wires (wiring_gameplay),
so it cannot drift; flipping a flag in the catalogue build flips the
export, the ticket's item id rides with popTicket, and all four rules
off publishes no unique object at all (native). The GameWorld wires
the rules with the beta growth on or off, so they publish either way;
a beta-off build publishes no beta object but still carries the unique
rules. Every setting the shard table marks for the catalogue and names
a unique rule must be one of the exported keys, in both settings.
================
*/
func TestDropTablesPublishUniqueRules(t *testing.T) {
	monsters := []Monster{{Level: 90, Country: 3, Table: equipmentTableKey(90, 3)}}
	itemID := func(codename string) uint32 {
		if codename == gacha.CardCodename {
			return 4055
		}
		return uint32(len(codename))
	}
	build := func() DropTablesFile { return BuildDropTables(monsters, itemID, Header{}) }

	t.Setenv(progression.EnvBetaGrowth, "on")

	// Shard defaults: gap exempt and the pop ticket on, the opt-ins off.
	setUniqueEnv(t, "", "", "", "")
	unique := build().Unique
	if unique == nil || !unique.GapExempt || !unique.PopTicket || unique.PopTicketItem != 4055 ||
		unique.MaxLevelLoot || unique.SealDrops {
		t.Fatalf("defaults: unique %+v, want gapExempt and popTicket(4055) only", unique)
	}

	// Beta off: no beta object, and the unique rules still publish
	// (the GameWorld applies them with the beta growth off too).
	t.Setenv(progression.EnvBetaGrowth, "off")
	built := build()
	if built.Beta != nil {
		t.Fatalf("beta off: beta %+v, want no beta object", built.Beta)
	}
	if built.Unique == nil || !built.Unique.GapExempt || !built.Unique.PopTicket {
		t.Fatalf("beta off: unique %+v, want gapExempt and popTicket", built.Unique)
	}
	t.Setenv(progression.EnvBetaGrowth, "on")
	if unique == nil || !unique.GapExempt || !unique.PopTicket || unique.PopTicketItem != 4055 ||
		unique.MaxLevelLoot || unique.SealDrops {
		t.Fatalf("defaults: unique %+v, want gapExempt and popTicket(4055) only", unique)
	}

	// Every Catalogue: true unique rule appears under both settings.
	keys := map[string]struct {
		env     string
		enabled func(*UniqueRules) bool
	}{
		"gapExempt":    {progression.EnvUniqueGapExempt, func(u *UniqueRules) bool { return u.GapExempt }},
		"maxLevelLoot": {progression.EnvUniqueMaxLevelLoot, func(u *UniqueRules) bool { return u.MaxLevelLoot }},
		"popTicket":    {progression.EnvUniquePopTicket, func(u *UniqueRules) bool { return u.PopTicket }},
		"sealDrops":    {progression.EnvUniqueSealDrops, func(u *UniqueRules) bool { return u.SealDrops }},
	}
	exported := map[string]bool{}
	for _, setting := range shardsettings.Settings {
		if !setting.Catalogue || !strings.HasPrefix(setting.Env, "SRO_UNIQUE_") {
			continue
		}
		found := ""
		for key, rule := range keys {
			if rule.env == setting.Env {
				found = key
			}
		}
		if found == "" {
			t.Fatalf("setting %s is marked for the catalogue but has no unique export key", setting.Env)
		}
		exported[found] = true

		setUniqueEnv(t, "off", "off", "off", "off")
		t.Setenv(setting.Env, "on")
		if u := build().Unique; u == nil || !keys[found].enabled(u) {
			t.Fatalf("%s on: unique %+v, want %s set", setting.Env, u, found)
		}
		raw, err := json.Marshal(build().Unique)
		if err != nil || !strings.Contains(string(raw), `"`+found+`":true`) {
			t.Fatalf("%s on: unique JSON %s, want %q present", setting.Env, raw, found)
		}
		setUniqueEnv(t, "off", "off", "off", "off")
		if u := build().Unique; u != nil {
			t.Fatalf("%s back off: unique %+v, want no unique object (native)", setting.Env, u)
		}
	}
	for key := range keys {
		if !exported[key] {
			t.Fatalf("export key %s has no Catalogue: true setting behind it", key)
		}
	}

	// The ticket's id rides only with the ticket on.
	setUniqueEnv(t, "on", "off", "off", "off")
	if u := build().Unique; u == nil || u.PopTicket || u.PopTicketItem != 0 {
		t.Fatalf("gap only: unique %+v, want no ticket and no ticket item", u)
	}
}

/*
================
setUniqueEnv

Pins all four unique switches at once; an empty value unsets (the
rule's own default then applies, as in UniqueRulesFromEnv).
================
*/
func setUniqueEnv(t *testing.T, gap, pop, maxLevel, seals string) {
	t.Helper()
	t.Setenv(progression.EnvUniqueGapExempt, gap)
	t.Setenv(progression.EnvUniquePopTicket, pop)
	t.Setenv(progression.EnvUniqueMaxLevelLoot, maxLevel)
	t.Setenv(progression.EnvUniqueSealDrops, seals)
}
