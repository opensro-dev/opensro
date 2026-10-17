package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/transport"
)

func TestDisabledPotionReportsUnavailable(t *testing.T) {
	actions := &botActions{potionSlot: -1}
	if err := actions.UsePotion(); err != bot.ErrRecoveryUnavailable {
		t.Fatalf("error=%v", err)
	}
}

func TestSceneEntryRestoresPositionAndSendsReady(t *testing.T) {
	socket, ready := sceneReadySocket(t)
	world := newBotWorld(100)
	world.self.GID = 100009
	world.setPosition(25256, 100, 0, 100)
	world.monsters[1] = bot.Actor{GID: 1}
	world.groundItems[2] = bot.GroundItem{GID: 2}
	world.pendingMove = true
	brain := bot.NewBrain(bot.BrainConfig{})
	entry := sceneEntry{}
	reset := make([]byte, 2)
	binary.LittleEndian.PutUint16(reset, 25257)
	if handled, err := entry.handle(transport.Frame{Opcode: bot.OpcodeSceneReset, Payload: reset}, world, brain, socket); !handled || err != nil {
		t.Fatalf("reset: %v %v", handled, err)
	}
	if world.Self().PositionKnown || world.pendingMove || len(world.Monsters()) != 0 || len(world.GroundItems()) != 0 {
		t.Fatal("old scene retained")
	}
	body := bytes.NewBuffer([]byte{0, 0, 0, 0, 0, 11})
	for _, value := range []any{uint32(100009), uint16(25257), float32(960), float32(-46), float32(458), uint16(0)} {
		if err := binary.Write(body, binary.LittleEndian, value); err != nil {
			t.Fatal(err)
		}
	}
	if handled, err := entry.handle(transport.Frame{Opcode: enterworld.OpcodeMyCharacterChunk, Payload: body.Bytes()}, world, brain, socket); !handled || err != nil {
		t.Fatalf("character: %v %v", handled, err)
	}
	if handled, err := entry.handle(transport.Frame{Opcode: bot.OpcodeObjectListFinalize}, world, brain, socket); !handled || err != nil {
		t.Fatalf("finalize: %v %v", handled, err)
	}
	self := world.Self()
	if entry.pending || !self.PositionKnown || self.Region != 25257 || self.X != 960 || self.Level != 11 {
		t.Fatalf("entry=%+v self=%+v", entry, self)
	}
	select {
	case frame := <-ready:
		if frame.Opcode != enterworld.OpcodeGameReady {
			t.Fatalf("frame=%+v", frame)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("game ready missing")
	}
}

func TestSceneEntryRejectsIncompleteCharacter(t *testing.T) {
	world := newBotWorld(100)
	brain := bot.NewBrain(bot.BrainConfig{})
	entry := sceneEntry{pending: true}
	if _, err := entry.handle(transport.Frame{Opcode: bot.OpcodeObjectListFinalize}, world, brain, nil); err == nil {
		t.Fatal("incomplete scene accepted")
	}
}

func sceneReadySocket(t *testing.T) (*bot.Socket, <-chan transport.Frame) {
	t.Helper()
	ready := make(chan transport.Frame, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, data, err := conn.ReadMessage()
		if err == nil {
			if frame, err := transport.DecodeFrame(data); err == nil {
				ready <- frame
			}
		}
	}))
	t.Cleanup(server.Close)
	socket, err := bot.DialWebSocket(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = socket.Close() })
	return socket, ready
}
