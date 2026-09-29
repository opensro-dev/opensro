/*
===========================================================================

gather.go - quest tool admission and admitted-character gathering timers

The item owner consumes the tool after admission. This owner retains the
short online countdown and awards the gathered material through inventory
authority. Leaving the world cancels the countdown, matching 8C8770.

===========================================================================
*/
package quest

import (
	"math"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	ivyMaterialQuest         = "QNO_EU_IVY_2"
	ivyKnife                 = "ITEM_QNO_EU_IVY_2_03"
	ivyVine                  = "ITEM_QNO_EU_IVY_2_02"
	ivyGatherSeconds         = 10
	ivyGatherRegion          = 0x655e
	ivyGatherRadius          = 600
	questSecondMs            = 1000
	OpQuestGatherStart       = 0x36bd
	OpQuestGatherCancel      = 0x775d
	OpQuestGatherCancelReply = 0xb75d
)

/*
================
gatherJob

Transient actor state. Persisted quest progress never doubles as a countdown.
================
*/
type gatherJob struct {
	quest     uint32
	remaining uint8
	nextMs    int64
}

/*
================
questToolCleanup

Materials and tools have different identities. Completion and abandonment
discard unused knives without touching the traps awarded by the same quest.
================
*/
func questToolCleanup(c *enterworld.Character, def *Definition) []inventory.ItemAmount {
	if def.Codename != ivyMaterialQuest {
		return nil
	}
	count := captureItemCount(c, ivyKnife)
	if count == 0 {
		return nil
	}
	return []inventory.ItemAmount{{Codename: ivyKnife, Count: count}}
}

/*
================
emptyQuestBagSlot

Native tool admission requires an empty slot before consuming the knife,
even when its resulting material could stack onto an existing inventory row.
================
*/
func emptyQuestBagSlot(c *enterworld.Character) bool {
	var occupied [inventory.BagSlotEnd]bool
	for _, row := range c.MissionInventory {
		if row.Slot >= int64(inventory.EquipmentSlotEnd) && row.Slot < int64(inventory.BagSlotEnd) {
			occupied[row.Slot] = true
		}
	}
	for slot := inventory.EquipmentSlotEnd; slot < inventory.BagSlotEnd; slot++ {
		if !occupied[slot] {
			return true
		}
	}
	return false
}

/*
================
BeginItemUse

8BB6C0 checks the active quest, Pond Ruins radius, material cap and empty
inventory slot. The caller holds the character door and owns tool consumption.
================
*/
func (rt *Runtime) BeginItemUse(c *enterworld.Character, code string, at simulation.Spawn, nowMs int64) ([]wire.Frame, bool) {
	if c == nil || c.DeletePending || code != ivyKnife || rt.PlanInventory == nil || !enterworld.CharacterAlive(c) {
		return nil, false
	}
	def, exists := rt.Defs.ByCodename(ivyMaterialQuest)
	if !exists || activeQuestIndex(c, def.RefID) < 0 {
		return []wire.Frame{questNotification("UIIT_MSG_QUEST_ERR_CANNOT_USE_ITEM")}, false
	}
	center := simulation.Spawn{RegionID: ivyGatherRegion, X: 253, Y: 85, Z: 449}
	distance := simulation.WorldDistance2D(at, center)
	if simulation.IsDungeonRegion(at.RegionID) || math.IsNaN(distance) || math.IsInf(distance, 0) || distance >= ivyGatherRadius {
		return []wire.Frame{questNotification("SN_TALK_QNO_EU_IVY_2_09")}, false
	}
	if heldCollectCount(c, def) >= def.CollectCount {
		return nil, false
	}
	if !emptyQuestBagSlot(c) {
		return []wire.Frame{questNotification("SN_TALK_QNO_EU_IVY_2_15")}, false
	}
	rt.gatherMu.Lock()
	defer rt.gatherMu.Unlock()
	if _, pending := rt.gatherJobs[c.ID]; pending {
		return nil, false
	}
	if rt.gatherJobs == nil {
		rt.gatherJobs = make(map[int64]gatherJob)
	}
	rt.gatherJobs[c.ID] = gatherJob{quest: def.RefID, remaining: ivyGatherSeconds, nextMs: nowMs + questSecondMs}
	return []wire.Frame{{Opcode: OpQuestGatherStart,
		Payload: wire.NewWriter(5).U32(def.RefID).U8(ivyGatherSeconds).Payload()}}, true
}

