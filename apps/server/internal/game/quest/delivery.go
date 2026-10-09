/*
===========================================================================

delivery.go - selected-NPC quest handoffs and inventory refusals

NPC identity comes from the conversation owner. Delivery grants and progress
commit together, so retrying a full-bag refusal cannot duplicate quest items.

===========================================================================
*/
package quest

import (
	"errors"
	"fmt"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"strings"
)

/*
================
dialogueRefusal

Carries authored client text alongside the underlying inventory failure.
================
*/
type dialogueRefusal struct {
	cause  error
	symbol string
}

/*
================
Error
================
*/
func (e *dialogueRefusal) Error() string {
	return e.cause.Error()
}

/*
================
Unwrap
================
*/
func (e *dialogueRefusal) Unwrap() error {
	return e.cause
}

/*
================
DialogueSymbol
================
*/
func (e *dialogueRefusal) DialogueSymbol() string {
	return e.symbol
}

/*
================
inventoryRefusal

Use the quest's inventory-full line only for an actual capacity failure.
================
*/
func inventoryRefusal(def *Definition, err error) error {
	var fault *inventory.Fault
	if errors.As(err, &fault) && fault.Code == wire.ErrCodeStorageFull && def.InventoryFullSymbol != "" {
		return &dialogueRefusal{err, def.InventoryFullSymbol}
	}
	return err
}

/*
================
AdvanceNpcQuest

Uses action's selection-bound NPC identity. The client cannot substitute an
intermediate delivery NPC or complete a stale tutorial stage.
================
*/
func (rt *Runtime) AdvanceNpcQuest(c *enterworld.Character, code, npc string) (OpResult, error) {
	if strings.HasPrefix(code, captureSupplyPrefix) {
		return rt.finishCaptureSupply(c, code, npc)
	}
	if strings.HasPrefix(code, sideTalkPrefix) {
		return rt.hearSideTalk(c, code, npc)
	}
	if strings.HasPrefix(code, handOverPrefix) {
		return rt.handOverDelivery(c, strings.TrimPrefix(code, handOverPrefix), npc)
	}
	if base, choice, picked := parseRewardChoiceToken(code); picked {
		def, ok := rt.Defs.ByCodename(base)
		if !ok || !questNpcMatches(def, def.EndNpcCodename, npc) {
			return OpResult{}, fmt.Errorf("quest %s wrong reward choice NPC", code)
		}
		return rt.completeRewardChoice(c, def, nil, npc, choice)
	}
	base, stage, staged := parseStageToken(code)
	def, ok := rt.Defs.ByCodename(base)
	if !ok {
		return OpResult{}, fmt.Errorf("unknown NPC quest %s", code)
	}
	if len(def.Stages) > 0 {
		if !staged {
			return OpResult{}, fmt.Errorf("stage-bound NPC confirmation required")
		}
		return rt.completeRewardAt(c, def, &stage, npc)
	}
	if staged {
		return OpResult{}, fmt.Errorf("unexpected quest stage token")
	}
	if def.DeliveryNpcCodename != "" && npc == def.DeliveryNpcCodename {
		return rt.collectDelivery(c, def)
	}
	if !rewardNpcMatches(def, npc) {
		return OpResult{}, fmt.Errorf("quest %s wrong completion NPC", code)
	}
	return rt.CompleteNpcQuest(c, code)
}

/*
================
collectDelivery

Plan the missing delivery items before committing inventory and objectives.
================
*/
func (rt *Runtime) collectDelivery(c *enterworld.Character, def *Definition) (OpResult, error) {
	if rt.PlanInventory == nil {
		return OpResult{}, fmt.Errorf("delivery inventory owner unavailable")
	}
	var refusal error
	var frames []wire.Frame
	changed := rt.deps.Update(c, "quest-delivery", func() bool {
		if c == nil || c.DeletePending || activeQuestIndex(c, def.RefID) < 0 {
			refusal = fmt.Errorf("delivery quest is not active")
			return false
		}
		held := heldCollectCount(c, def)
		if held >= def.CollectCount {
			refusal = fmt.Errorf("delivery item already held")
			return false
		}
		rows, updates, err := rt.PlanInventory(c, nil, []inventory.ItemAmount{{Codename: def.CollectItemCodename, Count: def.CollectCount - held}})
		if err != nil {
			refusal = inventoryRefusal(def, err)
			return false
		}
		c.MissionInventory = rows
		objectives, _ := rt.applyInventoryChange(c)
		frames = append(updates, objectives...)
		return true
	})
	if refusal != nil {
		return OpResult{}, refusal
	}
	if !changed {
		return OpResult{}, fmt.Errorf("delivery character no longer authoritative")
	}
	return OpResult{Frames: frames}, nil
}

