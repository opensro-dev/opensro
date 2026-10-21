package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"opensro.online/server/internal/testsupport/wait"
)

// fakeConn is an in-memory Conn for session-level tests: it records every
// written frame, tracks writer concurrency (the single-writer contract),
// and can gate writes to force queue buildup deterministically.
type fakeConn struct {
	mu     sync.Mutex
	wrote  []Frame
	gate   chan struct{} // nil = writes flow; non-nil = writes block until closed
	closed chan struct{}

	inbound chan Frame

	// dgrams records frames sent via WriteUnreliable, separately from the
	// reliable wrote list, so tests can assert which lane a frame took.
	dgrams []Frame

	closeOnce     sync.Once
	inFlight      atomic.Int32
	maxInFlight   atomic.Int32
	supportsDgram bool
	kind          string // Kind() override; empty means "fake"
}

func newFakeConn(gated bool) *fakeConn {
	fc := &fakeConn{
		closed:  make(chan struct{}),
		inbound: make(chan Frame, 16),
	}
	if gated {
		fc.gate = make(chan struct{})
	}
	return fc
}

func (f *fakeConn) openGate() { close(f.gate) }

func (f *fakeConn) ReadFrame(ctx context.Context) (Frame, error) {
	select {
	case fr := <-f.inbound:
		return fr, nil
	case <-f.closed:
		return Frame{}, errors.New("fake conn closed")
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	}
}

func (f *fakeConn) WriteFrame(fr Frame) error {
	n := f.inFlight.Add(1)
	defer f.inFlight.Add(-1)
	for {
		old := f.maxInFlight.Load()
		if n <= old || f.maxInFlight.CompareAndSwap(old, n) {
			break
		}
	}
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-f.closed:
			return errors.New("fake conn closed")
		}
	}
	select {
	case <-f.closed:
		return errors.New("fake conn closed")
	default:
	}
	f.mu.Lock()
	f.wrote = append(f.wrote, Frame{Opcode: fr.Opcode, Payload: append([]byte(nil), fr.Payload...)})
	f.mu.Unlock()
	return nil
}

func (f *fakeConn) written() []Frame {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Frame, len(f.wrote))
	copy(out, f.wrote)
	return out
}

func (f *fakeConn) SupportsUnreliable() bool { return f.supportsDgram }

func (f *fakeConn) WriteUnreliable(fr Frame) error {
	f.mu.Lock()
	f.dgrams = append(f.dgrams, Frame{Opcode: fr.Opcode, Payload: append([]byte(nil), fr.Payload...)})
	f.mu.Unlock()
	return nil
}

func (f *fakeConn) datagrams() []Frame {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Frame, len(f.dgrams))
	copy(out, f.dgrams)
	return out
}

func (f *fakeConn) Close(string) error {
	f.closeOnce.Do(func() { close(f.closed) })
	return nil
}

func (f *fakeConn) Kind() string {
	if f.kind != "" {
		return f.kind
	}
	return "fake"
}

func (f *fakeConn) RemoteAddr() string { return "fake:0" }

func testCfg() Config {
	cfg := Config{
		HelloTimeout:      time.Second,
		GracePeriod:       5 * time.Second,
		KeepaliveInterval: time.Hour, // keep keepalive out of these tests
		IdleTimeout:       time.Hour,
		// Big enough that the concurrent-send burst (8x50) can outrun the
		// single writer without tripping the slow-consumer kill.
		OutboundQueue: 4096,
	}
	cfg.applyDefaults()
	return cfg
}

func attachedSession(t *testing.T, gated bool) (*Hub, *Session, *fakeConn) {
	t.Helper()
	hub := newHub(testCfg())
	sess, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	fc := newFakeConn(gated)
	if err := sess.attach(fc, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(sess, nil) })
	return hub, sess, fc
}

func waitWritten(t *testing.T, fc *fakeConn, n int) []Frame {
	t.Helper()
	var got []Frame
	wait.Eventually(t, 3*time.Second, fmt.Sprintf("%d written frames", n), func() bool {
		got = fc.written()
		return len(got) >= n
	})
	return got
}

