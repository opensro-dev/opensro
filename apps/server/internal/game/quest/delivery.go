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
	if !questNpcMatches(def, def.EndNpcCodename, npc) {
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
