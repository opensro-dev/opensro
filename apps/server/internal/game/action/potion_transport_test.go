/*
===========================================================================

potion_transport_test.go - native-profile potion transactions over WebSocket

Run this checkout's action owner and durable store behind the real transport.
Drive the clock explicitly; no beta refill, live-server build, or wall-clock
sleep participates in the consumption and recovery assertions.

===========================================================================
*/
package action

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

/*
================
TestPotionTransportConsumptionAndResidentLifetime
================
*/
func TestPotionTransportConsumptionAndResidentLifetime(t *testing.T) {
	seed, items, request := recoveryFixture(1)
	seed.ModelCodename = "CHAR_CH_MAN_ADVENTURER"
	items["ITEM_ETC_HP_POTION_01"].RecoveryHP = 20
	door := openDoorRuntime(t, t.TempDir(), seed)
	deps := door.rt.deps.(*enterworld.Deps)
	deps.Items = items
	deps.UpdateCharacter = door.authority.UpdateCharacter
	rt, character, clock := door.rt, door.character, door.clock
	var operation sync.Mutex
	server, err := transport.NewServer(transport.Config{
		WTAddr: "127.0.0.1:0", WSAddr: "127.0.0.1:0", CertDir: t.TempDir(),
		HelloTimeout: 5 * time.Second, GracePeriod: time.Second,
		KeepaliveInterval: time.Hour, IdleTimeout: time.Hour, OutboundQueue: 256,
	})
	if err != nil {
		t.Fatal(err)
	}
	const ticket = "potion-transport-fixture"
	server.Hub.SetHelloAuth(func(value []byte) (transport.AdmissionIdentity, error) {
		if string(value) != ticket {
			return transport.AdmissionIdentity{}, errors.New("unexpected fixture admission")
		}
		return transport.AdmissionIdentity{AccountID: "test-account", ShardID: testDivision}, nil
	})
	server.Hub.SetEnterWorldAuth(func(_ *transport.Session, entry transport.EnterWorld) (bool, uint32) {
		return string(entry.AuthToken) == ticket, 1
	})
	server.Hub.Handle(wire.OpItemUseRequest, func(session *transport.Session, _ uint16, payload []byte) {
		operation.Lock()
		defer operation.Unlock()
		result := rt.HandleItemUse(testDivision, character, payload)
		frames := make([]transport.Frame, 0, len(result.Frames))
		for _, frame := range result.Frames {
			frames = append(frames, transport.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
		}
		if err := session.SendBatch(frames); err != nil {
			t.Errorf("item publication: %v", err)
		}
	})
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	})
	client, _, err := websocket.DefaultDialer.Dial("ws://"+server.WSAddr()+transport.PathWS, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sendPotionFrame(t, client, transport.Frame{Opcode: transport.OpHello,
		Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte(ticket)})})
	hello := readPotionFrame(t, client, transport.OpWelcome)
	welcome, err := transport.DecodeWelcome(hello.Payload)
	if err != nil {
		t.Fatal(err)
	}
	session, found := server.Hub.Session(welcome.SessionID)
	if !found {
		t.Fatal("missing authenticated session")
	}
	// Bind the fixture's durable character just as the production world-entry
	// composition does. Authentication itself is not the algorithm under test.
	session.BindCharacter(testDivision, character.Name, enterworld.ObjectIDForCharacter(character))
	rt.BindRecoverySession(testDivision, character, uint64(welcome.SessionID))
	use := transport.Frame{Opcode: wire.OpItemUseRequest, Payload: request}
	sendPotionFrame(t, client, use)
	assertPotionReceipt(t, readPotionFrame(t, client, wire.OpItemUseResponse), 19)
	readPotionFrame(t, client, simulation.OpVitalsUpdate)
	operation.Lock()
	if enterworld.CurrentHP(character) != 5 {
		t.Fatalf("immediate pulse: %d", enterworld.CurrentHP(character))
	}
	clock.Advance(time.Second)
	key := recoveryKey{testDivision, strings.ToLower(character.Name)}
	for _, batch := range rt.recoverPotionResident(key, clock.NowMs()) {
		if batch.OnlyCharacterID == character.ID {
			for _, frame := range batch.Frames {
				if err := session.Send(frame.Opcode, frame.Payload); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	operation.Unlock()
	pulse := readPotionFrame(t, client, simulation.OpVitalsUpdate)
	if binary.LittleEndian.Uint32(pulse.Payload[7:]) != 9 {
		t.Fatalf("queued wire pulse: %x", pulse.Payload)
	}
	operation.Lock()
	clock.Advance(99 * time.Millisecond)
	operation.Unlock()
	sendPotionFrame(t, client, use)
	refused := readPotionFrame(t, client, wire.OpItemUseResponse)
	if len(refused.Payload) != 2 || refused.Payload[0] != 2 || refused.Payload[1] != wire.ErrCodeItemReuseDelay {
		t.Fatalf("reuse refusal: %x", refused.Payload)
	}
	operation.Lock()
	clock.Advance(time.Millisecond)
	operation.Unlock()
	sendPotionFrame(t, client, use)
	assertPotionReceipt(t, readPotionFrame(t, client, wire.OpItemUseResponse), 18)
	readPotionFrame(t, client, simulation.OpVitalsUpdate)
	operation.Lock()
	if enterworld.CurrentHP(character) != 13 {
		t.Fatalf("overlapping potion: %d", enterworld.CurrentHP(character))
	}
	rt.ForgetCharacter(testDivision, character.Name)
	clock.Advance(time.Second)
	if frames := rt.recoverPotionResident(key, clock.NowMs()); len(frames) != 0 {
		t.Fatal("retired session emitted queued recovery")
	}
	operation.Unlock()
	client.Close()
	door = door.reboot(t)
	if enterworld.CurrentHP(door.character) != 13 ||
		bagRowByCodename(door.character, "ITEM_ETC_HP_POTION_01").StackCount != 18 {
		t.Fatal("reopened native profile lost the committed recovery or inventory debit")
	}
}

/*
================
sendPotionFrame
================
*/
func sendPotionFrame(t *testing.T, client *websocket.Conn, frame transport.Frame) {
	t.Helper()
	if err := client.WriteMessage(websocket.BinaryMessage, frame.Encode()); err != nil {
		t.Fatal(err)
	}
}

/*
================
readPotionFrame
================
*/
func readPotionFrame(t *testing.T, client *websocket.Conn, opcode uint16) transport.Frame {
	t.Helper()
	if err := client.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for range 32 {
		_, payload, err := client.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		frame, err := transport.DecodeFrame(payload)
		if err != nil {
			t.Fatal(err)
		}
		if frame.Opcode == opcode {
			return frame
		}
	}
	t.Fatalf("missing frame %04x", opcode)
	return transport.Frame{}
}

/*
================
assertPotionReceipt
================
*/
func assertPotionReceipt(t *testing.T, frame transport.Frame, remaining uint16) {
	t.Helper()
	if len(frame.Payload) != 6 || frame.Payload[0] != 1 ||
		binary.LittleEndian.Uint16(frame.Payload[2:]) != remaining {
		t.Fatalf("consumption receipt: %x", frame.Payload)
	}
}
