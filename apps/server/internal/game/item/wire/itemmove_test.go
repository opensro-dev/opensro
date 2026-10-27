package wire

import (
	"errors"
	"reflect"
	"testing"
)

// The CSOItem body sub_78c830 writes: refObjId, plus, variance, durability,
// magic-option count. Same shape the 0x31DB equipment rows use.
func TestItemBodyLayout(t *testing.T) {
	got := ItemBody{RefObjID: 11459, Durability: 100}.Encode()

	want := []byte{
		0xC3, 0x2C, 0x00, 0x00, // refObjId 11459
		0,                      // plus
		0, 0, 0, 0, 0, 0, 0, 0, // variance bits
		100, 0, 0, 0, // durability
		0, // magic-option count
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("body = % X, want % X", got, want)
	}
	if len(got) != ItemBodySize {
		t.Fatalf("size = %d, want %d", len(got), ItemBodySize)
	}
}

// Every success shape the (retired) launcher-api fixture emitted, byte for byte.
func TestItemMoveResultEncodings(t *testing.T) {
	cases := []struct {
		name string
		got  []byte
		want []byte
	}{
		{
			// sub_759a30 case 0 @0x00759ac5: [src][dst][qty u16], then the
			// sub-move count @0x00759aea.
			name: "type 0 bag move",
			got:  EncodeInventoryMoveResult(13, 14, 1, nil),
			want: []byte{0x01, 0x00, 13, 14, 1, 0, 0x00},
		},
		{
			// sub_759a30 case 7 @0x00759c8c: [src].
			name: "type 7 ground drop",
			got:  EncodeGroundDropResult(13),
			want: []byte{0x01, 0x07, 13},
		},
		{
			// sub_759a30 case 0xa @0x00759fbd: [amount u32].
			name: "type 0x0A gold drop",
			got:  EncodeGoldDropResult(8800),
			want: []byte{0x01, 0x0A, 0x60, 0x22, 0x00, 0x00},
		},
		{
			// Type 6 with the gold sentinel slot: the remainder is a u32.
			name: "type 6 gold pickup",
			got:  EncodePickupGoldResult(8800),
			want: []byte{0x01, 0x06, 0xFE, 0x60, 0x22, 0x00, 0x00},
		},
		{
			// Type 6 with a real slot: the remainder is the CSOItem body.
			name: "type 6 item pickup",
			got:  EncodePickupItemResult(13, ItemBody{RefObjID: 11459, Durability: 100}),
			want: []byte{
				0x01, 0x06, 13,
				0xC3, 0x2C, 0x00, 0x00,
				0,
				0, 0, 0, 0, 0, 0, 0, 0,
				100, 0, 0, 0,
				0,
			},
		},
		{
			// UIIT_MSG_STRGERR_INVENTORY_FULL
			name: "failure row",
			got:  EncodeItemMoveError(ErrCodeStorageFull),
			want: []byte{0x02, 0x07},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if !reflect.DeepEqual(testCase.got, testCase.want) {
				t.Fatalf("payload = % X, want % X", testCase.got, testCase.want)
			}
		})
	}
}

func TestDecodeItemMoveResultDiscriminatesEveryType(t *testing.T) {
	cases := []struct {
		name            string
		payload         []byte
		pickupTypeFlags uint16
		want            ItemMoveResult
	}{
		{
			name:    "failure carries only the code",
			payload: EncodeItemMoveError(ErrCodeCannotBePicked),
			want:    ItemMoveResult{Result: ResultError, ErrorCode: ErrCodeCannotBePicked},
		},
		{
			name:    "bag move",
			payload: EncodeInventoryMoveResult(13, 20, 5, nil),
			want: ItemMoveResult{
				Result:       ResultSuccess,
				MovementType: MoveTypeInventory,
				SourceSlot:   13,
				DestSlot:     20,
				Quantity:     5,
			},
		},
		{
			name:    "ground drop",
			payload: EncodeGroundDropResult(17),
			want: ItemMoveResult{
				Result:       ResultSuccess,
				MovementType: MoveTypeGroundDrop,
				SourceSlot:   17,
			},
		},
		{
			name:    "gold drop",
			payload: EncodeGoldDropResult(1234),
			want: ItemMoveResult{
				Result:       ResultSuccess,
				MovementType: MoveTypeGoldDrop,
				GoldAmount:   1234,
			},
		},
		{
			name:    "gold pickup",
			payload: EncodePickupGoldResult(4321),
			want: ItemMoveResult{
				Result:       ResultSuccess,
				MovementType: MoveTypePickup,
				PickupSlot:   PickupGoldSlot,
				GoldAmount:   4321,
			},
		},
		{
			name:            "item pickup",
			payload:         EncodePickupItemResult(14, ItemBody{RefObjID: 11459, Plus: 3, Durability: 100}),
			pickupTypeFlags: PackTypeFlags(3, 1, 6, 2),
			want: ItemMoveResult{
				Result:       ResultSuccess,
				MovementType: MoveTypePickup,
				PickupSlot:   14,
				Item: ItemBody{
					RefObjID:   11459,
					TypeFlags:  PackTypeFlags(3, 1, 6, 2),
					Plus:       3,
					Durability: 100,
				},
			},
		},
		{
			name: "gacha-card pickup uses authoritative type metadata",
			payload: EncodePickupItemResult(15, ItemBody{
				RefObjID:     50001,
				TypeFlags:    PackTypeFlags(3, 3, 14, 2),
				Quantity:     2,
				MagicOptions: []uint64{70001, 3},
			}),
			pickupTypeFlags: PackTypeFlags(3, 3, 14, 2),
			want: ItemMoveResult{
				Result:       ResultSuccess,
				MovementType: MoveTypePickup,
				PickupSlot:   15,
				Item: ItemBody{
					RefObjID:     50001,
					TypeFlags:    PackTypeFlags(3, 3, 14, 2),
					Quantity:     2,
					MagicOptions: []uint64{70001, 3},
				},
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := DecodeItemMoveResult(testCase.payload, testCase.pickupTypeFlags)
			if err != nil {
				t.Fatalf("DecodeItemMoveResult failed: %v", err)
			}
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("decoded = %+v, want %+v", got, testCase.want)
			}
		})
	}
}

