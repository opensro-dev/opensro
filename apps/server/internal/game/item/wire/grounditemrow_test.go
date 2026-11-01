package wire

import (
	"reflect"
	"testing"
)

// Type-flag words built the way the fixture does, from the itemdata TID
// quadruple: (tid1<<2) | (tid2<<5) | (tid3<<7) | (tid4<<11).
const (
	// ITEM_ETC_GOLD_0x, TID (3,3,5,0) -> the gold heap group 0x280.
	goldHeapTypeFlags uint16 = (3 << 2) | (3 << 5) | (5 << 7) | (0 << 11)
	// A CH garment head piece, TID (3,1,1,1) -> the equipment band.
	equipmentTypeFlags uint16 = (3 << 2) | (1 << 5) | (1 << 7) | (1 << 11)
	// An ETC row in the 0x400 group, TID (3,3,8,0) -> the codename band.
	codenameTypeFlags uint16 = (3 << 2) | (3 << 5) | (8 << 7) | (0 << 11)
)

func TestTypeFlagBandsAreMutuallyExclusive(t *testing.T) {
	cases := []struct {
		name      string
		typeFlags uint16
		equipment bool
		codename  bool
		gold      bool
	}{
		{"gold heap", goldHeapTypeFlags, false, false, true},
		{"equipment", equipmentTypeFlags, true, false, false},
		{"etc with codename", codenameTypeFlags, false, true, false},
		{"character class is not an item at all", 0x0002, false, false, false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := IsEquipmentBand(testCase.typeFlags); got != testCase.equipment {
				t.Fatalf("IsEquipmentBand(0x%04X) = %v, want %v", testCase.typeFlags, got, testCase.equipment)
			}
			if got := IsCodenameBand(testCase.typeFlags); got != testCase.codename {
				t.Fatalf("IsCodenameBand(0x%04X) = %v, want %v", testCase.typeFlags, got, testCase.codename)
			}
			if got := IsGoldBand(testCase.typeFlags); got != testCase.gold {
				t.Fatalf("IsGoldBand(0x%04X) = %v, want %v", testCase.typeFlags, got, testCase.gold)
			}
		})
	}
}

// The gold heap word the fixture computes must actually land in the gold band,
// otherwise a dropped pile would spawn without its amount.
func TestGoldHeapTypeFlagsLandInTheGoldBand(t *testing.T) {
	if goldHeapTypeFlags != 0x2EC {
		t.Fatalf("gold heap type flags = 0x%04X, want 0x02EC", goldHeapTypeFlags)
	}
	if !IsGoldBand(goldHeapTypeFlags) {
		t.Fatal("the gold heap TID quadruple does not satisfy the gold band predicate")
	}
}

// A gold heap row carries the amount between the ref id and the gid, and the
// single-object 0x30D7 form appends the appear byte the list path omits.
func TestGoldHeapRowLayout(t *testing.T) {
	row := GroundItemRow{
		RefObjID:       3810,
		TypeFlags:      goldHeapTypeFlags,
		GoldAmount:     8800,
		Gid:            300001,
		Position:       Position{RegionID: 0x6B4F, X: 1205, Y: 80, Z: 396, Heading: 0},
		WithAppearTail: true,
		AppearFlag:     1,
	}

	want := concat(
		[]byte{0xE2, 0x0E, 0x00, 0x00}, // refObjId 3810
		[]byte{0x60, 0x22, 0x00, 0x00}, // gold amount 8800, gold band only
		[]byte{0xE1, 0x93, 0x04, 0x00}, // gid 300001
		[]byte{0x4F, 0x6B},             // region
		f32le(1205), f32le(80), f32le(396),
		[]byte{0x00, 0x00}, // heading
		[]byte{0x00},       // hasOwner: unowned drop
		[]byte{0x00},       // tint
		[]byte{0x01},       // appear tail, 0x30D7 only
	)
	if got := row.Encode(); !reflect.DeepEqual(got, want) {
		t.Fatalf("row = % X, want % X", got, want)
	}
}

func TestOwnedGroundRowCarriesConditionalOwnerJIDBeforeTint(t *testing.T) {
	row := GroundItemRow{
		RefObjID: 3810, TypeFlags: goldHeapTypeFlags, GoldAmount: 28,
		Gid: 300001, Position: Position{RegionID: 0x62A8},
		HasOwner: 1, OwnerJID: 100003, Tint: 7,
		WithAppearTail: true, AppearFlag: 1,
	}
	encoded := row.Encode()
	decoded, err := DecodeGroundItemRow(encoded, goldHeapTypeFlags, true)
	if err != nil {
		t.Fatalf("owned row decode: %v", err)
	}
	if !reflect.DeepEqual(decoded, row) {
		t.Fatalf("owned row = %+v, want %+v", decoded, row)
	}
	wantTail := []byte{1, 0xA3, 0x86, 0x01, 0x00, 7, 1}
	if got := encoded[len(encoded)-len(wantTail):]; !reflect.DeepEqual(got, wantTail) {
		t.Fatalf("owned tail = % X, want % X", got, wantTail)
	}
}

