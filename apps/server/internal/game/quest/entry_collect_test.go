/*
===========================================================================

entry_collect_test.go - collect counts are re-derived at world entry

An in-game report: the quest window said 9 Tiger's grinding teeth while the
bag held 11. Native gather counts are never stored (91C7F0 / 91D5E0 count
the held items); a saved count that lags the saved bag must not survive
world entry, while a kill count, which only the record holds, must.

===========================================================================
*/
package quest

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
nodeValue
================
*/
func nodeValue(t *testing.T, record enterworld.ActiveQuestRecord, tag uint8) uint32 {
	t.Helper()
	for _, node := range record.Contents {
		if node.Tag == tag && len(node.ObjectiveValues) == 1 {
			return node.ObjectiveValues[0]
		}
	}
	t.Fatalf("no node %d in %+v", tag, record.Contents)
	return 0
}

/*
================
TestEntryRederivesALaggingCollectCount

The saved record says 9 while 11 teeth are held: entry shows 10 of 10.
================
*/
func TestEntryRederivesALaggingCollectCount(t *testing.T) {
	licensed.RequireGameData(t)
	rt := expansionRuntime(t)
	def := mustQuest(t, rt, "QNO_CH_SPECIAL_1")
	level, gold := int64(30), int64(0)
	c := &enterworld.Character{ID: 9, Name: "teeth", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: &level, Gold: &gold}
	if _, err := rt.StartQuest(c, def.Codename); err != nil {
		t.Fatal(err)
	}
	holdItems(t, rt, c, def.CollectItemCodename, 9)
	rt.InventoryUpdater()(c)
	if got := nodeValue(t, c.ActiveQuests[0], 1); got != 9 {
		t.Fatalf("saved count %d, want 9", got)
	}
	// Two more teeth land in the saved bag, but not in the saved record.
	holdItems(t, rt, c, def.CollectItemCodename, 2)
	if err := rt.NormalizeEntryRecords(c); err != nil {
		t.Fatal(err)
	}
	if got := nodeValue(t, c.ActiveQuests[0], 1); got != 10 {
		t.Fatalf("entry shows %d teeth with 11 held, want 10", got)
	}
}

/*
================
TestEntryKeepsKillsWhileRederivingItems

A parallel kill + change-item quest: entry re-derives the Filths and keeps
the four kills the record alone knows.
================
*/
func TestEntryKeepsKillsWhileRederivingItems(t *testing.T) {
	licensed.RequireGameData(t)
	rt := expansionRuntime(t)
	def := mustQuest(t, rt, stableQuest)
	level, gold := int64(10), int64(0)
	c := &enterworld.Character{ID: 9, Name: "stable", ModelCodename: "CHAR_EU_MAN_NOBLE", Level: &level, Gold: &gold}
	if _, err := rt.StartQuest(c, def.Codename); err != nil {
		t.Fatal(err)
	}
	for kill := 0; kill < 4; kill++ {
		rt.KillUpdater()(c, "MOB_QT_01_BAROI", 0)
	}
	holdItems(t, rt, c, stableFilth, 3)
	if err := rt.NormalizeEntryRecords(c); err != nil {
		t.Fatal(err)
	}
	if kills, filths := nodeValue(t, c.ActiveQuests[0], 1), nodeValue(t, c.ActiveQuests[0], 2); kills != 4 || filths != 3 {
		t.Fatalf("entry shows %d kills and %d filths, want 4 and 3", kills, filths)
	}
}
