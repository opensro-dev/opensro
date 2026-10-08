/*
===========================================================================

capture_time.go - captured-monster expiry without quest abandonment

Capture minutes advance only while the character is admitted online. Death
and timeout surrender the capture and reopen its objective while preserving
the quest and independent objectives. Inventory cleanup is planned first.

===========================================================================
*/
package quest

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

const captureTimerCorrectionMinutes = 10
const captureLastWarningMinutes = 5

/*
================
captureItemCount

Cleanup uses all bag stacks, independently of the saturated quest counter.
================
*/
func captureItemCount(c *enterworld.Character, code string) uint32 {
	var count uint32
	for _, row := range c.MissionInventory {
		if row.Codename == code && inventory.InBag(c, row.Slot) {
			count += uint32(max(row.StackCount, 1))
		}
	}
	return count
}

/*
================
clearCaptureClock

Expired captures must not become rewardable while a failed item cleanup is
waiting to retry. The persisted zero is shared by death and natural expiry.
================
*/
func clearCaptureClock(c *enterworld.Character, at int) wire.Frame {
	record := c.ActiveQuests[at]
	record.RemainingMinutes = 0
	record.Progress = 0
	record.Flags |= 4
	c.ActiveQuests[at] = record
	record.Flags = 4
	return wire.Frame{Opcode: OpQuestUpdate, Payload: EncodeQuestUpdateUpdate(record)}
}

/*
================
releaseCapture

8B9DDF decrements the capture timer; its zero branch removes the captured
item and restores the mission instead of invoking generic quest abandonment.
================
*/
func (rt *Runtime) releaseCapture(c *enterworld.Character, at int, rule captureRule, symbol string) ([]wire.Frame, bool) {
	count := captureItemCount(c, rule.item)
	if count == 0 && c.ActiveQuests[at].RemainingMinutes == 0 {
		return nil, false
	}
	rows := c.MissionInventory
	var frames []wire.Frame
	if count > 0 {
		if rt.PlanInventory == nil {
			return nil, false
		}
		var err error
		rows, frames, err = rt.PlanInventory(c, []inventory.ItemAmount{{Codename: rule.item, Count: count}}, nil)
		if err != nil {
			return nil, false
		}
	}
	c.MissionInventory = rows
	updates, _ := rt.applyInventoryChange(c)
	frames = append(frames, updates...)
	frames = append(frames, clearCaptureClock(c, at), questNotification(symbol))
	return frames, true
}

/*
================
advanceCaptureMinute

8B9F18 publishes ten- and five-minute warnings. Timer corrections occur on
ten-minute boundaries; the browser counts the intervening seconds locally.
================
*/
func (rt *Runtime) advanceCaptureMinute(c *enterworld.Character, at int, rule captureRule) ([]wire.Frame, bool) {
	record := c.ActiveQuests[at]
	if record.RemainingMinutes == 0 {
		return nil, false
	}
	if rule.quest == "QNO_EU_IVY_1" && rt.SpawnQuestMonster != nil {
		rt.SpawnQuestMonster(c, ivyGuardian, 0, ivyGuardianRadius)
	}
	if record.RemainingMinutes == 1 {
		frames, released := rt.releaseCapture(c, at, rule, rule.expired)
		if released {
			return frames, true
		}
		// A failed cleanup remains expired and non-rewardable. The next pulse
		// retries through the zero-timer/held-item branch in AdvanceMinute.
		return []wire.Frame{clearCaptureClock(c, at)}, true
	}
	record.RemainingMinutes--
	record.Progress = packQuestMinutes(uint16(record.RemainingMinutes))
	record.Flags |= 4
	c.ActiveQuests[at] = record
	var frames []wire.Frame
	if record.RemainingMinutes == captureTimerCorrectionMinutes {
		frames = append(frames, questNotification(rule.tenMinutes))
	}
	if record.RemainingMinutes == captureLastWarningMinutes {
		frames = append(frames, questNotification(rule.fiveMinutes))
	}
	if record.RemainingMinutes%captureTimerCorrectionMinutes == 0 {
		record.Flags = 4
		frames = append(frames, wire.Frame{Opcode: OpQuestUpdate, Payload: EncodeQuestUpdateUpdate(record)})
	}
	return frames, true
}

/*
================
ReleaseCapturesOnDeath

Door-free updater called by the shared fatal transition, before publication.
Every active capture is released in the same character transaction as death.
================
*/
func (rt *Runtime) ReleaseCapturesOnDeath(c *enterworld.Character) ([]wire.Frame, bool) {
	rt.ForgetItemUse(c)
	if c == nil || rt.Defs == nil {
		return nil, false
	}
	var frames []wire.Frame
	changed := false
	for at, record := range c.ActiveQuests {
		def, found := rt.Defs.ByRefID(record.RefID)
		if !found {
			continue
		}
		rule, found := captureRuleForQuest(def.Codename)
		if !found {
			continue
		}
		updates, released := rt.releaseCapture(c, at, rule, rule.death)
		if !released && record.RemainingMinutes > 0 {
			updates = append(updates, clearCaptureClock(c, at))
			released = true
		}
		frames = append(frames, updates...)
		changed = changed || released
	}
	return frames, changed
}
