package action

// The DROP-VANISH wire proof (BOOT seq 1371): after a successful 0x706D
// drop, the DROPPING session must receive the live ground-spawn 0x30D7 in
// the same reliable sequence as its 0xB06D result, and observing peer sessions
// must receive the same 0x30D7 - live, mid-session,
// without re-entering the world.
//
// The suite drives the REAL stack end to end: transport.Server on loopback
// WebSocket, Runtime.Register on the hub (the exact server.go wiring), and
// real client conns. Expected payloads are hand-rolled here with
// encoding/binary, mirroring the Node oracle byte for byte (server.mjs
// moveMissionItem type-7/type-0x0A packets + buildV150GroundItemSpawnRow),
// so the assertion cannot inherit a bug from the wire encoders.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

// The pinned settled position every wire test drops from: the character is
// not moving, so live == spawn and the row bytes are fully deterministic.
const (
	wireRegionID uint16  = 0x62A8
	wireX        float32 = 1201.5
	wireY        float32 = 80.25
	wireZ        float32 = 355.75
	wireHeading  uint16  = 3000
)

// wireCharacter is the runtime_test fixture settled at the pinned spawn.
func wireCharacter() *enterworld.Character {
	character := testCharacter()
	regionID := int64(wireRegionID)
	x, y, z := float64(wireX), float64(wireY), float64(wireZ)
	angle := int64(wireHeading)
	character.World = &enterworld.CharacterWorld{
		Spawn:    &enterworld.WorldSpawn{RegionID: &regionID, X: &x, Y: &y, Z: &z, Angle: &angle},
		SpawnSet: true,
	}
	return character
}

// The wire seam starts after the visibility owner has admitted the source.
// Binding a transport to the same division alone is not object observation.
func wireObserveCharacter(t *testing.T, srv *transport.Server, viewer *wireClient, character *enterworld.Character) {
	t.Helper()
	session, _ := srv.Hub.Session(viewer.sessionID)
	revision, _ := session.SceneRevision()
	gid := enterworld.ObjectIDForCharacter(character)
	row := simulation.BuildPeerSpawnRow(simulation.PeerAppearance{RefObjID: 1907, Name: character.Name}, gid,
		simulation.Spawn{RegionID: wireRegionID, X: float64(wireX), Y: float64(wireY), Z: float64(wireZ), Angle: wireHeading})
	if err := session.PublishSceneObjects(revision, []transport.ObjectScopeChange{{GID: gid, Visible: true}}, []transport.Frame{{Opcode: wire.OpSingleObjectSpawn, Payload: row}}); err != nil {
		t.Fatal(err)
	}
	viewer.expectFrame(t, wire.OpSingleObjectSpawn, row)
}

func wireStartServer(t *testing.T, rt *Runtime) *transport.Server {
	t.Helper()
	srv, err := transport.NewServer(transport.Config{
		WTAddr:            "127.0.0.1:0",
		WSAddr:            "127.0.0.1:0",
		CertDir:           t.TempDir(),
		HelloTimeout:      5 * time.Second,
		GracePeriod:       10 * time.Second,
		KeepaliveInterval: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		OutboundQueue:     64,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.Hub.SetHelloAuth(testHelloAdmission)
	entryauth.NewAuthenticatedSessionFixture(t, srv.Hub)
	rt.Register(srv.Hub)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	})
	return srv
}

func testHelloAdmission([]byte) (transport.AdmissionIdentity, error) {
	return transport.AdmissionIdentity{AccountID: "test-account", ShardID: "global-official"}, nil
}

// wireClient is one connected-and-bound WS client plus its server-side
// session id.
type wireClient struct {
	conn      *websocket.Conn
	sessionID uint64
}

