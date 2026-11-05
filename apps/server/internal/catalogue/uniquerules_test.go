/*
===========================================================================

uniquerules_test.go - the beta object publishes the unique rules (#2537)

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

droptables.json's beta object carries the unique rules the server runs
(progression.UniqueRulesFromEnv), read from the same source the
GameWorld wires so the export cannot drift: at shard defaults the gap
exemption and the guaranteed Magic Pop card are published with the
card's item id, a rule switched off leaves the object, and with every
rule native there is no unique object at all.
================
*/
func TestDropTablesPublishUniqueRules(t *testing.T) {
	monsters := []Monster{{Level: 30, Country: 3, Table: equipmentTableKey(30, 3)}}
	itemID := func(codename string) uint32 { return uint32(len(codename)) }
	t.Setenv(progression.EnvBetaGrowth, "on")
	setRules := func(gap, ticket, maxLevel, seals string) {
		t.Setenv(progression.EnvUniqueGapExempt, gap)
		t.Setenv(progression.EnvUniquePopTicket, ticket)
		t.Setenv(progression.EnvUniqueMaxLevelLoot, maxLevel)
		t.Setenv(progression.EnvUniqueSealDrops, seals)
	}

	setRules("", "", "", "")
	beta := BuildDropTables(monsters, itemID, Header{}).Beta
	if beta == nil || beta.Unique == nil {
		t.Fatalf("shard defaults: beta %+v, want a unique object", beta)
	}
	if unique := beta.Unique; !unique.GapExempt || !unique.PopTicket || unique.MaxLevelLoot || unique.SealDrops {
		t.Fatalf("shard defaults: unique %+v, want gap exempt and ticket on, the rest off", unique)
	}
	if want := uint32(len(gacha.CardCodename)); beta.Unique.PopTicketItem != want {
		t.Fatalf("shard defaults: popTicketItem %d, want the card id %d", beta.Unique.PopTicketItem, want)
	}

	setRules("off", "", "", "")
	if unique := BuildDropTables(monsters, itemID, Header{}).Beta.Unique; unique == nil || unique.GapExempt || !unique.PopTicket {
		t.Fatalf("gap off: unique %+v, want the exemption gone and the ticket kept", unique)
	}

	setRules("", "off", "", "")
	if unique := BuildDropTables(monsters, itemID, Header{}).Beta.Unique; unique == nil || unique.PopTicket || unique.PopTicketItem != 0 {
		t.Fatalf("ticket off: unique %+v, want no ticket and no card id", unique)
	}

	setRules("off", "off", "on", "on")
	if unique := BuildDropTables(monsters, itemID, Header{}).Beta.Unique; unique == nil || !unique.MaxLevelLoot || !unique.SealDrops {
		t.Fatalf("opt-ins on: unique %+v, want both published", unique)
	}

	setRules("off", "off", "off", "off")
	if unique := BuildDropTables(monsters, itemID, Header{}).Beta.Unique; unique != nil {
		t.Fatalf("all native: unique %+v, want no unique object", unique)
	}
}
