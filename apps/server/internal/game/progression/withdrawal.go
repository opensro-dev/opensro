/*
===========================================================================

withdrawal.go - commit restoration inventory, learned ranks and refunds

The action owner serializes this operation with casts and effect retirement.
The character door commits the inventory and progression plan together;
responses are emitted only after that commit succeeds.

===========================================================================
*/
package progression

import (
	"math"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

const withdrawalSkillBindingKind = 0x49

/*
================
WithdrawalHooks

Composition installs the existing action owners. The inventory planner
does not mutate; Finish retires the removed skill's resident effects and
updates inventory-dependent quests inside the accepted character door.
================
*/
type WithdrawalHooks struct {
	Lock          func(string) func()
	PlanInventory func(*enterworld.Character, []inventory.ItemAmount, []inventory.ItemAmount) ([]enterworld.InventoryRow, []wire.Frame, error)
	Finish        func(string, *enterworld.Character, uint32) []wire.Frame
}

/*
================
HandleSkillWithdrawal
================
*/
func (rt *Runtime) HandleSkillWithdrawal(division string, c *enterworld.Character, payload []byte) OpResult {
	return rt.handleWithdrawal(division, c, payload, false)
}

/*
================
HandleMasteryWithdrawal
================
*/
func (rt *Runtime) HandleMasteryWithdrawal(division string, c *enterworld.Character, payload []byte) OpResult {
	return rt.handleWithdrawal(division, c, payload, true)
}

/*
================
handleWithdrawal

5169D0/516BB0 explicitly whitelist the potion identities. Mall and Old Woman
restoration use flags 6: consume potions, return all SP, charge no gold.
They do not use the ordinary expendable-item acknowledgement.
================
*/
func (rt *Runtime) handleWithdrawal(division string, c *enterworld.Character, payload []byte, mastery bool) OpResult {
	opcode := wire.OpSkillWithdrawalResponse
	if mastery {
		opcode = wire.OpMasteryWithdrawalResponse
	}
	request, err := wire.DecodeWithdrawalRequest(payload)
	if err != nil || c == nil || rt.Withdrawal.Lock == nil || rt.Withdrawal.PlanInventory == nil || rt.Withdrawal.Finish == nil {
		return withdrawalRefusal(opcode, withdrawalUnavailable)
	}
	items, ok := rt.deps.ItemReferences().(interface {
		ItemRefByID(uint32) (*enterworld.ItemRef, bool)
	})
	if !ok {
		return withdrawalRefusal(opcode, withdrawalUnknown)
	}
	potion, found := items.ItemRefByID(request.PotionID)
	if !found || potion == nil || (!strings.EqualFold(potion.Codename, "ITEM_MALL_SKILL_RESTORATION_POTION") &&
		!strings.EqualFold(potion.Codename, "ITEM_QNO_RM_OLDWOMAN_2_02") &&
		!strings.EqualFold(potion.Codename, "ITEM_QSP_ALL_POTION_1_01")) {
		return withdrawalRefusal(opcode, withdrawalUnknown)
	}
	unlock := rt.Withdrawal.Lock(division)
	defer unlock()
	refusal := withdrawalUnavailable
	var frames []wire.Frame
	committed := rt.deps.Update(c, "skill-withdrawal", func() bool {
		if c.DeletePending {
			return false
		}
		var plan withdrawalPlan
		if mastery {
			plan, refusal = planMasteryWithdrawal(c, rt.deps.SkillData(), rt.deps.LevelData(), request)
		} else {
			catalog, supported := rt.deps.SkillData().(withdrawalCatalog)
			if !supported {
				return false
			}
			plan, refusal = planSkillWithdrawal(c, catalog, request)
		}
		if refusal != 0 {
			return false
		}
		if strings.EqualFold(potion.Codename, "ITEM_QSP_ALL_POTION_1_01") {
			refusal = priceResuscitation(&plan, c, rt.deps.LevelData())
			if refusal != 0 {
				return false
			}
		}
		available := coercePoints(c.SkillPoints)
		if available > math.MaxUint32-plan.Refund {
			refusal = withdrawalUnavailable
			return false
		}
		rows, inventoryFrames, err := rt.Withdrawal.PlanInventory(c, []inventory.ItemAmount{{Codename: potion.Codename, Count: plan.PotionCount}}, nil)
		if err != nil {
			refusal = withdrawalPotions
			return false
		}
		next := c.Snapshot()
		next.MissionInventory = rows
		if plan.Gold != 0 {
			gold := *c.Gold - plan.Gold
			next.Gold = &gold
		}
		available += plan.Refund
		next.SkillPoints = &available
		if mastery {
			next.Masteries = plan.Masteries
		} else {
			next.Skills = plan.Skills
			// Persist shortcut repair in the same transaction. A disconnect
			// before the client's HUD saves must not resurrect an old rank.
			for i := range next.QuickSlots {
				binding := &next.QuickSlots[i]
				if binding.Kind != withdrawalSkillBindingKind || binding.Payload != plan.PreviousSkill {
					continue
				}
				if plan.ReceiptID == plan.PreviousSkill {
					binding.Kind, binding.Payload = 0, 0
				} else {
					binding.Payload = plan.ReceiptID
				}
			}
		}
		stats, err := rt.playerBaseStats(next)
		if err != nil {
			refusal = withdrawalUnavailable
			return false
		}
		c.MissionInventory = next.MissionInventory
		c.SkillPoints = next.SkillPoints
		c.Gold = next.Gold
		c.Masteries = next.Masteries
		c.Skills = next.Skills
		c.QuickSlots = next.QuickSlots
		ack := wire.NewWriter(6).U8(1).U32(plan.ReceiptID)
		if mastery {
			ack.U8(request.Rank)
		}
		frames = append(inventoryFrames, wire.Frame{Opcode: opcode, Payload: ack.Payload()},
			wire.Frame{Opcode: wire.OpPointsUpdate, Payload: wire.EncodePointsSkillUpdate(uint32(available), false)},
			wire.Frame{Opcode: wire.OpBaseStats, Payload: enterworld.BuildLoginStatBlock(next, stats)})
		frames = append(frames, rt.Withdrawal.Finish(division, c, plan.PreviousSkill)...)
		if plan.Gold != 0 {
			frames = append(frames, wire.Frame{Opcode: wire.OpPointsUpdate,
				Payload: wire.GoldRefresh{Balance: uint64(*next.Gold)}.Encode()})
		}
		return true
	})
	if !committed {
		if refusal == 0 {
			refusal = withdrawalUnavailable
		}
		return withdrawalRefusal(opcode, refusal)
	}
	return OpResult{Frames: frames}
}

/*
================
withdrawalRefusal
================
*/
func withdrawalRefusal(opcode uint16, reason uint8) OpResult {
	return OpResult{Frames: []wire.Frame{{Opcode: opcode, Payload: []byte{2, reason}}}}
}
