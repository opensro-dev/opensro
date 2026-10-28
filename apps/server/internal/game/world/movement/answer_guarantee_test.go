/*
===========================================================================

answer_guarantee_test.go - every held predicted move is answered (#1978)

A predicted move sent while the client's scene loads is held until the
scene is ready. These tests drive the real hub, the real enter-world
lane and this lane over fake connections and pin the guarantee the
client's receipt watchdog relies on: a held command is drained with its
receipt at scene-ready, a command held across a scene replacement is
refused with a receipt (never executed into the new scene), a command
held at session close is counted rather than silently forgotten, and
commands sent after game-ready answer live - including across a
transport detach and resume.

===========================================================================
*/
package movement

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

// guaranteeCharacter is the bound character every guarantee flow enters
// as: the Europe-start adventurer the movement tests seed from.
func guaranteeCharacter() *enterworld.Character {
	return &enterworld.Character{ID: 7, Name: "Guarantor", ModelCodename: "CHAR_EU_MAN1"}
}

/*
================
guaranteeConn

A channel-driven transport.Conn: the test writes inbound frames, the
hub's session writer appends outbound ones here.
================
*/
type guaranteeConn struct {
	inbound chan transport.Frame
	closed  chan struct{}
	once    sync.Once

	// writeDelay, when set, throttles every delivered frame: the
	// session writer drains at this pace, as it does behind a client
	// that consumes its stream slowly.
	writeDelay time.Duration

	mu      sync.Mutex
	written []transport.Frame
}

/*
================
newGuaranteeConn
================
*/
func newGuaranteeConn() *guaranteeConn {
	return &guaranteeConn{inbound: make(chan transport.Frame, 64), closed: make(chan struct{})}
}

/*
================
guaranteeConn.ReadFrame
================
*/
func (c *guaranteeConn) ReadFrame(ctx context.Context) (transport.Frame, error) {
	select {
	case f := <-c.inbound:
		return f, nil
	case <-ctx.Done():
		return transport.Frame{}, ctx.Err()
	case <-c.closed:
		return transport.Frame{}, io.EOF
	}
}

/*
================
guaranteeConn.WriteFrame
================
*/
func (c *guaranteeConn) WriteFrame(f transport.Frame) error {
	if c.writeDelay > 0 {
		time.Sleep(c.writeDelay)
	}
	c.mu.Lock()
	c.written = append(c.written, f)
	c.mu.Unlock()
	return nil
}

/*
================
guaranteeConn.SupportsUnreliable
================
*/
func (c *guaranteeConn) SupportsUnreliable() bool { return false }

/*
================
guaranteeConn.WriteUnreliable
================
*/
func (c *guaranteeConn) WriteUnreliable(transport.Frame) error {
	return errors.New("guaranteeConn: no datagrams")
}

/*
================
guaranteeConn.Close
================
*/
func (c *guaranteeConn) Close(string) error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

/*
================
guaranteeConn.Kind
================
*/
func (c *guaranteeConn) Kind() string { return "websocket" }

/*
================
guaranteeConn.RemoteAddr
================
*/
func (c *guaranteeConn) RemoteAddr() string { return "fake:0" }

/*
================
guaranteeConn.frames

Every frame the session writer delivered, in order.
================
*/
func (c *guaranteeConn) frames() []transport.Frame {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]transport.Frame, len(c.written))
	copy(out, c.written)
	return out
}

/*
================
guaranteeConn.appliedPosition

The stream position this client has applied: reliable stream frames
(opcodes above the transport-reserved range) it received, counted as
the server counts them.
================
*/
func (c *guaranteeConn) appliedPosition() uint32 {
	var position uint32
	for _, frame := range c.frames() {
		if frame.Opcode > 0x0005 {
			position++
		}
	}
	return position
}

/*
================
movementReceipt

One decoded OpPredictedMoveResult body.
================
*/
type movementReceipt struct {
	ID       uint32 `json:"id"`
	Accepted bool   `json:"accepted"`
	Error    string `json:"error"`
}

/*
================
receiptsFor

Every movement receipt delivered on conn, in arrival order.
================
*/
func receiptsFor(conn *guaranteeConn) []movementReceipt {
	var receipts []movementReceipt
	for _, frame := range conn.frames() {
		if frame.Opcode != transport.OpPredictedMoveResult {
			continue
		}
		var receipt movementReceipt
		if err := json.Unmarshal(frame.Payload, &receipt); err == nil {
			receipts = append(receipts, receipt)
		}
	}
	return receipts
}