// handOverPrefix marks the dialogue token of a two-leg delivery's hand-over.
const handOverPrefix = "hand-over:"

/*
================
handOverToken
================
*/
func handOverToken(code string) string {
	return handOverPrefix + code
}

/*
================
rewardNpcMatches

Where a quest pays: its end NPC, or a two-leg delivery's hand-over NPC
once the hand-over is done (CBasicQuest_vf154 admits every NPC of the
quest's table, and the base talk pays an achieved quest at either).
================
*/
func rewardNpcMatches(def *Definition, npc string) bool {
	return questNpcMatches(def, def.EndNpcCodename, npc) || def.HandOverNpcCodename != "" && def.HandOverNpcCodename == npc
}

/*
================
handOverDelivery

91CA00's hand-over at the mission's NPC: check the bag has room for the
exchange, take the delivered items unless the mission keeps them, give the
exchange back and latch the mission. One inventory transaction; the
journal update and the achieved-now banner follow the latch.
================
*/
func (rt *Runtime) handOverDelivery(c *enterworld.Character, code, npc string) (OpResult, error) {
	def, ok := rt.Defs.ByCodename(code)
	if !ok || def.HandOverNpcCodename == "" || npc != def.HandOverNpcCodename {
		return OpResult{}, fmt.Errorf("quest %s has no hand-over at %s", code, npc)
	}
	if rt.PlanInventory == nil {
		return OpResult{}, fmt.Errorf("hand-over inventory owner unavailable")
	}
	var refusal error
	var frames []wire.Frame
	changed := rt.deps.Update(c, "quest-hand-over", func() bool {
		at := -1
		if c != nil && !c.DeletePending {
			at = activeQuestIndex(c, def.RefID)
		}
		if at < 0 || handedOver(c.ActiveQuests[at]) || !deliveryMet(c, def) {
			refusal = fmt.Errorf("quest %s hand-over is not due", code)
			return false
		}
		var taken []inventory.ItemAmount
		if !def.DeliveryKeepsItems {
			taken = deliveryAmounts(def)
		}
		var given []inventory.ItemAmount
		for _, item := range def.ExchangeItems {
			given = append(given, inventory.ItemAmount{Codename: item.ItemCodename, Count: item.Count})
		}
		rows, updates, err := rt.PlanInventory(c, taken, given)
		if err != nil {
			var fault *inventory.Fault
			if errors.As(err, &fault) && fault.Code == wire.ErrCodeStorageFull && def.ExchangeFullSymbol != "" {
				refusal = &dialogueRefusal{err, def.ExchangeFullSymbol}
			} else {
				refusal = inventoryRefusal(def, err)
			}
			return false
		}
		c.MissionInventory = rows
		previous := c.ActiveQuests[at]
		record := previous
		record.Contents = append([]enterworld.ActiveQuestContentsNode(nil), previous.Contents...)
		record.Contents[0].CompletionReached, record.Contents[0].Kind = true, 0
		record, _ = withJournalTargets(c, def, record)
		c.ActiveQuests[at] = record
		frames = append(updates, missionProgressFrames(def, previous, record)...)
		objectives, _ := rt.applyInventoryChange(c)
		frames = append(frames, objectives...)
		return true
	})
	if refusal != nil {
		return OpResult{}, refusal
	}
	if !changed {
		return OpResult{}, fmt.Errorf("hand-over character no longer authoritative")
	}
	return OpResult{Frames: frames}, nil
}
