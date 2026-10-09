package wire

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// Real shipped itemdata TID words (extracted/Media_extracted/server_dep/
// silkroad/textdata/itemdata_5000.txt, columns 9-12):
//
//	ITEM_CH_BLADE_01_A          refObjId 107  TID 3.1.6.3 -> 0x1B2C
//	ITEM_CH_M_CLOTHES_01_BA_A_DEF  3643      TID 3.1.1.3 -> 0x18AC
//	ITEM_CH_M_CLOTHES_01_LA_A_DEF  3644      TID 3.1.1.4 -> 0x20AC
//	ITEM_CH_M_CLOTHES_01_FA_A_DEF  3645      TID 3.1.1.6 -> 0x30AC
//	ITEM_ETC_GOLD_01               1         TID 3.3.5.0 -> 0x02EC
const (
	tidChBlade     = 0x1B2C
	tidChClothesBA = 0x18AC
	tidChClothesLA = 0x20AC
	tidChClothesFA = 0x30AC
	tidEtcGold     = 0x02EC
)

// u16le/u32le are the little-endian scalar helpers; f32le and concat come
// from objectmove_test.go (shared test package).
func u16le(v uint16) []byte { b := make([]byte, 2); binary.LittleEndian.PutUint16(b, v); return b }
func u32le(v uint32) []byte { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, v); return b }

// TestInterfaceEquipmentSlotForTid pins the sub_868a80 port on real shipped
// words plus the synthetic slot-8 family word (no TID *.*.7.* row exists in
// the v1.150 itemdata - the family itself is real, see the classifier's
// @0x868b5f arm).
func TestInterfaceEquipmentSlotForTid(t *testing.T) {
	cases := []struct {
		tid  uint16
		slot int
	}{
		{tidChClothesBA, 1},            // subtype 3 -> body (jump_table_868b90[3])
		{tidChClothesLA, 4},            // subtype 4 -> leg
		{tidChClothesFA, 5},            // subtype 6 -> foot
		{tidChBlade, 6},                // 0x780==0x300 gear @0x868b1b
		{PackTypeFlags(3, 1, 4, 2), 7}, // shield family -> sub_5932c0 protector group
		{PackTypeFlags(3, 1, 7, 1), 8}, // 0x780==0x380 family @0x868b5f
		{tidEtcGold, -1},               // ETC band: no interface slot @0x868b8e
	}
	for _, c := range cases {
		if got := InterfaceEquipmentSlotForTid(c.tid); got != c.slot {
			t.Errorf("InterfaceEquipmentSlotForTid(0x%04X) = %d, want %d", c.tid, got, c.slot)
		}
	}
}

// TestWeaponHoldTypeForTid pins the sub_868d00 port arm for arm. The
// TID *.*.7.* words are synthetic classifier pins (the family is absent from
// the shipped v1.150 itemdata, so live rows always classify 4).
func TestWeaponHoldTypeForTid(t *testing.T) {
	cases := []struct {
		name string
		ref  uint32
		tid  uint16
		want uint8
	}{
		{"unarmed refObjId 0 @0x868d06", 0, 0, 4},
		{"0x380/0x1800 family @0x868d27", 9001, PackTypeFlags(3, 1, 7, 3), 3},
		{"two-hand grip sub_5931b0 @0x868d61", 9002, PackTypeFlags(3, 1, 7, 1), 1},
		{"job uniform sub_5931f0 @0x868d73", 9003, PackTypeFlags(3, 1, 7, 2), 2},
		{"plain blade misses every arm", 107, tidChBlade, 4},
	}
	for _, c := range cases {
		if got := WeaponHoldTypeForTid(c.ref, c.tid); got != c.want {
			t.Errorf("%s: WeaponHoldTypeForTid(%d, 0x%04X) = %d, want %d", c.name, c.ref, c.tid, got, c.want)
		}
	}
}

func testRow() PlayerSpawnRow {
	return PlayerSpawnRow{
		RefObjID:       1907,
		BodyShapeByte:  4,
		Gid:            100042,
		Position:       Position{RegionID: 25000, X: 1616, Y: 20, Z: 1650, Heading: 0x1234},
		WalkSpeed:      20,
		RunSpeed:       50,
		ScaleDenom:     100,
		Name:           "PeerA",
		WithAppearTail: true,
		AppearFlag:     1,
	}
}

