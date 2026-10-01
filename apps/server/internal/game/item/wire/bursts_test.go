package wire

import (
	"bytes"
	"reflect"
	"testing"
)

func frameOpcodes(frames []Frame) []uint16 {
	out := make([]uint16, len(frames))
	for index, frame := range frames {
		out[index] = frame.Opcode
	}
	return out
}

func assertOpcodeOrder(t *testing.T, frames []Frame, want []uint16) {
	t.Helper()
	got := frameOpcodes(frames)
	if len(got) != len(want) {
		t.Fatalf("burst opcodes = %04X, want %04X", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("burst opcodes = %04X, want %04X", got, want)
		}
	}
}

// THE BUG B REGRESSION PIN. The 0x36AB despawn rides the SAME burst as the
// 0x35C7 scoop and the 0xB06D grant - packet arrival is the native trigger,
// and the visible linger after the scoop is the client's own fixed 1.5s
// dissolve. Anyone tempted to defer the despawn to an animation event turns
// a verified-native behavior into a divergence; this test is here to stop
// them.
func TestPickupGoldGrantBurstOrder(t *testing.T) {
	anim := PickupAnim{Gid: 100001, Heading: 42}
	frames := PickupGoldGrantFrames(anim, 1500, 99_000, 300007, false)

	assertOpcodeOrder(t, frames, []uint16{
		OpActionState, OpPickupAnim, OpItemMoveResponse, OpPointsUpdate, OpObjectDespawn,
	})

	if !bytes.Equal(frames[0].Payload, []byte{0x02, 0x00}) {
		t.Fatalf("the burst does not lead with the latch release: % X", frames[0].Payload)
	}
	if !bytes.Equal(frames[1].Payload, anim.Encode()) {
		t.Fatal("the anim frame does not carry the picker's scoop")
	}
	if !bytes.Equal(frames[2].Payload, []byte{0x01, 0x06, 0xFE, 0xDC, 0x05, 0x00, 0x00}) {
		t.Fatalf("gold grant payload = % X, want [01 06 FE][1500 le]", frames[2].Payload)
	}
	refresh, err := DecodeGoldRefresh(frames[3].Payload)
	if err != nil || refresh.Balance != 99_000 {
		t.Fatalf("gold refresh = %+v (%v), want balance 99000", refresh, err)
	}
	despawn, err := DecodeObjectDespawn(frames[4].Payload)
	if err != nil || despawn.Gid != 300007 {
		t.Fatalf("despawn = %+v (%v), want gid 300007", despawn, err)
	}
}

func TestPickupItemGrantBurstDespawnsInTheSameBurst(t *testing.T) {
	anim := PickupAnim{Gid: 100001, Heading: 7}
	body := ItemBody{RefObjID: 11459, Plus: 3, VarianceBits: 0x1234, Durability: 96}
	typeFlags := PackTypeFlags(3, 1, 6, 2)

	frames := PickupItemGrantFrames(anim, 15, body, 300009, 0)
	assertOpcodeOrder(t, frames, []uint16{
		OpActionState, OpPickupAnim, OpItemMoveResponse, OpObjectDespawn,
	})

	grant, err := DecodeItemMoveResult(frames[2].Payload, typeFlags)
	if err != nil {
		t.Fatalf("grant payload did not decode: %v", err)
	}
	body.TypeFlags = typeFlags
	if grant.MovementType != MoveTypePickup || grant.PickupSlot != 15 || !reflect.DeepEqual(grant.Item, body) {
		t.Fatalf("grant = %+v, want the type-6 slot-15 body grant", grant)
	}
}

func TestPickupItemGrantBurstWithholdsTheDespawnOnRemainder(t *testing.T) {
	anim := PickupAnim{Gid: 100001, Heading: 7}
	body := ItemBody{RefObjID: 3630}

	frames := PickupItemGrantFrames(anim, 15, body, 300009, 12)
	assertOpcodeOrder(t, frames, []uint16{
		OpActionState, OpPickupAnim, OpItemMoveResponse,
	})
	for _, frame := range frames {
		if frame.Opcode == OpObjectDespawn {
			t.Fatal("an over-cap pickup despawned the heap the remainder still lives on")
		}
	}
}

func TestPickupBroadcastFrames(t *testing.T) {
	anim := PickupAnim{Gid: 100001, Heading: 7}

	assertOpcodeOrder(t, PickupBroadcastFrames(anim, 300009, 0), []uint16{OpPickupAnim, OpObjectDespawn})
	assertOpcodeOrder(t, PickupBroadcastFrames(anim, 300009, 3), []uint16{OpPickupAnim})
}

func TestProgressionBroadcastFramesExposeOnlyLevelPresentation(t *testing.T) {
	effectPayload := []byte{0x41, 0x42, 0x0f, 0x00}
	frames := []Frame{
		{Opcode: OpLevelUpEffect, Payload: effectPayload},
		{Opcode: OpBaseStats, Payload: []byte{1}},
		{Opcode: OpPointsUpdate, Payload: []byte{2}},
		{Opcode: OpExpUpdate, Payload: []byte{3}},
	}
	public := ProgressionBroadcastFrames(frames)
	assertOpcodeOrder(t, public, []uint16{OpLevelUpEffect})
	if !bytes.Equal(public[0].Payload, effectPayload) {
		t.Fatalf("public level effect = % X, want % X", public[0].Payload, effectPayload)
	}

	// The routing projection owns its payload. A caller may recycle or mutate
	// the private burst after enqueue without corrupting the peer delivery.
	effectPayload[0] = 0
	if public[0].Payload[0] != 0x41 {
		t.Fatal("public progression payload aliases the private burst")
	}
}

