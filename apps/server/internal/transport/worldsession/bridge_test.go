package worldsession

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

const testAdmissionTicket = "test-worldsession-admission-ticket"

func startServer(t *testing.T) *transport.Server {
	t.Helper()
	srv, err := transport.NewServer(transport.Config{
		WTAddr:            "127.0.0.1:0",
		WSAddr:            "127.0.0.1:0",
		CertDir:           t.TempDir(),
		HelloTimeout:      5 * time.Second,
		GracePeriod:       10 * time.Second,
		KeepaliveInterval: time.Hour, // keep pings out of these assertions
		IdleTimeout:       2 * time.Hour,
		OutboundQueue:     256,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.Hub.SetHelloAuth(func(ticket []byte) (transport.AdmissionIdentity, error) {
		if string(ticket) != testAdmissionTicket {
			return transport.AdmissionIdentity{}, errors.New("unexpected test admission ticket")
		}
		return transport.AdmissionIdentity{AccountID: "worldsession-account", ShardID: "global-official"}, nil
	})
	srv.Hub.SetEnterWorldAuth(func(_ *transport.Session, ew transport.EnterWorld) (bool, uint32) {
		return len(ew.AuthToken) > 0, 0x00A1
	})
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

func dialAndHello(t *testing.T, srv *transport.Server) (*websocket.Conn, transport.Welcome) {
	t.Helper()
	url := fmt.Sprintf("ws://%s%s", srv.WSAddr(), transport.PathWS)
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	hello := transport.Frame{
		Opcode:  transport.OpHello,
		Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte(testAdmissionTicket)}),
	}
	if err := c.WriteMessage(websocket.BinaryMessage, hello.Encode()); err != nil {
		t.Fatal(err)
	}
	f := readFrame(t, c)
	if f.Opcode != transport.OpWelcome {
		t.Fatalf("first frame = 0x%04X, want WELCOME", f.Opcode)
	}
	w, err := transport.DecodeWelcome(f.Payload)
	if err != nil {
		t.Fatal(err)
	}
	return c, w
}

func readFrame(t *testing.T, c *websocket.Conn) transport.Frame {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		typ, data, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("ws read: %v", err)
		}
		if typ != websocket.BinaryMessage {
			continue
		}
		f, err := transport.DecodeFrame(data)
		if err != nil {
			t.Fatal(err)
		}
		if f.Opcode == transport.OpPing || f.Opcode == transport.OpPong {
			continue
		}
		return f
	}
}

// expectNoFrame asserts nothing arrives within the window (for exclusion
// checks).
func expectNoFrame(t *testing.T, c *websocket.Conn, window time.Duration) {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(window))
	_, data, err := c.ReadMessage()
	if err == nil {
		f, _ := transport.DecodeFrame(data)
		t.Fatalf("unexpected frame 0x%04X while expecting silence", f.Opcode)
	}
}

type staticProvider struct{ snap simulation.SessionSnapshot }

func (p staticProvider) WorldSnapshot() simulation.SessionSnapshot { return p.snap }

// TestTickPushReachesWebSocketClient is the end-to-end proof for the
// Pusher adapter: a real simulation.Ticker tick, through the bridge, lands
// 0x30E3 NPC patrol frames on a live WebSocket client.
func TestTickPushReachesWebSocketClient(t *testing.T) {
	t.Parallel()
	srv := startServer(t)
	c, w := dialAndHello(t, srv)

	sess, ok := srv.Hub.Session(w.SessionID)
	if !ok {
		t.Fatal("session missing from hub")
	}
	sess.SetWorldSnapshot("DIV_A", staticProvider{simulation.SessionSnapshot{
		DivisionID:  "DIV_A",
		CharacterID: 7,
		World:       simulation.DefaultWorldState(simulation.EuropeStartProfile()),
		NpcAnchor:   simulation.NpcShopSpawn(),
		NpcsEnabled: true,
	}})

	bridge := New(srv.Hub)

	snaps := bridge.SnapshotSessions()
	if len(snaps) != 1 {
		t.Fatalf("SnapshotSessions = %d entries, want 1", len(snaps))
	}
	if snaps[0].SessionID != SessionIDString(w.SessionID) || snaps[0].DivisionID != "DIV_A" {
		t.Fatalf("snapshot identity = %q/%q", snaps[0].SessionID, snaps[0].DivisionID)
	}

	ticker := simulation.NewTicker(bridge, bridge)
	ticker.Roster = simulation.DefaultNpcRoster()
	ticker.RunTick(time.Now().UnixMilli())

	// This harness has no bootstrap. The tick must create every roster NPC
	// before publishing movement for it; an unresolved move is not visibility.
	for _, npc := range ticker.Roster {
		f := readFrame(t, c)
		if f.Opcode != wire.OpSingleObjectSpawn || binary.LittleEndian.Uint32(f.Payload[4:]) != npc.ObjectID {
			t.Fatalf("NPC create before movement: %#v", f)
		}
	}
	f := readFrame(t, c)
	if f.Opcode != transport.OpObjectSourceMove {
		t.Fatalf("tick frame opcode = 0x%04X, want 0x30E3", f.Opcode)
	}
	if len(f.Payload) != wire.ObjectMoveSize {
		t.Fatalf("0x30E3 payload = %d bytes, want %d", len(f.Payload), wire.ObjectMoveSize)
	}
}