// The gold sentinel is what tells the two type-6 remainders apart; a real slot
// must not be mistaken for it.
func TestIsGoldPickupOnlyForSentinelSlot(t *testing.T) {
	gold, err := DecodeItemMoveResult(EncodePickupGoldResult(10), 0)
	if err != nil {
		t.Fatalf("decoding the gold grant failed: %v", err)
	}
	if !gold.IsGoldPickup() {
		t.Fatal("slot 0xFE grant was not reported as gold")
	}

	item, err := DecodeItemMoveResult(
		EncodePickupItemResult(13, ItemBody{RefObjID: 1}),
		PackTypeFlags(3, 1, 6, 2),
	)
	if err != nil {
		t.Fatalf("decoding the item grant failed: %v", err)
	}
	if item.IsGoldPickup() {
		t.Fatal("slot 13 grant was reported as gold")
	}
}

// sub_697e80 @0x00697f8e writes the sub-move count and then five bytes per
// row; sub_759a30 @0x00759b04 reads the same five back.
func TestInventoryMoveResultCarriesSubMoveRows(t *testing.T) {
	subMoves := []SubMove{
		{MovementType: MoveTypeInventory, SourceSlot: 13, DestSlot: 14, Quantity: 20},
		{MovementType: MoveTypeInventory, SourceSlot: 15, DestSlot: 16, Quantity: 30},
	}

	payload := EncodeInventoryMoveResult(13, 14, 50, subMoves)
	want := []byte{
		0x01, 0x00, 13, 14, 50, 0,
		2,                   // sub-move count
		0x00, 13, 14, 20, 0, // row 1
		0x00, 15, 16, 30, 0, // row 2
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("payload = % X, want % X", payload, want)
	}

	got, err := DecodeItemMoveResult(payload, 0)
	if err != nil {
		t.Fatalf("DecodeItemMoveResult failed: %v", err)
	}
	if !reflect.DeepEqual(got.SubMoves, subMoves) {
		t.Fatalf("sub-moves = %+v, want %+v", got.SubMoves, subMoves)
	}
}

func TestItemMoveRequestEncodings(t *testing.T) {
	cases := []struct {
		name    string
		request ItemMoveRequest
		want    []byte
	}{
		{
			// sub_697e80 @0x00697f7c
			name:    "type 0 bag move",
			request: ItemMoveRequest{MovementType: MoveTypeInventory, SourceSlot: 13, DestSlot: 14, Quantity: 1},
			want:    []byte{0x00, 13, 14, 1, 0},
		},
		{
			// sub_697e80 @0x006980d0
			name:    "type 7 ground drop",
			request: ItemMoveRequest{MovementType: MoveTypeGroundDrop, SourceSlot: 13},
			want:    []byte{0x07, 13},
		},
		{
			// sub_697e80 @0x006984cb
			name:    "type 0x0A gold drop",
			request: ItemMoveRequest{MovementType: MoveTypeGoldDrop, GoldAmount: 8800},
			want:    []byte{0x0A, 0x60, 0x22, 0x00, 0x00},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := testCase.request.Encode()
			if err != nil {
				t.Fatalf("Encode failed: %v", err)
			}
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("payload = % X, want % X", got, testCase.want)
			}

			decoded, err := DecodeItemMoveRequest(got)
			if err != nil {
				t.Fatalf("DecodeItemMoveRequest failed: %v", err)
			}
			if !reflect.DeepEqual(decoded, testCase.request) {
				t.Fatalf("decoded = %+v, want %+v", decoded, testCase.request)
			}
		})
	}
}

