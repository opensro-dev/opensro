/*
===========================================================================

storage.go - the NPC warehouse: list, open and item/gold transfers

The account warehouse is owned by the store (domain.StorageAuthority);
this file admits requests against the selected warehouse NPC and turns
them into atomic store transactions. The wire is item/wire/storage.go.

Admission rules:
  - the selected object is a live warehouse NPC (capability 0x4);
  - only bag slots trade with the room, never the equipment sockets
    ("Ooops! Trying to store equiped item to chest!" in the reference server);
  - a deposited item must be storable: CanBorrow (itemdata token 19,
    RefObjData+0xA7) has bit 0x80, or the client's own check raises
    UIIT_MSG_STRGERR_INVALID_TARGET_STORAGE (1:0x43, CIFStorage_OnSlotTransfer);
  - the guild warehouse judges by its own rule instead (the same code
    answers): the CanBorrow guild bit 0x40, never a quest-shaped row
    (TypeIDs 3/3/9), and only a tradeable one (CanTrade, token 16);
  - a deposit charges KeepingFee (token 30) per unit. INFERENCE: the column
    is authored per unit (potion 1, a 890-gold sword 21).

The remote warehouse ticket (ITEM_MALL_WAREHOUSE_TICKET, 3/3/13/10) has no
native rule in either binary: v1.188's special-consumable case 9 falls through, and the
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
	"opensro.online/server/internal/game/nasrun"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

// storableFlag is the CanBorrow bit that admits an item to the warehouse.
const storableFlag = 0x80

// guildStorableFlag is the CanBorrow bit the guild warehouse tests
// instead: the column byte's 0x40, where the account room tests 0x80
// (the server packs the two into its row flags word as bits 13 and 12).
const guildStorableFlag = 0x40

// errCodeInvalidStorageTarget is UIIT_MSG_STRGERR_INVALID_TARGET_STORAGE.
const errCodeInvalidStorageTarget uint8 = 0x43

// errCodeKeeperBusy is the B338 refusal UIIT_MSG_INTERACTION_FAIL_IM_BUSY
// (category 13, code 0x0B): the account's warehouse is open for another of
// its characters (accountStorageHolder), or the talk's busy state
// (stallBusy).
const errCodeKeeperBusy uint8 = 0x0b

// storageOpen is one character's open warehouse. account names the
// account whose warehouse it is; it is empty for a guild warehouse, which
// keeps its own single-user lock (npcguildstorage.go).
type storageOpen struct {
	gid      uint32
	division string
	name     string
	account  string
}

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

The warehouse a move may use: the one the character's warehouse session
names, while its keeper is still live (a ticket names the character's own
gid). Native warehouse moves read the player's own warehouse-open record
(pc+0x1D88: the native storage gold and deposit moves),
never the selection, so selecting the keeper again, or anything else,
while the window is open leaves the room usable.
================
*/
func (rt *Runtime) openStorageNpc(divisionID string, character *enterworld.Character, gid uint32) bool {
	open, ok := rt.storageSession(divisionID, character.Name)
	if !ok || open != gid {
		return false
	}
	if gid == enterworld.ObjectIDForCharacter(character) {
		return true
	}
	npc, live := rt.npcForCurrentViewer(divisionID, character, gid)
	return live && npc.TalkFlags&simulation.NpcTalkFlagStorage != 0
}

/*
================
openStorageSession

Records the warehouse a character just opened: a keeper's storage
function, a ticket, or a claimed guild warehouse. A later opening replaces
it.
================
*/
func (rt *Runtime) openStorageSession(divisionID, characterName string, gid uint32) {
	rt.storageSessions.Store(simulation.WorldKey(divisionID, characterName),
		storageOpen{gid: gid, division: divisionID, name: characterName})
}

/*
================
openAccountStorageSession

Records the account warehouse a character just opened (a keeper or a
ticket), so a sibling character of the same account finds it held.
================
*/
func (rt *Runtime) openAccountStorageSession(divisionID string, character *enterworld.Character, gid uint32) {
	rt.storageSessions.Store(simulation.WorldKey(divisionID, character.Name),
		storageOpen{gid: gid, division: divisionID, name: character.Name, account: character.AccountID})
}

