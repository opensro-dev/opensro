/*
===========================================================================

adventurer_test.go - Demetri's Yellow Eggs (QNO_EU_ADVENTURER_1)

Exercise the shipped compiled definition: the archers' Shiny Moss lures a
Red Spotted Crab by the Troy wooden horse (8B6840), the crabs' Yellow Eggs
alone gate the pay (vf17C's one required mission), and the leftover moss
leaves with the quest.

===========================================================================
*/
package quest

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
adventurerFixture

A European character that accepted the Yellow Eggs.
================
*/
func adventurerFixture(t *testing.T) (*Runtime, *enterworld.Character, *Definition) {
	t.Helper()
	rt := expansionRuntime(t)
	def := mustQuest(t, rt, adventurerQuest)
	level, gold := int64(20), int64(0)
	c := &enterworld.Character{ID: 9, Name: "egg", ModelCodename: "CHAR_EU_MAN_NOBLE", Level: &level, Gold: &gold}
	if _, err := rt.StartQuest(c, def.Codename); err != nil {
		t.Fatalf("accept: %v", err)
	}
	return rt, c, def
}

/*
================
TestShinyMossLuresACrabAtTheHorse

Away from the horse the moss answers _08; by it, a crab lands 20-100 from
the player with no timer of its own. A failed lure keeps the moss, and
the Yellow Eggs gathered, the moss answers _09.
================
*/
func TestShinyMossLuresACrabAtTheHorse(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, def := adventurerFixture(t)
	var lured []simulation.QuestMonsterSpawn
	spawned := true
	rt.SpawnQuestMonster = func(_ *enterworld.Character, request simulation.QuestMonsterSpawn) bool {
		lured = append(lured, request)
		return spawned
	}
	away := adventurerHorse()
	away.X += adventurerRadius + 1
	if frames, admitted := rt.BeginItemUse(c, adventurerMoss, away, 0); admitted || len(lured) != 0 || len(frames) != 1 {
		t.Fatal("moss admitted away from the horse", frames)
	}
	near := adventurerHorse()
	near.Z += adventurerRadius - 1
	if _, admitted := rt.BeginItemUse(c, adventurerMoss, near, 0); !admitted || len(lured) != 1 {
		t.Fatal("moss refused by the horse", lured)
	}
	want := simulation.QuestMonsterSpawn{Codename: adventurerCrab, RadiusMin: 20, RadiusSpan: 80}
	if lured[0] != want {
		t.Fatalf("crab lure %+v", lured[0])
	}
	spawned = false
	if _, admitted := rt.BeginItemUse(c, adventurerMoss, near, 0); admitted {
		t.Fatal("a failed lure consumed the moss")
	}
	holdItems(t, rt, c, "ITEM_QNO_EU_ADVENTURER_1_02", 5)
	spawned = true
	frames, admitted := rt.BeginItemUse(c, adventurerMoss, near, 0)
	if admitted || len(frames) != 1 || len(lured) != 2 {
		t.Fatal("moss admitted with the eggs gathered", frames)
	}
	if def.Objectives[1].Optional != true || def.Objectives[0].Optional {
		t.Fatal("the moss gather lost its optional mark", def.Objectives)
	}
}

/*
================
TestYellowEggsAlonePayDemetri

Five eggs and no moss stand the quest complete, with the achieved-now
banner; Demetri pays and takes the eggs and every leftover moss.
================
*/
func TestYellowEggsAlonePayDemetri(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, def := adventurerFixture(t)
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err == nil {
		t.Fatal("Demetri paid without the eggs")
	}
	holdItems(t, rt, c, adventurerMoss, 2)
	frames, _ := rt.InventoryUpdater()(c)
	if hasNotice(frames, def.AchievedNowSymbol) {
		t.Fatal("the moss alone announced completion")
	}
	holdItems(t, rt, c, "ITEM_QNO_EU_ADVENTURER_1_02", 5)
	frames, _ = rt.InventoryUpdater()(c)
	if !hasNotice(frames, def.AchievedNowSymbol) {
		t.Fatal("five eggs did not announce completion")
	}
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err != nil || !questCompleted(c, def.RefID) {
		t.Fatalf("Demetri did not pay: %v", err)
	}
	if captureItemCount(c, adventurerMoss) != 0 || captureItemCount(c, "ITEM_QNO_EU_ADVENTURER_1_02") != 0 {
		t.Fatal("completion kept the eggs or the moss", c.MissionInventory)
	}
}
