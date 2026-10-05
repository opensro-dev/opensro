/*
===========================================================================

storage.go - the NPC warehouse: list, open and item/gold transfers

The account warehouse is owned by the store (domain.StorageAuthority);
this file admits requests against the selected warehouse NPC and turns
them into atomic store transactions. The wire is item/wire/storage.go.

Admission rules:
  - the selected object is a live warehouse NPC (capability 0x4);
  - only bag slots trade with the room, never the equipment sockets
    ("Ooops! Trying to store equiped item to chest!", SR_GameServer 45AF70);
  - a deposited item must be storable: CanBorrow (itemdata token 19,
    RefObjData+0xA7) has bit 0x80, or the client's own check raises
    UIIT_MSG_STRGERR_INVALID_TARGET_STORAGE (1:0x43, CIFStorage_OnSlotTransfer);
  - a deposit charges KeepingFee (token 30) per unit. INFERENCE: the column
    is authored per unit (potion 1, a 890-gold sword 21).

The remote warehouse ticket (ITEM_MALL_WAREHOUSE_TICKET, 3/3/13/10) has no
native rule in either binary: v1.188 49C2B0 case 9 falls through, and the
v1.150 client only starts cooldown 0x1A on its 0xB04C success
(CPSMission_OnItemUseResponse0xB5BD). INFERENCE: the ticket opens the
same room with the player standing in for the NPC. A spent ticket selects
the player's own gid with its storage function open; the client then
drives the ordinary 0x72C3 / 0x7338 / 0x706D flow naming that gid, and no
range applies. Any new selection or release ends the session, exactly as
it ends an NPC's.

===========================================================================
*/
package action

