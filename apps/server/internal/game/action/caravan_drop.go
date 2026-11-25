/*
===========================================================================

caravan_drop.go - an AI trader's parley tribute hits the ground

Port-only, not native (SRO_AI_CARAVANS; #3704 slice 5). The tribute is
the trader's own cargo - the stacks the caravan runtime already took
off the unit - so what lands is `stacks` units of the origin town's
real specialty trade goods (caravanai.TradeGoodsCodename), dropped as
ordinary ground items anyone may pick up and sell, exactly like cargo
spilled from a killed transport. Goods are never conjured: an unknown
codename drops nothing.

===========================================================================
*/
package action

import (
	"time"

	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
DropCaravanCargo

Drops stacks units of the named trade goods at the pose (one ground
item per unit, as cargo spills) and returns how many landed. The
registry's Add allocates each gid; the tick projects the items to
nearby players like any other ground drop.
================
*/
func (rt *Runtime) DropCaravanCargo(division, codename string, stacks int, at simulation.Spawn, droppedBy string) int {
	refs := rt.deps.ItemReferences()
	if refs == nil || stacks <= 0 {
		return 0
	}
	ref, ok := refs.ItemRefByCodename(codename)
	if !ok || ref == nil {
		return 0
	}
	dropped := 0
	for range stacks {
		item := PlanItemDrop(
			inventory.Item{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), Quantity: 1},
			1, at, droppedBy, time.Now(),
		)
		if got := rt.Ground.Add(division, item); got.Gid != 0 {
			dropped++
		}
	}
	return dropped
}