// TestSendCopiesPayload verifies that mutating the caller's buffer after
// Send must not corrupt the delivered frame.
func TestSendCopiesPayload(t *testing.T) {
	_, sess, fc := attachedSession(t, false)
	buf := []byte{0x01, 0x02, 0x03}
	if err := sess.Send(0x3126, buf); err != nil {
		t.Fatal(err)
	}
	buf[0], buf[1], buf[2] = 0xFF, 0xFF, 0xFF

	frames := waitWritten(t, fc, 2) // WELCOME + ours
	got := frames[len(frames)-1]
	if got.Opcode != 0x3126 || !bytes.Equal(got.Payload, []byte{0x01, 0x02, 0x03}) {
		t.Fatalf("delivered frame = op 0x%04X payload % X", got.Opcode, got.Payload)
	}
}

// TestConcurrentSendSingleWriter is the send-path race check: many goroutines
// Send concurrently, exactly one writer ever touches the Conn.
func TestConcurrentSendSingleWriter(t *testing.T) {
	_, sess, fc := attachedSession(t, false)
	const goroutines, per = 8, 50
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				if err := sess.Send(0xB06D, []byte{byte(g), byte(i)}); err != nil {
					t.Errorf("Send: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	waitWritten(t, fc, 1+goroutines*per)
	if max := fc.maxInFlight.Load(); max != 1 {
		t.Fatalf("max concurrent writers = %d, want 1", max)
	}
}

// TestLossyCoalesceAndOrderedPriority verifies that while the transport is
// stalled, keyed loss-tolerant frames coalesce per (opcode, key) and the
// ordered queue always drains first.
func TestLossyCoalesceAndOrderedPriority(t *testing.T) {
	_, sess, fc := attachedSession(t, true) // gated: writeLoop stalls on WELCOME

	// Three positions for entity 42 (only the last may survive), one for 43.
	sess.SendUnreliableKeyed(0x30E3, 42, []byte{0x2A, 0x01})
	sess.SendUnreliableKeyed(0x30E3, 42, []byte{0x2A, 0x02})
	sess.SendUnreliableKeyed(0x30E3, 42, []byte{0x2A, 0x03})
	sess.SendUnreliableKeyed(0x30E3, 43, []byte{0x2B, 0x01})
	// A must-deliver frame queued after them still goes out first.
	if err := sess.Send(0xB06D, []byte{0x99}); err != nil {
		t.Fatal(err)
	}

	fc.openGate()
	frames := waitWritten(t, fc, 4) // WELCOME, 0xB06D, then 2 coalesced
	if len(frames) != 4 {
		t.Fatalf("wrote %d frames, want exactly 4 (coalesce failed): %+v", len(frames), frames)
	}
	if frames[0].Opcode != OpWelcome {
		t.Fatalf("frame 0 = 0x%04X, want WELCOME", frames[0].Opcode)
	}
	if frames[1].Opcode != 0xB06D {
		t.Fatalf("frame 1 = 0x%04X, want ordered 0xB06D before lossy", frames[1].Opcode)
	}
	if frames[2].Opcode != 0x30E3 || !bytes.Equal(frames[2].Payload, []byte{0x2A, 0x03}) {
		t.Fatalf("frame 2 = op 0x%04X payload % X, want latest for key 42", frames[2].Opcode, frames[2].Payload)
	}
	if frames[3].Opcode != 0x30E3 || !bytes.Equal(frames[3].Payload, []byte{0x2B, 0x01}) {
		t.Fatalf("frame 3 = op 0x%04X payload % X, want key 43", frames[3].Opcode, frames[3].Payload)
	}
}

// TestControlExtensionDispatch proves 0x0006+ control frames reach
// registered handlers while 0x0001-0x0005 stay session-internal.
func TestControlExtensionDispatch(t *testing.T) {
	hub, sess, fc := attachedSession(t, false)
	hub.SetEnterWorldAuth(func(_ *Session, _ EnterWorld) (bool, uint32) { return true, 0 })

	got := make(chan Frame, 1)
	hub.Handle(OpEnterWorld, func(s *Session, op uint16, payload []byte) {
		got <- Frame{Opcode: op, Payload: append([]byte(nil), payload...)}
	})

	bind := EncodeEnterWorld(EnterWorld{Division: "DIV01", CharName: "CG", AuthToken: []byte("test-token")})
	fc.inbound <- Frame{Opcode: OpEnterWorld, Payload: bind}

	select {
	case f := <-got:
		ew, err := DecodeEnterWorld(f.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if ew.Division != "DIV01" || ew.CharName != "CG" {
			t.Fatalf("dispatched bind = %+v", ew)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("EnterWorld never dispatched")
	}

	// PING stays internal: answered with PONG, never dispatched.
	fc.inbound <- Frame{Opcode: OpPing, Payload: []byte{0x77}}
	wait.Eventually(t, 3*time.Second, "a PONG for the internal PING", func() bool {
		for _, f := range fc.written() {
			if f.Opcode == OpPong && bytes.Equal(f.Payload, []byte{0x77}) {
				return true
			}
		}
		return false
	})
	_ = sess
}

// TestUnreliableAllowlist pins the rule that only 0x30E3/0xB2F5 may
// travel unreliably; every other opcode is forced onto the reliable lane
// even when the connection supports datagrams.
func TestUnreliableAllowlist(t *testing.T) {
	hub := newHub(testCfg())
	sess, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	fc := newFakeConn(false)
	fc.supportsDgram = true
	if err := sess.attach(fc, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(sess, nil) })

	// Allowlisted opcodes ride the datagram lane.
	if err := sess.SendUnreliable(OpObjectSourceMove, []byte{0x01}); err != nil {
		t.Fatal(err)
	}
	if err := sess.SendUnreliableKeyed(OpObjectSourceCorrection, 42, []byte{0x02}); err != nil {
		t.Fatal(err)
	}
	// Non-allowlisted opcodes are forced reliable — including via the
	// keyed variant, where coalescing would drop must-deliver frames.
	if err := sess.SendUnreliable(0xB06D, []byte{0x03}); err != nil {
		t.Fatal(err)
	}
	if err := sess.SendUnreliableKeyed(0x3126, 42, []byte{0x04}); err != nil {
		t.Fatal(err)
	}
	if err := sess.SendUnreliableKeyed(0x3126, 42, []byte{0x05}); err != nil {
		t.Fatal(err)
	}

	// WELCOME + the three reliable-forced frames.
	frames := waitWritten(t, fc, 4)
	if len(frames) != 4 {
		t.Fatalf("reliable frames = %d, want 4 (was a must-deliver frame coalesced away?)", len(frames))
	}
	var reliableOps []uint16
	for _, f := range frames[1:] {
		reliableOps = append(reliableOps, f.Opcode)
	}
	if reliableOps[0] != 0xB06D || reliableOps[1] != 0x3126 || reliableOps[2] != 0x3126 {
		t.Fatalf("reliable lane ops = %04X, want [B06D 3126 3126]", reliableOps)
	}

	dgrams := fc.datagrams()
	if len(dgrams) != 2 {
		t.Fatalf("datagram frames = %d, want 2", len(dgrams))
	}
	if dgrams[0].Opcode != OpObjectSourceMove || dgrams[1].Opcode != OpObjectSourceCorrection {
		t.Fatalf("datagram ops = 0x%04X, 0x%04X", dgrams[0].Opcode, dgrams[1].Opcode)
	}
	metrics := hub.Metrics()
	if metrics.DatagramsOut != 2 || metrics.DatagramBytesOut != 6 {
		t.Fatalf("datagram metrics = frames %d bytes %d, want 2/6", metrics.DatagramsOut, metrics.DatagramBytesOut)
	}
}

// TestMetricsCountAttachesByKind verifies the wt_ok / ws_ok
// counters move on completed handshakes, keyed by transport kind.
func TestMetricsCountAttachesByKind(t *testing.T) {
	hub := newHub(testCfg())
	allowTestHelloAdmission(hub)

	for _, kind := range []string{"webtransport", "websocket", "websocket"} {
		fc := newFakeConn(false)
		fc.kind = kind
		fc.inbound <- Frame{
			Opcode:  OpHello,
			Payload: EncodeHello(Hello{AdmissionToken: testHelloAdmissionToken}),
		}
		hub.AcceptConn(fc) // synchronous: returns once attached
	}

	m := hub.Metrics()
	if m.WTOk != 1 || m.WSOK != 2 {
		t.Fatalf("metrics = %+v, want wt_ok=1 ws_ok=2", m)
	}
	if m.LiveSessions != 3 {
		t.Fatalf("live_sessions = %d, want 3", m.LiveSessions)
	}
	for _, s := range hub.Sessions() {
		hub.closeSession(s, nil)
	}
	if m := hub.Metrics(); m.LiveSessions != 0 {
		t.Fatalf("live_sessions after close = %d, want 0", m.LiveSessions)
	}
}

// TestBindExclusiveLifecycle covers the registry edges around the eviction
// path the smoke test proves on the wire: idempotent rebind, key handover,
// and cleanup at close.
func TestBindExclusiveLifecycle(t *testing.T) {
	hub := newHub(testCfg())
	newSess := func() *Session {
		s, err := hub.createSession()
		if err != nil {
			t.Fatal(err)
		}
		if err := s.attach(newFakeConn(false), false); err != nil {
			t.Fatal(err)
		}
		return s
	}
	a, b := newSess(), newSess()

	if _, replaced := hub.BindExclusive("d1:cg", a); replaced {
		t.Fatal("first bind evicted something")
	}
	// Idempotent rebind of the same session.
	if _, replaced := hub.BindExclusive("d1:cg", a); replaced {
		t.Fatal("self rebind evicted itself")
	}
	// Moving A to a new character releases the old key without eviction.
	if _, replaced := hub.BindExclusive("d1:other", a); replaced {
		t.Fatal("key handover evicted someone")
	}
	if _, ok := hub.BoundSession("d1:cg"); ok {
		t.Fatal("old key still bound after handover")
	}
	// B can now take the freed key with no replacement.
	if _, replaced := hub.BindExclusive("d1:cg", b); replaced {
		t.Fatal("bind of freed key evicted someone")
	}
	// Closing B clears its binding.
	hub.closeSession(b, nil)
	if _, ok := hub.BoundSession("d1:cg"); ok {
		t.Fatal("binding survived session close")
	}
	hub.closeSession(a, nil)
	if _, ok := hub.BoundSession("d1:other"); ok {
		t.Fatal("binding survived session close")
	}
}

// TestEnterWorldAuthGate covers the enter-world identity seam: with a verifier
// installed, a bad token is refused by the transport (OpEnterWorldResult
// with the deny code, game handler never runs) and a good token passes
// through to the handler.
func TestEnterWorldAuthGate(t *testing.T) {
	hub, _, fc := attachedSession(t, false)

	const denyCode uint32 = 0x00A1
	hub.SetEnterWorldAuth(func(_ *Session, ew EnterWorld) (bool, uint32) {
		return bytes.Equal(ew.AuthToken, []byte("good-token")), denyCode
	})
	handled := make(chan EnterWorld, 1)
	hub.Handle(OpEnterWorld, func(_ *Session, _ uint16, payload []byte) {
		ew, err := DecodeEnterWorld(payload)
		if err != nil {
			t.Errorf("handler got malformed payload: %v", err)
			return
		}
		handled <- ew
	})

	// Bad token: refused by the gate.
	fc.inbound <- Frame{Opcode: OpEnterWorld, Payload: EncodeEnterWorld(EnterWorld{
		Division: "d1", CharName: "cg", AuthToken: []byte("wrong"),
	})}
	wait.Eventually(t, 3*time.Second, "an OpEnterWorldResult refusal on the wire", func() bool {
		refused := false
		for _, f := range fc.written() {
			if f.Opcode != OpEnterWorldResult {
				continue
			}
			res, err := DecodeEnterWorldResult(f.Payload)
			if err != nil {
				t.Fatal(err)
			}
			if res.OK || res.NativeErrorCode != denyCode {
				t.Fatalf("refusal = %+v, want ok=false code=0x%X", res, denyCode)
			}
			refused = true
		}
		return refused
	})
	if got := hub.Metrics().EnterWorldAuthRefused; got != 1 {
		t.Fatalf("enter_world_auth_refused = %d, want 1", got)
	}
	select {
	case ew := <-handled:
		t.Fatalf("handler ran despite auth refusal: %+v", ew)
	default:
	}

	// Good token: gate passes, handler runs with the token intact.
	fc.inbound <- Frame{Opcode: OpEnterWorld, Payload: EncodeEnterWorld(EnterWorld{
		Division: "d1", CharName: "cg", AuthToken: []byte("good-token"),
	})}
	select {
	case ew := <-handled:
		if ew.Division != "d1" || ew.CharName != "cg" || !bytes.Equal(ew.AuthToken, []byte("good-token")) {
			t.Fatalf("handler bind = %+v", ew)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler never ran for the authorized bind")
	}
}

// TestShutdownDetachedSessionClosesImmediately pins the shutdown-latency
// fix: Hub.shutdown on a DETACHED session must not sit out the 2s drain
// cap. The writeLoop only flushes while attached, so a detached session's
// BYE can never leave — CloseWhenDrained closes it on the spot instead.
func TestShutdownDetachedSessionClosesImmediately(t *testing.T) {
	hub, sess, fc := attachedSession(t, false)

	fc.Close("test: client dropped")
	waitUntil(t, "session to detach", func() bool { return sess.Kind() == "detached" })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	hub.shutdown(ctx)
	elapsed := time.Since(start)

	select {
	case <-sess.Done():
	default:
		t.Fatal("session not closed after shutdown returned")
	}
	if elapsed >= 500*time.Millisecond {
		t.Fatalf("shutdown of a detached session took %v, want well under the old 2s cap", elapsed)
	}
}

// TestCloseWhenDrainedDetachedKillsResumeToken pins the other half of the
// detached fast-close policy: the condemned session's resume token dies
// with it, so a client presenting the old token gets a FRESH session — a
// condemned session must not be resumable.
func TestCloseWhenDrainedDetachedKillsResumeToken(t *testing.T) {
	hub, sess, fc := attachedSession(t, false)
	allowTestHelloAdmission(hub)
	oldID := sess.ID
	token := sess.resumeToken

	fc.Close("test: client dropped")
	waitUntil(t, "session to detach", func() bool { return sess.Kind() == "detached" })

	sess.CloseWhenDrained(ByeReasonShutdown)
	select {
	case <-sess.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("detached session not closed promptly (old 2s drain cap?)")
	}

	fc2 := newFakeConn(false)
	fc2.inbound <- Frame{Opcode: OpHello, Payload: EncodeHello(Hello{
		ResumeToken:    token[:],
		AdmissionToken: testHelloAdmissionToken,
	})}
	hub.AcceptConn(fc2) // synchronous: returns once attached
	frames := waitWritten(t, fc2, 1)
	if frames[0].Opcode != OpWelcome {
		t.Fatalf("first frame = 0x%04X, want WELCOME", frames[0].Opcode)
	}
	w, err := DecodeWelcome(frames[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	if w.Resumed {
		t.Fatal("old token resumed a condemned session")
	}
	if w.SessionID == oldID {
		t.Fatalf("fresh session reused the condemned ID %d", oldID)
	}
	fresh, ok := hub.Session(w.SessionID)
	if !ok {
		t.Fatalf("fresh session %d not registered", w.SessionID)
	}
	hub.closeSession(fresh, nil)
}

// TestHandleRejectsSessionInternal pins the registration guard.
func TestHandleRejectsSessionInternal(t *testing.T) {
	hub := newHub(testCfg())
	for _, op := range []uint16{OpHello, OpWelcome, OpPing, OpPong, OpBye} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("Handle(0x%04X) did not panic", op)
				}
			}()
			hub.Handle(op, func(*Session, uint16, []byte) {})
		}()
	}
	// Extension range and native range must be registrable.
	hub.Handle(OpEnterWorld, func(*Session, uint16, []byte) {})
	hub.Handle(0x706D, func(*Session, uint16, []byte) {})
}

/*
================
TestCondemnedSessionClosesWhenItsConnectionDies

A session told to close once drained (CloseWhenDrained) whose connection
then dies cannot deliver its BYE and must not be resumed, so it closes at
once instead of sitting out the 2 s drain cap (#1288). The write gate keeps
the BYE queued, as a peer that stopped reading does.
================
*/
func TestCondemnedSessionClosesWhenItsConnectionDies(t *testing.T) {
	_, sess, fc := attachedSession(t, true)
	sess.CloseWhenDrained(ByeReasonShutdown)
	fc.Close("peer gone")
	select {
	case <-sess.Done():
	case <-time.After(time.Second):
		t.Fatal("condemned session waited for the drain cap after its connection died")
	}
	if len(fc.written()) != 0 {
		t.Fatalf("frames written to a dead connection: %v", fc.written())
	}
}
