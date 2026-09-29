package quest

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"os"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
)

func TestTimedQuestProductionStoreAndCharacterPulse(t *testing.T) {
	licensed.RequireGameData(t)
	textdata := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(textdata)
	defs, err := LoadDefinitions(NewCatalog(textdata), items)
	if err != nil {
		t.Fatal(err)
	}
	def, ok := defs.ByCodename("QNO_WC_SMITH_1")
	if !ok || def.TimeLimitMinutes != 120 || def.KillCount != 20 {
		t.Fatal("missing primary timed hunt")
	}
	dir := filepath.Join(t.TempDir(), "authority")
	var authority *store.Store
	var rt *Runtime
	var actions *action.Runtime
	var c *enterworld.Character
	var deps *enterworld.Deps
	now := time.Unix(1000, 0)
	open := func() {
		authority, err = store.Open(dir, store.Options{DefaultSkills: rewardTestSkillSeeder})
		if err != nil {
			t.Fatal(err)
		}
		deps = &enterworld.Deps{Characters: authority.Characters(), Items: items, Levels: enterworld.NewTextdataLevels(textdata), UpdateCharacter: authority.UpdateCharacter, MutateCharacter: authority.MutateCharacter}
		actions = action.NewRuntime(deps, nil)
		actions.Now = func() time.Time { return now }
		rt, err = NewRuntime(deps, defs, progression.NewRuntime(deps).ExperienceUpdater())
		if err != nil {
			t.Fatal(err)
		}
		rt.PlanInventory = actions.PlanQuestInventory
		actions.AdvanceQuestMinute = rt.AdvanceMinute
		rows := authority.Characters().CharactersForDivision("global-official")
		if len(rows) > 0 {
			c = rows[0]
		}
	}
	open()
	t.Cleanup(func() { authority.Close() })
	level, gold := int64(39), int64(100)
	c = &enterworld.Character{Name: "questclock", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: &level, Gold: &gold, CompletedQuestIds: append([]uint32(nil), def.RequiredQuestIDs...)}
	if err = authority.CreateCharacter("global-official", "quest-clock", c); err != nil {
		t.Fatal(err)
	}
	c = authority.Characters().CharactersForDivision("global-official")[0]
	accepted, err := rt.StartQuest(c, def.Codename)
	if err != nil {
		t.Fatal(err)
	}
	before := c.Snapshot()
	if before.ActiveQuests[0].RemainingMinutes != 120 || before.ActiveQuests[0].Progress != 2<<15 {
		t.Fatal("bad initial duration")
	}
	type entry struct {
		Minute  int    `json:"minute"`
		Opcode  uint16 `json:"opcode"`
		Payload string `json:"payloadHex"`
	}
	fixture := struct {
		Code   string  `json:"code"`
		Frames []entry `json:"frames"`
	}{Code: def.Codename}
	for _, f := range accepted.Frames {
		fixture.Frames = append(fixture.Frames, entry{0, f.Opcode, hex.EncodeToString(f.Payload)})
	}
	actions.BindRecoverySession("global-official", c, 1)
	for minute := 1; minute <= 120; minute++ {
		now = now.Add(time.Minute)
		if minute == 4 {
			zero := int64(0)
			deps.Update(c, "test-death", func() bool { c.CurrentHP = &zero; return true })
		}
		for _, batch := range actions.TickHook()(now.UnixMilli()) {
			for _, f := range batch.Frames {
				if f.Opcode == OpQuestUpdate || f.Opcode == 0x36bf {
					if batch.OnlyCharacterID != c.ID {
						t.Fatal("quest frame leaked")
					}
					fixture.Frames = append(fixture.Frames, entry{minute, f.Opcode, hex.EncodeToString(f.Payload)})
				}
			}
		}
		if minute == 10 {
			if c.ActiveQuests[0].RemainingMinutes != 110 || before.ActiveQuests[0].RemainingMinutes != 120 {
				t.Fatal("counter or detached snapshot changed incorrectly")
			}
			actions.ForgetCharacter("global-official", c.Name)
			now = now.Add(24 * time.Hour)
			actions.TickHook()(now.UnixMilli())
			if c.ActiveQuests[0].RemainingMinutes != 110 {
				t.Fatal("offline quest elapsed")
			}
			authority.Close()
			open()
			if c.ActiveQuests[0].RemainingMinutes != 110 || c.ActiveQuests[0].Progress != packQuestMinutes(110) {
				t.Fatal("restart renewed timer")
			}
			actions.BindRecoverySession("global-official", c, 2)
			actions.BindRecoverySession("global-official", c, 2)
		}
	}
	if len(c.ActiveQuests) != 0 || completionCount(c, def.RefID) != 0 || *c.Gold != 100 {
		t.Fatal("expiry paid rewards or failed removal")
	}
	if _, err = rt.CompleteNpcQuest(c, def.Codename); err == nil {
		t.Fatal("stale reward accepted")
	}
	if _, err = rt.StartQuest(c, def.Codename); err != nil {
		t.Fatal("failed quest cannot be accepted again", err)
	}
	if c.ActiveQuests[0].RemainingMinutes != 120 {
		t.Fatal("fresh acceptance retained expired timer")
	}
	raw, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	const path = "timed_quest_wire_fixture.json"
	if os.Getenv("SRO_WRITE_QUEST_TIMER_FIXTURE") == "1" {
		if err = os.WriteFile(path, raw, 0644); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(stored, raw) {
		t.Fatal("production timed quest wire fixture differs", err)
	}
}

func TestTimedCollectionCleanupAndStaleReward(t *testing.T) {
	licensed.RequireGameData(t)
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint(cancel), func(t *testing.T) {
			rt := testRuntime(t)
			base, _ := rt.Defs.ByCodename("QSP_ALL_POTION_1")
			def := *base
			def.TimeLimitMinutes = 1
			def.TimeoutSymbol = "SN_ABORT"
			def.KindByte = 1
			rt.Defs.byRefID[def.RefID] = &def
			rt.Defs.byCodename[def.Codename] = &def
			c := questCharacter()
			if _, err := rt.StartQuest(c, def.Codename); err != nil {
				t.Fatal(err)
			}
			c.MissionInventory = potionInventory(int64(def.CollectCount + 7))
			rt.NotifyInventoryChanged(c)
			plan := rt.PlanInventory
			rt.PlanInventory = func(*enterworld.Character, []inventory.ItemAmount, []inventory.ItemAmount) ([]enterworld.InventoryRow, []wire.Frame, error) {
				return nil, nil, fmt.Errorf("injected inventory refusal")
			}
			if cancel {
				if _, err := rt.HandleGiveUp(c, wire.NewWriter(4).U32(def.RefID).Payload()); err == nil {
					t.Fatal("cleanup refusal ignored")
				}
			} else {
				rt.AdvanceMinute(c)
				if c.ActiveQuests[0].RemainingMinutes != 0 || objectiveMet(c, &def, c.ActiveQuests[0]) {
					t.Fatal("expired cleanup retry still rewardable")
				}
				if _, err := rt.completeReward(c, &def); err == nil {
					t.Fatal("expired reward accepted")
				}
			}
			rt.PlanInventory = plan
			if cancel {
				if _, err := rt.HandleGiveUp(c, wire.NewWriter(4).U32(def.RefID).Payload()); err != nil {
					t.Fatal(err)
				}
			} else {
				rt.AdvanceMinute(c)
			}
			if len(c.ActiveQuests) != 0 || len(c.MissionInventory) != 0 {
				t.Fatal("cleanup left surplus quest items")
			}
			if _, err := rt.StartQuest(c, def.Codename); err != nil {
				t.Fatal(err)
			}
			if recordProgress(c.ActiveQuests[0]) != 0 {
				t.Fatal("reacceptance retained progress")
			}
		})
	}
}

func TestQuestPackedMinuteByte(t *testing.T) {
	for i := 0; i <= 255; i++ {
		p := packQuestMinutes(uint8(i))
		if int((p>>15)&31)*60+int((p>>20)&63) != i || p&0xfc007fff != 0 {
			t.Fatalf("minute %d -> %x", i, p)
		}
	}
}