/*
================
accountStorageHolder

Another character of the same account that has its account warehouse
open, if any.

Port-only, not native: the original holds one session per account, so its
warehouse is never open twice and its client lists the room once per
session (CIFStorageRoom +0x7BC). The port binds play per character, so
two characters of one account can stand at two keepers at once. One open
room per account keeps the second window from moving items out of a stale
copy. A holder that left the world is dropped, as guildStorageHolder does.
================
*/
func (rt *Runtime) accountStorageHolder(divisionID string, character *enterworld.Character) (string, bool) {
	if character.AccountID == "" {
		return "", false
	}
	holder := ""
	rt.storageSessions.Range(func(key, value any) bool {
		open := value.(storageOpen)
		if open.division != divisionID || open.account != character.AccountID || open.name == character.Name {
			return true
		}
		if _, online := rt.characterAdmissions.Load(key); !online {
			rt.storageSessions.CompareAndDelete(key, value)
			return true
		}
		holder = open.name
		return false
	})
	return holder, holder != ""
}

/*
================
storageSession

The warehouse a character has open, if any.
================
*/
func (rt *Runtime) storageSession(divisionID, characterName string) (uint32, bool) {
	value, ok := rt.storageSessions.Load(simulation.WorldKey(divisionID, characterName))
	if !ok {
		return 0, false
	}
	return value.(storageOpen).gid, true
}

/*
================
endStorageInteraction

Another interaction took the player's one native interaction state (a
shop, an exchange, a stall, death): the open warehouse ends, as the native gold move and
the item handlers then refuse it.
================
*/
func (rt *Runtime) endStorageInteraction(divisionID, characterName string) {
	rt.closeStorageSession(divisionID, characterName)
}

