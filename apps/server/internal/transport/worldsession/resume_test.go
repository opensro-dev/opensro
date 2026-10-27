/*
===========================================================================

resume_test.go - the resumed client's mover re-push, on the wire (#1818)

===========================================================================
*/

package worldsession

import (
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

// collectFrames reads until the window lapses with nothing left to read.
// A gorilla read timeout poisons the conn, so this is only for a conn the
// test is about to drop.
func collectFrames(c *websocket.Conn, window time.Duration) []transport.Frame {
	var out []transport.Frame
	for {
		c.SetReadDeadline(time.Now().Add(window))
		typ, data, err := c.ReadMessage()
		if err != nil {
			return out
		}
		if typ != websocket.BinaryMessage {
			continue
		}
		f, err := transport.DecodeFrame(data)
		if err != nil {
			return out
		}
		if f.Opcode == transport.OpPing || f.Opcode == transport.OpPong {
			continue
		}
		out = append(out, f)
	}
}

// TestResumedClientGetsNpcPositionsRepushed is the issue's acceptance at
// wire level: NPC patrol positions ride the lossy lane, so before any
// resume the client sees creates and 0x30E3 moves but never a correction;
// after an abrupt drop and a resume, the next tick re-pushes one ordered
// 0xB2F5 per patrolling NPC, at the canonical patrol position.
func TestResumedClientGetsNpcPositionsRepushed(t *testing.T) {
	t.Parallel()
	srv := startServer(t)
	c, w := dialAndHello(t, srv)

	sess, ok := srv.Hub.Session(w.SessionID)
	if !ok {
		t.Fatal("session missing from hub")
	}
	anchor := simulation.NpcShopSpawn()
	sess.SetWorldSnapshot("DIV_A", staticProvider{simulation.SessionSnapshot{
		DivisionID:  "DIV_A",
		CharacterID: 7,
		World:       simulation.DefaultWorldState(simulation.EuropeStartProfile()),
		NpcAnchor:   anchor,
		NpcsEnabled: true,
	}})

	patrol := map[uint32]simulation.NpcDef{}
	for _, npc := range simulation.DefaultNpcRoster() {
		if npc.Patrol {
			patrol[npc.ObjectID] = npc
		}
	}
	if len(patrol) == 0 {
		t.Fatal("default roster has no patrolling NPC")
	}

	ticker := NewTicker(srv.Hub, simulation.DefaultNpcRoster())
	now := time.Now().UnixMilli()
	ticker.RunTick(now)

	created := map[uint32]bool{}
	for _, f := range collectFrames(c, 2*time.Second) {
		switch f.Opcode {
		case wire.OpSingleObjectSpawn:
			created[binary.LittleEndian.Uint32(f.Payload[4:])] = true
		case transport.OpObjectSourceCorrection:
			t.Fatalf("correction 0x%04X arrived before any resume", f.Opcode)
		}
	}
	for gid := range patrol {
		if !created[gid] {
			t.Fatalf("patrol NPC %d was never created for the client", gid)
		}
	}

	// Abrupt transport death: no Close frame, just a dead socket.
	c.NetConn().Close()
	wait.Eventually(t, 4*time.Second, "session detach", func() bool { return sess.Kind() == "detached" })

	url := fmt.Sprintf("ws://%s%s", srv.WSAddr(), transport.PathWS)
	c2, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("ws redial: %v", err)
	}
	t.Cleanup(func() { c2.Close() })
	hello := transport.Frame{
		Opcode: transport.OpHello,
		Payload: transport.EncodeHello(transport.Hello{
			ResumeToken: w.ResumeToken, AdmissionToken: []byte(testAdmissionTicket),
			HasPosition: true,
		}),
	}
	if err := c2.WriteMessage(websocket.BinaryMessage, hello.Encode()); err != nil {
		t.Fatal(err)
	}
	f := readFrame(t, c2)
	if f.Opcode != transport.OpWelcome {
		t.Fatalf("first frame after redial = 0x%04X, want WELCOME", f.Opcode)
	}
	w2, err := transport.DecodeWelcome(f.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if !w2.Resumed || w2.SessionID != w.SessionID {
		t.Fatalf("resume welcome = resumed %v session %d, want resumed session %d", w2.Resumed, w2.SessionID, w.SessionID)
	}

	// The hub writes WELCOME inside attach and fires OnSessionResumed after
	// it, so the client can read WELCOME before the bridge holds the mark.
	// Wait for the mark: a tick that ran first would drain nothing, and the
	// re-push would land on the next tick instead (correct, but not this one).
	bridge := ticker.Source.(*Bridge)
	wait.Eventually(t, 4*time.Second, "resume mark recorded", func() bool {
		bridge.resumeMu.Lock()
		defer bridge.resumeMu.Unlock()
		return len(bridge.resumed) != 0
	})

	// The tick that serves the resume (tick index 1) re-pushes corrections.
	ticker.RunTick(now + 100)
	corrected := map[uint32]wire.ObjectSourceCorrection{}
	for len(corrected) < len(patrol) {
		f := readFrame(t, c2)
		if f.Opcode != transport.OpObjectSourceCorrection {
			continue
		}
		decoded, err := wire.DecodeObjectSourceCorrection(f.Payload)
		if err != nil {
			t.Fatalf("correction decode: %v", err)
		}
		if _, ok := patrol[decoded.Gid]; ok {
			corrected[decoded.Gid] = decoded
		}
	}
	for gid, npc := range patrol {
		decoded, ok := corrected[gid]
		if !ok {
			t.Fatalf("patrol NPC %d got no re-pushed correction after resume", gid)
		}
		wantAnchor := anchor
		wantAnchor.X += npc.SpawnOffsetX
		wantAnchor.Z += npc.SpawnOffsetZ
		want := simulation.ComputeNpcPatrolState(wantAnchor, 1)
		if decoded.Position.X != float32(want.X) || decoded.Position.Z != float32(want.Z) {
			t.Fatalf("NPC %d correction = (%v, %v), want patrol state (%v, %v)",
				gid, decoded.Position.X, decoded.Position.Z, want.X, want.Z)
		}
	}
}
