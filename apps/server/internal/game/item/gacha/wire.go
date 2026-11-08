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
	// OpWinAnnounce is the shard-wide win banner the client renders
	// as UIIT_MSG_GACHA_ANNOUNCE (client 768530, notice window only).
	// The reference server broadcasts it to every world when a WIN
	// voucher is exchanged (4EDF80; 0x3120 in v1.188), never at the
	// roll (#2852).
	OpWinAnnounce uint16 = 0x30C0

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

// EncodeWinAnnounce is 30C0 [prizeRef:u32][qty:u16]: the prize is named
// through the client's catalogue, never the winner, so one body serves
// every session that receives the banner.
func EncodeWinAnnounce(prizeRefObjID uint32, quantity uint16) []byte {
	return wire.NewWriter(6).U32(prizeRefObjID).U16(quantity).Payload()
}

// EncodeExchangeResult is the committed exchange's B1BE [1][win]: the
// reference server writes the success byte and the voucher reference's
// win flag (4EDF80); client 766A90 reads both and acts only on flag 2.
func EncodeExchangeResult(won bool) []byte {
	flag := uint8(0)
	if won {
		flag = 1
	}
	return []byte{1, flag}
}

// EncodeRefusal is the refused-request body a refused roll or exchange
// answers with. The v1.150 wire is two bytes, [flag=2][u8 code]: its reply
// handlers read exactly a flag byte and one code byte on 0xB053 and
// 0xB1BE. The stage codes stay u16 in the server as the native stage
// documentation; the encoder narrows to the low byte the client reads.
func EncodeRefusal(code uint16) []byte {
	return wire.NewWriter(2).U8(2).U8(byte(code)).Payload()
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