// TestPlayerSpawnRowEncodeUnarmedGolden is the byte-for-byte pin of the
// unarmed row against the layout the client's REAL sub_86afb0 fold chain
// reads (the wave-3 harness fixture shape, cicUserSpawnMinimapParity.test.ts
// userPayload, with live values).
func TestPlayerSpawnRowEncodeUnarmedGolden(t *testing.T) {
	row := testRow()

	want := concat(
		u32le(1907),   // sub_850c60 resolve target
		[]byte{4, 0},  // sub_869110: +0x758 body shape + cf0100 scratch
		[]byte{0, 0},  // equip loop: discard + count 0 (@0x0086afee)
		[]byte{0, 0},  // avatar loop: discard + count 0 (@0x0086b0ab)
		[]byte{0},     // no transform skin (@0x0086b14f)
		u32le(100042), // gid (sub_852f80)
		u16le(25000),  // region
		f32le(1616), f32le(20), f32le(1650),
		u16le(0x1234),        // heading word
		[]byte{0, 0},         // sub_776170 mode 0 + second flag
		[]byte{0},            // rotation flag (mode-0 arm)
		u16le(0x1234),        // packed heading word
		[]byte{0, 0, 0},      // +0x263/+0x264/+0x460
		f32le(20), f32le(50), // walk/run (+0x24c/+0x250)
		f32le(100),                // scale denominator FLOAT (@0x0085fba5)
		[]byte{0},                 // mastery count
		u16le(5), []byte("PeerA"), // display name (sub_4b1710)
		[]byte{0, 0},    // job type/grade (+0x782/+0x783)
		[]byte{0, 0, 0}, // +0x4f4 / ride state / +0x781
		[]byte{0, 0, 0}, // non-local +0x784 / title mode / +0x4f6
		u16le(0),        // guild name empty
		// hold type 4 (unarmed) -> the @0x0086a12d guild-member sub-block:
		u32le(0),                     // guild id
		u16le(0),                     // grant name empty
		u32le(0), u32le(0), u32le(0), // war dwords
		[]byte{0},    // team byte
		[]byte{0},    // action progress (+0x780)
		[]byte{0xff}, // inactive event team (+0x7e1)
		[]byte{1},    // vt+0x68 appear byte (single mode)
	)

	got := row.Encode()
	if !bytes.Equal(got, want) {
		t.Fatalf("unarmed row bytes drifted:\n got %x\nwant %x", got, want)
	}
}

// TestPlayerSpawnRowEquipLoopOptLevelGate: equip-band items carry the
// @0x0086b05c optLevel byte, non-equip-band items (the ETC gold word) do
// not - the same predicate on both sides or the stream desyncs.
func TestPlayerSpawnRowEquipLoopOptLevelGate(t *testing.T) {
	row := testRow()
	row.Equipment = []PlayerEquipItem{
		{RefObjID: 3643, TypeFlags: tidChClothesBA, OptLevel: 2},
		{RefObjID: 1, TypeFlags: tidEtcGold}, // not equip-band: no optLevel byte
		{RefObjID: 107, TypeFlags: tidChBlade, OptLevel: 5},
	}

	payload := row.Encode()
	// The equip loop starts after refObjId(4) + the two bind bytes: discard,
	// count, then the rows.
	loop := payload[6:]
	if loop[1] != 3 {
		t.Fatalf("equip count byte = %d, want 3", loop[1])
	}
	rows := loop[2:]
	expect := concat(
		u32le(3643), []byte{2}, // equip band: refId + optLevel
		u32le(1),              // ETC band: refId only
		u32le(107), []byte{5}, // equip band: refId + optLevel
	)
	if !bytes.Equal(rows[:len(expect)], expect) {
		t.Fatalf("equip rows drifted:\n got %x\nwant %x", rows[:len(expect)], expect)
	}

	decoded, err := DecodePlayerSpawnRow(payload, map[uint32]uint16{
		3643: tidChClothesBA, 1: tidEtcGold, 107: tidChBlade,
	}, true)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Equipment) != 3 || decoded.Equipment[0].OptLevel != 2 || decoded.Equipment[2].OptLevel != 5 {
		t.Fatalf("decoded equipment = %+v", decoded.Equipment)
	}
}

// TestPlayerSpawnRowGuildBlockHoldTypeGate: a slot-8 item whose hold type is
// not 4 (the synthetic two-hand-grip classifier word) removes the
// guild-member sub-block, exactly like the client's member-class vcall gate
// @0x0086a12d - and the row still decodes to completion (no desync).
func TestPlayerSpawnRowGuildBlockHoldTypeGate(t *testing.T) {
	gripTid := PackTypeFlags(3, 1, 7, 1)

	unarmed := testRow()
	gripped := testRow()
	gripped.Equipment = []PlayerEquipItem{{RefObjID: 9002, TypeFlags: gripTid, OptLevel: 1}}

	unarmedRow := unarmed.Encode()
	grippedRow := gripped.Encode()

	// The sub-block is 4+2+12+1 = 19 bytes; the gripped row additionally
	// carries 5 equip-loop bytes (u32 ref + optLevel), so the delta is
	// 19 - 5 = 14 bytes.
	if len(unarmedRow)-len(grippedRow) != 19-5 {
		t.Fatalf("guild sub-block gate did not fire: unarmed %d bytes, gripped %d bytes",
			len(unarmedRow), len(grippedRow))
	}

	tids := map[uint32]uint16{9002: gripTid}
	decoded, err := DecodePlayerSpawnRow(grippedRow, tids, true)
	if err != nil {
		t.Fatalf("gripped row decode: %v", err)
	}
	if decoded.Name != "PeerA" || decoded.AppearFlag != 1 {
		t.Fatalf("gripped row decoded = %+v", decoded)
	}
}

