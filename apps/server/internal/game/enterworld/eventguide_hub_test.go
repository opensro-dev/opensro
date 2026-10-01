package enterworld_test

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

// fakeConn drives the real Hub handshake + dispatch without a socket.
type fakeConn struct {
	inbound chan transport.Frame
	closed  chan struct{}
	once    sync.Once

	mu      sync.Mutex
	written []transport.Frame
}

func newFakeConn() *fakeConn {
	return &fakeConn{inbound: make(chan transport.Frame, 16), closed: make(chan struct{})}
}

func (c *fakeConn) ReadFrame(ctx context.Context) (transport.Frame, error) {
	select {
	case f := <-c.inbound:
		return f, nil
	case <-ctx.Done():
		return transport.Frame{}, ctx.Err()
	case <-c.closed:
		return transport.Frame{}, io.EOF
	}
}

func (c *fakeConn) WriteFrame(f transport.Frame) error {
	c.mu.Lock()
	c.written = append(c.written, f)
	c.mu.Unlock()
	return nil
}

func (c *fakeConn) SupportsUnreliable() bool { return false }
func (c *fakeConn) WriteUnreliable(transport.Frame) error {
	return errors.New("fakeConn: no datagrams")
}
func (c *fakeConn) Close(string) error { c.once.Do(func() { close(c.closed) }); return nil }
func (c *fakeConn) Kind() string       { return "websocket" }
func (c *fakeConn) RemoteAddr() string { return "fake:0" }

func (c *fakeConn) frames() []transport.Frame {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]transport.Frame, len(c.written))
	copy(out, c.written)
	return out
}

// waitFor polls until pred passes or the deadline hits.
func waitFor(t *testing.T, what string, pred func() bool) {
	t.Helper()
	wait.Eventually(t, 3*time.Second, what, pred)
}

// frameQuiescence is how long the write count must hold still before the
// serial session loop counts as drained.
const frameQuiescence = 15 * time.Millisecond

// waitFrameQuiescence returns once the serial session loop has drained:
// the write count must hold still for frameQuiescence.
func waitFrameQuiescence(t *testing.T, what string, conn *fakeConn) {
	t.Helper()
	last, since := -1, time.Now()
	waitFor(t, what, func() bool {
		if count := len(conn.frames()); count != last {
			last, since = count, time.Now()
			return false
		}
		return time.Since(since) >= frameQuiescence
	})
}

