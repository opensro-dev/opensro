/*
===========================================================================

serveractor_items.go - AI caravan actors drink real potions (port-only)

An AI caravan actor uses an item the way a player does: the item sits in
its bag and goes through HandleItemUse, so cooldowns, amounts, the potion
pulses, the buff and the effect every observer sees are the native ones.
An actor out of a supply is restocked with a stack first: the agents
carry what a player would carry, the runtime decides when they drink.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"slices"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// serverActorSupplyStack is the stack an actor is restocked with.
const serverActorSupplyStack = 50

/*
================
ServerActorUseItem

The actor named uses one codename item from its bag (a pet item on its
transport). The result's frames are for observers; a refusal (cooldown,
full gauge, no transport) changes nothing.
================
*/
func (rt *Runtime) ServerActorUseItem(division, name, codename string) OpResult {
	c := rt.findCharacter(division, name)
	refs := rt.deps.ItemReferences()
	if c == nil || !c.RuntimeOnly || refs == nil {
		return OpResult{}
	}
	ref, ok := refs.ItemRefByCodename(codename)
	if !ok || ref == nil {
		return OpResult{}
	}
	slot, flags := int64(-1), ref.TypeFlags()
	rt.deps.Mutate(c, "caravan supplies", func() {
		used := make([]int64, 0, len(c.MissionInventory))
		for _, row := range c.MissionInventory {
			if row.Codename == codename && row.StackCount > 0 {
				slot = row.Slot
				return
			}
			used = append(used, row.Slot)
		}
		for s := int64(inventory.EquipmentSlotEnd); inventory.InBag(c, s); s++ {
			if !slices.Contains(used, s) {
				slot = s
				c.MissionInventory = append(c.MissionInventory, domain.InventoryRow{Slot: s, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: flags, StackCount: serverActorSupplyStack})
				return
			}
		}
	})
	if slot < 0 {
		return OpResult{}
	}
	payload := binary.LittleEndian.AppendUint16([]byte{uint8(slot)}, flags)
	if _, pet := petPotionLane(ref.TypeIDs[3]); pet && ref.TypeIDs[0] == 3 && ref.TypeIDs[1] == 3 {
		// A pet potion names the transport it is for.
		var gid uint32
		rt.deps.Read(division, func() {
			if c.ActiveCOS != nil {
				gid = c.ActiveCOS.GID
			}
		})
		if gid == 0 {
			return OpResult{}
		}
		payload = binary.LittleEndian.AppendUint32(payload, gid)
	}
	return rt.HandleItemUse(division, c, payload)
}

/*
================
ItemUsed

Whether an item use result reports the item used.
================
*/
func ItemUsed(result OpResult) bool {
	for _, frame := range result.Frames {
		if frame.Opcode == wire.OpItemUseResponse && len(frame.Payload) > 0 && frame.Payload[0] == wire.ResultSuccess {
			return true
		}
	}
	return false
}

// ServerActorLoot is one ground item an AI actor may pick up, and where.
type ServerActorLoot struct {
	Gid      uint32
	RegionID uint16
	X, Y, Z  float32
	Goods    bool
}

/*
================
ServerActorLootNear

The ground items the actor may pick up, by the native rules: a Trader
only its own trade goods; a Thief anyone else's goods, gold and items.
A thief with a full transport can still merge compatible stacks. A drop
reserved for someone else, a forged drop or an arena power-up is not loot.
goodsOnly leaves out everything but trade goods. The caller supplies the
division's ground snapshot and its reach test.
================
*/
func (rt *Runtime) ServerActorLootNear(division, name string, goodsOnly bool, items []grounditem.Item, near func(grounditem.Point) bool) []ServerActorLoot {
	var candidates []grounditem.Item
	for _, item := range items {
		if near(item.Position) && item.ArenaPowerup == 0 && !item.ForgeBound {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	c := rt.findCharacter(division, name)
	snapshot := rt.characterSnapshot(division, c)
	if snapshot == nil || !snapshot.RuntimeOnly {
		return nil
	}
	gid := enterworld.ObjectIDForCharacter(snapshot)
	thief := enterworld.DressedJob(snapshot) == domain.JobThief
	var fullCargo *inventory.Inventory
	if cart := snapshot.ActiveCOS; thief && cart != nil && cart.Container != nil && len(cart.Container.Rows) >= int(cart.Container.Capacity) {
		fullCargo, _ = inventory.NewContainer(invItemsFromRowsWithin(cart.Container.Rows, int64(cart.Container.Capacity)), cart.Container.Capacity)
	}
	var out []ServerActorLoot
	for _, item := range candidates {
		if item.OwnerJID != 0 && item.OwnerJID != gid {
			continue
		}
		goods := isPickupTradeGoods(item.TypeFlags)
		switch {
		case goods && tradePickupRefusal(snapshot, item.TradeOwner) != 0:
			continue
		case goods && fullCargo != nil && !fullCargo.CanMergeStack(inventory.Item{
			RefObjID: item.RefObjID, TradeOwner: item.TradeOwner, TypeFlags: item.TypeFlags, Plus: item.Plus,
		}, rt.maxStackFor(item.TypeFlags, item.Codename)):
			continue
		case !goods && (goodsOnly || !thief):
			continue
		}
		out = append(out, ServerActorLoot{Gid: item.Gid, RegionID: item.Position.RegionID, X: item.Position.X, Y: item.Y, Z: item.Position.Z, Goods: goods})
	}
	return out
}

// GroundItems is the division's ground, for ServerActorLootNear.
func (rt *Runtime) GroundItems(division string) []grounditem.Item {
	if rt.Ground == nil {
		return nil
	}
	return rt.Ground.All(division)
}

/*
================
ServerActorPickup

The actor, standing at the item, picks it up through the native grant:
gold to its purse, an item to its bag (or the party's share), its own
goods back into its transport. An AI Thief summons a transport on the
first goods pickup if it has none.
================
*/
func (rt *Runtime) ServerActorPickup(division, name string, itemGid uint32) OpResult {
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	snapshot := rt.characterSnapshot(division, c)
	if snapshot == nil || !snapshot.RuntimeOnly || rt.Ground == nil {
		return OpResult{}
	}
	item, ok := rt.Ground.Get(division, itemGid)
	if !ok {
		return OpResult{}
	}
	if isPickupTradeGoods(item.TypeFlags) && enterworld.DressedJob(snapshot) == domain.JobThief && snapshot.ActiveCOS == nil {
		if !rt.summonServerActorTransportLocked(division, c) {
			return OpResult{}
		}
		snapshot = rt.characterSnapshot(division, c)
	}
	return rt.grantPickup(division, simulation.WorldKey(division, c.Name), c, snapshot, item, rt.tickDoors())
}

/*
================
ServerActorMovementBuff

Whether the actor has a movement speed buff running.
================
*/
func (rt *Runtime) ServerActorMovementBuff(division, name string) bool {
	for _, effect := range rt.effects.Snapshot(division, name) {
		if effect.MovementPercent > 0 {
			return true
		}
	}
	return false
}
