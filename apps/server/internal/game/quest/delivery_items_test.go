package quest

import (
	"bytes"
	"encoding/json"
	"errors"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"os"
	"reflect"
	"testing"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
)

// These are source-derived candidate contracts, not production admission.
// Travel restrictions and source defects remain separate catalog gates.
func sourceDeliverySpecs(t *testing.T) []QuestSpec {
	t.Helper()
	b, err := os.ReadFile("testdata/delivery-source-contracts.json")
	if err != nil {
		t.Fatal(err)
	}
	var specs []QuestSpec
	if err := json.Unmarshal(b, &specs); err != nil {
		t.Fatal(err)
	}
	if len(specs) != 10 {
		t.Fatalf("unexpected source candidate coverage: %d", len(specs))
	}
	return specs
}

func TestSourceDeliveryInventoryLifecycle(t *testing.T) {
	licensed.RequireGameData(t)
	for _, spec := range sourceDeliverySpecs(t) {
		t.Run(spec.Codename, func(t *testing.T) {
			rt := testRuntime(t)
			def := &Definition{QuestSpec: spec, RefID: 900, ContentsSymbol: "DELIVERY", CountryByte: 3}
			// Exercise inventory independently of the intentionally unresolved
			// travel/chain admission gate; this is not a playable-quest assertion.
			def.AcceptanceUnavailable = ""
			if err := loadDelivery(def, tutorialFixtureItems()); err != nil {
				t.Fatal(err)
			}
			rt.Defs = &Definitions{byRefID: map[uint32]*Definition{900: def}, byCodename: map[string]*Definition{def.Codename: def}, ordered: []*Definition{def}}
			rt.PlanInventory = action.NewRuntime(&enterworld.Deps{Items: tutorialFixtureItems()}, nil).PlanQuestInventory
			c := questCharacter()
			snapshot := func() string {
				b, err := json.Marshal(c)
				if err != nil {
					t.Fatal(err)
				}
				return string(b)
			}
			// Inventory admission failure leaves journal, inventory and scalars intact.
			planner := rt.PlanInventory
			rt.PlanInventory = func(*enterworld.Character, []inventory.ItemAmount, []inventory.ItemAmount) ([]enterworld.InventoryRow, []wire.Frame, error) {
				return nil, nil, errors.New("inventory unavailable")
			}
			before := snapshot()
			if out, err := rt.StartQuest(c, def.Codename); err == nil || len(out.Frames) != 0 || before != snapshot() {
				t.Fatal("failed acceptance partially committed")
			}
			rt.PlanInventory = planner
			for slot := inventory.EquipmentSlotEnd; slot < inventory.BagSlotEnd; slot++ {
				c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: int64(slot), RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", StackCount: 50})
			}
			before = snapshot()
			if _, err := rt.StartQuest(c, def.Codename); err == nil || before != snapshot() {
				t.Fatal("actual full bag accepted delivery or partially committed")
			} else {
				var refusal interface{ DialogueSymbol() string }
				if !errors.As(err, &refusal) || refusal.DialogueSymbol() != def.InventoryFullSymbol {
					t.Fatal("full bag omitted authored NPC refusal")
				}
			}
			c.MissionInventory = nil
			out, err := rt.StartQuest(c, def.Codename)
			if err != nil {
				t.Fatal(err)
			}
			if !deliveryMet(c, def) || len(out.Frames) < 2 || out.Frames[len(out.Frames)-1].Opcode != OpQuestUpdate {
				t.Fatal("grant/journal publication missing")
			}
			if node := c.ActiveQuests[0].Contents[0]; !node.ObjectiveSentinel || node.Kind != 1 {
				t.Fatal("possession incorrectly completes dialogue objective")
			}
			before = snapshot()
			if _, err := rt.StartQuest(c, def.Codename); err == nil || before != snapshot() {
				t.Fatal("duplicate acceptance granted again")
			}
			// Restore from persisted state; no acceptance replay or automatic reissue.
			var restored enterworld.Character
			if err := json.Unmarshal([]byte(before), &restored); err != nil {
				t.Fatal(err)
			}
			c = &restored
			if !deliveryMet(c, def) {
				t.Fatal("delivery lost on restore")
			}
			if _, err := rt.AdvanceNpcQuest(c, def.Codename, "WRONG"); err == nil {
				t.Fatal("wrong NPC accepted delivery")
			}
			held := c.MissionInventory
			if held[0].StackCount > 1 {
				c.MissionInventory = append([]enterworld.InventoryRow(nil), held...)
				c.MissionInventory[0].StackCount--
				if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err == nil {
					t.Fatal("partial delivery rewarded")
				}
				for _, option := range rt.OptionsForNpc(c, def.EndNpcCodename) {
					if option.Codename == def.Codename && option.Complete {
						t.Fatal("partial delivery publishes completion choice")
					}
				}
			}
			c.MissionInventory = nil
			if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err == nil {
				t.Fatal("missing item paid reward")
			}
			if _, err := rt.StartQuest(c, def.Codename); err == nil {
				t.Fatal("loss replayed acceptance")
			}
			c.MissionInventory = held
			// Failed reward application must not consume the delivery.
			apply := rt.ApplyExperience
			rt.ApplyExperience = func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) { return nil, false }
			before = snapshot()
			if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err == nil || before != snapshot() {
				t.Fatal("failed reward consumed delivery")
			}
			rt.ApplyExperience = apply
			if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err != nil {
				t.Fatal(err)
			}
			if len(c.ActiveQuests) != 0 || len(c.CompletedQuestIds) != 1 || len(c.MissionInventory) != 0 {
				t.Fatal("delivery/reward did not commit together")
			}
			if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err == nil {
				t.Fatal("duplicate reward")
			}
			// Abandonment removes every stack, including excess held copies.
			c = questCharacter()
			if _, err := rt.StartQuest(c, def.Codename); err != nil {
				t.Fatal(err)
			}
			c.MissionInventory[0].StackCount++
			if _, err := rt.HandleGiveUp(c, wire.NewWriter(4).U32(def.RefID).Payload()); err != nil {
				t.Fatal(err)
			}
			if len(c.MissionInventory) != 0 || len(c.CompletedQuestIds) != 0 {
				t.Fatal("abandonment retained items or completed quest")
			}
			if _, err := rt.StartQuest(c, def.Codename); err != nil {
				t.Fatal(err)
			}
			if !deliveryMet(c, def) {
				t.Fatal("reacceptance missing grant")
			}
		})
	}
}