// TestPlayerSpawnRowGuildBlockGolden pins the guild-armed tail byte for
// byte: a non-empty guild name at @0x0086a0cc (which IS the +0x7bc
// BindGuild gate - the +0x7a8 wstring's size field) followed by the
// @0x0086a12d member sub-block carrying the guild id and the three crest
// dwords the client forwards to sub_869810 BindGuild as crestParamA/B/C
// (var_88/var_84/var_8c order @0x0086a24a..0x0086a261).
func TestPlayerSpawnRowGuildBlockGolden(t *testing.T) {
	row := testRow()
	row.GuildName = "Nine"
	row.GuildID = 7
	row.GuildGrantName = "Chief"
	row.CrestParamA = 3
	row.CrestParamB = 0
	row.CrestParamC = 0
	// The trailing team byte of the sub-block: sub_869df0 @0x0086a1b2 ->
	// sub_869940 -> CICPlayer+0x7e0 (FortSiegeAuthority; 1 = Commander,
	// one of the two values the six-mark gate accepts).
	row.FortSiegeAuthority = 1

	payload := row.Encode()

	// The guild tail sits between the three non-local bytes and the two
	// trailing state bytes + appear byte; pin it from the end backwards.
	tail := concat(
		u16le(4), []byte("Nine"), // guild name -> +0x7a8 (arms +0x7bc)
		u32le(7),                  // guild id -> BindGuild arg 2
		u16le(5), []byte("Chief"), // grant name -> +0x78c
		u32le(3),           // var_88 -> crestParamA (G-crest filename param)
		u32le(0), u32le(0), // var_84/var_8c -> crestParamB/C (alliance, 0)
		[]byte{1},       // team byte -> sub_869940 +0x7e0 (Commander)
		[]byte{0, 0xff}, // action progress + inactive event team
		[]byte{1},       // appear byte
	)
	if !bytes.Equal(payload[len(payload)-len(tail):], tail) {
		t.Fatalf("guild tail drifted:\n got %x\nwant %x", payload[len(payload)-len(tail):], tail)
	}

	decoded, err := DecodePlayerSpawnRow(payload, map[uint32]uint16{}, true)
	if err != nil {
		t.Fatalf("guild row decode: %v", err)
	}
	if decoded.GuildName != "Nine" || decoded.GuildID != 7 ||
		decoded.GuildGrantName != "Chief" || decoded.CrestParamA != 3 ||
		decoded.CrestParamB != 0 || decoded.CrestParamC != 0 ||
		decoded.FortSiegeAuthority != 1 {
		t.Fatalf("guild round trip drifted: %+v", decoded)
	}
}

// TestDecodePlayerSpawnRowRoundTrip pins the full field set through
// encode -> decode, both 0x30D7 (appear tail) and object-list forms.
func TestDecodePlayerSpawnRowRoundTrip(t *testing.T) {
	row := testRow()
	row.JobType = 2
	row.JobGrade = 5
	row.Equipment = []PlayerEquipItem{
		{RefObjID: 3643, TypeFlags: tidChClothesBA, OptLevel: 1},
		{RefObjID: 107, TypeFlags: tidChBlade, OptLevel: 3},
	}
	row.StallTitle = "Cheap blades"
	row.StallDecoration = 24701
	tids := map[uint32]uint16{3643: tidChClothesBA, 107: tidChBlade}

	for _, withTail := range []bool{true, false} {
		row.WithAppearTail = withTail
		decoded, err := DecodePlayerSpawnRow(row.Encode(), tids, withTail)
		if err != nil {
			t.Fatalf("withTail=%v decode: %v", withTail, err)
		}
		if decoded.RefObjID != row.RefObjID || decoded.Gid != row.Gid ||
			decoded.Name != row.Name || decoded.BodyShapeByte != row.BodyShapeByte ||
			decoded.JobType != 2 || decoded.JobGrade != 5 ||
			decoded.RegionID != row.RegionID || decoded.X != row.X ||
			decoded.Z != row.Z || decoded.Heading != row.Heading ||
			decoded.WalkSpeed != 20 || decoded.RunSpeed != 50 ||
			decoded.ScaleDenom != 100 || len(decoded.Equipment) != 2 ||
			decoded.StallTitle != row.StallTitle || decoded.StallDecoration != row.StallDecoration {
			t.Fatalf("withTail=%v round trip drifted: %+v", withTail, decoded)
		}
		if withTail && decoded.AppearFlag != 1 {
			t.Fatalf("appear flag lost: %+v", decoded)
		}
	}
}
