/*
===========================================================================

capture_test.go - native capture probability and transaction boundaries

Uses the real inventory planner. Failed rolls and failed world retirement
must preserve the complete character; successful capture commits one item,
one timer and one objective update for each supported quest family.

===========================================================================
*/
package quest

import (
	"bytes"
	"encoding/json"
	"testing"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
)

/*
================
captureItems

Minimal item authority with the native nonstacking captured-item contract.
================
*/
type captureItems map[string]*enterworld.ItemRef

/*
================
ItemRefByCodename
================
*/
func (items captureItems) ItemRefByCodename(code string) (*enterworld.ItemRef, bool) {
	ref, found := items[code]
	return ref, found
}

/*
================
captureFixture

Definitions use synthetic IDs so this transaction test needs no retail files.
================
*/
func captureFixture(rule captureRule) (*Runtime, *enterworld.Character) {
	const refID = 1234
	def := &Definition{QuestSpec: QuestSpec{Codename: rule.quest, Objective: ObjectiveCollect,
		CollectItemCodename: rule.item, CollectCount: 1, KindByte: 1},
		RefID: 100, Level: 30, ContentsSymbol: "SN_CAPTURE_TEST", CollectItemRefID: refID}
	defs := &Definitions{ordered: []*Definition{def}, byCodename: map[string]*Definition{rule.quest: def},
		byRefID: map[uint32]*Definition{def.RefID: def}}
	items := captureItems{rule.item: {RefObjID: refID, Codename: rule.item, TypeIDs: [4]int64{3, 3, 9, 0},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 1})}}
	deps := &enterworld.Deps{Items: items}
	rt := &Runtime{deps: deps, Defs: defs, PlanInventory: action.NewRuntime(deps, nil).PlanQuestInventory,
		CaptureRoll: func() (uint32, error) { return 0, nil }}
	c := questCharacter()
	*c.Level = 30
	c.ActiveQuests = []enterworld.ActiveQuestRecord{BuildActiveQuestRecord(def, 0)}
	return rt, c
}

/*
================
TestTrapAdmissionRequiresRunnableQuestAndInventory

Admission is read-only. Reject unavailable promotion scripts, missing quests,
full bags and existing captures before the action lane consumes a trap.
================
*/
func TestTrapAdmissionRequiresRunnableQuestAndInventory(t *testing.T) {
	for _, rule := range captureRules {
		t.Run(rule.quest, func(t *testing.T) {
			rt, c := captureFixture(rule)
			if _, admitted := rt.CanPlaceTrap(c, rule.skill); !admitted {
				t.Fatal("active capture quest refused")
			}
			if _, admitted := rt.CanPlaceTrap(c, "SKILL_QNO_CH_HWAN_1_4_04_01"); admitted {
				t.Fatal("unavailable promotion quest admitted")
			}
			c.ActiveQuests = nil
			if _, admitted := rt.CanPlaceTrap(c, rule.skill); admitted {
				t.Fatal("missing quest admitted")
			}
			def, _ := rt.Defs.ByCodename(rule.quest)
			c.ActiveQuests = []enterworld.ActiveQuestRecord{BuildActiveQuestRecord(def, 0)}
			for slot := inventory.EquipmentSlotEnd; slot < inventory.BagSlotEnd; slot++ {
				c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
					Slot: int64(slot), Codename: "FILLER", RefObjID: 99, StackCount: 1,
				})
			}
			if _, admitted := rt.CanPlaceTrap(c, rule.skill); admitted {
				t.Fatal("full bag admitted")
			}
			c.MissionInventory = []enterworld.InventoryRow{{
				Slot: int64(inventory.EquipmentSlotEnd), Codename: rule.item, RefObjID: 1234, StackCount: 1,
			}}
			before, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			if _, admitted := rt.CanPlaceTrap(c, rule.skill); admitted {
				t.Fatal("held capture admitted")
			}
			after, err := json.Marshal(c)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("admission changed authority", err)
			}
		})
	}
}

/*
================
TestCaptureNativeRollBoundaries

50 itself fails; 100 stays in the native random domain instead of wrapping
to a guaranteed success as a percent-based implementation would do.
================
*/
func TestCaptureNativeRollBoundaries(t *testing.T) {
	for _, test := range []struct {
		level int64
		roll  uint32
		want  bool
	}{
		{30, 49, true}, {30, 50, false}, {30, 100, false}, {30, 101, true},
		{29, 46, true}, {29, 47, false}, {13, 0, false}, {100, 50, false},
	} {
		if got := captureChance(30, test.level, test.roll); got != test.want {
			t.Fatalf("level=%d roll=%d got=%v", test.level, test.roll, got)
		}
	}
}

