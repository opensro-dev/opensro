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
	"slices"
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
rewardFullDefinition

The definition whose bag-full line answers a refused reward.
================
*/
func rewardFullDefinition(def *Definition) *Definition {
	if def.RewardFullSymbol == "" {
		return def
	}
	d := *def
	d.InventoryFullSymbol = def.RewardFullSymbol
	return &d
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
(CBasicQuest_vf154 admits every NPC of the quest's table, and the base
talk pays an achieved quest at either). This only names the NPC: payment
still needs objectiveMet, which for a two-leg delivery is the hand-over
latch, so the hand-over NPC cannot pay before the hand-over.
================
*/
func rewardNpcMatches(def *Definition, npc string) bool {
	if questNpcMatches(def, def.EndNpcCodename, npc) {
		return true
	}
	for _, m := range deliveryMissions(def) {
		if m.HandOverNpcCodename != "" && m.HandOverNpcCodename == npc {
			return true
		}
	}
	return false
}

/*
================
pendingHandOver

The delivery mission npc still has to take, and its node in record: the
first not yet handed over (a parallel quest's missions each name their own
NPC, so at most one matches).
================
*/
func pendingHandOver(record enterworld.ActiveQuestRecord, def *Definition, npc string) (*Definition, int) {
	for _, m := range deliveryMissions(def) {
		if m.HandOverNpcCodename == "" || m.HandOverNpcCodename != npc {
			continue
		}
		at := missionNodeIndex(record.Contents, m.missionIndex+1, m.ContentsSymbol)
		if at < 0 || missionCompletionReached(record.Contents[at]) {
			continue
		}
		return m, at
	}
	return nil, -1
}

/*
================
handOverRow

91CA00 at a delivery mission's own NPC: the hand-over row while its items
are held, its not-delivered line while they are missing. handled reports
that the NPC belongs to a pending hand-over, so no other row of the quest
is offered there.
================
*/
func handOverRow(c *enterworld.Character, def *Definition, npc string) (*NpcOption, bool) {
	at := activeQuestIndex(c, def.RefID)
	if at < 0 {
		return nil, false
	}
	m, _ := pendingHandOver(c.ActiveQuests[at], def, npc)
	if m == nil {
		return nil, false
	}
	if deliveryMet(c, m) {
		return &NpcOption{Codename: handOverToken(def.Codename), TitleSymbol: def.TitleSymbol, PromptSymbol: m.HandOverSymbol,
			Pages: m.HandOverPages, Complete: true}, true
	}
	if m.NotAchievedSymbol != "" {
		return &NpcOption{Codename: def.Codename, TitleSymbol: def.TitleSymbol, PromptSymbol: m.NotAchievedSymbol, Informational: true}, true
	}
	return nil, true
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
	if !ok || !slices.ContainsFunc(deliveryMissions(def), func(m *Definition) bool { return m.HandOverNpcCodename != "" && m.HandOverNpcCodename == npc }) {
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
		// The latch lives in the mission's own node (91CEB0: one bit of
		// quest-user +3 per mission); a record without that node is refused,
		// never indexed.
		var m *Definition
		node := -1
		if at >= 0 {
			m, node = pendingHandOver(c.ActiveQuests[at], def, npc)
		}
		if m == nil || !deliveryMet(c, m) {
			refusal = fmt.Errorf("quest %s hand-over is not due", code)
			return false
		}
		var taken []inventory.ItemAmount
		if !m.DeliveryKeepsItems {
			taken = deliveryAmounts(m)
		}
		var given []inventory.ItemAmount
		for _, item := range m.ExchangeItems {
			given = append(given, inventory.ItemAmount{Codename: item.ItemCodename, Count: item.Count})
		}
		rows, updates, err := rt.PlanInventory(c, taken, given)
		if err != nil {
			var fault *inventory.Fault
			if errors.As(err, &fault) && fault.Code == wire.ErrCodeStorageFull && m.ExchangeFullSymbol != "" {
				refusal = &dialogueRefusal{err, m.ExchangeFullSymbol}
			} else {
				// Without its own +0xC8 line the quest's bag-full word answers.
				refusal = inventoryRefusal(def, err)
			}
			return false
		}
		c.MissionInventory = rows
		previous := c.ActiveQuests[at]
		record := previous
		record.Contents = append([]enterworld.ActiveQuestContentsNode(nil), previous.Contents...)
		record.Contents[node].CompletionReached, record.Contents[node].Kind = true, 0
		record, _ = withJournalTargets(c, def, record)
		c.ActiveQuests[at] = record
		frames = append(updates, missionProgressFrames(def, previous, record)...)
		frames = append(frames, pendingNotices(def, record)...)
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

/*
================
pendingNotices

After a hand-over, each parallel delivery still waiting sends its +0x108
line ("Doji's medicine has been delivered. Deliver Bori's book to Chau.").
================
*/
func pendingNotices(def *Definition, record enterworld.ActiveQuestRecord) []wire.Frame {
	var frames []wire.Frame
	for i := range def.Objectives {
		spec := def.Objectives[i]
		if spec.Objective != ObjectiveDelivery || spec.PendingNoticeSymbol == "" {
			continue
		}
		at := missionNodeIndex(record.Contents, uint8(i)+1, spec.ContentsSymbol)
		if at >= 0 && !missionCompletionReached(record.Contents[at]) {
			frames = append(frames, questNotification(spec.PendingNoticeSymbol))
		}
	}
	return frames
}
