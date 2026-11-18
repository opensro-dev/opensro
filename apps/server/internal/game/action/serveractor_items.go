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
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
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
