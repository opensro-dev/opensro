/*
===========================================================================

uniquerules_test.go - droptables.json publishes the unique rules (#2537)

===========================================================================
*/
package catalogue

import (
	"testing"

	"opensro.online/server/internal/game/item/gacha"
	"opensro.online/server/internal/game/progression"
)

/*
================
TestDropTablesPublishUniqueRules

droptables.json's unique object carries the unique rules the server runs
(progression.UniqueRulesFromEnv), read from the same source the
GameWorld wires so the export cannot drift: at shard defaults the gap
exemption and the guaranteed Magic Pop card are published with the
card's item id, a rule switched off leaves the object, and with every
rule native there is no unique object at all. The object stands outside
the beta rates: a beta-off shard that runs the rules still publishes
them, because the GameWorld runs them either way.
================
*/
func TestDropTablesPublishUniqueRules(t *testing.T) {
	monsters := []Monster{{Level: 30, Country: 3, Table: equipmentTableKey(30, 3)}}
	itemID := func(codename string) uint32 { return uint32(len(codename)) }
	setRules := func(gap, ticket, maxLevel, seals string) {
		t.Setenv(progression.EnvUniqueGapExempt, gap)
		t.Setenv(progression.EnvUniquePopTicket, ticket)
		t.Setenv(progression.EnvUniqueMaxLevelLoot, maxLevel)
		t.Setenv(progression.EnvUniqueSealDrops, seals)
	}

	// Beta growth off (and no beta rate in play): the beta object is
	// absent, the unique object is still published at the defaults.
	t.Setenv(progression.EnvBetaGrowth, "off")
	t.Setenv(progression.EnvSunDropRate, "0")
	setRules("", "", "", "")
	file := BuildDropTables(monsters, itemID, Header{})
	if file.Beta != nil {
		t.Fatalf("beta off: beta %+v, want no beta object", file.Beta)
	}
	if file.Unique == nil {
		t.Fatal("beta off: no unique object, want the rules published")
	}
	if unique := file.Unique; !unique.GapExempt || !unique.PopTicket || unique.MaxLevelLoot || unique.SealDrops {
		t.Fatalf("shard defaults: unique %+v, want gap exempt and ticket on, the rest off", unique)
	}
	if want := uint32(len(gacha.CardCodename)); file.Unique.PopTicketItem != want {
		t.Fatalf("shard defaults: popTicketItem %d, want the card id %d", file.Unique.PopTicketItem, want)
	}

	// Beta growth on: the same object, beside the beta rates.
	t.Setenv(progression.EnvBetaGrowth, "on")
	if unique := BuildDropTables(monsters, itemID, Header{}).Unique; unique == nil || !unique.GapExempt || !unique.PopTicket {
		t.Fatalf("beta on: unique %+v, want gap exempt and ticket on", unique)
	}

	setRules("off", "", "", "")
	if unique := BuildDropTables(monsters, itemID, Header{}).Unique; unique == nil || unique.GapExempt || !unique.PopTicket {
		t.Fatalf("gap off: unique %+v, want the exemption gone and the ticket kept", unique)
	}

	setRules("", "off", "", "")
	if unique := BuildDropTables(monsters, itemID, Header{}).Unique; unique == nil || unique.PopTicket || unique.PopTicketItem != 0 {
		t.Fatalf("ticket off: unique %+v, want no ticket and no card id", unique)
	}

	setRules("off", "off", "on", "on")
	if unique := BuildDropTables(monsters, itemID, Header{}).Unique; unique == nil || !unique.MaxLevelLoot || !unique.SealDrops {
		t.Fatalf("opt-ins on: unique %+v, want both published", unique)
	}

	setRules("off", "off", "off", "off")
	if unique := BuildDropTables(monsters, itemID, Header{}).Unique; unique != nil {
		t.Fatalf("all native: unique %+v, want no unique object", unique)
	}
}
