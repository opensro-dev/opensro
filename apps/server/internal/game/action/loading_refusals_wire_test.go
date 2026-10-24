/*
===========================================================================

loading_refusals_wire_test.go - named lanes answered while the scene loads

Falling-first adjudication for issue 1478: every deadline-bearing lane the
loading gate can receive must retire its client pending inside the window
instead of hanging to its ten-second watchdog.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/item/wire"
)

// tradingLoadingClient enters a world and parks the scene in loading.
func tradingLoadingClient(t *testing.T) (*Runtime, *wireClient) {
	t.Helper()
	character := wireCharacter()
	rt, _ := newTestRuntime(character, testItems())
	srv := wireStartServer(t, rt)
	client := wireConnect(t, srv, testDivision, character.Name)
	session, ok := srv.Hub.Session(client.sessionID)
	if !ok {
		t.Fatalf("session %d not registered in hub", client.sessionID)
	}
	session.BeginSceneAdmission()
	return rt, client
}

// TestNpcRepairAnsweredWhileSceneLoading pins the repair refusal (0xB46F).
func TestNpcRepairAnsweredWhileSceneLoading(t *testing.T) {
	t.Parallel()
	_, client := tradingLoadingClient(t)
	payload := make([]byte, 6)
	binary.LittleEndian.PutUint32(payload, 4242)
	payload[4] = repairOneSlot
	payload[5] = 13
	client.send(t, opNpcRepairRequest, payload)
	client.expectFrame(t, opNpcRepairResponse, []byte{2, wire.ErrCodeInvalidRequest})
}

// TestTargetReleaseAnsweredWhileSceneLoading pins the release refusal.
func TestTargetReleaseAnsweredWhileSceneLoading(t *testing.T) {
	t.Parallel()
	_, client := tradingLoadingClient(t)
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, 4242)
	client.send(t, wire.OpTargetReleaseRequest, payload)
	frame := client.readFrame(t)
	if frame.Opcode != wire.OpTalkCloseResult || frame.Payload[0] != 2 {
		t.Fatalf("release answer = 0x%04X % X, want 0xB4B3 mode 2", frame.Opcode, frame.Payload)
	}
}

// TestShopOpenAnsweredWhileSceneLoading pins the shop-open refusal (0xB338).
func TestShopOpenAnsweredWhileSceneLoading(t *testing.T) {
	t.Parallel()
	_, client := tradingLoadingClient(t)
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint32(payload, 4242)
	binary.LittleEndian.PutUint32(payload[4:], 1)
	client.send(t, wire.OpNpcActionRequest, payload)
	frame := client.readFrame(t)
	if frame.Opcode != wire.OpNpcInteractionAck {
		t.Fatalf("shop-open answer opcode = 0x%04X payload % X, want 0x%04X", frame.Opcode, frame.Payload, wire.OpNpcInteractionAck)
	}
	if len(frame.Payload) != 2 || frame.Payload[0] != 2 {
		t.Fatalf("shop-open refusal payload = % X, want [2 ..]", frame.Payload)
	}
}

// TestBuybackAnsweredWhileSceneLoading pins the buyback refusal (0xB7E7).
func TestBuybackAnsweredWhileSceneLoading(t *testing.T) {
	t.Parallel()
	_, client := tradingLoadingClient(t)
	payload := make([]byte, 5)
	binary.LittleEndian.PutUint32(payload, 4242)
	payload[4] = 0
	client.send(t, 0x77e7, payload)
	frame := client.readFrame(t)
	if frame.Opcode != 0xB7E7 {
		t.Fatalf("buyback answer opcode = 0x%04X payload % X, want 0xB7E7", frame.Opcode, frame.Payload)
	}
	if len(frame.Payload) < 1 || frame.Payload[0] != 2 {
		t.Fatalf("buyback refusal payload = % X, want [2 ..]", frame.Payload)
	}
}

// TestMallCatalogAnsweredWhileSceneLoading pins the catalogue-open answer.
func TestMallCatalogAnsweredWhileSceneLoading(t *testing.T) {
	t.Parallel()
	_, client := tradingLoadingClient(t)
	client.send(t, opMallCatalog, nil)
	frame := client.readFrame(t)
	if frame.Opcode != opMallCatalog {
		t.Fatalf("catalog answer opcode = 0x%04X payload % X, want 0x%04X", frame.Opcode, frame.Payload, opMallCatalog)
	}
}

// TestUnrelatedOpcodeStaysSilentWhileSceneLoading pins the silence.
func TestUnrelatedOpcodeStaysSilentWhileSceneLoading(t *testing.T) {
	t.Parallel()
	_, client := tradingLoadingClient(t)
	client.send(t, 0x7341, []byte{1})
	if err := client.silence(220 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
}