// TestEventGuideAckHubDispatch drives 0x707B through the REAL registration
// path (enterworld.Register on a real Hub) and a real session handshake:
// HELLO -> WELCOME, EnterWorld bind, then a malformed 2-byte 0x707B (must
// be refused without effect) followed by SCOUT-B's golden 78 56 34 12,
// which must land mask 0x12345678 on the store record — visible to the
// next bootstrap — and push NO reply frame (the pin: persist only).
// This test FAILS if enterworld.Register stops wiring the 0x707B handler.
func TestEventGuideAckHubDispatch(t *testing.T) {
	t.Parallel()
	character := &enterworld.Character{ID: 3, Name: "asd2", ModelCodename: "CHAR_CH_MAN_ADVENTURER"}
	source := enterworld.StaticCharacterSource{enterworld.DefaultDivisionID: {character}}
	// The persist hook doubles as the test's synchronization point: it
	// runs on the session goroutine right AFTER the mask write, so the
	// mask it resolves there is the applied one, and a channel receive in
	// the test happens-after that write — the record reads below are
	// race-free. (The production reader — the next bootstrap — is serial
	// on the session loop and needs no such edge.)
	persisted := make(chan uint32, 8)
	admitted := make(chan uint64, 1)
	deps := &enterworld.Deps{
		Roster:            &enterworld.Roster{},
		Characters:        source,
		Items:             enterworld.NewTextdataItems(filepath.Join(t.TempDir(), "missing-textdata")),
		ResolveDivisionID: enterworld.DevResolveDivisionIDFromCatalog(source),
		AdmitCharacterSession: func(division, name string, session uint64) error {
			if division != enterworld.DefaultDivisionID || name != character.Name || session == 0 {
				panic("invalid admitted identity")
			}
			admitted <- session
			return nil
		},
		MutateCharacter: func(c *enterworld.Character, _ string, fn func()) {
			fn()
			select {
			case persisted <- enterworld.ResolveEventGuideStateMask(c):
			default:
			}
		},
	}

	srv, err := transport.NewServer(transport.Config{
		CertDir:     t.TempDir(),
		GracePeriod: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("transport server: %v", err)
	}
	srv.Hub.SetHelloAuth(func([]byte) (transport.AdmissionIdentity, error) {
		return transport.AdmissionIdentity{AccountID: "test-account", ShardID: "global-official"}, nil
	})
	authenticatedSession := entryauth.NewAuthenticatedSessionFixture(t, srv.Hub)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()
	enterworld.Register(srv.Hub, deps)

	conn := newFakeConn()
	go srv.Hub.AcceptConn(conn)

	conn.inbound <- transport.Frame{Opcode: transport.OpHello, Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte("test-admission")})}
	waitFor(t, "WELCOME", func() bool {
		for _, f := range conn.frames() {
			if f.Opcode == transport.OpWelcome {
				return true
			}
		}
		return false
	})

	conn.inbound <- transport.Frame{Opcode: transport.OpEnterWorld, Payload: transport.EncodeEnterWorld(
		authenticatedSession.Entry("0", "asd2"),
	)}
	waitFor(t, "EnterWorld result", func() bool {
		for _, f := range conn.frames() {
			if f.Opcode != transport.OpEnterWorldResult {
				continue
			}
			res, decodeErr := transport.DecodeEnterWorldResult(f.Payload)
			if decodeErr != nil {
				t.Fatalf("EnterWorld result does not decode: %v", decodeErr)
			}
			if !res.OK {
				t.Fatalf("EnterWorld refused (code 0x%X, blob %s)", res.NativeErrorCode, res.Blob)
			}
			return true
		}
		return false
	})

	// Let the bootstrap native frames drain, then freeze the write count:
	select {
	case sessionID := <-admitted:
		session, ok := srv.Hub.Session(sessionID)
		if !ok || session.WorldReady() {
			t.Fatal("admission must precede game-ready")
		}
		division, name, bound := session.CharacterBinding()
		if !bound || division != enterworld.DefaultDivisionID || name != character.Name {
			t.Fatal("admission close cannot identify its character")
		}
		// Delivery finds the character's visibility from the binding alone.
		if gid, ok := session.CharacterObjectID(); !ok || gid != enterworld.ObjectIDForCharacter(character) {
			t.Fatalf("bound object id = %d/%v, want the character's %d", gid, ok, enterworld.ObjectIDForCharacter(character))
		}
	default:
		t.Fatal("entry result published without claiming runtime ownership")
	}
	// Let the bootstrap native frames drain, then freeze the write count:
	// nothing after this point may be a native push (the pin: no reply).
	// RegisterEnterWorld sends the result then the natives on the same
	// session goroutine, ending (with these deps) at ObjectListFinalize
	// (0x330A) - wait for that tail, then require the count to hold still.
	waitFor(t, "bootstrap native burst", func() bool {
		sawResult, sawFinalize := false, false
		for _, f := range conn.frames() {
			switch f.Opcode {
			case transport.OpEnterWorldResult:
				sawResult = true
			case enterworld.OpcodeObjectListFinalize:
				sawFinalize = true
			}
		}
		return sawResult && sawFinalize
	})
	waitFrameQuiescence(t, "bootstrap burst quiescence", conn)
	baseline := len(conn.frames())
	// Drain any first-bootstrap seed persist signal now, while no 0x707B
	// frame is in flight: the next signal can only be the golden ack's
	// (the malformed frame refuses without persisting).
	for len(persisted) > 0 {
		<-persisted
	}

	// Malformed first: exactly-4-bytes is the pin, a 2-byte frame must be
	// refused without touching state (and without persisting).
	conn.inbound <- transport.Frame{Opcode: enterworld.OpcodeEventGuideAck, Payload: []byte{0x78, 0x56}}
	// Golden: 78 56 34 12 -> 0x12345678.
	conn.inbound <- transport.Frame{Opcode: enterworld.OpcodeEventGuideAck, Payload: []byte{0x78, 0x56, 0x34, 0x12}}

	select {
	case mask := <-persisted:
		if mask != 0x12345678 {
			t.Fatalf("persisted mask = 0x%08X, want 0x12345678 (malformed frame must not persist)", mask)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no persist after the golden 0x707B (handler not registered or not applying)")
	}
	// Happens-after the golden write via the channel receive; no later
	// writer exists, so these record reads are race-free.
	if got := enterworld.ResolveEventGuideStateMask(character); got != 0x12345678 {
		t.Fatalf("store mask = 0x%08X, want 0x12345678", got)
	}
	if character.Mission == nil || character.Mission.EventGuideStateMaskUpdatedAt == "" {
		t.Error("event-guide timestamp not stamped on the store record")
	}

	// The malformed frame ran first on the serial session loop, so state
	// reflects only the golden; now pin the no-reply half. The persisted
	// receive above happens-after the handler ran and frames are written
	// synchronously on the session loop, so a stable write count means no
	// late reply is still in flight.
	waitFrameQuiescence(t, "post-ack quiescence", conn)
	for _, f := range conn.frames()[baseline:] {
		if !isControlOpcode(f.Opcode) {
			t.Errorf("0x707B pushed reply frame 0x%04X; the pin says persist only", f.Opcode)
		}
	}
}

