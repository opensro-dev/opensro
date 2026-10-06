/*
===========================================================================

operator_grant.go - live operator item grants through the inventory authority

The private operator endpoint owns authentication and replay protection.
This owner resolves authored items, plans the whole batch, commits once, and
publishes ordinary inventory receipts without retiring the player's session.

===========================================================================
*/
package action

import (
	"fmt"

	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

const maxOperatorItemGrants = 32

/*
================
OperatorGrantItems

Use the reward planner so stack merging, capacity and reference publication
have one implementation. No partial grant survives a refused batch.
================
*/
func (rt *Runtime) OperatorGrantItems(division, name string, grants []inventory.ItemAmount) error {
	if len(grants) == 0 || len(grants) > maxOperatorItemGrants {
		return fmt.Errorf("invalid item grant batch")
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil {
		return fmt.Errorf("character not found")
	}
	if rt.Stalls.Keeping(division, c.Name) || rt.Exchanges.Trading(division, c.Name) {
		return fmt.Errorf("inventory is locked by a stall or exchange")
	}
	var frames []wire.Frame
	var planErr error
	if !rt.deps.Update(c, "operator-grant-items", func() bool {
		rows, receipts, err := rt.PlanQuestInventory(c, nil, grants)
		if err != nil {
			planErr = err
			return false
		}
		c.MissionInventory = rows
		frames = receipts
		return true
	}) {
		if planErr != nil {
			return planErr
		}
		return fmt.Errorf("item grant refused")
	}
	if rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, c.Name, frames)
	}
	return nil
}
