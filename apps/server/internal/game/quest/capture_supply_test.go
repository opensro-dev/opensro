/*
===========================================================================

capture_supply_test.go - daily supplies, durable reservations and cleanup

Use real inventory planning and the production definition catalog. Replays,
wrong NPCs, full bags and cancellation must not create free extra traps.

===========================================================================
*/
package quest

import (
	"encoding/json"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"testing"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/calendar"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
captureCatalogRuntime

Load the shipped quest and item catalogs, including capture-only references.
================
*/
func captureCatalogRuntime(t *testing.T) *Runtime {
	t.Helper()
	textdata := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(textdata)
	defs, err := LoadDefinitions(NewCatalog(textdata), items)
	if err != nil {
		t.Fatal(err)
	}
	deps := &enterworld.Deps{Items: items, Levels: enterworld.NewTextdataLevels(textdata)}
	rt, err := NewRuntime(deps, defs, progression.NewRuntime(deps).ExperienceUpdater())
	if err != nil {
		t.Fatal(err)
	}
	rt.PlanInventory = action.NewRuntime(deps, nil).PlanQuestInventory
	return rt
}

/*
================
TestCaptureSupplyAcceptanceRefillAndCleanup

World day zero is valid. Opening a refill reserves the day, confirmation is
single-use and the saved reservation retains those rules after JSON reload.
================
*/
func TestCaptureSupplyAcceptanceRefillAndCleanup(t *testing.T) {
	licensed.RequireGameData(t)
	for _, supply := range captureSupplies {
		if supply.afterCompletion {
			continue
		}
		t.Run(supply.quest, func(t *testing.T) {
			rt := captureCatalogRuntime(t)
			day := uint16(0)
			rt.CalendarNow = func() calendar.Value { return calendar.Value{Day: day} }
			def, found := rt.Defs.ByCodename(supply.quest)
			if !found {
				t.Fatal("capture definition absent")
			}
			c := questCharacter()
			*c.Level = 60
			c.ModelCodename = "CHAR_EU_MAN_NOBLE"
			c.CompletedQuestIds = append([]uint32(nil), def.RequiredQuestIDs...)
			if _, err := rt.StartQuest(c, supply.quest); err != nil {
				t.Fatal(err)
			}
			if captureItemCount(c, supply.item) != captureSupplyCount {
				t.Fatal("acceptance omitted the native five traps")
			}
			c.MissionInventory = nil
			option, offered := rt.captureSupplyOption(c, def, def.StartNpcCodename)
			if !offered || !option.Informational || option.PromptSymbol != supply.exhausted {
				t.Fatal("initial grant did not spend day zero")
			}
			day++
			code := captureSupplyPrefix + supply.quest
			if _, err := rt.PrepareNpcQuest(c, code, "NPC_WRONG"); err == nil {
				t.Fatal("wrong NPC reserved supplies")
			}
			token, err := rt.PrepareNpcQuest(c, code, def.StartNpcCodename)
			if err != nil {
				t.Fatal(err)
			}
			saved, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(saved, c); err != nil {
				t.Fatal(err)
			}
			if _, err := rt.PrepareNpcQuest(c, code, def.StartNpcCodename); err == nil {
				t.Fatal("reopening the prompt bypassed the daily allowance")
			}
			if _, err := rt.AdvanceNpcQuest(c, token, def.StartNpcCodename); err != nil {
				t.Fatal(err)
			}
			if captureItemCount(c, supply.item) != captureSupplyCount {
				t.Fatal("refill grant missing")
			}
			c.MissionInventory = nil
			if _, err := rt.AdvanceNpcQuest(c, token, def.StartNpcCodename); err == nil {
				t.Fatal("spent token replayed after its items were consumed")
			}
			day++
			token, err = rt.PrepareNpcQuest(c, code, def.StartNpcCodename)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := rt.AdvanceNpcQuest(c, token, def.StartNpcCodename); err != nil {
				t.Fatal(err)
			}
			if _, err := rt.HandleGiveUp(c, u32le(def.RefID)); err != nil {
				t.Fatal(err)
			}
			if captureItemCount(c, supply.item) != 0 || len(c.ActiveQuests) != 0 {
				t.Fatal("abandonment retained mission traps")
			}
		})
	}
}
