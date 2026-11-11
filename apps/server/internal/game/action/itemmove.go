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
		return rt.applyAvatarTransfer(divisionID, character, request)

	case wire.MoveTypeCosPickup, wire.MoveTypeCosDrop:
		result := rt.applyCosGround(divisionID, character, request)
		rt.registerCaravanForMove(divisionID, character, request.MovementType)
		return rt.withTradeStars(divisionID, character, request.MovementType, result)

	case wire.MoveTypeCosToPlayer, wire.MoveTypePlayerToCos:
		result := rt.applyCosTransfer(character, request)
		rt.registerCaravanForMove(divisionID, character, request.MovementType)
		return rt.withTradeStars(divisionID, character, request.MovementType, result)

	case wire.MoveTypeCosInventory:
		return rt.applyCosContainerMove(character, request)

	case wire.MoveTypeShopBuy, wire.MoveTypeShopSell,
		wire.MoveTypeCosShopBuy, wire.MoveTypeCosShopSell:
		result := rt.applyCommerce(divisionID, character, request)
		rt.registerCaravanForMove(divisionID, character, request.MovementType)
		return rt.withTradeStars(divisionID, character, request.MovementType, result)

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
	// Companions a committed cape equip retired; published with the
	// ended effects, after the same door.
	var dismissed []*enterworld.CharacterCOS

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
		// INFERENCE: v1.150 notice 0x77 forbids taking off a cape in combat.
		// Capes use ordinary transfers, not the job-suit timer. Either swap
		// leg can displace slot eight; reject before committing the trial bag.
		if (request.SourceSlot == jobSuitSlot || request.DestSlot == jobSuitSlot) &&
			character.BattleUntilMs > rt.Now().UnixMilli() && rt.inFreeBattle(character) {
			result = failureResult(capeStripErrBattle)
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

		// After a move, natively self buffs whose reqi the new equipment fails end,
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

		// INFERENCE: the v1.150 client warns that free-battle equipment
		// unsummons growth pets automatically (its drop handler prints
		// UIIT_MSG_COSPETERR_UNSUMMON_FREEBATTLE ahead of the equip) and
		// composes nothing but this ordinary move, and the v1.188 equip
		// path traced for #2290 shows no client-composed dismissal, so
		// the unsummon is the server's part of a committed cape equip:
		// every summoned growth (attack-band) companion retires, the
		// mirror of the summon gate that refuses one to a caped owner.
		// The record only loses its summoned bit, like a cancellation;
		// a dead record retires too, as the warning counts it as out.
		if request.DestSlot == jobSuitSlot {
			if _, cape := rt.playerRelationEquipment(character); cape != 0 {
				for _, pet := range character.Companions() {
					if !pet.Summoned {
						continue
					}
					if ref, valid := rt.cosReference(pet); !valid || ref.TidWord>>11 != attackPetBand {
						continue
					}
					pet.Summoned = false
					pet.StateFlags &^= cosStateSummoned
					dismissed = append(dismissed, pet)
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
		// 777800 resolves the worn item for any gid, the owner's included.
		// Every audience gets the references right before the visuals
		// (peer_references.go): prod showed an owner's browser without the
		// reference of an item it wore, and the visual ended its session.
		references := rt.socketVisualReferences(changes)
		shown := append(append([]wire.Frame(nil), references...), visuals...)

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
			}, shown...),
			// The spawn row is the only other carrier of worn equipment, so a
			// viewer that already sees this character needs the same pushes:
			// 777800/777980 resolve any gid, not just the local player's.
			Broadcast: append([]wire.Frame(nil), shown...),
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
	// The cape dismissal publishes after the move's own commit, like the
	// ended effects above: the owner hears the retirement behind the
	// move answer, the neighbourhood only the despawn.
	for _, pet := range dismissed {
		despawn := wire.Frame{Opcode: wire.OpObjectDespawn, Payload: wire.ObjectDespawn{Gid: pet.GID}.Encode()}
		result.Frames = append(result.Frames, rt.retireCosRuntime(divisionID, character, pet.GID)...)
		result.Frames = append(result.Frames, cosRemovalNotice(pet.GID), despawn)
		result.Frames = append(result.Frames, companionItemStateFrames(character, pet)...)
		result.Broadcast = append(result.Broadcast, despawn)
	}
	return result
}

/*
================
groundDropBarred

The two native ground-drop bars, read off the item's reference row:
the COS-summoner family (TID 3/2) may never leave for the ground -
monster capsules (TID3 2) are the one exemption, a capsule exists to
be set down - and a row whose authored CanDrop bit is clear is kept;
the quest items are the visible mass of the second bar. Either bar
answers the indisposable refusal (01:38); the native routine checks
the family first, then the bit.
================
*/
func groundDropBarred(ref *enterworld.ItemRef) bool {
	t := ref.TypeIDs
	// Trade goods (TID 3/3/8) are authored CanDrop clear (the shipped
	// trade rows carry column 20 = 0), so without an exemption a trade
	// good that reached the personal bag could never leave it again -
	// the exact stuck stack of #3144. Retail answers a second drop of
	// an owned trade good from the personal bag with the trade refusal
	// instead of letting it fall (the native check is scoped to the
	// cargo a player owns); the personal bag is not the trade route:
	// goods bought onto a transport sell at the destination specialty
	// shop as before. Dropping (and destroying) one from the bag is
	// the disposal path; the drop lands on the ground like any other.
	if t[0] == 3 && t[1] == 3 && t[2] == 8 {
		return false
	}
	if t[0] == 3 && t[1] == 2 && t[2] != 2 {
		return true
	}
	return !ref.CanDrop
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

		// Port-only, not native (halloween.go): a sealed event helmet
		// never leaves for the ground.
		if row, present := inv.At(sourceSlot); present && rt.HalloweenHelmetSeal.Sealed(row.Codename) {
			result = failureResult(wire.ErrCodeCannotBeDropped)
			return false
		}

		// Port-only (forge.go, issue #1980): a bound forged item never
		// leaves for the ground by a player's hand - the forge's own
		// creation drop is the only bound drop there is.
		if row, present := inv.At(sourceSlot); present && row.ForgeBound {
			result = failureResult(wire.ErrCodeCannotBeDropped)
			return false
		}

		// The native drop routine bars barred bag rows before anything
		// moves, with the indisposable refusal (01:38). A worn row
		// never reaches these gates: Drop answers it 01:6D below, in
		// the native order (the equipment fault precedes the drop).
		if refs := rt.deps.ItemReferences(); refs != nil && !inventory.IsEquipmentSlot(sourceSlot) {
			if row, present := inv.At(sourceSlot); present {
				if ref, ok := refs.ItemRefByCodename(row.Codename); ok && ref != nil && groundDropBarred(ref) {
					result = failureResult(wire.ErrCodeCannotBeDropped)
					return false
				}
			}
		}

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
