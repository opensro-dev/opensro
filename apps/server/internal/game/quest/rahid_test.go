/*
===========================================================================

rahid_test.go - Rahid 5's story offer, Guardian Rocky Essences and seven peaks

Exercise the shipped QNO_RM_OLDWOMAN_5 definition with real item planning:
the four pages before the offer, the giant-only five-Essence drop, and the
peaks that must be gathered in order, each with its own timer and chance.

===========================================================================
*/
package quest

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/licensed"
)

const rahidGiant = 4

/*
================
rahidFixture

A character that finished Rahid 4 and stands before Shiphr.
================
*/
func rahidFixture(t *testing.T) (*Runtime, *enterworld.Character, *Definition) {
	t.Helper()
	rt := captureCatalogRuntime(t)
	def, exists := rt.Defs.ByCodename(rahidQuest)
	if !exists {
		t.Fatal("Rahid 5 is not loaded")
	}
	c := questCharacter()
	*c.Level = 100
	c.CompletedQuestIds = append([]uint32(nil), def.RequiredQuestIDs...)
	return rt, c, def
}

/*
================
rahidPeakSpawn
================
*/
func rahidPeakSpawn(i int) simulation.Spawn {
	peak := rahidPeaks[i]
	return simulation.Spawn{RegionID: peak.region, X: peak.x, Y: peak.y, Z: peak.z}
}

/*
================
rahidRoll

A fixed rand() value for the gathering roll, as rand % 101.
================
*/
func rahidRoll(rt *Runtime, draw uint32) {
	rt.CaptureRoll = func() (uint32, error) { return draw, nil }
}

/*
================
finishRahidPeak

Runs one admitted countdown to its end.
================
*/
func finishRahidPeak(rt *Runtime, c *enterworld.Character, peak int, startMs int64) {
	for second := int64(1); second <= int64(rahidPeaks[peak].seconds); second++ {
		rt.AdvanceItemUse(c, startMs+second*questSecondMs)
	}
}

/*
================
TestRahidOfferPagesBeforeTheOffer

Shiphr offers Rahid 5 through the four story pages, then _09.
================
*/
func TestRahidOfferPagesBeforeTheOffer(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, def := rahidFixture(t)
	for _, row := range rt.OptionsForNpc(c, def.StartNpcCodename) {
		if row.Codename != rahidQuest {
			continue
		}
		if row.PromptSymbol != "SN_TALK_QNO_RM_OLDWOMAN_5_09" || len(row.Pages) != 4 ||
			row.Pages[0].PromptSymbol != "SN_TALK_QNO_RM_OLDWOMAN_5_01" || row.Pages[3].ReplySymbol != "SN_TALK_QNO_RM_OLDWOMAN_5_08" {
			t.Fatalf("offer row %+v", row)
		}
		return
	}
	t.Fatal("Shiphr does not offer Rahid 5 after Rahid 4")
}

/*
================
TestRahidEssencesDropOnlyFromGuardianRockies

Only a giant MOB_RM_ROCKY (grade 4, party or not) drops, and it drops five.
================
*/
func TestRahidEssencesDropOnlyFromGuardianRockies(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, _ := rahidFixture(t)
	if _, err := rt.StartQuest(c, rahidQuest); err != nil {
		t.Fatal(err)
	}
	roll := func() (uint32, error) { return 1, nil }
	for _, rarity := range []uint8{0, 1, 3} {
		if drops := rt.MonsterDrops(c, "MOB_RM_ROCKY", rarity, roll); len(drops) != 0 {
			t.Fatalf("rarity %d Rocky dropped %v", rarity, drops)
		}
	}
	for _, rarity := range []uint8{rahidGiant, 0x10 | rahidGiant} {
		drops := rt.MonsterDrops(c, "MOB_RM_ROCKY", rarity, roll)
		if len(drops) != 1 || drops[0].Codename != rahidEssence || drops[0].Count != 5 {
			t.Fatalf("rarity %#x Rocky dropped %v", rarity, drops)
		}
	}
}

/*
================
TestRahidPeaksRunInOrder

An Essence works only at the next peak. A roll above the peak's chance fails
with _18 and keeps the step; at most the chance it yields a Pile and moves on.
Seven Piles stand the objective complete and completion clears the Essences.
================
*/
func TestRahidPeaksRunInOrder(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, def := rahidFixture(t)
	if _, err := rt.StartQuest(c, rahidQuest); err != nil {
		t.Fatal(err)
	}
	holdItems(t, rt, c, rahidEssence, 20)
	at := activeQuestIndex(c, def.RefID)
	if _, admitted := rt.BeginItemUse(c, rahidEssence, rahidPeakSpawn(1), 0); admitted {
		t.Fatal("the second peak admitted the first step")
	}
	rahidRoll(rt, uint32(rahidPeaks[0].chance)+1)
	if _, admitted := rt.BeginItemUse(c, rahidEssence, rahidPeakSpawn(0), 0); !admitted {
		t.Fatal("the first peak refused the Essence")
	}
	finishRahidPeak(rt, c, 0, 0)
	if c.ActiveQuests[at].ToolStep != 0 || captureItemCount(c, rahidPile) != 0 {
		t.Fatal("a roll above the chance gathered a Pile")
	}
	clock := int64(100 * questSecondMs)
	for peak := range rahidPeaks {
		rahidRoll(rt, uint32(rahidPeaks[peak].chance))
		if _, admitted := rt.BeginItemUse(c, rahidEssence, rahidPeakSpawn(peak), clock); !admitted {
			t.Fatalf("peak %d refused its step", peak)
		}
		finishRahidPeak(rt, c, peak, clock)
		clock += 100 * questSecondMs
		if int(c.ActiveQuests[at].ToolStep) != peak+1 || captureItemCount(c, rahidPile) != uint32(peak+1) {
			t.Fatalf("peak %d: step %d, piles %d", peak, c.ActiveQuests[at].ToolStep, captureItemCount(c, rahidPile))
		}
	}
	if _, admitted := rt.BeginItemUse(c, rahidEssence, rahidPeakSpawn(6), clock); admitted {
		t.Fatal("an Essence was admitted after the seventh peak")
	}
	if _, err := rt.AdvanceNpcQuest(c, rahidQuest, def.EndNpcCodename); err != nil {
		t.Fatal(err)
	}
	if !questCompleted(c, def.RefID) || captureItemCount(c, rahidEssence) != 0 || captureItemCount(c, rahidPile) != 0 {
		t.Fatal("completion kept the Rahid items", c.MissionInventory)
	}
}