// wireConnect dials, handshakes and binds one client the way EnterWorld
// leaves a session: the division and character-name keys set server-side.
// Binding directly (rather than through the full bootstrap build) keeps the
// test on the seam under proof - hub dispatch, runtime, reply and fan-out.
func wireConnect(t *testing.T, srv *transport.Server, division, characterName string) *wireClient {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+srv.WSAddr()+transport.PathWS, nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	t.Cleanup(func() {
		// A protocol BYE, not a bare socket drop: the server closes the
		// session synchronously with the read, so srv.Shutdown in the
		// server cleanup finds nothing to drain. A drop instead detaches
		// the session and relies on CloseWhenDrained's detached fast-close
		// at shutdown — still prompt, but the BYE is the protocol-correct
		// path. TCP ordering guarantees the BYE is read before our FIN.
		bye := transport.Frame{Opcode: transport.OpBye, Payload: []byte{0}}
		_ = conn.WriteMessage(websocket.BinaryMessage, bye.Encode())
		conn.Close()
	})

	hello := transport.Frame{
		Opcode:  transport.OpHello,
		Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte("test-admission")}),
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, hello.Encode()); err != nil {
		t.Fatalf("writing HELLO: %v", err)
	}
	client := &wireClient{conn: conn}
	frame := client.readFrame(t)
	if frame.Opcode != transport.OpWelcome {
		t.Fatalf("first frame = 0x%04X, want WELCOME", frame.Opcode)
	}
	welcome, err := transport.DecodeWelcome(frame.Payload)
	if err != nil {
		t.Fatalf("decoding WELCOME: %v", err)
	}
	client.sessionID = welcome.SessionID

	session, ok := srv.Hub.Session(welcome.SessionID)
	if !ok {
		t.Fatalf("session %d not registered in hub", welcome.SessionID)
	}
	session.BindCharacter(division, characterName)
	return client
}

func (c *wireClient) send(t *testing.T, opcode uint16, payload []byte) {
	t.Helper()
	frame := transport.Frame{Opcode: opcode, Payload: payload}
	if err := c.conn.WriteMessage(websocket.BinaryMessage, frame.Encode()); err != nil {
		t.Fatalf("writing frame 0x%04X: %v", opcode, err)
	}
}

// readFrame returns the next internal/game/handshake frame, skipping keepalive.
func (c *wireClient) readFrame(t *testing.T) transport.Frame {
	t.Helper()
	c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		messageType, data, err := c.conn.ReadMessage()
		if err != nil {
			t.Fatalf("reading ws message: %v", err)
		}
		if messageType != websocket.BinaryMessage {
			t.Fatalf("ws message type = %d, want binary", messageType)
		}
		frame, err := transport.DecodeFrame(data)
		if err != nil {
			t.Fatalf("decoding ws frame: %v", err)
		}
		if frame.Opcode == transport.OpPing || frame.Opcode == transport.OpPong {
			continue
		}
		return frame
	}
}

// expectFrame asserts the next frame's opcode and exact payload bytes.
func (c *wireClient) expectFrame(t *testing.T, wantOpcode uint16, wantPayload []byte) {
	t.Helper()
	frame := c.readFrame(t)
	if frame.Opcode != wantOpcode {
		t.Fatalf("frame = 0x%04X payload % X, want 0x%04X", frame.Opcode, frame.Payload, wantOpcode)
	}
	if !bytes.Equal(frame.Payload, wantPayload) {
		t.Fatalf("0x%04X payload\n got % X\nwant % X", wantOpcode, frame.Payload, wantPayload)
	}
}

/*
==================
silence

Reports the first non-keepalive frame to arrive inside the window, nil when
the window elapses quietly. Only the read deadline proves silence: a closed
or failed connection is an error, not a quiet one. It is each client's LAST
read: the fired deadline poisons the conn for reads.
==================
*/
func (c *wireClient) silence(window time.Duration) error {
	c.conn.SetReadDeadline(time.Now().Add(window))
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return nil
			}
			return fmt.Errorf("connection failed during silence window: %w", err)
		}
		frame, decodeErr := transport.DecodeFrame(data)
		if decodeErr != nil {
			return fmt.Errorf("undecodable frame during silence window: %v", decodeErr)
		}
		if frame.Opcode == transport.OpPing || frame.Opcode == transport.OpPong {
			continue
		}
		return fmt.Errorf("unexpected frame 0x%04X payload % X inside the silence window", frame.Opcode, frame.Payload)
	}
}