func TestProgressionPrivateFramesExcludePublicLevelPresentation(t *testing.T) {
	effectPayload := []byte{0x41, 0x42, 0x0f, 0x00}
	statsPayload := []byte{1}
	frames := []Frame{
		{Opcode: OpLevelUpEffect, Payload: effectPayload},
		{Opcode: OpBaseStats, Payload: statsPayload},
		{Opcode: OpPointsUpdate, Payload: []byte{2}},
		{Opcode: OpExpUpdate, Payload: []byte{3}},
	}
	private := ProgressionPrivateFrames(frames)
	assertOpcodeOrder(t, private, []uint16{OpBaseStats, OpPointsUpdate, OpExpUpdate})

	statsPayload[0] = 0xff
	if private[0].Payload[0] != 1 {
		t.Fatalf("private progression projection retained caller payload: % X", private[0].Payload)
	}
	plain := ProgressionPrivateFrames([]Frame{{Opcode: OpExpUpdate, Payload: []byte{9}}})
	assertOpcodeOrder(t, plain, []uint16{OpExpUpdate})
}

func TestPickupRefusalFramesReleaseTheLatch(t *testing.T) {
	frames := PickupRefusalFrames(ErrCodeCannotBePicked)
	assertOpcodeOrder(t, frames, []uint16{OpActionState, OpItemMoveResponse})

	if !bytes.Equal(frames[0].Payload, []byte{0x02, 0x00}) {
		t.Fatalf("refusal does not release the latch: % X", frames[0].Payload)
	}
	if !bytes.Equal(frames[1].Payload, []byte{0x02, 0x39}) {
		t.Fatalf("refusal notice = % X, want [02 39]", frames[1].Payload)
	}
}

func TestPickupApproachArmFrame(t *testing.T) {
	frame := PickupApproachArmFrame()
	if frame.Opcode != OpActionState || !bytes.Equal(frame.Payload, []byte{0x01, 0x01}) {
		t.Fatalf("approach arm = %04X % X, want B2CD 01 01", frame.Opcode, frame.Payload)
	}
}

func TestGoldDropBurstOrderAndSpawnTail(t *testing.T) {
	goldWord := PackTypeFlags(3, 3, 5, 0)
	if !IsGoldBand(goldWord) {
		t.Fatalf("the packed gold word 0x%04X is not in the gold band", goldWord)
	}
	row := GroundItemRow{
		RefObjID:   62,
		TypeFlags:  goldWord,
		GoldAmount: 1500,
		Gid:        300010,
		Position:   Position{RegionID: 0x6B4F, X: 981, Y: 20, Z: 1178, Heading: 100},
	}

	frames := GoldDropFrames(1500, 42_000, row)
	assertOpcodeOrder(t, frames, []uint16{OpItemMoveResponse, OpPointsUpdate, OpSingleObjectSpawn})

	result, err := DecodeItemMoveResult(frames[0].Payload, 0)
	if err != nil || result.MovementType != MoveTypeGoldDrop || result.GoldAmount != 1500 {
		t.Fatalf("gold drop result = %+v (%v), want type 0x0A amount 1500", result, err)
	}

	// A fresh drop always rides the single-object form: the composer forces
	// the appear tail even when the caller's row left it unset.
	spawn, err := DecodeGroundItemRow(frames[2].Payload, goldWord, true)
	if err != nil {
		t.Fatalf("spawn row did not decode with the appear tail: %v", err)
	}
	if spawn.GoldAmount != 1500 || spawn.Gid != 300010 || spawn.AppearFlag != 1 {
		t.Fatalf("spawn = %+v, want the gold heap with appear flag 1", spawn)
	}
}

func TestGroundDropBurstOrder(t *testing.T) {
	equipWord := PackTypeFlags(3, 1, 6, 2)
	row := GroundItemRow{
		RefObjID:  11459,
		TypeFlags: equipWord,
		Gid:       300011,
		Position:  Position{RegionID: 0x6B4F, X: 981, Y: 20, Z: 1178, Heading: 100},
	}

	frames := GroundDropFrames(20, row)
	assertOpcodeOrder(t, frames, []uint16{OpItemMoveResponse, OpSingleObjectSpawn})

	result, err := DecodeItemMoveResult(frames[0].Payload, 0)
	if err != nil || result.MovementType != MoveTypeGroundDrop || result.SourceSlot != 20 {
		t.Fatalf("ground drop result = %+v (%v), want type 7 source 20", result, err)
	}
	if _, err := DecodeGroundItemRow(frames[1].Payload, equipWord, true); err != nil {
		t.Fatalf("spawn row did not decode with the appear tail: %v", err)
	}

	assertOpcodeOrder(t, DropBroadcastFrames(row), []uint16{OpSingleObjectSpawn})
}

func TestPackTypeFlags(t *testing.T) {
	// The CH one-hand sword word: TID 3.1.6.2.
	if got := PackTypeFlags(3, 1, 6, 2); got != 0x132C {
		t.Fatalf("sword word = 0x%04X, want 0x132C", got)
	}
	if !IsEquipmentBand(PackTypeFlags(3, 1, 6, 2)) {
		t.Fatal("the sword word is not in the equipment band")
	}
	// The ETC codename band (the 0x400 group).
	if !IsCodenameBand(PackTypeFlags(3, 3, 8, 0)) {
		t.Fatal("the tid3-8 ETC word is not in the codename band")
	}
	// Out-of-range tids mask off rather than bleeding into neighbours.
	if got := PackTypeFlags(0xFF, 0xFF, 0xFF, 0xFF); got != 0xFFFC {
		t.Fatalf("saturated word = 0x%04X, want 0xFFFC (bit 1 stays clear)", got)
	}
}
