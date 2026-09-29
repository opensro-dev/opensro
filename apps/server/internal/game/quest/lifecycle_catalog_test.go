/*
===========================================================================

lifecycle_catalog_test.go - durable acceptance and reward coverage

Discover quests from the production catalog and drive their actual objective
owners. Restart the store between transitions so timer and inventory state
must survive persistence before a reward can be paid.

===========================================================================
*/
package quest

import (
	"bytes"
	"encoding/json"
	"opensro.online/server/internal/testsupport/licensed"
	"path/filepath"
	"slices"
	"testing"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/calendar"
)

/*
================
TestEveryLoadedQuestSurvivesRestartAndCompletesOnce

Unknown mechanics fail explicitly instead of silently taking the talk branch.
Capture objectives must acquire their item and clock through the capture owner.
================
*/
func TestEveryLoadedQuestSurvivesRestartAndCompletesOnce(t *testing.T) {
	licensed.RequireGameData(t)
	textdata := filepath.Join("..", "..", "..", "..", "..", ".generated", "game-data", "1.150", "server", "textdata")
	items := enterworld.NewTextdataItems(textdata)
	defs, err := LoadDefinitions(NewCatalog(textdata), items)
	if err != nil {
		t.Fatal(err)
	}
	if defs.Len() == 0 {
		t.Fatal("production quest catalog unavailable")
	}
	for _, def := range defs.All() {
		t.Run(def.Codename, func(t *testing.T) {
			// Each row has its own store and runtime; bound parallelism with
			// go test -parallel 4 to amortize durable filesystem flush latency.
			t.Parallel()
			if len(def.Stages) > 0 {
				verifyStagedQuestLifecycle(t, defs, items, def)
				return
			}
			dir := filepath.Join(t.TempDir(), "authority")
			var authority *store.Store
			var rt *Runtime
			var character *enterworld.Character
			open := func() {
				var err error
				authority, err = store.Open(dir, store.Options{DefaultSkills: rewardTestSkillSeeder})
				if err != nil {
					t.Fatal(err)
				}
				deps := &enterworld.Deps{Characters: authority.Characters(), Items: items, Levels: enterworld.NewTextdataLevels(textdata), UpdateCharacter: authority.UpdateCharacter, MutateCharacter: authority.MutateCharacter}
				rt, err = NewRuntime(deps, defs, progression.NewRuntime(deps).ExperienceUpdater())
				if err != nil {
					t.Fatal(err)
				}
				rt.PlanInventory = action.NewRuntime(deps, nil).PlanQuestInventory
				rt.CaptureRoll = func() (uint32, error) { return 0, nil }
				// Calendar boundaries and the shared quota have their own
				// discriminating production tests. Keep this restart matrix
				// inside the quest's admitted period.
				rt.CalendarNow = func() calendar.Value {
					if def.DayOrNight == 1 {
						return calendar.Value{Hour: 20}
					}
					return calendar.Value{Hour: 4}
				}
				rows := authority.Characters().CharactersForDivision("global-official")
				if len(rows) > 0 {
					character = rows[0]
				}
			}
			open()
			t.Cleanup(func() { authority.Close() })
			level, gold, experience := max(int64(60), int64(def.Level)), int64(1000), int64(0)
			model := "CHAR_CH_MAN_ADVENTURER"
			if def.CountryByte == 1 {
				model = "CHAR_EU_MAN_NOBLE"
			}
			character = &enterworld.Character{Name: "questmatrix", ModelCodename: model, Level: &level, Gold: &gold, Experience: &experience, CompletedQuestIds: append([]uint32(nil), def.RequiredQuestIDs...)}
			for _, id := range def.RequiredActiveQuestIDs {
				parent, exists := defs.ByRefID(id)
				if !exists {
					t.Fatal("active prerequisite definition missing")
				}
				character.ActiveQuests = append(character.ActiveQuests, BuildActiveQuestRecord(parent, 0))
			}
			if err := authority.CreateCharacter("global-official", "quest-matrix", character); err != nil {
				t.Fatal(err)
			}
			character = authority.Characters().CharactersForDivision("global-official")[0]
			snapshot := func() []byte {
				b, err := json.Marshal(character)
				if err != nil {
					t.Fatal(err)
				}
				return b
			}
			restart := func() {
				before := snapshot()
				authority.Close()
				open()
				if !bytes.Equal(before, snapshot()) {
					t.Fatal("quest/inventory/scalar state changed across restart")
				}
				// The login composer must contain every persisted quest record, not just
				// the starter fixture. It shares the native SQuestInfo body with 31ED.
				payload := enterworld.BuildLocalPlayerEntryPayload(character, &enterworld.LocalPlayerEntry{}, 0, nil)
				for _, record := range character.ActiveQuests {
					encoded := EncodeQuestUpdateInsert(record)[1:]
					if !bytes.Contains(payload, encoded) {
						t.Fatalf("login omitted active quest %d", record.RefID)
					}
				}
			}
			for _, missing := range def.RequiredQuestIDs {
				authority.UpdateCharacter(character, "test-prerequisite", func() bool {
					character.CompletedQuestIds = slices.DeleteFunc(slices.Clone(def.RequiredQuestIDs), func(id uint32) bool { return id == missing })
					return true
				})
				before := snapshot()
				if _, err := rt.StartQuest(character, def.Codename); err == nil {
					t.Fatalf("accepted without predecessor %d", missing)
				}
				if !bytes.Equal(before, snapshot()) {
					t.Fatal("prerequisite refusal mutated authority")
				}
				for _, option := range rt.OptionsForNpc(character, def.StartNpcCodename) {
					if option.Codename == def.Codename {
						t.Fatal("NPC offered missing-prerequisite quest")
					}
				}
			}
			authority.UpdateCharacter(character, "test-prerequisites", func() bool {
				character.CompletedQuestIds = slices.Clone(def.RequiredQuestIDs)
				return true
			})
			assertIneligible := func(reason string) {
				t.Helper()
				before := snapshot()
				if _, err := rt.StartQuest(character, def.Codename); err == nil {
					t.Fatalf("accepted despite %s", reason)
				}
				if !bytes.Equal(before, snapshot()) {
					t.Fatalf("%s refusal mutated authority", reason)
				}
				for _, option := range rt.OptionsForNpc(character, def.StartNpcCodename) {
					if option.Codename == def.Codename {
						t.Fatalf("NPC offered quest despite %s", reason)
					}
				}
			}
			if def.Level > 1 {
				authority.UpdateCharacter(character, "test-low-level", func() bool {
					low := int64(def.Level) - 1
					character.Level = &low
					return true
				})
				assertIneligible("insufficient level")
				authority.UpdateCharacter(character, "test-restore-level", func() bool {
					restored := max(int64(60), int64(def.Level))
					character.Level = &restored
					return true
				})
			}
			if def.CountryByte != 3 {
				authority.UpdateCharacter(character, "test-wrong-country", func() bool {
					character.ModelCodename = "CHAR_EU_MAN_NOBLE"
					if def.CountryByte == 1 {
						character.ModelCodename = "CHAR_CH_MAN_ADVENTURER"
					}
					return true
				})
				assertIneligible("wrong country")
				authority.UpdateCharacter(character, "test-restore-country", func() bool {
					character.ModelCodename = model
					return true
				})
			}
			if def.AcceptanceUnavailable != "" {
				assertIneligible("unresolved native prerequisite")
				// Migration case: a quest accepted by an older unrestricted
				// catalog remains finishable, without allowing new acceptance.
				authority.UpdateCharacter(character, "test-existing-active-quest", func() bool {
					character.ActiveQuests = append(character.ActiveQuests, BuildActiveQuestRecord(def, 0))
					return true
				})
			} else if _, err := rt.StartQuest(character, def.Codename); err != nil {
				t.Fatal(err)
			}
			restart()
			before := snapshot()
			if _, err := rt.StartQuest(character, def.Codename); err == nil {
				t.Fatal("duplicate acceptance")
			}
			if !bytes.Equal(before, snapshot()) {
				t.Fatal("duplicate changed state")
			}
			complete := func() (OpResult, error) {
				if def.EndNpcCodename != "" {
					return rt.AdvanceNpcQuest(character, def.Codename, def.EndNpcCodename)
				}
				// Creation-seeded tutorial has no reconstructed NPC script. This tests
				// persistence/core semantics only; it does not certify that missing script.
				return rt.CompleteTalkQuest(character, def.Codename)
			}
			progress := func(count uint32) {
				authority.UpdateCharacter(character, "test-objective-event", func() bool {
					switch def.Objective {
					case ObjectiveKill:
						for recordProgress(character.ActiveQuests[activeQuestIndex(character, def.RefID)]) < count {
							if _, changed := rt.KillUpdater()(character, def.KillMonsterCodenames[0], fixtureKillRank(def)); !changed {
								t.Fatal("kill did not advance")
							}
						}
					case ObjectiveCollect:
						if rule, capture := captureRuleForQuest(def.Codename); capture {
							if _, changed := rt.CaptureQuestTrap(character, rule.skill, rule.monster, func() bool { return true }); !changed {
								t.Fatal("capture did not grant its item and timer")
							}
							break
						}
						rows, _, err := rt.PlanInventory(character, nil, []inventory.ItemAmount{{Codename: def.CollectItemCodename, Count: count - heldCollectCount(character, def)}})
						if err != nil {
							t.Fatal(err)
						}
						character.MissionInventory = rows
						rt.InventoryUpdater()(character)
					default:
						t.Fatalf("unhandled objective kind %d", def.Objective)
					}
					return true
				})
			}
			switch def.Objective {
			case ObjectiveTalk:
			case ObjectiveDelivery:
				if def.AcceptanceUnavailable == "" && !deliveryMet(character, def) {
					t.Fatal("acceptance did not grant delivery")
				}
				authority.UpdateCharacter(character, "test-delivery-loss", func() bool {
					character.MissionInventory = nil
					rt.InventoryUpdater()(character)
					return true
				})
				restart()
				if _, err := complete(); err == nil {
					t.Fatal("delivery completed without items")
				}
				authority.UpdateCharacter(character, "test-delivery-recovery", func() bool {
					rows, _, err := rt.PlanInventory(character, nil, deliveryAmounts(def))
					if err != nil {
						t.Fatal(err)
					}
					character.MissionInventory = rows
					rt.InventoryUpdater()(character)
					return true
				})
				restart()
			case ObjectiveParallel:
				for i := range def.Objectives {
					if _, err := complete(); err == nil {
						t.Fatal("completed before all parallel objectives")
					}
					m := missionDefinition(def, i)
					authority.UpdateCharacter(character, "test-parallel-objective", func() bool {
						switch m.Objective {
						case ObjectiveKill:
							for recordProgress(missionRecord(character.ActiveQuests[activeQuestIndex(character, def.RefID)], m)) < m.KillCount {
								if _, changed := rt.KillUpdater()(character, m.KillMonsterCodenames[0], fixtureKillRank(m)); !changed {
									t.Fatal("parallel kill did not advance")
								}
							}
						case ObjectiveCollect:
							if rule, capture := captureRuleForQuest(def.Codename); capture && rule.item == m.CollectItemCodename {
								if _, changed := rt.CaptureQuestTrap(character, rule.skill, rule.monster, func() bool { return true }); !changed {
									t.Fatal("parallel capture did not grant its item and timer")
								}
								break
							}
							rows, _, err := rt.PlanInventory(character, nil, []inventory.ItemAmount{{Codename: m.CollectItemCodename, Count: m.CollectCount}})
							if err != nil {
								t.Fatal(err)
							}
							character.MissionInventory = rows
							rt.InventoryUpdater()(character)
						default:
							t.Fatal("unsupported parallel objective")
						}
						return true
					})
					restart()
				}
			case ObjectiveKill, ObjectiveCollect:
				if _, err := complete(); err == nil {
					t.Fatal("completed before objective")
				}
				required := objectiveRequired(def)
				if required > 1 {
					progress(required - 1)
					restart()
					if _, err := complete(); err == nil {
						t.Fatal("partial objective completed")
					}
				}
				if def.DeliveryNpcCodename != "" {
					if _, err := rt.AdvanceNpcQuest(character, def.Codename, def.DeliveryNpcCodename); err != nil {
						t.Fatal(err)
					}
				} else {
					progress(required)
				}
				restart()
			default:
				t.Fatalf("new objective %d needs lifecycle acceptance", def.Objective)
			}
			if _, err := complete(); err != nil {
				t.Fatal(err)
			}
			if len(character.ActiveQuests) != len(def.RequiredActiveQuestIDs) || activeQuestIndex(character, def.RefID) >= 0 || !slices.Contains(character.CompletedQuestIds, def.RefID) {
				t.Fatal("completion state missing")
			}
			if character.Gold == nil || *character.Gold != 1000+def.RewardGold {
				t.Fatal("gold reward mismatch")
			}
			if character.Experience == nil || *character.Experience != def.RewardExp {
				t.Fatal("experience reward mismatch")
			}
			if def.RewardSkillExp > 0 && (character.SkillExp == nil || *character.SkillExp != def.RewardSkillExp%progression.SkillExpPerSP || (character.SkillPoints == nil && def.RewardSkillExp >= progression.SkillExpPerSP) || (character.SkillPoints != nil && *character.SkillPoints != def.RewardSkillExp/progression.SkillExpPerSP)) {
				t.Fatal("skill experience reward mismatch")
			}
			for i := 0; i < missionCount(def); i++ {
				m := missionDefinition(def, i)
				if m.Objective == ObjectiveCollect && heldCollectCount(character, m) != 0 {
					t.Fatal("objective item was not consumed")
				}
			}
			for _, reward := range def.RewardItems {
				var count int64
				for _, row := range character.MissionInventory {
					if row.Codename == reward.ItemCodename {
						count += row.StackCount
					}
				}
				if count != int64(reward.Count) {
					t.Fatalf("reward %s count %d", reward.ItemCodename, count)
				}
			}
			restart()
			before = snapshot()
			if _, err := complete(); err == nil {
				t.Fatal("duplicate reward after login")
			}
			if !bytes.Equal(before, snapshot()) {
				t.Fatal("duplicate reward mutated authority")
			}
			_, err = rt.StartQuest(character, def.Codename)
			canRepeat := def.AcceptanceUnavailable == "" && (def.Repeatable || def.MaxCompletions > 1)
			if (err == nil) != canRepeat {
				t.Fatalf("repeatability=%v error=%v", def.Repeatable, err)
			}
			if canRepeat {
				restart()
			}
		})
	}
}