// The serializer clamps at its head (@0x00697eb5) before the amount is ever
// written, so an over-ceiling request goes out clamped rather than truncated.
func TestGoldDropRequestClampsAtTheNativeCeiling(t *testing.T) {
	request := ItemMoveRequest{MovementType: MoveTypeGoldDrop, GoldAmount: MaxGold + 1000}

	got, err := request.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	want := []byte{0x0A, 0x00, 0xE1, 0xF5, 0x05} // 0x5F5E100
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = % X, want % X", got, want)
	}
}

func TestGoldDropResultClampsAtTheNativeCeiling(t *testing.T) {
	got := EncodeGoldDropResult(MaxGold + 1)

	want := []byte{0x01, 0x0A, 0x00, 0xE1, 0xF5, 0x05}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = % X, want % X", got, want)
	}
}

// Guessing at a layout is worse than refusing: the native jump table covers
// 0x00..0x24 but only the wave's types have established layouts here.
func TestUnsupportedMovementTypesAreRefused(t *testing.T) {
	if _, err := (ItemMoveRequest{MovementType: 0x18}).Encode(); err == nil {
		t.Fatal("encoding an unestablished movement type returned nil error")
	}
	var target ErrUnsupportedMovementType
	_, err := DecodeItemMoveRequest([]byte{0x18, 0, 0, 0})
	if !errors.As(err, &target) {
		t.Fatalf("error = %v, want ErrUnsupportedMovementType", err)
	}
	if uint8(target) != 0x18 {
		t.Fatalf("error carried type 0x%02X, want 0x18", uint8(target))
	}
}

func TestDecodeItemMoveResultRejectsUnknownResultByte(t *testing.T) {
	if _, err := DecodeItemMoveResult([]byte{0x03, 0x00}, 0); err == nil {
		t.Fatal("unknown result byte returned nil error")
	}
}

// A type-0 request that ends after the quantity is well-formed: the serializer
// only writes the sub-move count on the branch that has rows to describe.
func TestItemMoveRequestSubMoveCountIsOptional(t *testing.T) {
	got, err := DecodeItemMoveRequest([]byte{0x00, 13, 14, 1, 0})
	if err != nil {
		t.Fatalf("DecodeItemMoveRequest failed: %v", err)
	}
	if len(got.SubMoves) != 0 {
		t.Fatalf("sub-moves = %+v, want none", got.SubMoves)
	}
}

/*
================
TestGoldGroupItemBodyCarriesU32Quantity

The v1.150 client reads a u32 amount for the gold-heap group (TID3 5) and a
u16 stack count for every other ETC group (CSOItem 78C830). An SD token
(TID 3/3/5/1) belongs to that group, so a u16 body ended the session on pickup
with "Truncated inventory item".
================
*/
func TestGoldGroupItemBodyCarriesU32Quantity(t *testing.T) {
	const sdToken = 0x0c | 0x60 | 0x280 | 0x800
	body := ItemBody{RefObjID: 42864, TypeFlags: sdToken, Quantity: 5}
	got := body.Encode()
	want := []byte{
		0x70, 0xA7, 0x00, 0x00, // refObjId 42864
		5, 0, 0, 0, // u32 amount
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("body = % X, want % X", got, want)
	}
	if len(got) != body.EncodedSize() {
		t.Fatalf("EncodedSize = %d, body is %d bytes", body.EncodedSize(), len(got))
	}
	back, err := readItemBody(NewReader(got), sdToken)
	if err != nil || back.Quantity != 5 || back.RefObjID != 42864 {
		t.Fatalf("round trip = %+v, %v", back, err)
	}
}

/*
================
TestOtherEtcGroupsKeepU16Quantity
================
*/
func TestOtherEtcGroupsKeepU16Quantity(t *testing.T) {
	const potion = 0x0c | 0x60 | 0x180 // TID 3/3/3: not the gold group
	got := ItemBody{RefObjID: 1, TypeFlags: potion, Quantity: 5}.Encode()
	if want := []byte{1, 0, 0, 0, 5, 0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("body = % X, want % X", got, want)
	}
}

/*
================
TestGoldHeapExcludesShopTokens

Group 0x280 is one wire width but two meanings: gold heaps (TID 3.3.5.0)
and the later shop tokens (TID 3.3.5.1), which the native gold test
excludes by TID4.
================
*/
func TestGoldHeapExcludesShopTokens(t *testing.T) {
	const gold, sdToken = 0x0c | 0x60 | 0x280, 0x0c | 0x60 | 0x280 | 0x800
	if !IsGoldBand(gold) || !IsGoldBand(sdToken) {
		t.Fatal("both rows carry the group's u32 amount")
	}
	if !IsGoldHeap(gold) || IsGoldHeap(sdToken) {
		t.Fatalf("gold heap: gold %v, token %v; want true, false", IsGoldHeap(gold), IsGoldHeap(sdToken))
	}
}
