/*
===========================================================================

bodystatus.go - owns bodystatus behavior and its checked data boundaries

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// untouchableBodyStatus is body mode 2 (CGObjChar_GetBodyMode, vtable
// +0x100): CGObjChar_CheckTargetAttackable (5291D0) refuses an attacker in it.
const untouchableBodyStatus uint8 = 2

/*
================
bodyStatusFrame
================
*/
func bodyStatusFrame(gid uint32, value uint8) wire.Frame {
	return wire.Frame{Opcode: wire.OpObjectStateRefresh, Payload: (wire.ObjectStateRefresh{Gid: gid, StateType: wire.StateChannelBody, Value: value}).Encode()}
}

// Native 51DE90 command 6 -> 520A40: create count monsters of one reference
// at the GM's position. The monster owner applies the native clamps and the
// 520D90 rarity resolution; this owner supplies authority, residency and pose.
/*
================
LoadGMMonsters
================
*/
func (rt *Runtime) LoadGMMonsters(division, name string, id uint32, count, monsterType uint8) bool {
	if rt == nil || rt.deps == nil || rt.Monsters == nil {
		return false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil || c.DeletePending || !c.GMPrivilege || !enterworld.CharacterAlive(c) {
		return false
	}
	lease, admitted := rt.EntryPopulationLease(division, c.Name)
	if !admitted {
		return false
	}
	now := rt.Now().UnixMilli()
	return rt.Monsters.SpawnGMMonsters(simulation.GMMonsterSpawn{
		Division: division, Population: lease, RefObjID: id, Count: count, Type: monsterType, NowMs: now,
		Position: rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now),
	}) > 0
}

// Native 51DE90 command 7 -> CGObjPC vtable+254 (4AA680): create a
// ground item at the actor. Inventory admission remains the pickup owner's job.
/*
================
MakeGMItem
================
*/
func (rt *Runtime) MakeGMItem(division, name string, id uint32, amount uint8) bool {
	if rt == nil || rt.deps == nil || rt.Ground == nil {
		return false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil {
		return false
	}
	source, ok := rt.deps.ItemReferences().(interface {
		ItemRefByID(uint32) (*enterworld.ItemRef, bool)
	})
	if !ok {
		return false
	}
	ref, ok := source.ItemRefByID(id)
	if !ok || ref == nil || ref.TypeIDs[0] != 3 {
		return false
	}
	item := inventory.Item{RefObjID: id, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), Quantity: 1}
	if inventory.IsEtcStackableTypeFlags(item.TypeFlags) {
		if amount < 1 {
			amount = 1
		}
		item.Quantity = uint16(amount)
		if cap := rt.maxStackFor(item.TypeFlags, item.Codename); item.Quantity > cap {
			item.Quantity = cap
		}
	} else if ref.TypeIDs[1] == 1 {
		// The server factory applies its final +8 cap after the console's +12 cap.
		item.Plus = amount
		if item.Plus > 8 {
			item.Plus = 8
		}
		item.Durability = uint32(clampInt64(ref.MaxDurability, 0, 0xffffffff))
	}
	var added grounditem.Item
	committed := rt.deps.Update(c, "gm-ground-drop", func() bool {
		if c.DeletePending || !c.GMPrivilege {
			return false
		}
		now := rt.Now()
		at := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now.UnixMilli())
		added = rt.addCharacterGround(division, c, PlanItemDrop(item, item.Quantity, at, c.Name, now))
		return added.Gid != 0
	})
	if !committed || added.Gid == 0 {
		return false
	}
	frames := append(rt.groundReferences([]grounditem.Item{added}), wire.DropBroadcastFrames(added.SpawnRow(true))...)
	if rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, c.Name, frames)
	}
	if rt.PushDivisionPeerFrames != nil {
		rt.PushDivisionPeerFrames(division, c.Name, frames)
	}
	return true
}

// ToggleGMBodyStatus is called by the authenticated GM dispatcher. Rechecking
// privilege inside Update closes the gap between dispatch and mutation.
/*
================
ToggleGMBodyStatus
================
*/
func (rt *Runtime) ToggleGMBodyStatus(division, name string, requested uint8) bool {
	if rt == nil || rt.deps == nil {
		return false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil {
		return false
	}
	var frames []wire.Frame
	if !rt.deps.Update(c, "gm-body-status", func() bool {
		if c.DeletePending || !c.GMPrivilege {
			return false
		}
		next, allowed := domain.GMToggleBodyStatus(c.NativeBodyStatus, requested)
		if !allowed {
			return false
		}
		if c.TransitionBodyStatus(domain.BodyStatusTransition{Value: next}) {
			frames = append(frames, bodyStatusFrame(enterworld.ObjectIDForCharacter(c), next))
			frames = append(frames, rt.refreshMovementEffects(division, c, rt.Now().UnixMilli())...)
		}
		// 4FD840 propagates to every existing owned companion.
		for _, cos := range c.Companions() {
			if !cos.Summoned || cos.NativeBodyStatus == next {
				continue
			}
			cos.NativeBodyStatus = next
			frames = append(frames, bodyStatusFrame(cos.GID, next))
		}
		return true
	}) {
		return false
	}
	rt.publishBodyStatus(division, c.Name, frames)
	return true
}

// Enqueue before releasing the action lock, after leaving the store door.
/*
================
publishBodyStatus
================
*/
func (rt *Runtime) publishBodyStatus(division, name string, frames []wire.Frame) {
	if len(frames) == 0 {
		return
	}
	if rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, name, frames)
	}
	if rt.PushDivisionPeerFrames != nil {
		rt.PushDivisionPeerFrames(division, name, frames)
	}
}