/*
================
TestQuestWireCapacityRefusesBeforeMutation

Both login lists have byte-sized counts. Refuse overflow without paying rewards.
================
*/
func TestQuestWireCapacityRefusesBeforeMutation(t *testing.T) {
	licensed.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	for i := 0; i < 255; i++ {
		c.ActiveQuests = append(c.ActiveQuests, enterworld.ActiveQuestRecord{RefID: uint32(10000 + i)})
	}
	before, _ := json.Marshal(c)
	if _, err := rt.StartQuest(c, "QNO_CH_SOLDIER_EA1_1"); err == nil {
		t.Fatal("accepted an active quest that cannot survive login")
	}
	after, _ := json.Marshal(c)
	if !bytes.Equal(before, after) {
		t.Fatal("capacity refusal mutated character")
	}
	c.ActiveQuests = c.ActiveQuests[:254]
	if _, err := rt.StartQuest(c, "QNO_CH_SOLDIER_EA1_1"); err != nil {
		t.Fatal(err)
	}
	def, _ := rt.Defs.ByCodename("QNO_CH_SOLDIER_EA1_1")
	c.ActiveQuests = []enterworld.ActiveQuestRecord{BuildActiveQuestRecord(def, def.KillCount)}
	for i := 0; i < 255; i++ {
		c.CompletedQuestIds = append(c.CompletedQuestIds, uint32(10000+i))
	}
	before, _ = json.Marshal(c)
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err == nil {
		t.Fatal("paid completion that cannot survive login")
	}
	after, _ = json.Marshal(c)
	if !bytes.Equal(before, after) {
		t.Fatal("completion capacity refusal paid or removed quest")
	}
	c.CompletedQuestIds = c.CompletedQuestIds[:254]
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err != nil {
		t.Fatal(err)
	}
	if len(c.CompletedQuestIds) != 255 || !slices.Contains(c.CompletedQuestIds, def.RefID) {
		t.Fatal("last wire slot was not usable")
	}
}

