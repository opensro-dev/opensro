/*
===========================================================================

itemmove.go - 0x706D item moves: bag and equipment transfers, ground drops

===========================================================================
*/

package action

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
HandleItemMove

HandleItemMove ports moveMissionItem over the native 0x706D payload:
type 0x00 bag/equip transfer (with the M1 visual pushes), type 0x07 ground
drop and type 0x0A gold drop (both at the LIVE position).
==================
*/
func (rt *Runtime) HandleItemMove(
	divisionID string,
	character *enterworld.Character,
	payload []byte,
) OpResult {
	if len(payload) > 0 && payload[0] == wire.MoveTypeMallBuy {
		return rt.HandleMallPurchase(divisionID, character, payload)
	}
	request, err := wire.DecodeItemMoveRequest(payload)
	if err != nil {
		// Unsupported movement types and malformed bodies both answer the
		// generic invalid-request notice, like the fixture's 0x02 arm.
		return failureResult(wire.ErrCodeInvalidRequest)
	}

	if character == nil {
		return failureResult(wire.ErrCodeInvalidRequest)
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()

	snapshot := rt.characterSnapshot(divisionID, character)
	if snapshot == nil || snapshot.DeletePending {
		return failureResult(wire.ErrCodeInvalidRequest)
	}

	worldKey := simulation.WorldKey(divisionID, character.Name)

	// Any item operation supersedes a pickup approach in flight (the native
	// interact latch is one slot).
	rt.Pending.Clear(grounditem.PendingKey(divisionID, character.Name))

	// A stall's owner keeps its bag as the stall shows it (stall.go).
	if rt.Stalls.Keeping(divisionID, character.Name) {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	// The bag is locked to the exchange while one is open (exchange.go).
	if rt.Exchanges.Trading(divisionID, character.Name) {
		switch request.MovementType {
		case wire.MoveTypeExchangePut, wire.MoveTypeExchangeTake, wire.MoveTypeExchangeGold:
			return rt.applyExchangeMove(divisionID, character, request)
		}
		return failureResult(wire.ErrCodeInvalidRequest)
	}

	switch request.MovementType {
	case wire.MoveTypeAvatarToPlayer, wire.MoveTypePlayerToAvatar:
		return rt.applyAvatarTransfer(character, request)

	case wire.MoveTypeCosPickup, wire.MoveTypeCosDrop:
		result := rt.applyCosGround(divisionID, character, request)
		rt.registerCaravanForMove(divisionID, character, request.MovementType)
		return result

	case wire.MoveTypeCosToPlayer, wire.MoveTypePlayerToCos:
		result := rt.applyCosTransfer(character, request)
		rt.registerCaravanForMove(divisionID, character, request.MovementType)
		return result

	case wire.MoveTypeCosInventory:
		return rt.applyCosContainerMove(character, request)

	case wire.MoveTypeShopBuy, wire.MoveTypeShopSell,
		wire.MoveTypeCosShopBuy, wire.MoveTypeCosShopSell:
		result := rt.applyCommerce(divisionID, character, request)
		rt.registerCaravanForMove(divisionID, character, request.MovementType)
		return result

	case wire.MoveTypeInventory:
		if jobSuitMove(character, request) {
			return rt.beginJobDress(divisionID, character, request)
		}
		return rt.applyInventoryMove(divisionID, character, request)

	case wire.MoveTypeGroundDrop:
		return rt.applyGroundDrop(divisionID, worldKey, character, request.SourceSlot)

	case wire.MoveTypeGoldDrop:
		return rt.applyGoldDrop(divisionID, worldKey, character, request.GoldAmount)

	case wire.MoveTypeStorage, wire.MoveTypeStorageDeposit, wire.MoveTypeStorageWithdraw,
		wire.MoveTypeStorageGoldWithdraw, wire.MoveTypeStorageGoldDeposit:
		return rt.applyStorageMove(divisionID, character, request)

	case wire.MoveTypeGuildStorage, wire.MoveTypeGuildStorageDeposit, wire.MoveTypeGuildStorageWithdraw,
		wire.MoveTypeGuildStorageGoldDeposit, wire.MoveTypeGuildStorageGoldWithdraw:
		return rt.applyGuildStorageMove(divisionID, character, request)

	default:
		return failureResult(wire.ErrCodeInvalidRequest)
	}
}

/*
==================
applyInventoryMove
==================
*/
func (rt *Runtime) applyInventoryMove(
	divisionID string,
	character *enterworld.Character,
	request wire.ItemMoveRequest,
) OpResult {
	result := failureResult(wire.ErrCodeInvalidRequest)
	// Ended-effect frames are pushed after Update returns: peer delivery
	// reads session characters through the store, whose door is not
	// reentrant, so publishing inside the callback deadlocks the shard.
	var endedPublic, endedActor []wire.Frame

	rt.deps.Update(character, "inv-move", func() bool {
		if character.DeletePending {
			return false
		}

		inv := bagOf(character)

		// The character-vs-itemdata equip gates (level/stats/gender/country):
		// consulted by both Transfer equip legs around the exclusivity scan.
		inv.Requirements = equipRequirements(
			rt.deps.ItemReferences(),
			character,
			rt.FortressGuildRole,
		)

		stackCap := uint16(1)
		if sourceRow, ok := inv.At(request.SourceSlot); ok {
			stackCap = rt.maxStackFor(sourceRow.TypeFlags, sourceRow.Codename)
		}

		if _, fault := inv.Transfer(
			request.SourceSlot,
			request.DestSlot,
			request.Quantity,
			stackCap,
		); fault != nil {
			result = failureResult(fault.Code)
			return false
		}

		subMoves, fault := rt.completeEquipmentPair(inv, request.SourceSlot, request.DestSlot)
		if fault != nil {
			result = failureResult(fault.Code)
			return false
		}

		nextRows := rowsFromInvItems(inv.Items())
		equipmentChanged := inventory.IsEquipmentSlot(request.SourceSlot) ||
			inventory.IsEquipmentSlot(request.DestSlot)
		var statFrame *wire.Frame
		if equipmentChanged {
			next := character.Snapshot()
			next.MissionInventory = nextRows
			display, err := rt.PlayerBaseStats(divisionID, next)
			if err != nil {
				log.Warnf(
					"action: inventory move %d->%d refused - %v",
					request.SourceSlot,
					request.DestSlot,
					err,
				)
				result = failureResult(wire.ErrCodeInvalidRequest)
				return false
			}

			frame := wire.Frame{
				Opcode:  wire.OpBaseStats,
				Payload: enterworld.BuildLoginStatBlock(next, display),
			}

			statFrame = &frame
		}

		// Publish the inventory only after every fallible post-move stat
		// derivation succeeded. The visual and 0x343C frames describe this
		// exact committed occupancy snapshot.
		character.MissionInventory = nextRows

		// 50F1F0 -> 59F0E0: self buffs whose reqi the new equipment fails end,
		// and the stats the move publishes are the ones without them.
		if equipmentChanged {
			if ended := rt.retireUnmetEquipmentEffects(divisionID, character); len(ended) != 0 {
				endedPublic, endedActor = rt.finishEndedEffects(divisionID, character, ended, rt.Now().UnixMilli())
				if display, err := rt.PlayerBaseStats(divisionID, character); err == nil {
					frame := wire.Frame{Opcode: wire.OpBaseStats, Payload: enterworld.BuildLoginStatBlock(character, display)}
					statFrame = &frame
				}
			}
		}

		// THE M1 CONTRACT: every transfer that touched a socket < 13 appends
		// its visual pushes behind the 0xB06D row.
		changes := inv.EquipVisualChanges(request.SourceSlot, request.DestSlot)
		for _, move := range subMoves {
			changes = append(changes, inv.EquipVisualChanges(move.SourceSlot, move.DestSlot)...)
		}
		visuals := FramesFromSocketVisuals(enterworld.ObjectIDForCharacter(character), changes)

		result = OpResult{
			Frames: append([]wire.Frame{
				{
					Opcode: wire.OpItemMoveResponse,
					Payload: wire.EncodeInventoryMoveResult(
						request.SourceSlot,
						request.DestSlot,
						request.Quantity,
						subMoves,
					),
				},
			}, visuals...),
			// The spawn row is the only other carrier of worn equipment, so a
			// viewer that already sees this character needs the same pushes:
			// 777800/777980 resolve any gid, not just the local player's. The
			// owner holds its own references; a viewer gets them first.
			Broadcast: append(rt.socketVisualReferences(changes), visuals...),
		}

		if statFrame != nil {
			hp, mp := rt.clampStoredGaugeToKeeper(divisionID, character)
			result.Frames = append(result.Frames, *statFrame)
			result.Frames = append(result.Frames, rt.gaugeDropFrames(divisionID, character, hp, mp, false)...)
		}

		return true
	})

	rt.publishBodyStatus(divisionID, character.Name, endedPublic)
	if len(endedActor) != 0 && rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(divisionID, character.Name, endedActor)
	}
	return result
}

/*
==================
applyGroundDrop
==================
*/
func (rt *Runtime) applyGroundDrop(
	divisionID, worldKey string,
	character *enterworld.Character,
	sourceSlot uint8,
) OpResult {
	now := rt.Now()
	var added grounditem.Item
	var questFrames []wire.Frame
	result := failureResult(wire.ErrCodeInvalidRequest)

	if !rt.deps.Update(character, "ground-drop", func() bool {
		if character.DeletePending {
			return false
		}

		inv := bagOf(character)

		// The native type-7 wire has no quantity field: the whole row goes.
		dropped, fault := inv.Drop(sourceSlot)
		if fault != nil {
			result = failureResult(fault.Code)
			return false
		}

		// The drop lands where the character IS (live plane), never at the
		// goal a mid-move Spawn still points at.
		live := rt.liveSpawn(worldKey, character, now.UnixMilli())
		added = rt.addCharacterGround(
			divisionID,
			character,
			PlanItemDrop(dropped, dropped.Quantity, live, character.Name, now),
		)
		if added.Gid == 0 {
			return false
		}

		character.MissionInventory = rowsFromInvItems(inv.Items())
		questFrames = rt.updateQuestInventory(character)
		return true
	}) {
		return result
	}

	spawnRow := added.SpawnRow(true)
	return OpResult{
		Frames: append(
			append(
				rt.groundReferences([]grounditem.Item{added}),
				wire.GroundDropFrames(sourceSlot, spawnRow)...,
			),
			questFrames...,
		),
		Broadcast: append(
			rt.groundReferences([]grounditem.Item{added}),
			wire.DropBroadcastFrames(spawnRow)...,
		),
	}
}

/*
==================
applyGoldDrop
==================
*/
func (rt *Runtime) applyGoldDrop(
	divisionID, worldKey string,
	character *enterworld.Character,
	requested uint32,
) OpResult {
	now := rt.Now()
	var added grounditem.Item
	var amount uint32
	var balance uint64
	result := failureResult(wire.ErrCodeInvalidRequest)

	if !rt.deps.Update(character, "gold-drop", func() bool {
		if character.DeletePending {
			return false
		}

		var fault *inventory.Fault
		balance, amount, fault = inventory.DropGold(goldOf(character), requested)
		if fault != nil {
			result = failureResult(fault.Code)
			return false
		}

		// Resolve the heap row after validation but before changing either
		// authoritative plane. A missing row must not leak the debit.
		tier := inventory.GoldHeapTier(amount)
		if rt.deps.ItemReferences() == nil {
			return false
		}

		ref, ok := rt.deps.ItemReferences().ItemRefByCodename(tier)
		if !ok || ref == nil {
			return false
		}

		heapRef := GoldHeapRef{
			RefObjID: ref.RefObjID,
			Codename: ref.Codename,
			Tid1:     uint8(ref.TypeIDs[0]),
			Tid2:     uint8(ref.TypeIDs[1]),
			Tid3:     uint8(ref.TypeIDs[2]),
			Tid4:     uint8(ref.TypeIDs[3]),
		}

		// Same live-plane read as the type-7 leg: a mid-run drop spawns at
		// the actor's current position, not the path destination.
		live := rt.liveSpawn(worldKey, character, now.UnixMilli())
		added = rt.addCharacterGround(
			divisionID,
			character,
			PlanGoldDrop(heapRef, amount, live, character.Name, now),
		)
		if added.Gid == 0 {
			return false
		}

		setGold(character, balance)
		return true
	}) {
		return result
	}

	spawnRow := added.SpawnRow(true)
	return OpResult{
		Frames:    wire.GoldDropFrames(amount, balance, spawnRow),
		Broadcast: wire.DropBroadcastFrames(spawnRow),
	}
}
