/*
===========================================================================

cerberus_test.go - Cerberus 1 scissors, Golden Apple lure and daily refill

Exercise the shipped QNO_EU_EASTEU_19 definition with real item planning:
the Witch's thirty scissors, the ten-second cut that always yields an apple,
the apple's Ladon lure and the refill that spends a day only on its grant.

===========================================================================
*/
package quest

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/world/calendar"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
cerberusFixture

A European character that accepted Cerberus 1 on world day zero.
================
*/
func cerberusFixture(t *testing.T) (*Runtime, *enterworld.Character, *Definition, *uint16) {
	t.Helper()
	rt := captureCatalogRuntime(t)
	day := new(uint16)
	rt.CalendarNow = func() calendar.Value { return calendar.Value{Day: *day} }
	def, exists := rt.Defs.ByCodename(cerberusQuest)
	if !exists {
		t.Fatal("Cerberus 1 is not loaded")
	}
	c := questCharacter()
	*c.Level = 60
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	if _, err := rt.StartQuest(c, cerberusQuest); err != nil {
		t.Fatal(err)
	}
	return rt, c, def, day
}

/*
================
holdItems

Places items through the real inventory planner.
================
*/
func holdItems(t *testing.T, rt *Runtime, c *enterworld.Character, code string, count uint32) {
	t.Helper()
	rows, _, err := rt.PlanInventory(c, nil, []inventory.ItemAmount{{Codename: code, Count: count}})
	if err != nil {
		t.Fatal(err)
	}
	c.MissionInventory = rows
}

/*
================
TestCerberusScissorsCutAnAppleWithoutARoll

Acceptance hands over thirty scissors. Outside the lure point the scissors
refuse with _12; inside, ten pulses always award one Golden Apple, even on
the roll that fails Ivy's knife. Twenty held apples refuse another cut.
================
*/
func TestCerberusScissorsCutAnAppleWithoutARoll(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, _, _ := cerberusFixture(t)
	if captureItemCount(c, cerberusScissors) != 30 {
		t.Fatal("acceptance did not grant thirty Long Scissors")
	}
	rt.CaptureRoll = func() (uint32, error) { return 0, nil }
	outside := cerberusLurePoint()
	outside.X += cerberusRadius + 1
	frames, admitted := rt.BeginItemUse(c, cerberusScissors, outside, 0)
	if admitted || len(frames) != 1 {
		t.Fatal("scissors admitted outside the lure point", frames)
	}
	if _, admitted := rt.BeginItemUse(c, cerberusScissors, cerberusLurePoint(), 0); !admitted {
		t.Fatal("scissors refused at the lure point")
	}
	for second := int64(1); second < cerberusCutSeconds; second++ {
		rt.AdvanceItemUse(c, second*questSecondMs)
	}
	if captureItemCount(c, cerberusApple) != 0 {
		t.Fatal("apple awarded before ten pulses")
	}
	rt.AdvanceItemUse(c, cerberusCutSeconds*questSecondMs)
	if captureItemCount(c, cerberusApple) != 1 {
		t.Fatal("the cut did not yield exactly one Golden Apple")
	}
	holdItems(t, rt, c, cerberusApple, cerberusHeldLimit-1)
	if _, admitted := rt.BeginItemUse(c, cerberusScissors, cerberusLurePoint(), 20*questSecondMs); admitted {
		t.Fatal("scissors admitted with twenty apples held")
	}
}