func TestDeliveryLoaderRejectsMissingAndCrossObjectiveContracts(t *testing.T) {
	d := &Definition{QuestSpec: QuestSpec{Objective: ObjectiveDelivery, DeliveryItems: []RewardItemLead{{ItemCodename: "MISSING", Count: 1}}, InventoryFullSymbol: "FULL"}}
	if err := loadDelivery(d, tutorialFixtureItems()); err == nil {
		t.Fatal("unresolved item admitted")
	}
	d.Objective = ObjectiveTalk
	if err := loadDelivery(d, tutorialFixtureItems()); err == nil {
		t.Fatal("cross-objective grant admitted")
	}
	if !reflect.DeepEqual(deliveryAmounts(&Definition{}), []inventory.ItemAmount{}) {
		t.Fatal("empty contract has a grant")
	}
}

// This uses the actual SQLite authority and entry composer. Candidate admission
// gates are explicitly removed only to qualify the shared inventory mechanism.
func TestSourceDeliveryAuthorityRestart(t *testing.T) {
	licensed.RequireGameData(t)
	for _, spec := range sourceDeliverySpecs(t) {
		t.Run(spec.Codename, func(t *testing.T) {
			def := &Definition{QuestSpec: spec, RefID: 900, CountryByte: 3, ContentsSymbol: "DELIVERY"}
			def.AcceptanceUnavailable = ""
			if err := loadDelivery(def, tutorialFixtureItems()); err != nil {
				t.Fatal(err)
			}
			defs := &Definitions{byRefID: map[uint32]*Definition{900: def}, byCodename: map[string]*Definition{def.Codename: def}, ordered: []*Definition{def}}
			dir := t.TempDir()
			var authority *store.Store
			var rt *Runtime
			var c *enterworld.Character
			open := func() {
				var err error
				authority, err = store.Open(dir, store.Options{DefaultSkills: rewardTestSkillSeeder})
				if err != nil {
					t.Fatal(err)
				}
				deps := &enterworld.Deps{Items: tutorialFixtureItems(), Levels: enterworld.NewTextdataLevels(gamedatatest.TextdataDir(t)), Characters: authority.Characters(), UpdateCharacter: authority.UpdateCharacter, MutateCharacter: authority.MutateCharacter}
				rt, err = NewRuntime(deps, defs, progression.NewRuntime(deps).ExperienceUpdater())
				if err != nil {
					t.Fatal(err)
				}
				rt.PlanInventory = action.NewRuntime(deps, nil).PlanQuestInventory
				rows := authority.Characters().CharactersForDivision("global-official")
				if len(rows) > 0 {
					c = rows[0]
				}
			}
			open()
			t.Cleanup(func() { authority.Close() })
			c = questCharacter()
			if err := authority.CreateCharacter("global-official", "delivery-fixture", c); err != nil {
				t.Fatal(err)
			}
			c = authority.Characters().CharactersForDivision("global-official")[0]
			restart := func() {
				before, err := json.Marshal(c)
				if err != nil {
					t.Fatal(err)
				}
				authority.Close()
				open()
				after, err := json.Marshal(c)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(before, after) {
					t.Fatal("authority restart changed inventory/journal/reward")
				}
				payload := enterworld.BuildLocalPlayerEntryPayload(c, &enterworld.LocalPlayerEntry{}, 0, nil)
				for _, record := range c.ActiveQuests {
					if !bytes.Contains(payload, EncodeQuestUpdateInsert(record)[1:]) {
						t.Fatal("reconnect entry omitted delivery record")
					}
				}
			}
			if _, err := rt.StartQuest(c, def.Codename); err != nil {
				t.Fatal(err)
			}
			restart()
			if _, err := rt.StartQuest(c, def.Codename); err == nil {
				t.Fatal("reconnect granted duplicate items")
			}
			// Persist actual inventory removal through its owner, then ensure re-login
			// cannot reconstruct the missing item from the active quest alone.
			authority.UpdateCharacter(c, "delivery-loss-fixture", func() bool {
				rows, _, err := rt.PlanInventory(c, deliveryAmounts(def), nil)
				if err != nil {
					t.Fatal(err)
				}
				c.MissionInventory = rows
				rt.InventoryUpdater()(c)
				return true
			})
			restart()
			if deliveryMet(c, def) {
				t.Fatal("reconnect silently reissued missing delivery")
			}
			if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err == nil {
				t.Fatal("lost delivery rewarded after restart")
			}
			if _, err := rt.HandleGiveUp(c, wire.NewWriter(4).U32(def.RefID).Payload()); err != nil {
				t.Fatal(err)
			}
			restart()
			if _, err := rt.StartQuest(c, def.Codename); err != nil {
				t.Fatal(err)
			}
			restart()
			if !deliveryMet(c, def) {
				t.Fatal("reaccept grant lost on disk")
			}
			if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err != nil {
				t.Fatal(err)
			}
			restart()
			if len(c.ActiveQuests) != 0 || !questCompleted(c, def.RefID) || deliveryMet(c, def) {
				t.Fatal("completion not persisted")
			}
			if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err == nil {
				t.Fatal("restart replayed reward")
			}
		})
	}
}