// TestBootTickerPushesThroughLiveHub is the boot-wiring proof: the SAME
// composition main uses (worldsession.NewTicker + go Run) pushes both
// tick legs to a live WebSocket client with NO manual RunTick calls —
// the NPC patrol 0x30E3 (via the session's SnapshotProvider) and a
// TickHook's division frames (via the bootstrap "divisionId" alias key,
// exactly how a bound session looks after EnterWorld).
func TestBootTickerPushesThroughLiveHub(t *testing.T) {
	t.Parallel()
	srv := startServer(t)
	c, w := dialAndHello(t, srv)

	sess, ok := srv.Hub.Session(w.SessionID)
	if !ok {
		t.Fatal("session missing from hub")
	}
	sess.SetWorldSnapshot("DIV_A", staticProvider{simulation.SessionSnapshot{
		DivisionID:  "DIV_A",
		CharacterID: 7,
		World:       simulation.DefaultWorldState(simulation.EuropeStartProfile()),
		NpcAnchor:   simulation.NpcShopSpawn(),
		NpcsEnabled: true,
	}})

	sweep := simulation.Frame{Opcode: wire.OpObjectDespawn, Payload: []byte{0x09, 0x09, 0x09, 0x09}}
	hook := func(nowMs int64) []simulation.DivisionFrames {
		return []simulation.DivisionFrames{{DivisionID: "DIV_A", Frames: []simulation.Frame{sweep}}}
	}

	ticker := NewTicker(srv.Hub, simulation.DefaultNpcRoster(), hook)
	ticker.Interval = 20 * time.Millisecond // production cadence is 100ms; fast for the test
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ticker.Run(ctx)

	var sawPatrol, sawSweep bool
	for i := 0; i < 500 && (!sawPatrol || !sawSweep); i++ {
		f := readFrame(t, c)
		switch f.Opcode {
		case transport.OpObjectSourceMove:
			sawPatrol = true
		case wire.OpObjectDespawn:
			sawSweep = true
		}
	}
	if !sawPatrol || !sawSweep {
		t.Fatalf("running ticker delivered patrol=%v sweep=%v, want both", sawPatrol, sawSweep)
	}
}

// TestPushToDivisionRoutesAndExcludes covers the division fanout and the
// except-origin rule, using a must-deliver opcode to prove the reliable
// routing path.
func TestDivisionMotionCannotOvertakeEnterWorldAdmission(t *testing.T) {
	t.Parallel()
	srv := startServer(t)
	c, welcome := dialAndHello(t, srv)
	sess, _ := srv.Hub.Session(welcome.SessionID)
	bridge := New(srv.Hub)
	sess.BeginSceneAdmission()
	sess.BindCharacter("DIV_A", "Alice", 0)
	motion := simulation.Frame{Opcode: wire.OpObjectSourceMove, Payload: make([]byte, 20)}
	bridge.PushToDivision("DIV_A", []simulation.Frame{motion}, "")
	if err := sess.SendSceneReset([]transport.Frame{{Opcode: transport.OpEnterWorldResult, Payload: []byte{1}}}); err != nil {
		t.Fatal(err)
	}
	bridge.PushToDivision("DIV_A", []simulation.Frame{motion}, "")
	if f := readFrame(t, c); f.Opcode != transport.OpEnterWorldResult {
		t.Fatalf("native frame overtook admission: 0x%04X", f.Opcode)
	}
	sess.FinishSceneReentry()
	bridge.PushToDivision("DIV_A", []simulation.Frame{motion}, "")
	if f := readFrame(t, c); f.Opcode != wire.OpObjectSourceMove {
		t.Fatalf("ready scene did not receive motion: 0x%04X", f.Opcode)
	}
	expectNoFrame(t, c, 100*time.Millisecond)
}