/*
================
receiptFor

The receipt answering command id, if one arrived.
================
*/
func receiptFor(conn *guaranteeConn, id uint32) (movementReceipt, bool) {
	for _, receipt := range receiptsFor(conn) {
		if receipt.ID == id {
			return receipt, true
		}
	}
	return movementReceipt{}, false
}

/*
================
predictedPayload

One predicted-move envelope: version 1, the command id, then the
native move body toward a point 200 units east of the Europe start.
================
*/
func predictedPayload(id uint32) []byte {
	start := simulation.EuropeStartProfile()
	body := make([]byte, 9)
	body[0] = 1
	binary.LittleEndian.PutUint16(body[1:3], start.RegionID)
	binary.LittleEndian.PutUint16(body[3:5], uint16(int16(start.X)+200))
	binary.LittleEndian.PutUint16(body[5:7], uint16(int16(start.Y)))
	binary.LittleEndian.PutUint16(body[7:9], uint16(int16(start.Z)))
	out := make([]byte, 5+len(body))
	out[0] = 1
	binary.LittleEndian.PutUint32(out[1:5], id)
	copy(out[5:], body)
	return out
}

/*
================
guaranteeWorld

A real server + hub with the enter-world lane and this movement lane
registered over one shared deps, as production composes them.
================
*/
type guaranteeWorld struct {
	server    *transport.Server
	character *enterworld.Character
	entry     *entryauth.SessionFixture
	runtime   *Runtime
}