/*
================
HandleGatherCancel

766950 accepts B75D only for the current quest identity. Cancellation owns
the same short-job lock as its timer, so an acknowledged cancellation cannot
subsequently award a material. A consumed knife is never refunded.
================
*/
func (rt *Runtime) HandleGatherCancel(c *enterworld.Character, payload []byte) (OpResult, error) {
	id, err := DecodeQuestRefRequest(payload)
	if err != nil || c == nil {
		return OpResult{Frames: []wire.Frame{{Opcode: OpQuestGatherCancelReply, Payload: []byte{2, 0}}}}, nil
	}
	rt.gatherMu.Lock()
	defer rt.gatherMu.Unlock()
	job, exists := rt.gatherJobs[c.ID]
	if !exists || job.quest != id {
		return OpResult{Frames: []wire.Frame{{Opcode: OpQuestGatherCancelReply, Payload: []byte{2, 0}}}}, nil
	}
	delete(rt.gatherJobs, c.ID)
	return OpResult{Frames: []wire.Frame{{Opcode: OpQuestGatherCancelReply,
		Payload: wire.NewWriter(5).U8(1).U32(id).Payload()}}}, nil
}

/*
================
ForgetItemUse

Actor teardown cancels the transient countdown; reconnect cannot release it.
================
*/
func (rt *Runtime) ForgetItemUse(c *enterworld.Character) {
	if c == nil {
		return
	}
	rt.gatherMu.Lock()
	delete(rt.gatherJobs, c.ID)
	rt.gatherMu.Unlock()
}

/*
================
AdvanceItemUse

Advance once per admitted second. 8BB5F0 fails rolls below 50, so its success
boundary differs from trap capture. Bag changes during the wait are rechecked.
================
*/
func (rt *Runtime) AdvanceItemUse(c *enterworld.Character, nowMs int64) []wire.Frame {
	if c == nil {
		return nil
	}
	rt.gatherMu.Lock()
	job, pending := rt.gatherJobs[c.ID]
	if !pending || nowMs < job.nextMs {
		rt.gatherMu.Unlock()
		return nil
	}
	job.remaining--
	job.nextMs += questSecondMs
	if job.remaining > 0 {
		rt.gatherJobs[c.ID] = job
		rt.gatherMu.Unlock()
		return nil
	}
	delete(rt.gatherJobs, c.ID)
	rt.gatherMu.Unlock()
	var frames []wire.Frame
	rt.deps.Update(c, "quest-gather", func() bool {
		if c.DeletePending || activeQuestIndex(c, job.quest) < 0 || !enterworld.CharacterAlive(c) {
			return false
		}
		roll := rt.CaptureRoll
		if roll == nil {
			roll = combat.SecureRoll32767
		}
		value, err := roll()
		if err != nil || value > captureMaximumRoll {
			return false
		}
		if value%captureRandomDomain < captureBaseChance {
			frames = []wire.Frame{questNotification("SN_TALK_QNO_EU_IVY_2_10")}
			return false
		}
		rows, updates, err := rt.PlanInventory(c, nil, []inventory.ItemAmount{{Codename: ivyVine, Count: 1}})
		if err != nil {
			frames = []wire.Frame{questNotification("SN_TALK_QNO_EU_IVY_2_15")}
			return false
		}
		c.MissionInventory = rows
		objectives, _ := rt.applyInventoryChange(c)
		frames = append(updates, objectives...)
		return true
	})
	return frames
}
