/*
===========================================================================

giveup.go - abandoning an active quest

The player's give-up (0x71EB) and the GM's /REMOVEQ, /INITQ and /RESETQ end an
active quest the same way: cleanup items leave, the journal record goes and no
completion history is written. abandonActiveLocked is that one transaction
body; each caller owns its checks and its abandon frame.

===========================================================================
*/
package quest

import (
	"fmt"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
HandleGiveUp

Abandon an active, abandonable quest without appending completion history.
Cleanup items and journal removal share one transaction.
================
*/
func (rt *Runtime) HandleGiveUp(character *enterworld.Character, payload []byte) (OpResult, error) {
	refID, err := DecodeQuestRefRequest(payload)
	if err != nil {
		return OpResult{}, err
	}
	def, ok := rt.Defs.ByRefID(refID)
	if !ok {
		return OpResult{}, fmt.Errorf("quest give-up: id %d has no loaded definition", refID)
	}
	if def.KindByte == 2 {
		// The kind-2 window's button composes 0x729A, never 0x71EB
		// (CIFQuestReward_ConfirmReward): a give-up for a reward-kind quest
		// is a crafted frame.
		return OpResult{}, fmt.Errorf("quest give-up: %s (id %d) is kind 2 (reward) - the client cannot compose 0x71EB for it", def.Codename, refID)
	}

	var refusal error
	var inventoryFrames []wire.Frame
	changed := rt.deps.Update(character, "quest-giveup", func() bool {
		if character == nil || character.DeletePending {
			refusal = fmt.Errorf("quest give-up: character unavailable")
			return false
		}
		at := activeQuestIndex(character, refID)
		if at < 0 {
			refusal = fmt.Errorf("quest give-up: %s (id %d) is not active", def.Codename, refID)
			return false
		}
		if len(def.Stages) > 0 && character.ActiveQuests[at].Stage > 0 {
			refusal = fmt.Errorf("quest tutorial already advanced; abandonment would reset paid stages")
			return false
		}
		frames, err := rt.abandonActiveLocked(character, def, at)
		if err != nil {
			refusal = err
			return false
		}
		inventoryFrames = frames
		return true
	})
	if refusal != nil {
		return OpResult{}, refusal
	}
	if !changed {
		return OpResult{}, fmt.Errorf("quest give-up: character is no longer authoritative")
	}
	if def.Codename == ivyMaterialQuest {
		rt.ForgetItemUse(character)
	}
	return OpResult{Frames: append(inventoryFrames, wire.Frame{Opcode: OpQuestUpdate, Payload: EncodeQuestUpdateAbandon(refID)})}, nil
}

/*
================
abandonActiveLocked

Drops the active record at index at without completion history: cleanup items
leave, the record goes, collect progress is recomputed and a voluntary
cancellation gives the period slot back. Returns the inventory frames.
Callers hold the character door and own the abandon frame.
================
*/
func (rt *Runtime) abandonActiveLocked(character *enterworld.Character, def *Definition, at int) ([]wire.Frame, error) {
	rows, frames, err := rt.planQuestCleanup(character, def)
	if err != nil {
		return nil, err
	}
	character.MissionInventory = rows
	next := make([]enterworld.ActiveQuestRecord, 0, len(character.ActiveQuests)-1)
	next = append(next, character.ActiveQuests[:at]...)
	next = append(next, character.ActiveQuests[at+1:]...)
	character.ActiveQuests = next
	objectives, _ := rt.applyInventoryChange(character)
	rt.releasePeriodStart(def)
	return append(frames, objectives...), nil
}
