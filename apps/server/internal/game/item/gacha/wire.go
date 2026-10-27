package gacha

import (
	"fmt"

	"opensro.online/server/internal/game/item/wire"
)

const (
	// OpNpcAction is CIFNPCTalk's generic bound-NPC action request.
	OpNpcAction uint16 = 0x7338
	// OpInteractionState is CPSMission_HandlePacketB338. Kind 1 plus
	// InteractionFlagGacha opens control 0x8c.
	OpInteractionState uint16 = 0xB338
	// OpRoll is CIFGhaCha_OnTimer state 2's exact nine-byte request.
	OpRoll uint16 = 0x7053
	// OpItemStateDelta replaces the ticket with the native result-card row.
	OpItemStateDelta uint16 = 0x3645
	// OpResult is CNetProcessSecond_OnPacket_B053's two-byte result.
	OpResult uint16 = 0xB053
	// OpExchange is CIFNPCTalk's operator row (5DB67B): the bound operator NPC's gid.
	OpExchange uint16 = 0x71BE
	// OpExchangeResult answers OpExchange, as OpResult answers OpRoll.
	OpExchangeResult uint16 = 0xB1BE

	InteractionFlagGacha uint32 = 0x10000
	ResultLose           uint8  = 0
	ResultWin            uint8  = 1
)

// RollRequest is [boundNpcGid:u32][selectedEntryId:u32][inventorySlot:u8].
type RollRequest struct {
	BoundNpcGID     uint32
	SelectedEntryID uint32
	InventorySlot   uint8
}

func DecodeRollRequest(payload []byte) (RollRequest, error) {
	reader := wire.NewReader(payload)
	var out RollRequest
	var err error
	if out.BoundNpcGID, err = reader.U32(); err != nil {
		return out, err
	}
	if out.SelectedEntryID, err = reader.U32(); err != nil {
		return out, err
	}
	if out.InventorySlot, err = reader.U8(); err != nil {
		return out, err
	}
	return out, reader.Done()
}

// DecodeExchangeRequest reads 0x71BE [boundNpcGid:u32].
func DecodeExchangeRequest(payload []byte) (uint32, error) {
	reader := wire.NewReader(payload)
	gid, err := reader.U32()
	if err != nil {
		return 0, err
	}
	return gid, reader.Done()
}

// DecodeNpcAction reads the generic 0x7338 [boundNpcGid:u32][actionFlags:u32].
func DecodeNpcAction(payload []byte) (uint32, uint32, error) {
	reader := wire.NewReader(payload)
	gid, err := reader.U32()
	if err != nil {
		return 0, 0, err
	}
	flags, err := reader.U32()
	if err != nil {
		return 0, 0, err
	}
	return gid, flags, reader.Done()
}

// EncodeOpenInteraction is the B338 kind-1 body that the folded v1.150
// handler routes to CGInterface_ToggleGhachaWindow.
func EncodeOpenInteraction() []byte {
	return wire.NewWriter(5).
		U8(1).
		U32(InteractionFlagGacha).
		Payload()
}

// EncodeResult is B053 [successFlag=1][winOrLoseCode].
func EncodeResult(code uint8) []byte {
	return []byte{1, code}
}

// EncodeRefusal is the refused-request body [flag=2][u16 code] the reference
// server answers every refused roll or exchange with.
func EncodeRefusal(code uint16) []byte {
	return wire.NewWriter(3).U8(2).U16(code).Payload()
}

// EncodeResultItemDelta replaces the ticket with the selected win/loss card and
// installs the exact indexed reward pair consumed by CIFGhaCha_ApplyResult:
// mask 0x21 = ref-id + magic params.
func EncodeResultItemDelta(slot uint8, resultRefObjID, rewardRefObjID, quantity uint32) []byte {
	return wire.NewWriter(2 + 4 + 1 + 16).
		U8(slot).
		U8(0x21).
		U32(resultRefObjID).
		U8(2).
		U64(uint64(rewardRefObjID)).
		U64(uint64(quantity)).
		Payload()
}

func ValidateResultCode(code uint8) error {
	if code != ResultLose && code != ResultWin {
		return fmt.Errorf("gacha: invalid result code %d", code)
	}
	return nil
}
