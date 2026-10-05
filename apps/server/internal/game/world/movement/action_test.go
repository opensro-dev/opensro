package movement

import (
	"bytes"
	"testing"

	itemcodec "opensro.online/server/internal/game/item/wire"
)

// wantEmotePush is the exact S->C 0x324B body sub_778190 reads for the
// shared test character (ID 7 -> gid 100007 = 0x000186A7 little-endian, the
// same pinning motionstate_test's want3122 uses).
func wantEmotePush(action uint8) []byte {
	return []byte{0xA7, 0x86, 0x01, 0x00, action}
}

// TestHandleActionEmoteEchoesAndBroadcastsTheClientBytes drives the handler
// with every byte the v1.150 client can emit (sub_695420's emote arms:
// {0,6,1,5,2,3,4,0} for cmds 0xfa0,0xfa1..0xfa6,0xfab) and pins BOTH output
// legs byte-exact: the sender echo (the native client does NOT animate
// locally - only the inbound push plays, so a missing echo means the actor
// never sees their own emote) and the identical division broadcast.
func TestHandleActionEmoteEchoesAndBroadcastsTheClientBytes(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)

	for _, wire := range []uint8{0, 6, 1, 5, 2, 3, 4, 0} {
		outcome := rt.HandleActionEmote("0", character, []byte{wire})
		if outcome.Refusal != "" {
			t.Errorf("client byte %d refused: %s", wire, outcome.Refusal)
			continue
		}
		if len(outcome.Frames) != 1 ||
			outcome.Frames[0].Opcode != itemcodec.OpActionEmote ||
			!bytes.Equal(outcome.Frames[0].Payload, wantEmotePush(wire)) {
			t.Errorf("byte %d echo = %+v, want one 0x324B % X", wire, outcome.Frames, wantEmotePush(wire))
		}
		if len(outcome.Broadcast) != 1 ||
			outcome.Broadcast[0].Opcode != itemcodec.OpActionEmote ||
			!bytes.Equal(outcome.Broadcast[0].Payload, wantEmotePush(wire)) {
			t.Errorf("byte %d broadcast = %+v, want the same 0x324B push", wire, outcome.Broadcast)
		}
	}
}

// TestHandleActionEmoteRefusalsShipNoPackets pins the rejection posture: a
// shape no retail client composes is discarded with an EMPTY wire (the emote
// channel has no error consumer client-side), and unauthorized states refuse
// before any decode. A handler that echoes out-of-range bytes would let one
// hostile session drive arbitrary SetMotionState params on every peer.
func TestHandleActionEmoteRefusalsShipNoPackets(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)

	malformed := [][]byte{
		{},                             // empty
		{7},                            // first byte past the client space
		{0xFF},                         // far out of range
		{0, 0},                         // over-long
		{0xA7, 0x86, 0x01, 0x00, 0x02}, // push-shaped loopback
	}
	for _, payload := range malformed {
		outcome := rt.HandleActionEmote("0", character, payload)
		if outcome.Refusal == "" || len(outcome.Frames) != 0 || len(outcome.Broadcast) != 0 {
			t.Errorf("payload % X: refusal=%q frames=%d broadcast=%d, want silent refusal",
				payload, outcome.Refusal, len(outcome.Frames), len(outcome.Broadcast))
		}
	}

	if outcome := rt.HandleActionEmote("0", nil, []byte{0}); outcome.Refusal == "" {
		t.Error("nil character accepted")
	}
	character.DeletePending = true
	if outcome := rt.HandleActionEmote("0", character, []byte{0}); outcome.Refusal == "" {
		t.Error("deletePending session accepted")
	}
	character.DeletePending = false
}