func TestPushToDivisionRoutesAndExcludes(t *testing.T) {
	t.Parallel()
	srv := startServer(t)
	cA, wA := dialAndHello(t, srv)
	cB, wB := dialAndHello(t, srv)

	sessA, _ := srv.Hub.Session(wA.SessionID)
	sessB, _ := srv.Hub.Session(wB.SessionID)
	sessA.BindCharacter("DIV_A", "Alice", 0)
	sessB.BindCharacter("DIV_A", "Bob", 0)

	bridge := New(srv.Hub)
	despawn := simulation.Frame{Opcode: wire.OpObjectDespawn, Payload: []byte{0xE1, 0x93, 0x04, 0x00}}
	bridge.PushToDivision("DIV_A", []simulation.Frame{despawn}, SessionIDString(wA.SessionID))

	f := readFrame(t, cB)
	if f.Opcode != wire.OpObjectDespawn || !bytes.Equal(f.Payload, despawn.Payload) {
		t.Fatalf("division push = op 0x%04X payload % X", f.Opcode, f.Payload)
	}
	expectNoFrame(t, cA, 400*time.Millisecond)
}

// TestPushToDivisionFollowsRebindAndClose drives the division index
// through the wire path: a session that changes division must receive
// pushes for the NEW division only, and a closed session must simply drop
// out of the fanout. A stale index entry here is a player silently losing
// server pushes — the failure mode the index must never introduce.
func TestPushToDivisionFollowsRebindAndClose(t *testing.T) {
	t.Parallel()
	srv := startServer(t)
	cA, wA := dialAndHello(t, srv)
	cB, wB := dialAndHello(t, srv)

	sessA, _ := srv.Hub.Session(wA.SessionID)
	sessB, _ := srv.Hub.Session(wB.SessionID)
	sessA.BindCharacter("DIV_A", "Alice", 0)
	sessB.BindCharacter("DIV_A", "Bob", 0)

	bridge := New(srv.Hub)
	despawnA := simulation.Frame{Opcode: wire.OpObjectDespawn, Payload: []byte{0xAA, 0xAA, 0xAA, 0xAA}}
	despawnB := simulation.Frame{Opcode: wire.OpObjectDespawn, Payload: []byte{0xBB, 0xBB, 0xBB, 0xBB}}

	// B moves division: DIV_A pushes stop reaching it, DIV_B pushes start.
	// Distinct payloads make a mis-routed frame unambiguous, and the
	// silence checks come LAST because a gorilla read timeout poisons the
	// conn for all later reads.
	sessB.BindCharacter("DIV_B", "Bob", 0)
	bridge.PushToDivision("DIV_A", []simulation.Frame{despawnA}, "")
	bridge.PushToDivision("DIV_B", []simulation.Frame{despawnB}, "")
	f := readFrame(t, cA)
	if f.Opcode != wire.OpObjectDespawn || !bytes.Equal(f.Payload, despawnA.Payload) {
		t.Fatalf("A's frame = op 0x%04X payload % X, want the DIV_A despawn", f.Opcode, f.Payload)
	}
	f = readFrame(t, cB)
	if f.Opcode != wire.OpObjectDespawn || !bytes.Equal(f.Payload, despawnB.Payload) {
		t.Fatalf("B's frame = op 0x%04X payload % X, want the DIV_B despawn only", f.Opcode, f.Payload)
	}
	expectNoFrame(t, cB, 400*time.Millisecond)

	// A closes for good: a dropped ws conn only DETACHES the session
	// (resume grace), and a detached member deliberately stays indexed —
	// so drive the FINAL close the way the hub would and watch the index
	// drop it.
	cA.Close()
	sessA.CloseWhenDrained(transport.ByeReasonNormal)
	wait.Eventually(t, 4*time.Second, "DIV_A to drop the closed session", func() bool {
		return len(srv.Hub.SessionsInDivision("DIV_A")) == 0
	})
	if got := len(srv.Hub.SessionsInDivision("DIV_A")); got != 0 {
		t.Fatalf("DIV_A still has %d members after the session closed", got)
	}
	bridge.PushToDivision("DIV_A", []simulation.Frame{despawnA}, "") // must not panic or deliver
}

