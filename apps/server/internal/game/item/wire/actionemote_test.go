package wire

import (
	"bytes"
	"testing"
)

// TestDecodeActionEmoteRequestAcceptsExactlyTheClientBytes drives the decoder
// with the EXACT single-byte bodies the v1.150 client emits: sub_695420's
// emote arms stage {0,6,1,5,2,3,4,0} for commands 0xfa0,0xfa1..0xfa6,0xfab
// (jump_table_6960d8; the client-side harness pins the same table as REAL
// built frames in actionPaneEmitChainParity.test.ts). Every emitted byte must
// decode to itself - the request byte IS the action code, there is no
// re-mapping server-side.
func TestDecodeActionEmoteRequestAcceptsExactlyTheClientBytes(t *testing.T) {
	// wire byte -> the named constant the handler passes on (the pairing
	// W2 pinned against actionwnddata records 4000..4006).
	wantNames := map[uint8]uint8{
		0: EmoteActionGreeting,
		1: EmoteActionPokun,
		2: EmoteActionRush,
		3: EmoteActionJoy,
		4: EmoteActionNo,
		5: EmoteActionYes,
		6: EmoteActionLaugh,
	}
	// The client's emit order (cmd 0xfa0, 0xfa1..0xfa6, 0xfab).
	for _, wire := range []uint8{0, 6, 1, 5, 2, 3, 4, 0} {
		got, err := DecodeActionEmoteRequest([]byte{wire})
		if err != nil {
			t.Errorf("client byte %d refused: %v", wire, err)
			continue
		}
		if got != wire || got != wantNames[wire] {
			t.Errorf("client byte %d decoded to %d, want identity", wire, got)
		}
	}
}

func TestDecodeActionEmoteRequestRejectsNonClientShapes(t *testing.T) {
	cases := [][]byte{
		{},                             // empty
		{7},                            // first code past EmoteActionMax
		{0xFF},                         // far out of range
		{0, 0},                         // over-long (client appends exactly one byte)
		{0xA7, 0x86, 0x01, 0x00, 0x02}, // an S->C push body looped back C->S
	}
	for _, payload := range cases {
		if action, err := DecodeActionEmoteRequest(payload); err == nil {
			t.Errorf("payload % X accepted as action %d, want error", payload, action)
		}
	}
}

// TestActionEmotePushEncodeMatchesTheClientReadOrder pins the S->C body
// byte-for-byte against the consumer: sub_778190 / CPSMission_HandlePacket324B
// reads a u32 gid (CMsgStreamBuffer little-endian) THEN the action byte.
// Field-swapped or wide encodings fail on the literal bytes.
func TestActionEmotePushEncodeMatchesTheClientReadOrder(t *testing.T) {
	got := ActionEmotePush{Gid: 0x12345678, Action: EmoteActionLaugh}.Encode()
	want := []byte{0x78, 0x56, 0x34, 0x12, 0x06}
	if !bytes.Equal(got, want) {
		t.Fatalf("push = % X, want % X ([u32 gid LE][u8 action])", got, want)
	}

	decoded, err := DecodeActionEmotePush(got)
	if err != nil {
		t.Fatalf("round-trip decode: %v", err)
	}
	if decoded.Gid != 0x12345678 || decoded.Action != EmoteActionLaugh {
		t.Errorf("round-trip = %+v, want gid 0x12345678 action 6", decoded)
	}

	// The push decoder (test support) keeps the same strictness.
	for _, payload := range [][]byte{{}, {0x78, 0x56, 0x34, 0x12}, {0x78, 0x56, 0x34, 0x12, 0x06, 0x00}} {
		if _, err := DecodeActionEmotePush(payload); err == nil {
			t.Errorf("push payload % X accepted, want error", payload)
		}
	}
}