/*
================
closeStorageSession

Ends a character's warehouse session: the window's close (its target
release), a relocation, or leaving the world.
================
*/
func (rt *Runtime) closeStorageSession(divisionID, characterName string) {
	rt.storageSessions.Delete(simulation.WorldKey(divisionID, characterName))
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
	rt.openAccountStorageSession(divisionID, character, self)
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
	// takes the same hit-range gate as the function request that follows.
	if !rt.remoteStorageOpen(divisionID, character, gid) && !rt.npcWithinHitRange(divisionID, character, npc) {
		return npcFunctionTooFar(), fmt.Sprintf("NPC %s is beyond its interaction range", npc.Codename)
	}
	if holder, held := rt.accountStorageHolder(divisionID, character); held {
		rt.closeStorageSession(divisionID, character.Name)
		return npcFunctionRefusal(errCodeKeeperBusy), "the account warehouse is open for " + holder
	}
	storage, err := rt.storageAuthority.AccountStorage(character)
	if err != nil {
		return nil, err.Error()
	}
	// Native deletes an out-of-period rental as the warehouse loads
	// (_DelExpiredRentItemInStorage, rental_items.go): an expired
	// rental leaves before the room lists.
	if rentalRowsExpired(storage.Rows, rt.Now().Unix()) {
		storage, err = rt.storageAuthority.TransactStorage(character, func(_ *domain.Character, room *domain.AccountStorage) error {
			removeExpiredRentRows(&room.Rows, rt.Now().Unix())
			return nil
		})
		if err != nil {
			return nil, err.Error()
		}
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
	// Port-only, not native (halloween.go): a sealed event helmet never
	// enters either warehouse, however storable its row is.
	if rt.HalloweenHelmetSeal.Sealed(item.Codename) {
		return false
	}
	// Port-only, not native (#4459): a mall copy of the Devil's Spirit
	// keeps the later row's warehouse bit, which the imported row clears
	// for the bound token and drop copies (nasrun.StorableMallCopy).
	if nasrun.StorableMallCopy(item.TypeFlags, item.ForgeBound, item.EarnedBought) {
		return true
	}
	ref, ok := rt.deps.ItemReferences().ItemRefByCodename(item.Codename)
	if !ok || ref == nil {
		return false
	}
	borrow, present := ref.NativeFields.Lookup("canBorrow")
	return present && int64(borrow)&storableFlag != 0
}

/*
================
guildStorableItem

The guild warehouse's deposit admission, read in the v1.188 server:
where the account room tests the CanBorrow account bit, the guild door
tests the guild bit, and adds two more refusals of its own - a
quest-shaped row (TypeIDs 3/3/9, however borrowable or tradeable its
columns read: shipped quest rows carry 255 and CanTrade 1) never
enters, and neither does an untradeable one. Every failure answers the
same 0x43 the account room uses. The port-only bars storableItem
carries (a sealed helmet, the mall copy) hold here too.
================
*/
func (rt *Runtime) guildStorableItem(item inventory.Item) bool {
	if rt.HalloweenHelmetSeal.Sealed(item.Codename) {
		return false
	}
	if nasrun.StorableMallCopy(item.TypeFlags, item.ForgeBound, item.EarnedBought) {
		return true
	}
	ref, ok := rt.deps.ItemReferences().ItemRefByCodename(item.Codename)
	if !ok || ref == nil {
		return false
	}
	borrow, present := ref.NativeFields.Lookup("canBorrow")
	if !present || int64(borrow)&guildStorableFlag == 0 {
		return false
	}
	if ref.TypeIDs[0] == 3 && ref.TypeIDs[1] == 3 && ref.TypeIDs[2] == 9 {
		return false
	}
	trade, present := ref.NativeFields.Lookup("canTrade")
	return present && int64(trade) != 0
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
	// Item moves name the NPC; gold moves name none and use the open
	// warehouse (natively the open record is read, not the selection).
	gid := q.NpcGID
	if q.MovementType != wire.MoveTypeStorage && q.MovementType != wire.MoveTypeStorageDeposit && q.MovementType != wire.MoveTypeStorageWithdraw {
		gid, _ = rt.storageSession(divisionID, character.Name)
	}
	if !rt.openStorageNpc(divisionID, character, gid) {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	// The native room move, deposit and withdraw item moves
	// run the NPC-function range test
	// on the packet's keeper and answer its code: 4 beyond the range. Gold
	// has no range test. A ticket's room has no keeper to reach.
	if gid != enterworld.ObjectIDForCharacter(character) && (q.MovementType == wire.MoveTypeStorage ||
		q.MovementType == wire.MoveTypeStorageDeposit || q.MovementType == wire.MoveTypeStorageWithdraw) {
		if npc, live := rt.npcForCurrentViewer(divisionID, character, gid); !live || !rt.npcWithinHitRange(divisionID, character, npc) {
			return failureResult(hitRangeTooFar)
		}
	}
	var questFrames []wire.Frame
	_, err := rt.storageAuthority.TransactStorage(character, rt.storageMutation(q.MovementType, q, false))
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
(wire.PersonalStorageMove). guild says which room is served, because
a deposit is judged by the room's own admission (storableItem for the
account room, guildStorableItem for the guild's).
================
*/
func (rt *Runtime) storageMutation(movement uint8, q wire.ItemMoveRequest, guild bool) func(next *domain.Character, storage *domain.AccountStorage) error {
	return func(next *domain.Character, storage *domain.AccountStorage) error {
		room, fault := inventory.NewStorageRoom(invItemsFromRowsWithin(storage.Rows, storage.Capacity), uint8(storage.Capacity))
		if fault != nil {
			return fault
		}
		bag := bagOf(next)
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
			if !present || !inventory.IsBagSlot(q.SourceSlot, inventory.BagEnd(next)) {
				return storageRefusal(wire.ErrCodeInvalidRequest)
			}
			admitted := rt.storableItem(item)
			if guild {
				admitted = rt.guildStorableItem(item)
			}
			if !admitted {
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
			if !present || !inventory.IsBagSlot(q.DestSlot, inventory.BagEnd(next)) {
				return storageRefusal(wire.ErrCodeInvalidRequest)
			}
			if fault := room.TransferWholeTo(bag, q.SourceSlot, q.DestSlot, rt.maxStackFor(item.TypeFlags, item.Codename)); fault != nil {
				return storageRefusal(fault.Code)
			}
		case wire.MoveTypeStorageGoldDeposit:
			amount := int64(q.GoldAmount)
			// No amount is the original's own refusal, before any cap
			// (#4490 U6-16); too little gold keeps its code below.
			if amount <= 0 {
				return storageRefusal(wire.ErrCodePositiveNumberOnly)
			}
			// The client caps a deposit at 100,000,000 natively (59A430);
			// SRO_RAISED_GOLD_CAPS raises both sides, to 10,000,000,000 or
			// to the shard's chosen value (goldcaps.go).
			if uint64(amount) > rt.storageGoldDepositLimit() {
				return storageRefusal(wire.ErrCodeInvalidRequest)
			}
			if amount > gold {
				return storageRefusal(wire.ErrCodeNotEnoughGold)
			}
			if storage.Gold > math.MaxInt64-amount {
				return storageRefusal(wire.ErrCodeInvalidRequest)
			}
			gold -= amount
			storage.Gold += amount
		case wire.MoveTypeStorageGoldWithdraw:
			amount := int64(q.GoldAmount)
			if amount <= 0 {
				return storageRefusal(wire.ErrCodePositiveNumberOnly)
			}
			if amount > storage.Gold {
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