// TestPushToSessionUnknownIDIsSilent covers churn: pushing to a session
// that closed between snapshot and push must be a no-op.
func TestPushToSessionUnknownIDIsSilent(t *testing.T) {
	t.Parallel()
	srv := startServer(t)
	bridge := New(srv.Hub)
	bridge.PushToSession("999999", []simulation.Frame{{Opcode: wire.OpObjectDespawn, Payload: []byte{1, 2, 3, 4}}})
	bridge.PushToSession("not-a-number", nil)
}

// stallConn is an in-memory transport.Conn for drain-window tests: reads
// feed from a buffered channel (so AcceptConn's HELLO handshake works with
// no network), and gated writes park until the conn closes — a session
// whose conn never completes a write can never finish its BYE drain, so
// the eviction window deterministically stays open.
type stallConn struct {
	inbound    chan transport.Frame
	closed     chan struct{}
	closeOnce  sync.Once
	gateWrites bool
}

func newStallConn(gateWrites bool) *stallConn {
	return &stallConn{
		inbound:    make(chan transport.Frame, 4),
		closed:     make(chan struct{}),
		gateWrites: gateWrites,
	}
}

func (c *stallConn) ReadFrame(ctx context.Context) (transport.Frame, error) {
	select {
	case f := <-c.inbound:
		return f, nil
	case <-c.closed:
		return transport.Frame{}, errors.New("stall conn closed")
	case <-ctx.Done():
		return transport.Frame{}, ctx.Err()
	}
}

func (c *stallConn) WriteFrame(transport.Frame) error {
	if c.gateWrites {
		<-c.closed
		return errors.New("stall conn closed")
	}
	return nil
}

func (c *stallConn) SupportsUnreliable() bool              { return false }
func (c *stallConn) WriteUnreliable(transport.Frame) error { return errors.New("no datagram lane") }

func (c *stallConn) Close(string) error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *stallConn) Kind() string       { return "fake" }
func (c *stallConn) RemoteAddr() string { return "fake:0" }

// TestSnapshotSessionsSkipsEvictedSession covers the eviction tick path: a
// session evicted by the single-bind swap must vanish from the tick's
// enumeration IMMEDIATELY — while its BYE is still draining — not just
// after final teardown. The victim's conn parks every write, so the drain
// window stays open for the whole test deterministically.
//
// The victim remains a hub member with a world snapshot while draining, but
// its eviction latch must keep it out of tick enumeration immediately.
func TestSnapshotSessionsSkipsEvictedSession(t *testing.T) {
	t.Parallel()
	srv := startServer(t)

	var opened []*transport.Session
	srv.Hub.OnSessionOpen(func(s *transport.Session) { opened = append(opened, s) })

	hello := transport.Frame{
		Opcode:  transport.OpHello,
		Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte(testAdmissionTicket)}),
	}
	victimConn := newStallConn(true) // writes park: the BYE can never flush
	victimConn.inbound <- hello
	srv.Hub.AcceptConn(victimConn) // synchronous: attached on return
	winnerConn := newStallConn(false)
	winnerConn.inbound <- hello
	srv.Hub.AcceptConn(winnerConn)
	if len(opened) != 2 {
		t.Fatalf("opened %d sessions, want 2", len(opened))
	}
	victim, winner := opened[0], opened[1]

	for _, s := range []*transport.Session{victim, winner} {
		s.SetWorldSnapshot("DIV_A", staticProvider{simulation.SessionSnapshot{
			DivisionID:  "DIV_A",
			CharacterID: 7,
			World:       simulation.DefaultWorldState(simulation.EuropeStartProfile()),
			NpcAnchor:   simulation.NpcShopSpawn(),
			NpcsEnabled: true,
		}})
	}

	bridge := New(srv.Hub)
	if snaps := bridge.SnapshotSessions(); len(snaps) != 2 {
		t.Fatalf("pre-evict snapshots = %d, want 2", len(snaps))
	}

	if _, replaced := srv.Hub.BindExclusive("d1:cg", victim); replaced {
		t.Fatal("first bind replaced something")
	}
	if old, replaced := srv.Hub.BindExclusive("d1:cg", winner); !replaced || old != victim {
		t.Fatalf("second bind: replaced=%v old=%v, want victim eviction", replaced, old)
	}
	if !victim.Evicted() {
		t.Fatal("victim not flagged evicted after replacement")
	}

	// The victim is mid-drain and still a hub member — exactly the
	// drain window the old code kept ticking into.
	if _, ok := srv.Hub.Session(victim.ID); !ok {
		t.Fatal("victim already torn down; the drain window did not stay open")
	}
	snaps := bridge.SnapshotSessions()
	if len(snaps) != 1 {
		t.Fatalf("post-evict snapshots = %d, want only the winner", len(snaps))
	}
	if snaps[0].SessionID != SessionIDString(winner.ID) {
		t.Fatalf("surviving snapshot = %q, want winner %d", snaps[0].SessionID, winner.ID)
	}
}