/*
================
TestUnimplementedCatalogQuestsArePreservedThroughLogin

Unknown records retain every field through storage, unrelated progress and login.
================
*/
func TestUnimplementedCatalogQuestsArePreservedThroughLogin(t *testing.T) {
	licensed.RequireGameData(t)
	textdata := filepath.Join("..", "..", "..", "..", "..", ".generated", "game-data", "1.150", "server", "textdata")
	catalog := NewCatalog(textdata)
	defs, err := LoadDefinitions(catalog, enterworld.NewTextdataItems(textdata))
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Len() == 0 {
		t.Fatal("production quest catalog unavailable")
	}
	c := questCharacter()
	codes := make([]string, 0, len(catalog.rows))
	for code := range catalog.rows {
		codes = append(codes, code)
	}
	slices.Sort(codes)
	for _, code := range codes {
		row := catalog.rows[code]
		c.CompletedQuestIds = append(c.CompletedQuestIds, row.ID)
		if _, implemented := defs.ByRefID(row.ID); implemented {
			continue
		}
		// Synthetic full-field persistence fixtures, not claims about these quests'
		// actual objectives. Unsupported records belong to their future owner.
		c.ActiveQuests = append(c.ActiveQuests, enterworld.ActiveQuestRecord{RefID: row.ID, U08: 2, U09: 1, Flags: 0x5c, Progress: 123, U10: 8, Contents: []enterworld.ActiveQuestContentsNode{{Tag: 1, Kind: 1, Description: "PERSISTENCE_FIXTURE", ObjectiveValues: []uint32{7, 9}}, {Tag: 2, Kind: 2, Description: "PERSISTENCE_SENTINEL", ObjectiveSentinel: true}}, TargetIds: []uint32{1000 + row.ID}})
	}
	if len(c.ActiveQuests) == 0 || len(c.ActiveQuests) > 255 || len(c.CompletedQuestIds) > 255 {
		t.Fatal("catalog does not fit the declared native login boundary")
	}
	dir := t.TempDir()
	seedCalls := 0
	options := store.Options{
		DefaultSkills: rewardTestSkillSeeder,
		DefaultQuests: func(string) ([]enterworld.ActiveQuestRecord, error) {
			seedCalls++
			return nil, nil
		},
	}
	authority, err := store.Open(dir, options)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.CreateCharacter("global-official", "quest-matrix", c); err != nil {
		authority.Close()
		t.Fatal(err)
	}
	c = authority.Characters().CharactersForDivision("global-official")[0]
	before, _ := json.Marshal(c)
	authority.Close()
	authority, err = store.Open(dir, options)
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	c = authority.Characters().CharactersForDivision("global-official")[0]
	after, _ := json.Marshal(c)
	if !bytes.Equal(before, after) || seedCalls != 0 {
		t.Fatal("login rewrote/reseeded unsupported quests")
	}
	rt, err := NewRuntime(&enterworld.Deps{Characters: authority.Characters(), UpdateCharacter: authority.UpdateCharacter}, defs, testRuntime(t).ApplyExperience)
	if err != nil {
		t.Fatal(err)
	}
	if frames := rt.NotifyInventoryChanged(c); len(frames) != 0 {
		t.Fatal("inventory owner rewrote unknown objectives")
	}
	if frames, changed := rt.KillUpdater()(c, "MOB_CH_GYO", 0); changed || len(frames) != 0 {
		t.Fatal("kill owner rewrote unknown objectives")
	}
	after, _ = json.Marshal(c)
	if !bytes.Equal(before, after) {
		t.Fatal("unsupported records changed")
	}
	payload := enterworld.BuildLocalPlayerEntryPayload(c, &enterworld.LocalPlayerEntry{}, 0, nil)
	for _, q := range c.ActiveQuests {
		if !bytes.Contains(payload, EncodeQuestUpdateInsert(q)[1:]) {
			t.Fatalf("login lost unknown quest %d", q.RefID)
		}
	}
	completed := []byte{byte(len(c.CompletedQuestIds))}
	for _, id := range c.CompletedQuestIds {
		completed = append(completed, u32le(id)...)
	}
	if !bytes.Contains(payload, completed) {
		t.Fatal("login lost completed catalog references")
	}
	t.Logf("catalog=%d executable-definitions=%d unsupported-records-preserved=%d", catalog.Len(), defs.Len(), len(c.ActiveQuests))
}

/*
================
fixtureKillRank

Select an admitted native rank; rank refusal has its own behavioral coverage.
================
*/
func fixtureKillRank(def *Definition) uint8 {
	if len(def.KillRanks) > 0 {
		return def.KillRanks[0]
	}
	return 0
}
