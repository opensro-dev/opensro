package quest

import (
	"fmt"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/calendar"
)

func TestQuestCalendarHourBranches(t *testing.T) {
	for mode := 0; mode < 256; mode++ {
		for hour := 0; hour < 256; hour++ {
			want := hour < 24 && (mode == 0 || mode == 1 && (hour < 4 || hour >= 20) || mode == 2 && hour >= 4 && hour < 20)
			if questHourAvailable(uint8(mode), uint8(hour)) != want {
				t.Fatalf("mode %d hour %d", mode, hour)
			}
		}
	}
}

func TestQuestCalendarProductionQuotaAndCancellation(t *testing.T) {
	licensed.RequireGameData(t)
	textdata := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(textdata)
	defs, err := LoadDefinitions(NewCatalog(textdata), items)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"QNO_WC_GENARAL_SW_1", "QNO_KT_SOLDIER_WE2_1"} {
		t.Run(code, func(t *testing.T) {
			authority, err := store.Open(filepath.Join(t.TempDir(), "authority"), store.Options{DefaultSkills: rewardTestSkillSeeder})
			if err != nil {
				t.Fatal(err)
			}
			defer authority.Close()
			deps := &enterworld.Deps{Characters: authority.Characters(), Items: items, Levels: enterworld.NewTextdataLevels(textdata), UpdateCharacter: authority.UpdateCharacter, MutateCharacter: authority.MutateCharacter}
			rt, err := NewRuntime(deps, defs, progression.NewRuntime(deps).ExperienceUpdater())
			if err != nil {
				t.Fatal(err)
			}
			actions := action.NewRuntime(deps, nil)
			rt.PlanInventory = actions.PlanQuestInventory
			actions.AdvanceQuestCalendar = rt.AdvanceCalendar
			def, _ := defs.ByCodename(code)
			hour := uint8(4)
			if def.DayOrNight == 1 {
				hour = 20
			}
			rt.CalendarNow = func() calendar.Value { return calendar.Value{Hour: hour} }
			var chars []*enterworld.Character
			for i := uint32(0); i <= def.PeriodStartLimit; i++ {
				level := int64(def.Level)
				c := &enterworld.Character{Name: fmt.Sprintf("quota%d", i), ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: &level}
				if err := authority.CreateCharacter("global-official", fmt.Sprintf("quest-calendar-%d", i), c); err != nil {
					t.Fatal(err)
				}
				for _, resident := range authority.Characters().CharactersForDivision("global-official") {
					if resident.Name == c.Name {
						c = resident
						break
					}
				}
				chars = append(chars, c)
				_, err := rt.StartQuest(c, code)
				if (err == nil) != (i < def.PeriodStartLimit) {
					t.Fatalf("quota admission %d: %v", i, err)
				}
			}
			last := chars[len(chars)-1]
			if _, ok := rt.MarkerStates(last)[def.RefID]; !ok {
				t.Fatal("native marker quota bypass lost")
			}
			if _, err := rt.StartQuest(chars[0], code); err == nil {
				t.Fatal("duplicate acceptance")
			}
			// Complete through actual objective producers and the reward owner.
			// Generic completion spends this period's opportunity permanently.
			finished := chars[1]
			if def.Objective == ObjectiveKill {
				update := rt.KillUpdater()
				deps.Update(finished, "test-fatal-events", func() bool {
					for i := uint32(0); i < def.KillCount; i++ {
						update(finished, def.KillMonsterCodenames[0], 0)
					}
					return true
				})
			} else {
				deps.Update(finished, "test-quest-pickup", func() bool {
					rows, _, err := rt.PlanInventory(finished, nil, []inventory.ItemAmount{{Codename: def.CollectItemCodename, Count: def.CollectCount}})
					if err != nil {
						t.Fatal(err)
					}
					finished.MissionInventory = rows
					rt.applyInventoryChange(finished)
					return true
				})
			}
			if _, err := rt.CompleteNpcQuest(finished, code); err != nil {
				t.Fatal(err)
			}
			if rt.periodStarts[def.RefID] != 0 {
				t.Fatal("completion returned a first-come slot")
			}
			if _, err := rt.HandleGiveUp(chars[0], wire.NewWriter(4).U32(def.RefID).Payload()); err != nil {
				t.Fatal(err)
			}
			if _, err := rt.StartQuest(last, code); err != nil {
				t.Fatal("cancel did not return quota", err)
			}
			if _, err := rt.StartQuest(chars[0], code); err == nil {
				t.Fatal("quota was returned twice")
			}
			// Existing action clock drives the world-definition pulse. A skipped
			// start-hour boundary does not manufacture the exact native reset.
			startHour := hour
			hour = startHour - 2
			actions.TickHook()(5000)
			hour = startHour
			actions.TickHook()(10000)
			if _, err := rt.StartQuest(chars[0], code); err == nil {
				t.Fatal("skipped transition refilled quota")
			}
			hour = startHour - 1
			actions.TickHook()(15000)
			hour = startHour
			actions.TickHook()(20000)
			if _, ok := rt.MarkerStates(chars[0])[def.RefID]; !ok {
				t.Fatal("period reset did not restore offer")
			}
			if _, err := rt.StartQuest(chars[0], code); err != nil {
				t.Fatal("next period rejected", err)
			}
			// Crossing out of the permitted acceptance period does not expire it.
			hour = uint8(4)
			if def.DayOrNight == 2 {
				hour = 20
			}
			actions.TickHook()(25000)
			if len(chars[0].ActiveQuests) != 1 {
				t.Fatal("day/night condition became a deadline")
			}
			if _, err := rt.HandleGiveUp(chars[0], wire.NewWriter(4).U32(def.RefID).Payload()); err != nil {
				t.Fatal(err)
			}
			if _, err := rt.StartQuest(chars[0], code); err == nil {
				t.Fatal("outside-period acceptance")
			}
		})
	}
}