// TestMoverGid pins the opcode-aware coalesce-key extraction against the
// asm-pinned layouts.
func TestMoverGid(t *testing.T) {
	t.Parallel()
	move := wire.ObjectSourceMove{
		Position: wire.Position{RegionID: 0x6B4F, X: 1205, Y: 80, Z: 396, Heading: 0x8000},
		Gid:      777,
	}
	if got := moverGid(simulation.Frame{Opcode: transport.OpObjectSourceMove, Payload: move.Encode()}); got != 777 {
		t.Fatalf("0x30E3 gid = %d, want 777 (gid is LAST in this layout)", got)
	}

	corr := wire.ObjectSourceCorrection{
		Gid:      888,
		Position: wire.Position{RegionID: 0x6B4F, X: 1205, Y: 80, Z: 396, Heading: 0x8000},
	}
	if got := moverGid(simulation.Frame{Opcode: transport.OpObjectSourceCorrection, Payload: corr.Encode()}); got != 888 {
		t.Fatalf("0xB2F5 gid = %d, want 888 (gid is FIRST in this layout)", got)
	}

	if got := moverGid(simulation.Frame{Opcode: transport.OpObjectSourceMove, Payload: []byte{1, 2}}); got != 0 {
		t.Fatalf("malformed payload gid = %d, want 0", got)
	}
}

func TestSceneReplacementRejectsOldTickAndChangesVisibilityIdentity(t *testing.T) {
	srv := startServer(t)
	c, w := dialAndHello(t, srv)
	s, _ := srv.Hub.Session(w.SessionID)
	s.SetWorldSnapshot("DIV_A", staticProvider{simulation.SessionSnapshot{DivisionID: "DIV_A", CharacterID: 7}})
	b := New(srv.Hub)
	old := b.SnapshotSessions()[0].SessionID
	for cycle := 0; cycle < 2; cycle++ {
		if err := s.SendSceneReset([]transport.Frame{{Opcode: 0x3369, Payload: []byte{1, 2}}, {Opcode: 0x330a}}); err != nil {
			t.Fatal(err)
		}
		if len(b.SnapshotSessions()) != 0 {
			t.Fatal("loading scene admitted to ticker")
		}
		stale := []simulation.Frame{{Opcode: wire.OpSingleObjectSpawn, Payload: []byte{99}}, {Opcode: transport.OpObjectSourceCorrection, Payload: make([]byte, 20)}}
		b.PushToSession(old, stale)
		if !s.FinishSceneReentry() || s.FinishSceneReentry() {
			t.Fatal("scene-ready must be one-shot")
		}
		current := b.SnapshotSessions()[0].SessionID
		if current == old {
			t.Fatal("visibility identity survived scene replacement")
		}
		b.PushToSession(old, stale)
		b.PushToSession(current, []simulation.Frame{{Opcode: 0x7777, Payload: []byte{byte(cycle)}}})
		for _, op := range []uint16{0x3369, 0x330a, 0x7777} {
			if f := readFrame(t, c); f.Opcode != op {
				t.Fatalf("stale tick escaped or reset interleaved: %#x instead of %#x", f.Opcode, op)
			}
		}
		old = current
	}
}