/*
================
TestCaptureRetiresTargetBeforeAwardAndRejectsReplay
================
*/
func TestCaptureRetiresTargetBeforeAwardAndRejectsReplay(t *testing.T) {
	for _, rule := range captureRules {
		t.Run(rule.quest, func(t *testing.T) {
			rt, c := captureFixture(rule)
			retirements := 0
			retire := func() bool {
				retirements++
				if len(c.MissionInventory) != 0 || c.ActiveQuests[0].RemainingMinutes != 0 {
					t.Fatal("reward committed before world retirement")
				}
				return true
			}
			frames, changed := rt.CaptureQuestTrap(c, rule.skill, rule.monster, retire)
			if !changed || len(frames) == 0 || retirements != 1 || len(c.MissionInventory) != 1 ||
				c.MissionInventory[0].Codename != rule.item || c.ActiveQuests[0].RemainingMinutes != rule.minutes {
				t.Fatalf("capture did not commit: changed=%v frames=%v character=%+v", changed, frames, c)
			}
			before, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			_, changed = rt.CaptureQuestTrap(c, rule.skill, rule.monster, retire)
			after, err := json.Marshal(c)
			if err != nil || changed || retirements != 1 || !bytes.Equal(before, after) {
				t.Fatal("duplicate capture changed authority", err)
			}
		})
	}
}

/*
================
TestCaptureRefusalsLeaveInventoryAndJournalUntouched

Exercise independent failure doors, including an unavailable world target
after inventory planning and a full bag before randomness is consumed.
================
*/
func TestCaptureRefusalsLeaveInventoryAndJournalUntouched(t *testing.T) {
	rule := captureRules[0]
	for _, failure := range []string{"world", "roll", "full", "quest", "target"} {
		t.Run(failure, func(t *testing.T) {
			rt, c := captureFixture(rule)
			monster := rule.monster
			rolls := 0
			rt.CaptureRoll = func() (uint32, error) {
				rolls++
				if failure == "roll" {
					return 50, nil
				}
				return 0, nil
			}
			if failure == "full" {
				for slot := inventory.EquipmentSlotEnd; slot < inventory.BagSlotEnd; slot++ {
					c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
						Slot: int64(slot), RefObjID: 99, Codename: "FILLER", StackCount: 1})
				}
			}
			if failure == "quest" {
				c.ActiveQuests = nil
			}
			if failure == "target" {
				monster = "OTHER_MONSTER"
			}
			before, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			retirements := 0
			_, changed := rt.CaptureQuestTrap(c, rule.skill, monster, func() bool { retirements++; return false })
			after, err := json.Marshal(c)
			if err != nil || changed || !bytes.Equal(before, after) {
				t.Fatal("refused capture mutated character", err)
			}
			if failure == "world" && retirements != 1 || failure != "world" && retirements != 0 {
				t.Fatal("retirement passed the wrong admission door", retirements)
			}
			if failure == "full" && rolls != 0 {
				t.Fatal("full bag consumed capture RNG")
			}
		})
	}
}

/*
================
TestCaptureTimerExpiresWithoutAbandoningQuest

The capture's timer starts at success, survives serialization and releases
the item at zero while leaving its journal entry available for another try.
================
*/
func TestCaptureTimerExpiresWithoutAbandoningQuest(t *testing.T) {
	for _, rule := range captureRules {
		t.Run(rule.quest, func(t *testing.T) {
			rt, c := captureFixture(rule)
			if _, ok := rt.CaptureQuestTrap(c, rule.skill, rule.monster, func() bool { return true }); !ok {
				t.Fatal("capture failed")
			}
			encoded, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			var restored enterworld.Character
			if err := json.Unmarshal(encoded, &restored); err != nil {
				t.Fatal(err)
			}
			c = &restored
			for remaining := int(rule.minutes) - 1; remaining >= 0; remaining-- {
				frames := rt.AdvanceMinute(c)
				if len(c.ActiveQuests) != 1 || int(c.ActiveQuests[0].RemainingMinutes) != remaining {
					t.Fatalf("capture minute %d abandoned or lost timer: %+v", remaining, c.ActiveQuests)
				}
				if remaining == 10 || remaining == 5 || remaining == 0 {
					if len(frames) == 0 {
						t.Fatal("native timer transition was silent", remaining)
					}
				}
			}
			if len(c.MissionInventory) != 0 || len(c.CompletedQuestIds) != 0 {
				t.Fatal("expiry kept captured item or awarded completion")
			}
			def, _ := rt.Defs.ByCodename(rule.quest)
			if objectiveMet(c, def, c.ActiveQuests[0]) {
				t.Fatal("expired capture remained rewardable")
			}
			if frames := rt.AdvanceMinute(c); len(frames) != 0 {
				t.Fatal("expiry replayed", frames)
			}
			if _, ok := rt.CaptureQuestTrap(c, rule.skill, rule.monster, func() bool { return true }); !ok {
				t.Fatal("expiry prevented recapture")
			}
			if _, changed := rt.ReleaseCapturesOnDeath(c); !changed || len(c.MissionInventory) != 0 ||
				c.ActiveQuests[0].RemainingMinutes != 0 {
				t.Fatal("death retained capture")
			}
		})
	}
}