/*
================
TestCerberusAppleLuresALadon

The apple spawns MOB_QT_01_LADON 20-100 from the player inside the area,
refuses outside it with _13, keeps the apple when the spawn fails, and
refuses once twenty Bloody Orbs are held.
================
*/
func TestCerberusAppleLuresALadon(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, def, _ := cerberusFixture(t)
	var lured []string
	spawned := true
	rt.SpawnQuestMonster = func(_ *enterworld.Character, request simulation.QuestMonsterSpawn) bool {
		if request.RadiusMin != cerberusLureMin || request.RadiusSpan != cerberusLureSpan ||
			request.Position != (simulation.Spawn{}) || request.LifetimeMs != 0 {
			t.Fatalf("lure %+v", request)
		}
		lured = append(lured, request.Codename)
		return spawned
	}
	outside := cerberusLurePoint()
	outside.Z += cerberusRadius + 1
	if _, admitted := rt.BeginItemUse(c, cerberusApple, outside, 0); admitted || len(lured) != 0 {
		t.Fatal("apple lured outside the area")
	}
	if _, admitted := rt.BeginItemUse(c, cerberusApple, cerberusLurePoint(), 0); !admitted || len(lured) != 1 || lured[0] != cerberusLadon {
		t.Fatal("apple did not lure a Ladon", lured)
	}
	spawned = false
	if _, admitted := rt.BeginItemUse(c, cerberusApple, cerberusLurePoint(), 0); admitted {
		t.Fatal("a failed spawn consumed the apple")
	}
	holdItems(t, rt, c, def.CollectItemCodename, def.CollectCount)
	spawned = true
	if _, admitted := rt.BeginItemUse(c, cerberusApple, cerberusLurePoint(), 0); admitted {
		t.Fatal("apple lured with the objective complete")
	}
	if drops := rt.MonsterDrops(c, cerberusLadon, 0, func() (uint32, error) { return 0, nil }); len(drops) != 0 {
		t.Fatal("a Ladon dropped past the objective", drops)
	}
}

/*
================
TestCerberusRefillSpendsTheDayAtTheGrant

With the first scissors gone, the Witch refills thirty on the day of
acceptance, then answers _10 until the next day. Completion clears the
leftover scissors and apples.
================
*/
func TestCerberusRefillSpendsTheDayAtTheGrant(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, def, day := cerberusFixture(t)
	if _, offered := rt.captureSupplyOption(c, def, def.StartNpcCodename); offered {
		t.Fatal("refill offered while scissors are held")
	}
	c.MissionInventory = nil
	code := captureSupplyPrefix + cerberusQuest
	option, offered := rt.captureSupplyOption(c, def, def.StartNpcCodename)
	if !offered || option.Informational || option.TitleSymbol != "SN_TALK_QNO_EU_EASTEU_19_08" || option.PromptSymbol != "SN_TALK_QNO_EU_EASTEU_19_07" {
		t.Fatal("acceptance spent the refill day", option)
	}
	token, err := rt.PrepareNpcQuest(c, code, def.StartNpcCodename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.AdvanceNpcQuest(c, token, def.StartNpcCodename); err != nil {
		t.Fatal(err)
	}
	if captureItemCount(c, cerberusScissors) != 30 {
		t.Fatal("refill did not grant thirty scissors")
	}
	c.MissionInventory = nil
	option, offered = rt.captureSupplyOption(c, def, def.StartNpcCodename)
	if !offered || !option.Informational || option.PromptSymbol != "SN_TALK_QNO_EU_EASTEU_19_10" {
		t.Fatal("a second refill was offered on the same day", option)
	}
	*day++
	if option, _ = rt.captureSupplyOption(c, def, def.StartNpcCodename); option.Informational {
		t.Fatal("the next day did not restore the refill")
	}
	holdItems(t, rt, c, cerberusScissors, 3)
	holdItems(t, rt, c, cerberusApple, 2)
	holdItems(t, rt, c, def.CollectItemCodename, def.CollectCount)
	if _, err := rt.AdvanceNpcQuest(c, cerberusQuest, def.EndNpcCodename); err != nil {
		t.Fatal(err)
	}
	if captureItemCount(c, cerberusScissors) != 0 || captureItemCount(c, cerberusApple) != 0 || !questCompleted(c, def.RefID) {
		t.Fatal("completion kept the Cerberus tools", c.MissionInventory)
	}
	if _, admitted := rt.BeginItemUse(c, cerberusScissors, cerberusLurePoint(), 0); admitted {
		t.Fatal("scissors admitted after completion")
	}
}
