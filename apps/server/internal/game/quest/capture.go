/*
===========================================================================

capture.go - native trap outcomes under the character mutation door

The skill-object owner selects and retires a trap. This owner plans the
captured item and retires the target before publishing inventory or journal
changes. Failed world retirement cannot award an item.

===========================================================================
*/
package quest

import (
	"errors"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

const (
	captureRandomDomain     = 101
	captureBaseChance       = 50
	captureLevelPenalty     = 3
	captureMaximumRoll      = 32767
	questNotificationOpcode = 0x36bf
)

/*
================
captureRule

Native handlers share a transaction but retain their item, timer and text.
Ivy's trap comes from quest two; its capture belongs to quest one (8BA700).
================
*/
type captureRule struct {
	quest, skill, monster, item             string
	minutes                                 uint8
	success, full, already, failure         string
	expired, death, tenMinutes, fiveMinutes string
}

var captureRules = []captureRule{
	{
		quest: "QNO_EU_EASTEU_14_1", skill: "SKILL_QNO_EU_EASTEU_14_02_01",
		monster: "MOB_EU_LION_CLON", item: "ITEM_QNO_EU_EASTEU_14_1_02", minutes: 20,
		success: "SN_TALK_QNO_EU_EASTEU_14_1_08", full: "SN_TALK_QNO_EU_EASTEU_14_1_10",
		already: "SN_TALK_QNO_EU_EASTEU_14_1_18", failure: "SN_TALK_QNO_EU_EASTEU_14_1_20",
		expired: "SN_TALK_QNO_EU_EASTEU_14_1_15", death: "SN_TALK_QNO_EU_EASTEU_14_1_11",
		tenMinutes: "SN_TALK_QNO_EU_EASTEU_14_1_16", fiveMinutes: "SN_TALK_QNO_EU_EASTEU_14_1_17",
	},
	{
		quest: "QNO_EU_GENERAL_1", skill: "SKILL_QNO_EU_GENERAL_1_01_01",
		monster: "MOB_AM_ROGUE", item: "ITEM_QNO_EU_GENERAL_1_02", minutes: 20,
		success: "SN_TALK_QNO_EU_GENERAL_1_11", full: "SN_TALK_QNO_EU_GENERAL_1_12",
		already: "SN_TALK_QNO_EU_GENERAL_1_17", failure: "SN_TALK_QNO_EU_GENERAL_1_19",
		expired: "SN_TALK_QNO_EU_GENERAL_1_14", death: "SN_TALK_QNO_EU_GENERAL_1_13",
		tenMinutes: "SN_TALK_QNO_EU_GENERAL_1_15", fiveMinutes: "SN_TALK_QNO_EU_GENERAL_1_16",
	},
	{
		quest: "QNO_EU_IVY_1", skill: "SKILL_QNO_EU_IVY_2_01_01",
		monster: "MOB_QT_01_PUNISHER_CLON", item: "ITEM_QNO_EU_IVY_1_01", minutes: 30,
		success: "SN_TALK_QNO_EU_IVY_1_07", full: "SN_TALK_QNO_EU_IVY_1_08",
		already: "SN_TALK_QNO_EU_IVY_1_13", failure: "SN_TALK_QNO_EU_IVY_1_17",
		expired: "SN_TALK_QNO_EU_IVY_1_10", death: "SN_TALK_QNO_EU_IVY_1_09",
		tenMinutes: "SN_TALK_QNO_EU_IVY_1_11", fiveMinutes: "SN_TALK_QNO_EU_IVY_1_12",
	},
	{
		quest: "QNO_CA_GORIA_6", skill: "SKILL_QNO_CA_GORIA_6_01_01",
		monster: "MOB_QT_01_HUNARCHER_CLON", item: "ITEM_QNO_CA_GORIA_6_02", minutes: 20,
		success: "SN_TALK_QNO_CA_GORIA_6_12", full: "SN_TALK_QNO_CA_GORIA_6_13",
		already: "SN_TALK_QNO_CA_GORIA_6_18", failure: "SN_TALK_QNO_CA_GORIA_6_20",
		expired: "SN_TALK_QNO_CA_GORIA_6_15", death: "SN_TALK_QNO_CA_GORIA_6_14",
		tenMinutes: "SN_TALK_QNO_CA_GORIA_6_16", fiveMinutes: "SN_TALK_QNO_CA_GORIA_6_17",
	},
}

/*
================
captureRuleForQuest

Capture timers belong to the quest, not the similarly named supplying item.
================
*/
func captureRuleForQuest(code string) (captureRule, bool) {
	for _, rule := range captureRules {
		if rule.quest == code {
			return rule, true
		}
	}
	return captureRule{}, false
}

/*
================
questNotification

36BF resolves localized notification text, independently of NPC dialogue.
================
*/
func questNotification(symbol string) wire.Frame {
	return wire.Frame{Opcode: questNotificationOpcode,
		Payload: wire.NewWriter(len(symbol) + 2).U16(uint16(len(symbol))).Bytes([]byte(symbol)).Payload()}
}

/*
================
captureChance

8BA7E1..8BA823 uses rand modulo 101 and strict less-than. Higher levels
never improve the base chance; sufficiently low levels cannot succeed.
================
*/
func captureChance(required uint8, level int64, roll uint32) bool {
	penalty := max(int64(required)-max(level, 0), 0) * captureLevelPenalty
	return int64(roll%captureRandomDomain) < captureBaseChance-penalty
}

/*
================
CanPlaceTrap

The item lane must have a runnable quest before it consumes a trap. Native
8B05B0 checks inventory space and an existing capture for the lion family.
Inference: apply that admission contract to the shared capture owner; unavailable
promotion quests never create an object that cannot deliver an outcome.
================
*/
func (rt *Runtime) CanPlaceTrap(c *enterworld.Character, skill string) ([]wire.Frame, bool) {
	if c == nil || c.DeletePending || !enterworld.CharacterAlive(c) || rt.Defs == nil || rt.PlanInventory == nil {
		return nil, false
	}
	for _, rule := range captureRules {
		if rule.skill != skill {
			continue
		}
		def, exists := rt.Defs.ByCodename(rule.quest)
		if !exists || activeQuestIndex(c, def.RefID) < 0 {
			return []wire.Frame{questNotification("UIIT_MSG_QUEST_ERR_CANNOT_USE_ITEM")}, false
		}
		if !emptyQuestBagSlot(c) {
			return []wire.Frame{questNotification(rule.full)}, false
		}
		if captureItemCount(c, rule.item) > 0 {
			return []wire.Frame{questNotification(rule.already)}, false
		}
		return nil, true
	}
	return nil, false
}

/*
================
CaptureQuestTrap

Door-free updater. The caller holds division and character authority. Target
retirement follows all fallible quest checks and precedes character writes.
================
*/
func (rt *Runtime) CaptureQuestTrap(c *enterworld.Character, skill, monster string, retire func() bool) ([]wire.Frame, bool) {
	if c == nil || c.DeletePending || !enterworld.CharacterAlive(c) || rt.Defs == nil || rt.PlanInventory == nil || retire == nil {
		return nil, false
	}
	for _, rule := range captureRules {
		if rule.skill != skill || rule.monster != monster {
			continue
		}
		def, exists := rt.Defs.ByCodename(rule.quest)
		if !exists {
			return nil, false
		}
		at := activeQuestIndex(c, def.RefID)
		if at < 0 {
			return nil, false
		}
		rows, frames, err := rt.PlanInventory(c, nil, []inventory.ItemAmount{{Codename: rule.item, Count: 1}})
		if err != nil {
			var fault *inventory.Fault
			if errors.As(err, &fault) && fault.Code == wire.ErrCodeStorageFull {
				return []wire.Frame{questNotification(rule.full)}, false
			}
			return nil, false
		}
		if c.ActiveQuests[at].RemainingMinutes > 0 {
			return []wire.Frame{questNotification(rule.already)}, false
		}
		for _, row := range c.MissionInventory {
			if row.Slot >= int64(inventory.EquipmentSlotEnd) && row.Slot < int64(inventory.BagSlotEnd) && row.Codename == rule.item {
				return []wire.Frame{questNotification(rule.already)}, false
			}
		}
		roll := rt.CaptureRoll
		if roll == nil {
			roll = combat.SecureRoll32767
		}
		value, err := roll()
		if err != nil || value > captureMaximumRoll {
			return nil, false
		}
		level := int64(1)
		if c.Level != nil {
			level = *c.Level
		}
		if !captureChance(def.Level, level, value) {
			return []wire.Frame{questNotification(rule.failure)}, false
		}
		if !retire() {
			return nil, false
		}
		c.MissionInventory = rows
		updates, _ := rt.applyInventoryChange(c)
		frames = append(frames, updates...)
		record := c.ActiveQuests[at]
		record.RemainingMinutes = rule.minutes
		record.Progress = packQuestMinutes(rule.minutes)
		record.Flags |= 4
		c.ActiveQuests[at] = record
		record.Flags = 4
		frames = append(frames, wire.Frame{Opcode: OpQuestUpdate, Payload: EncodeQuestUpdateUpdate(record)}, questNotification(rule.success))
		return frames, true
	}
	return nil, false
}