/*
================
newGuaranteeWorld
================
*/
func newGuaranteeWorld(t *testing.T) *guaranteeWorld {
	t.Helper()
	character := guaranteeCharacter()
	source := enterworld.StaticCharacterSource{enterworld.DefaultDivisionID: {character}}
	deps := &enterworld.Deps{
		Roster:            &enterworld.Roster{},
		Characters:        source,
		Items:             enterworld.NewTextdataItems(filepath.Join(t.TempDir(), "missing-textdata")),
		ResolveDivisionID: enterworld.DevResolveDivisionIDFromCatalog(source),
		PlayerBaseStats: func(*enterworld.Character) (wire.BaseStats, error) {
			return wire.BaseStats{}, nil
		},
	}
	srv, err := transport.NewServer(transport.Config{
		CertDir:     t.TempDir(),
		GracePeriod: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.Hub.SetHelloAuth(func([]byte) (transport.AdmissionIdentity, error) {
		return transport.AdmissionIdentity{AccountID: "test-account", ShardID: "global-official"}, nil
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	})
	enterworld.Register(srv.Hub, deps)
	runtime := NewRuntime(deps, simulation.NewWorldStore())
	runtime.Register(srv.Hub)
	return &guaranteeWorld{
		server:    srv,
		character: character,
		entry:     entryauth.NewAuthenticatedSessionFixture(t, srv.Hub),
		runtime:   runtime,
	}
}

/*
================
guaranteeWorld.attach

Runs the hub handshake on a fresh fake connection and returns it with
the session's resume token.
================
*/
func (w *guaranteeWorld) attach(t *testing.T) (*guaranteeConn, []byte) {
	t.Helper()
	conn := newGuaranteeConn()
	go w.server.Hub.AcceptConn(conn)
	conn.inbound <- transport.Frame{Opcode: transport.OpHello, Payload: transport.EncodeHello(transport.Hello{
		AdmissionToken: []byte("test-admission"),
	})}
	var token []byte
	wait.Eventually(t, 3*time.Second, "WELCOME", func() bool {
		for _, frame := range conn.frames() {
			if frame.Opcode != transport.OpWelcome {
				continue
			}
			welcome, err := transport.DecodeWelcome(frame.Payload)
			if err == nil {
				token = append([]byte(nil), welcome.ResumeToken...)
			}
			return true
		}
		return false
	})
	return conn, token
}

/*
================
guaranteeWorld.enterWorld

Sends EnterWorld and waits for the bootstrap tail, which means the
admission handler finished and the scene is loading.
================
*/
func (w *guaranteeWorld) enterWorld(t *testing.T, conn *guaranteeConn) {
	t.Helper()
	conn.inbound <- transport.Frame{Opcode: transport.OpEnterWorld, Payload: transport.EncodeEnterWorld(
		w.entry.Entry("0", w.character.Name),
	)}
	wait.Eventually(t, 3*time.Second, "enter-world finalize", func() bool {
		for _, frame := range conn.frames() {
			if frame.Opcode == enterworld.OpcodeObjectListFinalize {
				return true
			}
		}
		return false
	})
}

/*
================
guaranteeWorld.sendGameReady

Sends the game-ready announcement and waits until the session's scene
is live.
================
*/
func (w *guaranteeWorld) sendGameReady(t *testing.T, conn *guaranteeConn) {
	t.Helper()
	conn.inbound <- transport.Frame{Opcode: enterworld.OpcodeGameReady}
	wait.Eventually(t, 3*time.Second, "scene live", func() bool {
		sessions := w.server.Hub.Sessions()
		if len(sessions) != 1 {
			return false
		}
		_, live := sessions[0].SceneRevision()
		return live
	})
}

/*
================
waitReceipt

Waits for the movement receipt answering command id.
================
*/
func waitReceipt(t *testing.T, conn *guaranteeConn, id uint32) movementReceipt {
	t.Helper()
	var receipt movementReceipt
	wait.Eventually(t, 3*time.Second, "movement receipt", func() bool {
		found, ok := receiptFor(conn, id)
		receipt = found
		return ok
	})
	return receipt
}

/*
================
waitQuiescent

Waits until the connection's delivered-frame count holds still, so a
"nothing was answered" assertion runs after the serial session loop
has drained.
================
*/
func waitQuiescent(t *testing.T, conn *guaranteeConn) {
	t.Helper()
	last, since := -1, time.Now()
	wait.Eventually(t, 3*time.Second, "frame quiescence", func() bool {
		if count := len(conn.frames()); count != last {
			last, since = count, time.Now()
			return false
		}
		return time.Since(since) >= 25*time.Millisecond
	})
}

/*
================
TestReceiptLatencyFollowsOutboundBacklog

Characterization (#1978): a receipt is generated the moment its
command settles, but it is DELIVERED in stream order behind whatever
reliable backlog is still draining to a slow-consuming client. With
240 throttled frames ahead of it, the scene-ready drain's receipt for
a held command lands seconds after game-ready - the server answered
promptly; the answer was in transit. This is the delay the client's
ten-second receipt watchdog cannot distinguish from loss after a
multi-megabyte scene bootstrap on a slow client.
================
*/
func TestReceiptLatencyFollowsOutboundBacklog(t *testing.T) {
	t.Parallel()
	world := newGuaranteeWorld(t)
	conn, _ := world.attach(t)
	conn.writeDelay = 10 * time.Millisecond
	world.enterWorld(t, conn)

	conn.inbound <- transport.Frame{Opcode: transport.OpPredictedMove, Payload: predictedPayload(1)}

	// World traffic still draining when the scene goes ready: the
	// receipt queues behind all of it.
	session := world.server.Hub.Sessions()[0]
	for i := 0; i < 240; i++ {
		if err := session.Send(0x9999, make([]byte, 8192)); err != nil {
			t.Fatalf("backlog frame %d: %v", i, err)
		}
	}

	sent := time.Now()
	world.sendGameReady(t, conn)
	var elapsed time.Duration
	wait.Eventually(t, 15*time.Second, "backlogged receipt", func() bool {
		if _, ok := receiptFor(conn, 1); ok {
			elapsed = time.Since(sent)
			return true
		}
		return false
	})
	if elapsed < 1500*time.Millisecond {
		t.Fatalf("receipt arrived %v after game-ready despite the backlog; want it stuck behind the draining stream", elapsed)
	}
}

/*
================
TestHeldPredictedMovesAreCountedOnSessionClose

A session that closes with commands still held counts each one
instead of forgetting it silently (#1978): a well-formed predicted
command lands under sceneClosed (its refusal receipt cannot be
built here - the session carries no binding - but the count and its
log line can), a malformed one under malformed, and the queue is
left empty.
================
*/
func TestHeldPredictedMovesAreCountedOnSessionClose(t *testing.T) {
	t.Parallel()
	rt := testRuntime(testCharacter())
	rt.predictLedger = newPredictionLedger()
	session := &transport.Session{ID: 11}
	session.BeginSceneAdmission()
	if !rt.queueLoadingMove(session, true, predictedPayload(5)) {
		t.Fatal("predicted command was not held during loading")
	}
	if !rt.queueLoadingMove(session, true, []byte{9, 9}) {
		t.Fatal("malformed command was not held during loading")
	}
	if !rt.queueLoadingMove(session, false, []byte{1}) {
		t.Fatal("native command was not held during loading")
	}

	sends := 0
	rt.answerHeldOnClose(session, func(uint16, []byte) error {
		sends++
		return nil
	})

	if got := len(rt.loadingQueues[session.ID]); got != 0 {
		t.Fatalf("queue holds %d commands after close, want 0", got)
	}
	if got := rt.predictLedger.drops[predictionSceneClosedReason]; got != 1 {
		t.Fatalf("sceneClosed count = %d, want 1", got)
	}
	if got := rt.predictLedger.drops[predictionDropMalformed]; got != 1 {
		t.Fatalf("malformed count = %d, want 1", got)
	}
	if sends != 0 {
		t.Fatalf("unbound close sent %d receipts, want 0", sends)
	}
}

/*
================
TestHeldPredictedMovesAreAnsweredAtGameReady

Commands sent while the scene loads are silent until game-ready, then
each is answered in order; a command sent into the live scene answers
at once.
================
*/
func TestHeldPredictedMovesAreAnsweredAtGameReady(t *testing.T) {
	t.Parallel()
	world := newGuaranteeWorld(t)
	conn, _ := world.attach(t)
	world.enterWorld(t, conn)

	conn.inbound <- transport.Frame{Opcode: transport.OpPredictedMove, Payload: predictedPayload(1)}
	conn.inbound <- transport.Frame{Opcode: transport.OpPredictedMove, Payload: predictedPayload(2)}
	waitQuiescent(t, conn)
	if got := receiptsFor(conn); len(got) != 0 {
		t.Fatalf("receipts during loading = %+v, want none yet", got)
	}

	world.sendGameReady(t, conn)
	if receipt := waitReceipt(t, conn, 1); !receipt.Accepted {
		t.Fatalf("held command was refused at game-ready instead of executed: %+v", receipt)
	}
	if _, ok := receiptFor(conn, 2); !ok {
		t.Fatal("second held command was not answered at game-ready")
	}

	conn.inbound <- transport.Frame{Opcode: transport.OpPredictedMove, Payload: predictedPayload(3)}
	wait.Eventually(t, 3*time.Second, "live receipt", func() bool {
		_, ok := receiptFor(conn, 3)
		return ok
	})
}

/*
================
TestHeldPredictedMovesAreAnsweredInSecondScene

The scene-ready drain re-arms: commands held by a second scene load
answer at that scene's game-ready.
================
*/
func TestHeldPredictedMovesAreAnsweredInSecondScene(t *testing.T) {
	t.Parallel()
	world := newGuaranteeWorld(t)
	conn, _ := world.attach(t)
	world.enterWorld(t, conn)
	world.sendGameReady(t, conn) // no held command; just reach the live scene

	session := world.server.Hub.Sessions()[0]
	if err := session.SendSceneReset([]transport.Frame{
		{Opcode: enterworld.OpcodeResetClient, Payload: []byte{1, 2}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, live := session.SceneRevision(); live {
		t.Fatal("scene still live after the reset")
	}
	conn.inbound <- transport.Frame{Opcode: transport.OpPredictedMove, Payload: predictedPayload(1)}
	waitQuiescent(t, conn)
	if got := receiptsFor(conn); len(got) != 0 {
		t.Fatalf("receipts during second loading = %+v, want none yet", got)
	}
	world.sendGameReady(t, conn)
	if receipt := waitReceipt(t, conn, 1); !receipt.Accepted {
		t.Fatalf("second-scene held command was refused instead of executed: %+v", receipt)
	}
}

/*
================
TestHeldPredictedMovesAreRefusedWhenSceneIsReplaced

A command held by a scene that is then replaced (a second admission
before game-ready) must not execute into the new scene: at the new
scene's game-ready it is answered with a refusal receipt, exactly
once.
================
*/
func TestHeldPredictedMovesAreRefusedWhenSceneIsReplaced(t *testing.T) {
	t.Parallel()
	world := newGuaranteeWorld(t)
	conn, _ := world.attach(t)
	world.enterWorld(t, conn)

	conn.inbound <- transport.Frame{Opcode: transport.OpPredictedMove, Payload: predictedPayload(1)}
	waitQuiescent(t, conn)

	// A second admission replaces the loading scene before it ever
	// went live (the resume / re-entry shape).
	world.enterWorld(t, conn)
	world.sendGameReady(t, conn)
	receipt := waitReceipt(t, conn, 1)
	if receipt.Accepted {
		t.Fatalf("held command executed into the replacement scene: %+v", receipt)
	}
	if receipt.Error == "" {
		t.Fatalf("replacement refusal carries no error code: %+v", receipt)
	}
	count := 0
	for _, got := range receiptsFor(conn) {
		if got.ID == 1 {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("command 1 answered %d times, want exactly once", count)
	}
}

/*
================
TestPredictedMovesAnswerAcrossDetachAndResume

The scene survives a transport detach: after a positioned resume the
live scene keeps answering, with no new admission.
================
*/
func TestPredictedMovesAnswerAcrossDetachAndResume(t *testing.T) {
	t.Parallel()
	world := newGuaranteeWorld(t)
	conn, token := world.attach(t)
	world.enterWorld(t, conn)
	world.sendGameReady(t, conn)

	conn.inbound <- transport.Frame{Opcode: transport.OpPredictedMove, Payload: predictedPayload(1)}
	wait.Eventually(t, 3*time.Second, "pre-detach receipt", func() bool {
		_, ok := receiptFor(conn, 1)
		return ok
	})
	waitQuiescent(t, conn)
	position := conn.appliedPosition()
	if err := conn.Close("client gone"); err != nil {
		t.Fatal(err)
	}

	resumed := newGuaranteeConn()
	go world.server.Hub.AcceptConn(resumed)
	resumed.inbound <- transport.Frame{Opcode: transport.OpHello, Payload: transport.EncodeHello(transport.Hello{
		AdmissionToken: []byte("test-admission"),
		ResumeToken:    token,
		HasPosition:    true,
		LastApplied:    position,
	})}
	wait.Eventually(t, 3*time.Second, "resumed WELCOME", func() bool {
		for _, frame := range resumed.frames() {
			if frame.Opcode != transport.OpWelcome {
				continue
			}
			welcome, err := transport.DecodeWelcome(frame.Payload)
			return err == nil && welcome.Resumed
		}
		return false
	})

	resumed.inbound <- transport.Frame{Opcode: transport.OpPredictedMove, Payload: predictedPayload(2)}
	wait.Eventually(t, 3*time.Second, "post-resume receipt", func() bool {
		_, ok := receiptFor(resumed, 2)
		return ok
	})
}

/*
================
TestHeldPredictedMovesSurviveDetachDuringLoading

A detach mid-load does not strand held commands: the scene is still
loading on the same session, so the game-ready after a positioned
resume drains them.
================
*/
func TestHeldPredictedMovesSurviveDetachDuringLoading(t *testing.T) {
	t.Parallel()
	world := newGuaranteeWorld(t)
	conn, token := world.attach(t)
	world.enterWorld(t, conn)

	conn.inbound <- transport.Frame{Opcode: transport.OpPredictedMove, Payload: predictedPayload(1)}
	waitQuiescent(t, conn)
	position := conn.appliedPosition()
	if err := conn.Close("client gone"); err != nil {
		t.Fatal(err)
	}

	resumed := newGuaranteeConn()
	go world.server.Hub.AcceptConn(resumed)
	resumed.inbound <- transport.Frame{Opcode: transport.OpHello, Payload: transport.EncodeHello(transport.Hello{
		AdmissionToken: []byte("test-admission"),
		ResumeToken:    token,
		HasPosition:    true,
		LastApplied:    position,
	})}
	wait.Eventually(t, 3*time.Second, "resumed WELCOME", func() bool {
		for _, frame := range resumed.frames() {
			if frame.Opcode != transport.OpWelcome {
				continue
			}
			welcome, err := transport.DecodeWelcome(frame.Payload)
			return err == nil && welcome.Resumed
		}
		return false
	})

	world.sendGameReady(t, resumed)
	waitReceipt(t, resumed, 1)
}