// isControlOpcode mirrors the transport's reserved/control split: PING and
// friends may flow any time; native game opcodes must not.
func isControlOpcode(op uint16) bool { return op <= 0x00FF }

// TestEventGuideAckConcurrentWithMove is REV-4 702's race gate: 0x707B
// mask writes and movement writes hammer the SAME character record from
// two goroutines (the mask lane owns character.Mission, the move lane owns
// character.World). Run under -race; both lanes must stay clean and the
// last mask write must win.
func TestEventGuideAckConcurrentWithMove(t *testing.T) {
	character := &enterworld.Character{ID: 7, Name: "Asd", ModelCodename: "CHAR_EU_MAN1"}
	deps := &enterworld.Deps{Characters: enterworld.StaticCharacterSource{"0": {character}}}
	moveRt := movement.NewRuntime(deps, simulation.NewWorldStore())

	start := simulation.EuropeStartProfile()
	moveBody := func(offset int16) []byte {
		out := make([]byte, 9)
		out[0] = 1
		binary.LittleEndian.PutUint16(out[1:3], start.RegionID)
		binary.LittleEndian.PutUint16(out[3:5], uint16(int16(start.X)+offset))
		binary.LittleEndian.PutUint16(out[5:7], uint16(int16(start.Y)))
		binary.LittleEndian.PutUint16(out[7:9], uint16(int16(start.Z)))
		return out
	}

	const rounds = 200
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		payload := make([]byte, 4)
		for i := 0; i < rounds; i++ {
			binary.LittleEndian.PutUint32(payload, uint32(i))
			if _, err := enterworld.HandleEventGuideAck(deps, character, payload, time.Now()); err != nil {
				t.Errorf("ack %d refused: %v", i, err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			if outcome := moveRt.HandleMove("0", character, moveBody(int16(i%40))); outcome.Refusal != nil {
				t.Errorf("move %d refused: %v", i, outcome.Refusal)
				return
			}
		}
	}()
	wg.Wait()

	if got := enterworld.ResolveEventGuideStateMask(character); got != rounds-1 {
		t.Errorf("final mask = %d, want %d (last write wins)", got, rounds-1)
	}
	if character.World == nil || character.World.Spawn == nil {
		t.Error("move write-back missing after concurrent run")
	}
}

// TestEventGuideAckConcurrentWithEnterWorld is REV-4 797's exact window: a
// SECOND session's EnterWorld bootstrap (HandleEnterWorld -> Build ->
// Resolve/Snapshot on the store record) runs BEFORE BindExclusive
// lame-ducks the first session, so the first can still be mid-0x707B.
// Hammer both sides of that window on one record under -race.
func TestEventGuideAckConcurrentWithEnterWorld(t *testing.T) {
	character := &enterworld.Character{ID: 3, Name: "asd2", ModelCodename: "CHAR_CH_MAN_ADVENTURER"}
	source := enterworld.StaticCharacterSource{enterworld.DefaultDivisionID: {character}}
	deps := &enterworld.Deps{
		Roster:            &enterworld.Roster{},
		Characters:        source,
		Items:             enterworld.NewTextdataItems(filepath.Join(t.TempDir(), "missing-textdata")),
		ResolveDivisionID: enterworld.DevResolveDivisionIDFromCatalog(source),
	}
	enterWorldPayload := transport.EncodeEnterWorld(entryauth.NewAuthenticatedEntryFixture(t, "0", "asd2"))

	const rounds = 50
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		payload := make([]byte, 4)
		for i := 0; i < rounds; i++ {
			binary.LittleEndian.PutUint32(payload, uint32(i))
			if _, err := enterworld.HandleEventGuideAck(deps, character, payload, time.Now()); err != nil {
				t.Errorf("ack %d refused: %v", i, err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			if outcome := enterworld.HandleEnterWorld(deps, enterWorldPayload); !outcome.OK {
				t.Errorf("enter-world %d failed: %+v", i, outcome.Result)
				return
			}
		}
	}()
	wg.Wait()

	if got := enterworld.ResolveEventGuideStateMask(character); got != rounds-1 {
		t.Errorf("final mask = %d, want %d", got, rounds-1)
	}
}
