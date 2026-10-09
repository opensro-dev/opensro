/*
===========================================================================

treasure_test.go - Hidden Treasure 5 Seal Keys, guardian call and turn-in

Exercise the shipped QNO_CA_TREASURE_5 definition with real item planning:
Tricia's five Seal Keys, the key that calls a Treasure Guardian around the
habitat point, the refill that keeps no daily allowance, and the turn-in
that pages through the box's opening before paying.

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
treasureFixture

A European character (the quest is level 34, country 1) that finished
Hidden Treasure 4 and accepted Hidden Treasure 5.
================
*/
func treasureFixture(t *testing.T) (*Runtime, *enterworld.Character, *Definition) {
	t.Helper()
	rt := captureCatalogRuntime(t)
	def, exists := rt.Defs.ByCodename(treasureQuest)
	previous, known := rt.Defs.ByCodename("QNO_CA_TREASURE_4")
	if !exists || !known {
		t.Fatal("Hidden Treasure 4 or 5 is not loaded")
	}
	c := questCharacter()
	*c.Level = 40
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	c.CompletedQuestIds = []uint32{previous.RefID}
	recordCompletion(c, previous.RefID)
	if _, err := rt.StartQuest(c, treasureQuest); err != nil {
		t.Fatal(err)
	}
	return rt, c, def
}

/*
================
TestTreasureKeyCallsTheGuardianAtTheHabitat

Acceptance hands over five Seal Keys. Away from the habitat a key refuses
with _17; there it calls MOB_QT_01_ONG 20-100 from the habitat point (not
the player) for five minutes. A failed call keeps the key, and the held
Treasure Box refuses another.
================
*/
func TestTreasureKeyCallsTheGuardianAtTheHabitat(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, def := treasureFixture(t)
	if captureItemCount(c, treasureKey) != treasureKeyCount {
		t.Fatal("acceptance did not grant five Seal Keys", c.MissionInventory)
	}
	var called []simulation.QuestMonsterSpawn
	spawned := true
	rt.SpawnQuestMonster = func(_ *enterworld.Character, request simulation.QuestMonsterSpawn) bool {
		called = append(called, request)
		return spawned
	}
	outside := treasureHabitat()
	outside.X += treasureRadius + 1
	frames, admitted := rt.BeginItemUse(c, treasureKey, outside, 0)
	if admitted || len(called) != 0 || len(frames) != 1 {
		t.Fatal("key admitted away from the habitat", frames)
	}
	near := treasureHabitat()
	near.Z -= treasureRadius - 1
	if _, admitted := rt.BeginItemUse(c, treasureKey, near, 0); !admitted || len(called) != 1 {
		t.Fatal("key refused at the habitat", called)
	}
	want := simulation.QuestMonsterSpawn{Codename: treasureGuardian, Position: treasureHabitat(), FixedPosition: true,
		RadiusMin: 20, RadiusSpan: 80, LifetimeMs: 300000}
	if called[0] != want {
		t.Fatalf("guardian call %+v", called[0])
	}
	spawned = false
	if _, admitted := rt.BeginItemUse(c, treasureKey, near, 0); admitted {
		t.Fatal("a failed call consumed the key")
	}
	if drops := rt.MonsterDrops(c, treasureGuardian, 0, func() (uint32, error) { return 1, nil }); len(drops) != 1 || drops[0].Codename != treasureBox {
		t.Fatal("the guardian did not drop the Treasure Box", drops)
	}
	holdItems(t, rt, c, def.CollectItemCodename, def.CollectCount)
	spawned = true
	if _, admitted := rt.BeginItemUse(c, treasureKey, near, 0); admitted {
		t.Fatal("key admitted with the Treasure Box held")
	}
}

/*
================
TestTreasureRefillAndTurnIn

With no key held Tricia answers _05 with the _06 row instead of _04, and
grants five keys as often as they run out. Holding the box, her turn-in
pages _09, _11 and _13 before _16 pays, and completion takes the keys.
================
*/
func TestTreasureRefillAndTurnIn(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, def := treasureFixture(t)
	if _, offered := rt.captureSupplyOption(c, def, def.StartNpcCodename); offered {
		t.Fatal("refill offered while keys are held")
	}
	code := captureSupplyPrefix + treasureQuest
	for visit := 0; visit < 2; visit++ {
		c.MissionInventory = nil
		option, offered := rt.captureSupplyOption(c, def, def.StartNpcCodename)
		if !offered || option.Informational || option.TitleSymbol != "SN_TALK_QNO_CA_TREASURE_5_06" ||
			option.PromptSymbol != "SN_TALK_QNO_CA_TREASURE_5_05" {
			t.Fatal("refill unavailable on visit", visit, option)
		}
		token, err := rt.PrepareNpcQuest(c, code, def.StartNpcCodename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := rt.AdvanceNpcQuest(c, token, def.StartNpcCodename); err != nil {
			t.Fatal(err)
		}
		if captureItemCount(c, treasureKey) != treasureKeyCount {
			t.Fatal("refill did not grant five keys on visit", visit)
		}
	}
	holdItems(t, rt, c, def.CollectItemCodename, def.CollectCount)
	var turnIn *NpcOption
	for _, option := range rt.OptionsForNpc(c, def.EndNpcCodename) {
		if option.Codename == treasureQuest && option.Complete {
			turnIn = &option
		}
	}
	if turnIn == nil || turnIn.PromptSymbol != "SN_TALK_QNO_CA_TREASURE_5_16" || len(turnIn.Pages) != 3 ||
		turnIn.Pages[0].PromptSymbol != "SN_TALK_QNO_CA_TREASURE_5_09" || turnIn.Pages[2].ReplySymbol != "SN_TALK_QNO_CA_TREASURE_5_15" {
		t.Fatal("turn-in lost its pages", turnIn)
	}
	if _, err := rt.AdvanceNpcQuest(c, treasureQuest, def.EndNpcCodename); err != nil {
		t.Fatal(err)
	}
	if !questCompleted(c, def.RefID) || captureItemCount(c, treasureKey) != 0 || captureItemCount(c, treasureBox) != 0 ||
		captureItemCount(c, "ITEM_QNO_CA_TREASURE_5_03") != 1 {
		t.Fatal("completion kept the quest items or lost the present", c.MissionInventory)
	}
}