import (
	"errors"
	"fmt"
	"math"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

// storableFlag is the CanBorrow bit that admits an item to the warehouse.
const storableFlag = 0x80

// errCodeInvalidStorageTarget is UIIT_MSG_STRGERR_INVALID_TARGET_STORAGE.
const errCodeInvalidStorageTarget uint8 = 0x43

// storageRefusal carries the native notice code of a refused transfer.
type storageRefusal uint8

/*
================
Error
================
*/
func (r storageRefusal) Error() string { return fmt.Sprintf("storage refused 0x%02X", uint8(r)) }

/*
================
ConfigureStorage
================
*/
func (rt *Runtime) ConfigureStorage(authority domain.StorageAuthority) {
	rt.storageAuthority = authority
}

/*
================
registerStorage
================
*/
func (rt *Runtime) registerStorage(hub *transport.Hub) {
	hub.Handle(wire.OpStorageListRequest, func(session *transport.Session, opcode uint16, payload []byte) {
		character, divisionID, bound := enterworld.SessionCharacter(rt.deps, session)
		if !bound {
			return
		}
		frames, refusal := rt.HandleStorageList(divisionID, character, payload)
		if refusal != "" {
			// A typed refusal (0xB338 kind 2, too far) still answers the client.
			log.Debugf("storage: 0x%04X refused for %s: %s", opcode, character.Name, refusal)
		}
		sendFrames(session, frames)
	})
}

/*
================
storageNpc

The selected, live warehouse NPC the request names.
================
*/
func (rt *Runtime) storageNpc(divisionID string, character *enterworld.Character, gid uint32) (simulation.NpcDef, bool) {
	if selected, ok := rt.Selected.Get(divisionID, character.Name); !ok || selected != gid {
		return simulation.NpcDef{}, false
	}
	if rt.remoteStorageOpen(divisionID, character, gid) {
		return simulation.NpcDef{}, true
	}
	npc, ok := rt.npcForCurrentViewer(divisionID, character, gid)
	return npc, ok && npc.TalkFlags&simulation.NpcTalkFlagStorage != 0
}

/*
================
openStorageNpc

The warehouse a move may use: the one whose storage function an in-range
request opened (npcaction.go). As with trades, moves check that function
state, not distance.
================
*/
func (rt *Runtime) openStorageNpc(divisionID string, character *enterworld.Character, gid uint32) bool {
	_, ok := rt.storageNpc(divisionID, character, gid)
	return ok && rt.Selected.FunctionOpen(divisionID, character.Name, gid)
}

/*
================
openRemoteStorage

A spent warehouse ticket: the player's own gid becomes the selection with
its storage function open.
================
*/
func (rt *Runtime) openRemoteStorage(divisionID string, character *enterworld.Character) {
	self := enterworld.ObjectIDForCharacter(character)
	rt.Selected.Set(divisionID, character.Name, self)
	rt.Selected.OpenFunction(divisionID, character.Name, self)
}

/*
================
remoteStorageOpen

True when gid is the player's own and a ticket opened its warehouse.
================
*/
func (rt *Runtime) remoteStorageOpen(divisionID string, character *enterworld.Character, gid uint32) bool {
	return gid == enterworld.ObjectIDForCharacter(character) && rt.Selected.FunctionOpen(divisionID, character.Name, gid)
}

/*
================
HandleStorageList

0x72C3 -> 0x3126 gold, 0x321A list.
================
*/
func (rt *Runtime) HandleStorageList(divisionID string, character *enterworld.Character, payload []byte) ([]wire.Frame, string) {
	if character == nil || rt.storageAuthority == nil {
		return nil, "storage unavailable"
	}
	gid, err := wire.DecodeStorageListRequest(payload)
	if err != nil {
		return nil, err.Error()
	}
	unlock := rt.lockDivision(divisionID)
	defer unlock()
	npc, ok := rt.storageNpc(divisionID, character, gid)
	if !ok {
		return nil, "the selected object is not a warehouse NPC"
	}
	// INFERENCE: the list request is the storage row's first step, so it
	// takes the same 4A8E10 range gate as the function request that follows.
	if !rt.remoteStorageOpen(divisionID, character, gid) && !rt.npcWithinHitRange(divisionID, character, npc) {
		return npcFunctionTooFar(), fmt.Sprintf("NPC %s is beyond its interaction range", npc.Codename)
	}
	storage, err := rt.storageAuthority.AccountStorage(character)
	if err != nil {
		return nil, err.Error()
	}
	list, err := rt.storageListPayload(storage)
	if err != nil {
		return nil, err.Error()
	}
	// The room's items are not carried, so their references (type word,
	// icon, tooltip) reach the browser ahead of the rows that need them.
	return []wire.Frame{
		rt.commerceReferences(invItemsFromRowsWithin(storage.Rows, storage.Capacity), nil),
		{Opcode: wire.OpStorageGold, Payload: wire.EncodeStorageGold(uint64(storage.Gold))},
		{Opcode: wire.OpStorageList, Payload: list},
	}, ""
}

/*
================
storageListPayload
================
*/
func (rt *Runtime) storageListPayload(storage domain.AccountStorage) ([]byte, error) {
	rows := make([]wire.StorageListRow, 0, len(storage.Rows))
	for _, item := range invItemsFromRowsWithin(storage.Rows, storage.Capacity) {
		if item.Summon != nil {
			item.Summon.RefreshRentalTimes(rt.Now().Unix())
		}
		body := item.Body().Encode()
		if len(body) == 0 {
			return nil, fmt.Errorf("storage: invalid persistent item in slot %d", item.Slot)
		}
		rows = append(rows, wire.StorageListRow{Slot: item.Slot, Body: body})
	}
	if len(rows) != len(storage.Rows) || len(rows) > 255 {
		return nil, fmt.Errorf("storage: %d rows cannot be listed", len(storage.Rows))
	}
	return wire.EncodeStorageList(uint8(storage.Capacity), rows), nil
}

/*
================
storableItem
================
*/
func (rt *Runtime) storableItem(item inventory.Item) bool {
	ref, ok := rt.deps.ItemReferences().ItemRefByCodename(item.Codename)
	if !ok || ref == nil {
		return false
	}
	borrow, present := ref.NativeFields.Lookup("canBorrow")
	return present && int64(borrow)&storableFlag != 0
}

/*
================
keepingFee
================
*/
func (rt *Runtime) keepingFee(item inventory.Item) (int64, bool) {
	ref, ok := rt.deps.ItemReferences().ItemRefByCodename(item.Codename)
	if !ok || ref == nil {
		return 0, false
	}
	fee, present := ref.NativeFields.Lookup("keepingFee")
	if !present || fee < 0 || math.Trunc(fee) != fee || fee > math.MaxInt32 {
		return 0, false
	}
	units := int64(1)
	if inventory.IsEtcStackableTypeFlags(item.TypeFlags) && item.Quantity > 0 {
		units = int64(item.Quantity)
	}
	return int64(fee) * units, true
}

/*
================
applyStorageMove

The five warehouse move types, each one store transaction.
================
*/
func (rt *Runtime) applyStorageMove(divisionID string, character *enterworld.Character, q wire.ItemMoveRequest) OpResult {
	if rt.storageAuthority == nil {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	// Item moves name the NPC; gold moves use the selection.
	gid := q.NpcGID
	if q.MovementType != wire.MoveTypeStorage && q.MovementType != wire.MoveTypeStorageDeposit && q.MovementType != wire.MoveTypeStorageWithdraw {
		gid, _ = rt.Selected.Get(divisionID, character.Name)
	}
	if !rt.openStorageNpc(divisionID, character, gid) {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	var questFrames []wire.Frame
	_, err := rt.storageAuthority.TransactStorage(character, rt.storageMutation(q.MovementType, q))
	var refusal storageRefusal
	if errors.As(err, &refusal) {
		return failureResult(uint8(refusal))
	}
	if err != nil {
		log.Warnf("storage: %s move 0x%02X failed: %v", character.Name, q.MovementType, err)
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	frames := []wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: wire.EncodeStorageMoveSuccess(q)}}
	if q.MovementType != wire.MoveTypeStorage {
		frames = append(frames, goldFrame(character))
	}
	rt.deps.Update(character, "storage-quest-inventory", func() bool {
		questFrames = rt.updateQuestInventory(character)
		return len(questFrames) > 0
	})
	return OpResult{Frames: append(frames, questFrames...)}
}

/*
================
storageMutation

One warehouse move on detached copies of the character and a room: the
five personal types, which the guild warehouse's five stand for
(wire.PersonalStorageMove).
================
*/
func (rt *Runtime) storageMutation(movement uint8, q wire.ItemMoveRequest) func(next *domain.Character, storage *domain.AccountStorage) error {
	return func(next *domain.Character, storage *domain.AccountStorage) error {
		room, fault := inventory.NewStorageRoom(invItemsFromRowsWithin(storage.Rows, storage.Capacity), uint8(storage.Capacity))
		if fault != nil {
			return fault
		}
		bag := inventory.New(invItemsFromRows(next.MissionInventory))
		gold := int64(0)
		if next.Gold != nil {
			gold = *next.Gold
		}
		switch movement {
		case wire.MoveTypeStorage:
			item, present := room.At(q.SourceSlot)
			if !present {
				return storageRefusal(wire.ErrCodeInvalidRequest)
			}
			if _, fault := room.Transfer(q.SourceSlot, q.DestSlot, q.Quantity, rt.maxStackFor(item.TypeFlags, item.Codename)); fault != nil {
				return storageRefusal(fault.Code)
			}
		case wire.MoveTypeStorageDeposit:
			item, present := bag.At(q.SourceSlot)
			if !present || !inventory.IsBagSlot(q.SourceSlot) {
				return storageRefusal(wire.ErrCodeInvalidRequest)
			}
			if !rt.storableItem(item) {
				return storageRefusal(errCodeInvalidStorageTarget)
			}
			fee, ok := rt.keepingFee(item)
			if !ok {
				return storageRefusal(wire.ErrCodeInvalidRequest)
			}
			if fee > gold {
				return storageRefusal(wire.ErrCodeNotEnoughGold)
			}
			if fault := bag.TransferWholeTo(room, q.SourceSlot, q.DestSlot, rt.maxStackFor(item.TypeFlags, item.Codename)); fault != nil {
				return storageRefusal(fault.Code)
			}
			gold -= fee
		case wire.MoveTypeStorageWithdraw:
			item, present := room.At(q.SourceSlot)
			if !present || !inventory.IsBagSlot(q.DestSlot) {
				return storageRefusal(wire.ErrCodeInvalidRequest)
			}
			if fault := room.TransferWholeTo(bag, q.SourceSlot, q.DestSlot, rt.maxStackFor(item.TypeFlags, item.Codename)); fault != nil {
				return storageRefusal(fault.Code)
			}
		case wire.MoveTypeStorageGoldDeposit:
			amount := int64(q.GoldAmount)
			if amount <= 0 || amount > gold {
				return storageRefusal(wire.ErrCodeNotEnoughGold)
			}
			if storage.Gold > math.MaxInt64-amount {
				return storageRefusal(wire.ErrCodeInvalidRequest)
			}
			gold -= amount
			storage.Gold += amount
		case wire.MoveTypeStorageGoldWithdraw:
			amount := int64(q.GoldAmount)
			if amount <= 0 || amount > storage.Gold {
				return storageRefusal(wire.ErrCodeNotEnoughGold)
			}
			if gold > math.MaxInt64-amount {
				return storageRefusal(wire.ErrCodeInvalidRequest)
			}
			gold += amount
			storage.Gold -= amount
		default:
			return storageRefusal(wire.ErrCodeInvalidRequest)
		}
		storage.Rows = rowsFromInvItems(room.Items())
		next.MissionInventory = rowsFromInvItems(bag.Items())
		next.Gold = &gold
		return nil
	}
}

/*
================
goldFrame

The character's balance after a warehouse transfer; not a gain notice.
================
*/
func goldFrame(c *enterworld.Character) wire.Frame {
	return wire.Frame{Opcode: wire.OpPointsUpdate, Payload: wire.GoldRefresh{Balance: goldOf(c)}.Encode()}
}