// The list path (0x3417) must produce the same row minus the appear byte.
func TestListRowOmitsTheAppearTail(t *testing.T) {
	base := GroundItemRow{
		RefObjID:  3810,
		TypeFlags: goldHeapTypeFlags,
		Gid:       300001,
		Position:  Position{RegionID: 0x6B4F},
	}

	withTail := base
	withTail.WithAppearTail = true
	withTail.AppearFlag = 1

	listRow := base.Encode()
	singleRow := withTail.Encode()

	if len(singleRow) != len(listRow)+1 {
		t.Fatalf("single-object row is %d bytes and the list row %d; want exactly one more",
			len(singleRow), len(listRow))
	}
	if !reflect.DeepEqual(singleRow[:len(listRow)], listRow) {
		t.Fatalf("rows diverge before the tail:\n single = % X\n list   = % X", singleRow, listRow)
	}
	if singleRow[len(singleRow)-1] != 1 {
		t.Fatalf("appear byte = 0x%02X, want 0x01", singleRow[len(singleRow)-1])
	}
}

func TestEquipmentRowCarriesTheDiscardByte(t *testing.T) {
	row := GroundItemRow{
		RefObjID:  11459,
		TypeFlags: equipmentTypeFlags,
		Gid:       300002,
		Position:  Position{RegionID: 0x6B4F},
	}

	got := row.Encode()
	want := concat(
		[]byte{0xC3, 0x2C, 0x00, 0x00}, // refObjId
		[]byte{0x00},                   // equipment discard byte
		[]byte{0xE2, 0x93, 0x04, 0x00}, // gid 300002
		[]byte{0x4F, 0x6B},
		f32le(0), f32le(0), f32le(0),
		[]byte{0x00, 0x00, 0x00, 0x00},
	)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("row = % X, want % X", got, want)
	}
}

/*
================
TestCodenameRowCarriesTheOwnerLabel

The 0x400/0x480 band carries the owner label CIItem_ParseGroundSpawn
(client 86E5D8) reads: empty without an owner (u16 0), the trader's alias for
dropped trade goods. The registry's codename never reaches the wire.
================
*/
func TestCodenameRowCarriesTheOwnerLabel(t *testing.T) {
	for _, label := range []string{"", "Trader"} {
		row := GroundItemRow{
			RefObjID:   1234,
			TypeFlags:  codenameTypeFlags,
			Codename:   "ITEM_ETC_E_1",
			OwnerLabel: label,
			Gid:        300003,
			Position:   Position{RegionID: 0x6B4F},
		}
		want := concat(
			[]byte{0xD2, 0x04, 0x00, 0x00}, // refObjId 1234
			[]byte{byte(len(label)), 0x00}, // owner label length
			[]byte(label),
			[]byte{0xE3, 0x93, 0x04, 0x00}, // gid 300003
			[]byte{0x4F, 0x6B},
			f32le(0), f32le(0), f32le(0),
			[]byte{0x00, 0x00, 0x00, 0x00},
		)
		if got := row.Encode(); !reflect.DeepEqual(got, want) {
			t.Fatalf("label %q: row = % X, want % X", label, got, want)
		}
		decoded, err := DecodeGroundItemRow(want, codenameTypeFlags, false)
		if err != nil || decoded.OwnerLabel != label || decoded.Codename != "" {
			t.Fatalf("label %q: decoded %+v, %v", label, decoded, err)
		}
	}
}

func TestGroundItemRowRoundTripsEveryBand(t *testing.T) {
	cases := []struct {
		name string
		row  GroundItemRow
	}{
		{
			name: "gold heap",
			row: GroundItemRow{
				RefObjID: 3810, TypeFlags: goldHeapTypeFlags, GoldAmount: 8800,
				Gid: 300001, Position: testPosition(), Tint: 0,
				WithAppearTail: true, AppearFlag: 1,
			},
		},
		{
			name: "equipment",
			row: GroundItemRow{
				RefObjID: 11459, TypeFlags: equipmentTypeFlags,
				Gid: 300002, Position: testPosition(),
			},
		},
		{
			// The encoder emits the empty label, so only an empty Codename
			// round-trips; the foreign-label decode has its own test.
			name: "etc with empty label",
			row: GroundItemRow{
				RefObjID: 1234, TypeFlags: codenameTypeFlags,
				Gid: 300003, Position: testPosition(),
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			encoded := testCase.row.Encode()
			got, err := DecodeGroundItemRow(encoded, testCase.row.TypeFlags, testCase.row.WithAppearTail)
			if err != nil {
				t.Fatalf("DecodeGroundItemRow failed: %v", err)
			}
			if !reflect.DeepEqual(got, testCase.row) {
				t.Fatalf("decoded = %+v, want %+v", got, testCase.row)
			}
		})
	}
}