// expectSilenceAll proves the silence window on every client at once: the
// same per-client proof, but the wall cost is one window, not one per
// client. It is the tests' LAST assertion (poisoned read deadlines).
func expectSilenceAll(t *testing.T, window time.Duration, clients ...*wireClient) {
	t.Helper()
	errs := make(chan error, len(clients))
	for _, c := range clients {
		go func() { errs <- c.silence(window) }()
	}
	for range clients {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}

// oracle mirrors the Node fixture's NativePacketWriter, so the expected
// bytes are derived independently of internal/game/item/wire's Writer.
type oracle struct{ buf []byte }

func (o *oracle) u8(v uint8) *oracle { o.buf = append(o.buf, v); return o }
func (o *oracle) u16(v uint16) *oracle {
	o.buf = binary.LittleEndian.AppendUint16(o.buf, v)
	return o
}
func (o *oracle) u32(v uint32) *oracle {
	o.buf = binary.LittleEndian.AppendUint32(o.buf, v)
	return o
}
func (o *oracle) u64(v uint64) *oracle {
	o.buf = binary.LittleEndian.AppendUint64(o.buf, v)
	return o
}
func (o *oracle) f32(v float32) *oracle { return o.u32(math.Float32bits(v)) }

// oracleRowTail appends the band-independent tail of a ground spawn row:
// gid, position, heading, hasOwner 0, tint 0, appear 1 (fresh 0x30D7 drop).
func (o *oracle) oracleRowTail(gid uint32) []byte {
	return o.u32(gid).
		u16(wireRegionID).
		f32(wireX).f32(wireY).f32(wireZ).
		u16(wireHeading).
		u8(0).u8(0).
		u8(1).buf
}

// TestGroundDropDeliversLiveSpawnOverTransport is the type-7 wire proof:
//
//	dropper: [0xB06D [01][07][src]] [0x30D7 row] then silence
//	division peer: [0x30D7 row] then silence
//	other-division session: silence
//
// byte-for-byte the Node oracle's moveMissionItem type-7 packets/broadcast.
func TestGroundDropDeliversLiveSpawnOverTransport(t *testing.T) {
	t.Parallel()
	character := wireCharacter()
	rt, _ := newTestRuntime(character, testItems())
	srv := wireStartServer(t, rt)

	dropper := wireConnect(t, srv, testDivision, character.Name)
	peer := wireConnect(t, srv, testDivision, "peer1")
	stranger := wireConnect(t, srv, "other-division", "peer2")
	unobserved := wireConnect(t, srv, testDivision, "outside-interest")
	wireObserveCharacter(t, srv, peer, character)

	dropper.send(t, wire.OpItemMoveRequest, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGroundDrop,
		SourceSlot:   20,
	}))

	// The registry allocates the first gid of the band; the row must carry
	// the sword fixture's identity (equipment band: one discard byte).
	gid := grounditem.GidBase + 1
	spawnRow := (&oracle{}).u32(11459).u8(0).oracleRowTail(gid)

	dropper.expectFrame(t, wire.OpItemMoveResponse, []byte{0x01, 0x07, 20})
	dropper.expectFrame(t, wire.OpSingleObjectSpawn, spawnRow)

	peer.expectFrame(t, wire.OpSingleObjectSpawn, spawnRow)

	if count := rt.Ground.Count(testDivision); count != 1 {
		t.Fatalf("ground registry holds %d drops, want 1", count)
	}

	// Origin exclusion + division scoping: no duplicate spawn back to the
	// dropper, nothing at all to the other division.
	expectSilenceAll(t, 300*time.Millisecond, dropper, peer, stranger, unobserved)
}

// TestGoldDropDeliversLiveSpawnOverTransport is the type-0x0A twin:
//
//	dropper: [0xB06D [01][0A][amount u32]] [0x30B3 [01][u64 balance][00]] [0x30D7 gold row]
//	division peer: [0x30D7 gold row]
//
// The gold row carries the heap tier's refObjId and the u32 amount (gold
// band), per server.mjs type-0x0A + buildV150GroundItemSpawnRow.
func TestGoldDropDeliversLiveSpawnOverTransport(t *testing.T) {
	t.Parallel()
	character := wireCharacter()
	rt, _ := newTestRuntime(character, testItems())
	srv := wireStartServer(t, rt)

	dropper := wireConnect(t, srv, testDivision, character.Name)
	peer := wireConnect(t, srv, testDivision, "peer1")
	wireObserveCharacter(t, srv, peer, character)

	dropper.send(t, wire.OpItemMoveRequest, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGoldDrop,
		GoldAmount:   1500,
	}))

	// 1500 gold: the ITEM_ETC_GOLD_02 tier heap (refObjId 62), balance
	// 5000-1500=3500 on the refresh.
	gid := grounditem.GidBase + 1
	goldRow := (&oracle{}).u32(62).u32(1500).oracleRowTail(gid)

	dropper.expectFrame(t, wire.OpItemMoveResponse, (&oracle{}).u8(0x01).u8(0x0A).u32(1500).buf)
	dropper.expectFrame(t, wire.OpPointsUpdate, (&oracle{}).u8(0x01).u64(3500).u8(0x00).buf)
	dropper.expectFrame(t, wire.OpSingleObjectSpawn, goldRow)

	peer.expectFrame(t, wire.OpSingleObjectSpawn, goldRow)

	expectSilenceAll(t, 300*time.Millisecond, dropper, peer)
}
