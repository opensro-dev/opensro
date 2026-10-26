/*
===========================================================================

beginner_test.go - beginner milestones, selection, persistence and replay

Portable fixtures exercise the real quest runtime and inventory planner.
The licensed-data test separately checks the production catalog composition.

===========================================================================
*/
package quest

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
beginnerFixtureItems
================
*/
type beginnerFixtureItems map[string]*enterworld.ItemRef

/*
================
ItemRefByCodename
================
*/
func (items beginnerFixtureItems) ItemRefByCodename(code string) (*enterworld.ItemRef, bool) {
	ref, ok := items[code]
	return ref, ok
}

/*
================
ItemRefByID
================
*/
func (items beginnerFixtureItems) ItemRefByID(id uint32) (*enterworld.ItemRef, bool) {
	for _, ref := range items {
		if ref.RefObjID == id {
			return ref, true
		}
	}
	return nil, false
}

/*
================
beginnerFixture

Use synthetic item references, not source-text mutation or licensed data.
================
*/
func beginnerFixture(t *testing.T) (*Runtime, beginnerFixtureItems) {
	t.Helper()
	content, err := beginnerQuestContent()
	if err != nil {
		t.Fatal(err)
	}
	items := beginnerFixtureItems{}
	for _, m := range content.Milestones {
		if m.Degree == 0 {
			continue
		}
		for _, family := range content.Weapons {
			code := fmt.Sprintf("ITEM_%s_%02d_B_RARE", family, m.Degree)
			country := int64(0)
			if strings.HasPrefix(family, "EU_") {
				country = 1
			}
			items[code] = &enterworld.ItemRef{Codename: code, RefObjID: uint32(len(items) + 1), Country: country, NameStrID: "SN_" + code,
				TypeIDs: [4]int64{3, 1, 6, 2}, NativeFields: enterworld.NewNativeFields(map[string]float64{"itemClass": float64((m.Degree-1)*3 + 2), "rarity": 2})}
		}
	}
	for grade := 1; grade <= 4; grade++ {
		for _, kind := range []string{"HP", "MP"} {
			code := fmt.Sprintf("ITEM_ETC_%s_POTION_%02d", kind, grade)
			items[code] = &enterworld.ItemRef{Codename: code, RefObjID: uint32(len(items) + 1), TypeIDs: [4]int64{3, 3, 1, 1}, NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 50})}
		}
	}
	specs, err := beginnerQuestSpecs(items)
	if err != nil {
		t.Fatal(err)
	}
	defs := &Definitions{byCodename: map[string]*Definition{}, byRefID: map[uint32]*Definition{}}
	for i, row := range beginnerQuestRows(content) {
		country := uint8(0)
		if i >= len(content.Milestones) {
			country = 1
		}
		def := &Definition{QuestSpec: specs[i], RefID: row.ID, Level: row.Level, CountryByte: country, TitleSymbol: row.TitleSymbol, ContentsSymbol: "SN_CON_" + row.Codename}
		defs.ordered = append(defs.ordered, def)
		defs.byCodename[def.Codename], defs.byRefID[def.RefID] = def, def
	}
	rt, err := NewRuntime(&enterworld.Deps{}, defs, func(c *enterworld.Character, exp, sp int64, _ uint32) ([]wire.Frame, bool) {
		if c.Experience == nil {
			c.Experience = new(int64)
		}
		if c.SkillExp == nil {
			c.SkillExp = new(int64)
		}
		*c.Experience += exp
		*c.SkillExp += sp
		return nil, true
	})
	if err != nil {
		t.Fatal(err)
	}
	rt.PlanInventory = action.NewRuntime(&enterworld.Deps{Items: items}, nil).PlanQuestInventory
	return rt, items
}

