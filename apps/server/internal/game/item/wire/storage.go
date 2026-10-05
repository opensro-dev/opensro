/*
===========================================================================

storage.go - the NPC warehouse wire (v1.150 client)

The talk menu's storage action (CIFNpcTalk_ExecuteMenuAction case 3) sends
0x72C3 [u32 npc][u8 0] while the room is unloaded. The server answers with
the gold (0x3126, [u64]) and the item list (0x321A), after which the client
marks the room loaded (+0x7BC) and asks for the storage function itself:
0x7338 [u32 npc][u32 4], answered by B338 [1][u32 4], which opens the room
beside the inventory (CGInterface_SetNpcShopVisible( 5 )).

Native fills the 0x321A list through a buffer the transport appends to;
the port carries the parsed body in one frame:
[u8 capacity][u8 count] count x {[u8 slot][CSOItem]}.

Item moves ride 0x706D (ItemMoveRequest_Serialize) and answer on 0xB06D
with the request's body echoed (the serializer's response arm):

	0x01 room to room       [src][dst][u16 count][u32 npc] -> [src][dst][u16 count]
	0x02 bag to room        [src][dst][u32 npc]            -> [src][dst]
	0x03 room to bag        [src][dst][u32 npc]            -> [src][dst]
	0x0B gold room to bag   [u32 amount]                    -> [u32 amount]
	0x0C gold bag to room   [u32 amount]                    -> [u32 amount]

The guild warehouse (CIFStorageRoom window 0x91) has its own five types
with the same layouts, chosen by CGInterface_RequestItemMove (699250) and
CGInterface_RequestCosItemMove for the window pair: 0x1D room to room,
0x1E bag to room, 0x1F room to bag, 0x20 gold bag to room, 0x21 gold room
to bag. Its gold and list ride 0x34A9 and 0x3363 in the personal layouts.

===========================================================================
*/
package wire

const (
	OpStorageListRequest uint16 = 0x72C3
	OpStorageGold        uint16 = 0x3126
	OpStorageList        uint16 = 0x321A
)

const (
	MoveTypeStorage             uint8 = 0x01
	MoveTypeStorageDeposit      uint8 = 0x02
	MoveTypeStorageWithdraw     uint8 = 0x03
	MoveTypeStorageGoldWithdraw uint8 = 0x0B
	MoveTypeStorageGoldDeposit  uint8 = 0x0C

	MoveTypeGuildStorage             uint8 = 0x1D
	MoveTypeGuildStorageDeposit      uint8 = 0x1E
	MoveTypeGuildStorageWithdraw     uint8 = 0x1F
	MoveTypeGuildStorageGoldDeposit  uint8 = 0x20
	MoveTypeGuildStorageGoldWithdraw uint8 = 0x21
)

// The guild warehouse's gold and list pushes (CNetProcessSecond 0x34A9 ->
// CIFStorageRoom_SetGold, 0x3363 -> 7665D0).
const (
	OpGuildStorageGold uint16 = 0x34A9
	OpGuildStorageList uint16 = 0x3363
)

/*
================
PersonalStorageMove

The personal warehouse type a guild warehouse type stands for, and
whether it was one.
================
*/
func PersonalStorageMove(movement uint8) (uint8, bool) {
	switch movement {
	case MoveTypeGuildStorage:
		return MoveTypeStorage, true
	case MoveTypeGuildStorageDeposit:
		return MoveTypeStorageDeposit, true
	case MoveTypeGuildStorageWithdraw:
		return MoveTypeStorageWithdraw, true
	case MoveTypeGuildStorageGoldDeposit:
		return MoveTypeStorageGoldDeposit, true
	case MoveTypeGuildStorageGoldWithdraw:
		return MoveTypeStorageGoldWithdraw, true
	}
	return movement, false
}

// StorageFunctionMask is the NPC capability bit and the B338 lock value of
// the warehouse (CPSMission_OnNpcInteractionResponse0xB338, lock 4).
const StorageFunctionMask uint32 = 4

/*
================
StorageListRow
================
*/
type StorageListRow struct {
	Slot uint8
	Body []byte
}

/*
================
DecodeStorageListRequest

[u32 npc gid][u8 0].
================
*/
func DecodeStorageListRequest(payload []byte) (uint32, error) {
	r := NewReader(payload)
	gid, err := r.U32()
	if err != nil {
		return 0, err
	}
	if _, err := r.U8(); err != nil {
		return 0, err
	}
	return gid, r.Done()
}

/*
================
EncodeStorageGold
================
*/
func EncodeStorageGold(gold uint64) []byte {
	return NewWriter(8).U64(gold).Payload()
}

/*
================
EncodeStorageList
================
*/
func EncodeStorageList(capacity uint8, rows []StorageListRow) []byte {
	w := NewWriter(2 + len(rows)*64).U8(capacity).U8(uint8(len(rows)))
	for _, row := range rows {
		w.U8(row.Slot).Bytes(row.Body)
	}
	return w.Payload()
}

/*
================
EncodeStorageMoveSuccess

0xB06D [1][type] then the serializer's response arm for that type.
================
*/
func EncodeStorageMoveSuccess(q ItemMoveRequest) []byte {
	w := NewWriter(8).U8(1).U8(q.MovementType)
	switch q.MovementType {
	case MoveTypeStorage, MoveTypeGuildStorage:
		w.U8(q.SourceSlot).U8(q.DestSlot).U16(q.Quantity)
	case MoveTypeStorageDeposit, MoveTypeStorageWithdraw, MoveTypeGuildStorageDeposit, MoveTypeGuildStorageWithdraw:
		w.U8(q.SourceSlot).U8(q.DestSlot)
	default:
		w.U32(q.GoldAmount)
	}
	return w.Payload()
}
