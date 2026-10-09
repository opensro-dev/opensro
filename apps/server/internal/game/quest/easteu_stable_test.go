/*
===========================================================================

easteu_stable_test.go - the Sunset Witch's stable and Uvetino's holy water

Exercise the compiled QNO_EU_EASTEU_1 -> _2 -> _3 chain: holy water used by
the stable purifies it for ten seconds and yields a Stable Filth on a roll
(8AA700 / 8AA630), the Witch asks for holy water while none is held
(8AA0C0), and Uvetino gives ten a day once his own order is done (8AB070).

===========================================================================
*/
package quest

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/calendar"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
stableFixture

A European character that accepted the Sunset Witch's stable.
================
*/
func stableFixture(t *testing.T) (*Runtime, *enterworld.Character, *Definition, *uint16) {
	t.Helper()
	rt := expansionRuntime(t)
	day := new(uint16)
	rt.CalendarNow = func() calendar.Value { return calendar.Value{Day: *day} }
	def := mustQuest(t, rt, stableQuest)
	level, gold := int64(10), int64(0)
	c := &enterworld.Character{ID: 9, Name: "witch", ModelCodename: "CHAR_EU_MAN_NOBLE", Level: &level, Gold: &gold}
	if _, err := rt.StartQuest(c, def.Codename); err != nil {
		t.Fatalf("accept: %v", err)
	}
	return rt, c, def, day
}

/*
================
stablePoint
================
*/
func stablePoint() simulation.Spawn {
	return simulation.Spawn{RegionID: stableRegion, X: 249, Y: 24, Z: 759}
}

/*
================
TestHolyWaterPurifiesTheStable

Away from the stable the water answers _09; by it, ten pulses end in a
Filth on a roll of at most 50 and in _12 above it. A purification under
way refuses another, and five Filths refuse the water (_11).
================
*/
func TestHolyWaterPurifiesTheStable(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, _, _ := stableFixture(t)
	away := stablePoint()
	away.X += stableRadius + 1
	if frames, admitted := rt.BeginItemUse(c, holyWater, away, 0); admitted || !hasNotice(frames, "SN_TALK_QNO_EU_EASTEU_1_09") {
		t.Fatal("holy water admitted away from the stable", frames)
	}
	draw := uint32(50)
	rt.CaptureRoll = func() (uint32, error) { return draw, nil }
	for _, want := range []uint32{1, 1} {
		if _, admitted := rt.BeginItemUse(c, holyWater, stablePoint(), 0); !admitted {
			t.Fatal("holy water refused by the stable")
		}
		if frames, admitted := rt.BeginItemUse(c, holyWater, stablePoint(), 0); admitted || !hasNotice(frames, "SN_TALK_QNO_EU_EASTEU_3_13") {
			t.Fatal("a second purification started", frames)
		}
		var frames [][]wire.Frame
		for second := int64(1); second <= stablePurifySeconds; second++ {
			frames = append(frames, rt.AdvanceItemUse(c, second*questSecondMs))
		}
		if got := captureItemCount(c, stableFilth); got != want {
			t.Fatalf("filths %d after a roll of %d, want %d", got, draw, want)
		}
		if draw == 51 && !hasNotice(frames[len(frames)-1], "SN_TALK_QNO_EU_EASTEU_1_12") {
			t.Fatal("a failed purification sent no _12")
		}
		draw = 51
	}
	holdItems(t, rt, c, stableFilth, stableFilthTarget-1)
	if frames, admitted := rt.BeginItemUse(c, holyWater, stablePoint(), 0); admitted || !hasNotice(frames, "SN_TALK_QNO_EU_EASTEU_1_11") {
		t.Fatal("holy water admitted with five Filths held", frames)
	}
}

/*
================
TestUvetinoGivesHolyWaterOnceADay

The Witch asks for holy water (_04) until some is held (_05). Uvetino's
hat delivery waits for her quest; his own order pays ten holy water, and
once it is done he answers _11 while water is held, then grants ten more
(_05 / _09) the same day, then _12 until the next day.
================
*/
func TestUvetinoGivesHolyWaterOnceADay(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, def, day := stableFixture(t)
	if row, _ := npcRow(rt, c, def.Codename, def.EndNpcCodename); row.PromptSymbol != "SN_TALK_QNO_EU_EASTEU_1_04" {
		t.Fatalf("the Witch without holy water answered %s", row.PromptSymbol)
	}
	hat, order := mustQuest(t, rt, "QNO_EU_EASTEU_2"), mustQuest(t, rt, "QNO_EU_EASTEU_3")
	if _, err := rt.StartQuest(c, hat.Codename); err != nil {
		t.Fatalf("hat: %v", err)
	}
	if _, err := rt.AdvanceNpcQuest(c, hat.Codename, hat.EndNpcCodename); err != nil || !questCompleted(c, hat.RefID) {
		t.Fatalf("hat delivery: %v", err)
	}
	if _, err := rt.StartQuest(c, order.Codename); err != nil {
		t.Fatalf("order: %v", err)
	}
	holdItems(t, rt, c, order.CollectItemCodename, order.CollectCount)
	if _, err := rt.AdvanceNpcQuest(c, order.Codename, order.EndNpcCodename); err != nil || captureItemCount(c, holyWater) != 10 {
		t.Fatalf("Uvetino's order did not pay ten holy water: %v", err)
	}
	if row, _ := npcRow(rt, c, def.Codename, def.EndNpcCodename); row.PromptSymbol != "SN_TALK_QNO_EU_EASTEU_1_05" {
		t.Fatalf("the Witch with holy water answered %s", row.PromptSymbol)
	}
	option, offered := rt.captureSupplyOption(c, order, order.StartNpcCodename)
	if !offered || !option.Informational || option.PromptSymbol != "SN_TALK_QNO_EU_EASTEU_3_11" {
		t.Fatal("Uvetino did not answer _11 while water is held", option)
	}
	clearItem(c, holyWater)
	option, offered = rt.captureSupplyOption(c, order, order.StartNpcCodename)
	if !offered || option.Informational || option.PromptSymbol != "SN_TALK_QNO_EU_EASTEU_3_05" || option.TitleSymbol != "SN_TALK_QNO_EU_EASTEU_3_09" {
		t.Fatal("completion spent the day's holy water", option)
	}
	token, err := rt.PrepareNpcQuest(c, captureSupplyPrefix+order.Codename, order.StartNpcCodename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.AdvanceNpcQuest(c, token, order.StartNpcCodename); err != nil || captureItemCount(c, holyWater) != 10 {
		t.Fatalf("the supply did not grant ten: %v", err)
	}
	clearItem(c, holyWater)
	if option, _ = rt.captureSupplyOption(c, order, order.StartNpcCodename); !option.Informational || option.PromptSymbol != "SN_TALK_QNO_EU_EASTEU_3_12" {
		t.Fatal("a second supply was offered the same day", option)
	}
	*day++
	if option, _ = rt.captureSupplyOption(c, order, order.StartNpcCodename); option.Informational {
		t.Fatal("the next day did not restore the supply", option)
	}
}

/*
================
clearItem
================
*/
func clearItem(c *enterworld.Character, code string) {
	kept := c.MissionInventory[:0]
	for _, row := range c.MissionInventory {
		if row.Codename != code {
			kept = append(kept, row)
		}
	}
	c.MissionInventory = kept
}