/*
================
TestBeginnerTrackMilestonesAndRewards
================
*/
func TestBeginnerTrackMilestonesAndRewards(t *testing.T) {
	rt, _ := beginnerFixture(t)
	want := []uint8{8, 10, 16, 20, 24, 30, 32, 40, 42, 50, 52, 60, 64}
	degrees := map[uint8]int{8: 2, 16: 3, 24: 4, 32: 5, 42: 6, 52: 7, 64: 8}
	for country := uint8(0); country <= 1; country++ {
		var levels []uint8
		for _, def := range rt.Defs.All() {
			if def.CountryByte != country {
				continue
			}
			levels = append(levels, def.Level)
			if !def.BoundRewards || def.RewardGold != 0 || def.MaxCompletions != 1 || len(def.RequiredQuests) != 0 || !strings.HasSuffix(def.StartNpcCodename, "_POTION") || def.StartNpcCodename != def.EndNpcCodename {
				t.Fatalf("invalid milestone contract: %+v", def)
			}
			if degree := degrees[def.Level]; degree > 0 {
				if len(def.RewardChoices) != 14 || len(def.RewardItems) != 0 || def.RewardChoiceCheckCountry {
					t.Fatalf("weapon choices: %+v", def)
				}
				for _, choice := range def.RewardChoices {
					if len(choice.Items) != 1 || choice.Items[0].Count != 1 {
						t.Fatal("weapon choice does not grant exactly one")
					}
				}
			} else if len(def.RewardChoices) != 0 || len(def.RewardItems) != 2 {
				t.Fatal("support milestone must grant HP and MP potions")
			}
		}
		if !reflect.DeepEqual(levels, want) {
			t.Fatalf("country %d levels %v", country, levels)
		}
	}
}

/*
================
TestBeginnerWeaponSelectionAndReplay

Run every choice for both routes through acceptance, real fatal-hit progress,
NPC completion, a persisted round trip and replay refusal.
================
*/
func TestBeginnerWeaponSelectionAndReplay(t *testing.T) {
	rt, _ := beginnerFixture(t)
	for _, def := range rt.Defs.All() {
		if len(def.RewardChoices) == 0 {
			continue
		}
		for choice := range def.RewardChoices {
			t.Run(fmt.Sprintf("%s/%d", def.Codename, choice), func(t *testing.T) {
				level := int64(def.Level) - 1
				c := &enterworld.Character{Name: "beginner", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: &level}
				if def.CountryByte == 1 {
					c.ModelCodename = "CHAR_EU_MAN_NOBLE"
				}
				if _, err := rt.StartQuest(c, def.Codename); err == nil {
					t.Fatal("accepted below milestone")
				}
				level++
				if _, err := rt.StartQuest(c, def.Codename); err != nil {
					t.Fatal(err)
				}
				token := rewardChoiceToken(def.Codename, choice)
				options := rewardChoiceOptions(def, int(def.CountryByte))
				if len(options) != 14 || options[choice].PromptSymbol != def.RewardChoices[choice].CompletePromptSymbol || options[choice].PromptSymbol == "" {
					t.Fatal("selected weapon has no exact confirmation")
				}
				if _, err := rt.AdvanceNpcQuest(c, token, def.EndNpcCodename); err == nil {
					t.Fatal("paid before objective")
				}
				rt.KillUpdater()(c, "MOB_UNRELATED", 0)
				if recordProgress(c.ActiveQuests[0]) != 0 {
					t.Fatal("unrelated kill counted")
				}
				for n := uint32(0); n < def.KillCount; n++ {
					rt.KillUpdater()(c, def.KillMonsterCodenames[0], 0)
				}
				if _, err := rt.AdvanceNpcQuest(c, token, "NPC_CH_SMITH"); err == nil {
					t.Fatal("wrong NPC paid reward")
				}
				if _, err := rt.AdvanceNpcQuest(c, rewardChoiceToken(def.Codename, 999), def.EndNpcCodename); err == nil {
					t.Fatal("invalid choice paid")
				}
				if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err == nil {
					t.Fatal("reward window bypassed selection")
				}
				if _, err := rt.AdvanceNpcQuest(c, token, def.EndNpcCodename); err != nil {
					t.Fatal(err)
				}
				if len(c.MissionInventory) != 1 {
					t.Fatalf("reward rows: %+v", c.MissionInventory)
				}
				row := c.MissionInventory[0]
				if !row.CharacterBound || row.StackCount != 1 || row.Codename != def.RewardChoices[choice].Items[0].ItemCodename {
					t.Fatalf("wrong reward: %+v", row)
				}
				data, err := json.Marshal(c)
				if err != nil {
					t.Fatal(err)
				}
				var restored enterworld.Character
				if err := json.Unmarshal(data, &restored); err != nil {
					t.Fatal(err)
				}
				before := restored.Snapshot()
				if _, err := rt.StartQuest(&restored, def.Codename); err == nil {
					t.Fatal("reaccepted completed milestone")
				}
				if _, err := rt.AdvanceNpcQuest(&restored, token, def.EndNpcCodename); err == nil {
					t.Fatal("replayed reward")
				}
				if !reflect.DeepEqual(before.MissionInventory, restored.MissionInventory) || *before.Experience != *restored.Experience {
					t.Fatal("replay mutated rewards")
				}
			})
		}
	}
}

/*
================
TestBeginnerFullBagAndSupportBinding
================
*/
func TestBeginnerFullBagAndSupportBinding(t *testing.T) {
	rt, _ := beginnerFixture(t)
	level := int64(64)
	c := &enterworld.Character{Name: "fullbag", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: &level}
	def, _ := rt.Defs.ByCodename("QPORT_BEGINNER_CH_10")
	if _, err := rt.StartQuest(c, def.Codename); err != nil {
		t.Fatal(err)
	}
	for n := uint32(0); n < def.KillCount; n++ {
		rt.KillUpdater()(c, def.KillMonsterCodenames[0], 0)
	}
	for slot := int64(13); slot < int64(domain.DefaultInventorySize); slot++ {
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: slot, RefObjID: 999, StackCount: 1})
	}
	before := c.Snapshot()
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err == nil {
		t.Fatal("full bag accepted")
	}
	if !reflect.DeepEqual(before.MissionInventory, c.MissionInventory) || len(c.CompletedQuestIds) != 0 || c.Experience != nil {
		t.Fatal("full bag partially paid")
	}
	c.MissionInventory = nil
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err != nil {
		t.Fatal(err)
	}
	if len(c.MissionInventory) != 2 {
		t.Fatalf("support rewards: %+v", c.MissionInventory)
	}
	for _, row := range c.MissionInventory {
		if !row.CharacterBound || row.StackCount != 50 {
			t.Fatalf("unbound support reward: %+v", row)
		}
	}
}

/*
================
TestBeginnerQuestsProductionFlag

The real loader, media references and actor identities must
resolve with the switch on; switching off excludes every custom definition.
================
*/
func TestBeginnerQuestsProductionFlag(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(dir)
	for _, setting := range []string{"off", "on"} {
		t.Run(setting, func(t *testing.T) {
			t.Setenv(EnvBeginnerQuests, setting)
			defs, err := LoadDefinitions(NewCatalog(dir), items)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, def := range defs.All() {
				if !def.BoundRewards {
					continue
				}
				count++
				for _, code := range append([]string{def.StartNpcCodename}, def.KillMonsterCodenames...) {
					if id, ok := items.CharacterRefObjIDByCodename(code); !ok || id == 0 {
						t.Fatalf("unresolved beginner actor %s", code)
					}
				}
			}
			want := 0
			if setting == "on" {
				want = 26
			}
			if count != want {
				t.Fatalf("loaded %d beginner definitions; want %d", count, want)
			}
		})
	}
}

/*
================
TestBeginnerQuestSwitch
================
*/
func TestBeginnerQuestSwitch(t *testing.T) {
	for _, setting := range []string{"", "off", "false", "0", "on", "true", "1", " ON ", "invalid"} {
		t.Setenv(EnvBeginnerQuests, setting)
		on, err := BeginnerQuestsFromEnv()
		want := setting == "on" || setting == "true" || setting == "1" || setting == " ON "
		if on != want || (err != nil) != (setting == "invalid") {
			t.Fatalf("%q: %v, %v", setting, on, err)
		}
	}
}

/*
================
TestBeginnerWeaponReferencesMustResolveAsMoonOfTheRequestedDegree
================
*/
func TestBeginnerWeaponReferencesMustResolveAsMoonOfTheRequestedDegree(t *testing.T) {
	for _, invalid := range []string{"missing", "sun", "degree"} {
		t.Run(invalid, func(t *testing.T) {
			_, items := beginnerFixture(t)
			code := "ITEM_CH_SWORD_02_B_RARE"
			switch invalid {
			case "missing":
				delete(items, code)
			case "sun":
				items[code].NativeFields = items[code].NativeFields.With("itemClass", 6)
			case "degree":
				items[code].NativeFields = items[code].NativeFields.With("itemClass", 8)
			}
			if _, err := beginnerQuestSpecs(items); err == nil {
				t.Fatal("invalid weapon reference was accepted")
			}
		})
	}
}
