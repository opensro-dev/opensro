/*
===========================================================================

itemuse_resume_publication_test.go - capture inventory publication races

Drive the real Hub, authenticated EnterWorld registration and item handlers.
Channel barriers pause committed operations before their ordinary publishers;
no packets are fabricated or reordered after the transport writes them.
The resumed bootstrap must follow every receipt committed by the old handler.

===========================================================================
*/
package action

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

const (
	publicationFence   = 0x7ffd
	publicationTimeout = 5 * time.Second
	publicationSlot    = 21
)

/*
================
publicationConn

The transport writes into an ordered channel instead of a socket. The real
session writer still owns ordering, queueing and attachment replacement.
================
*/
type publicationConn struct {
	in     chan transport.Frame
	out    chan transport.Frame
	closed chan struct{}
	once   sync.Once
}

/*
================
newPublicationConn
================
*/
func newPublicationConn() *publicationConn {
	return &publicationConn{in: make(chan transport.Frame, 16), out: make(chan transport.Frame, 4096), closed: make(chan struct{})}
}

/*
================
ReadFrame
================
*/
func (c *publicationConn) ReadFrame(ctx context.Context) (transport.Frame, error) {
	select {
	case f := <-c.in:
		return f, nil
	case <-ctx.Done():
		return transport.Frame{}, ctx.Err()
	case <-c.closed:
		return transport.Frame{}, io.EOF
	}
}

/*
================
WriteFrame
================
*/
func (c *publicationConn) WriteFrame(f transport.Frame) error {
	select {
	case <-c.closed:
		return io.ErrClosedPipe
	default:
	}
	f.Payload = append([]byte(nil), f.Payload...)
	select {
	case c.out <- f:
		return nil
	case <-c.closed:
		return io.ErrClosedPipe
	}
}

/*
================
SupportsUnreliable
================
*/
func (*publicationConn) SupportsUnreliable() bool { return false }

/*
================
WriteUnreliable
================
*/
func (*publicationConn) WriteUnreliable(transport.Frame) error { return io.ErrClosedPipe }

/*
================
Close
================
*/
func (c *publicationConn) Close(string) error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

/*
================
Kind
================
*/
func (*publicationConn) Kind() string { return "websocket" }

/*
================
RemoteAddr
================
*/
func (*publicationConn) RemoteAddr() string { return "publication-test:0" }

/*
================
publicationThrough
================
*/
func publicationThrough(t *testing.T, c *publicationConn, opcode uint16) []transport.Frame {
	t.Helper()
	var frames []transport.Frame
	for {
		var f transport.Frame
		select {
		case f = <-c.out:
		case <-time.After(publicationTimeout):
			t.Fatalf("timed out waiting for opcode %04x", opcode)
		}
		frames = append(frames, f)
		if f.Opcode == opcode {
			return frames
		}
	}
}

/*
================
publicationHarness
================
*/
type publicationHarness struct {
	rt        *Runtime
	clock     *fakeClock
	deps      *enterworld.Deps
	character *enterworld.Character
	server    *transport.Server
	auth      *entryauth.SessionFixture
	request   []byte
}

/*
================
newPublicationHarness
================
*/
func newPublicationHarness(t *testing.T, refill bool, quantity int64) *publicationHarness {
	t.Helper()
	c, items, request := recoveryFixture(1)
	c.MissionInventory = c.MissionInventory[len(c.MissionInventory)-1:]
	c.MissionInventory[0].StackCount = quantity
	rt, clock := newTestRuntime(c, items)
	deps := rt.deps.(*enterworld.Deps)
	deps.Roster = &enterworld.Roster{}
	deps.ResolveDivisionID = enterworld.DevResolveDivisionIDFromCatalog(deps.Characters)
	var door sync.RWMutex
	deps.ReadCharacter = func(_ string, read func()) { door.RLock(); defer door.RUnlock(); read() }
	deps.UpdateCharacter = func(_ *enterworld.Character, _ string, update func() bool) bool {
		door.Lock()
		defer door.Unlock()
		return update()
	}
	deps.MutateCharacter = func(_ *enterworld.Character, _ string, mutate func()) {
		door.Lock()
		defer door.Unlock()
		mutate()
	}
	if refill {
		row := c.MissionInventory[0]
		deps.StarterRefills = []enterworld.StarterRefill{{
			Grades: []enterworld.WireItem{{RefObjID: row.RefObjID, Codename: row.Codename, TypeFlags: row.TypeFlags}},
			Stack:  []int64{50},
		}}
	}
	srv, err := transport.NewServer(transport.Config{CertDir: t.TempDir(), GracePeriod: time.Minute, KeepaliveInterval: time.Hour, IdleTimeout: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), publicationTimeout)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	srv.Hub.SetHelloAuth(func([]byte) (transport.AdmissionIdentity, error) {
		return transport.AdmissionIdentity{AccountID: "publication-test", ShardID: testDivision}, nil
	})
	auth := entryauth.NewAuthenticatedSessionFixture(t, srv.Hub)
	rt.CaptureCharacterFrames = func(division, name string) func([]wire.Frame) {
		type recipient struct {
			session  *transport.Session
			revision uint64
		}
		var recipients []recipient
		for _, session := range srv.Hub.CharacterSessions(division, name) {
			revision, valid := session.SceneReceiptRevision()
			if valid {
				recipients = append(recipients, recipient{session: session, revision: revision})
			}
		}
		if len(recipients) == 0 {
			return nil
		}
		return func(frames []wire.Frame) {
			batch := make([]transport.Frame, len(frames))
			for i, frame := range frames {
				batch[i] = transport.Frame{Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: transport.ScopeChanges(frame.Scope)}
			}
			for _, recipient := range recipients {
				// Overflow tests deliberately exercise synchronous close hooks.
				_ = recipient.session.SendSceneReceiptBatch(recipient.revision, batch)
			}
		}
	}
	enterworld.RegisterEnterWorld(srv.Hub, deps)
	enterworld.RegisterGameReady(srv.Hub, deps)
	srv.Hub.Handle(publicationFence, func(s *transport.Session, opcode uint16, payload []byte) {
		if err := s.Send(opcode, payload); err != nil {
			panic(err)
		}
	})
	return &publicationHarness{rt: rt, clock: clock, deps: deps, character: c, server: srv, auth: auth, request: request}
}

/*
================
connect
================
*/
func (h *publicationHarness) connect(t *testing.T, resume []byte) (*publicationConn, transport.Welcome) {
	t.Helper()
	c := newPublicationConn()
	c.in <- transport.Frame{Opcode: transport.OpHello, Payload: transport.EncodeHello(transport.Hello{ResumeToken: resume, AdmissionToken: []byte("publication-test")})}
	go h.server.Hub.AcceptConn(c)
	f := publicationThrough(t, c, transport.OpWelcome)
	welcome, err := transport.DecodeWelcome(f[len(f)-1].Payload)
	if err != nil {
		t.Fatal(err)
	}
	return c, welcome
}

/*
================
enter
================
*/
func (h *publicationHarness) enter(t *testing.T, c *publicationConn) []transport.Frame {
	t.Helper()
	c.in <- transport.Frame{Opcode: transport.OpEnterWorld, Payload: transport.EncodeEnterWorld(h.auth.Entry(testDivision, h.character.Name))}
	c.in <- transport.Frame{Opcode: publicationFence}
	return publicationThrough(t, c, publicationFence)
}

/*
================
ready

Exercise the real game-ready transition; TryMarkWorldReady alone leaves the
scene-loading fence set and does not admit scene-scoped receipts.
================
*/
func (h *publicationHarness) ready(t *testing.T, c *publicationConn) []transport.Frame {
	t.Helper()
	c.in <- transport.Frame{Opcode: enterworld.OpcodeGameReady}
	c.in <- transport.Frame{Opcode: publicationFence}
	return publicationThrough(t, c, publicationFence)
}

/*
================
publicationBootstrapQuantity

Read the actual browser inventory body, not a fresh authority snapshot.
================
*/
func publicationBootstrapQuantity(t *testing.T, frames []transport.Frame) uint16 {
	t.Helper()
	for _, f := range frames {
		if f.Opcode != transport.OpEnterWorldResult {
			continue
		}
		result, err := transport.DecodeEnterWorldResult(f.Payload)
		if err != nil || !result.OK {
			t.Fatalf("entry failed: %+v %v", result, err)
		}
		var blob struct {
			Bootstrap struct {
				EquipItems []enterworld.EquipItemRow `json:"equipItems"`
			} `json:"bootstrap"`
		}
		if err := json.Unmarshal(result.Blob, &blob); err != nil {
			t.Fatal(err)
		}
		for _, row := range blob.Bootstrap.EquipItems {
			if row.Slot == publicationSlot {
				if len(row.Body) != 6 {
					t.Fatalf("unexpected potion body: %v", row.Body)
				}
				return uint16(row.Body[4]) | uint16(row.Body[5])<<8
			}
		}
	}
	t.Fatal("bootstrap omitted potion")
	return 0
}

/*
================
publicationReceipt
================
*/
func publicationReceipt(t *testing.T, frames []transport.Frame, remaining uint16) uint16 {
	t.Helper()
	for _, f := range frames {
		if f.Opcode == wire.OpItemUseResponse {
			want := []byte{1, publicationSlot, byte(remaining), byte(remaining >> 8), 0xec, 8}
			if !bytes.Equal(f.Payload, want) {
				t.Fatalf("receipt = %x, want %x", f.Payload, want)
			}
			return binary.LittleEndian.Uint16(f.Payload[2:4])
		}
	}
	t.Fatal("missing item use receipt")
	return 0
}

/*
================
TestItemUseResumePublishesCommittedReceiptBeforeBootstrap

Pause the real authority update after commit, outside its store lock. The
production adapter still owns its action lock, which EnterWorld does not
share. Only transport dispatch ordering prevents a snapshot overtaking it.
================
*/
func TestItemUseResumePublishesCommittedReceiptBeforeBootstrap(t *testing.T) {
	for _, refill := range []bool{false, true} {
		t.Run(fmt.Sprintf("refill=%t", refill), func(t *testing.T) {
			h := newPublicationHarness(t, refill, 50)
			h.rt.Register(h.server.Hub)
			committed, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			var armed atomic.Bool
			update := h.deps.UpdateCharacter
			h.deps.UpdateCharacter = func(c *enterworld.Character, division string, op func() bool) bool {
				changed := update(c, division, op)
				if armed.Swap(false) {
					close(committed)
					<-release
				}
				return changed
			}
			old, first := h.connect(t, nil)
			if got := publicationBootstrapQuantity(t, h.enter(t, old)); got != 50 {
				t.Fatalf("initial = %d", got)
			}
			armed.Store(true)
			old.in <- transport.Frame{Opcode: wire.OpItemUseRequest, Payload: h.request}
			select {
			case <-committed:
			case <-time.After(publicationTimeout):
				t.Fatal("use did not commit")
			}
			current, resumed := h.connect(t, first.ResumeToken)
			if !resumed.Resumed || resumed.SessionID != first.SessionID {
				t.Fatal("did not resume the same session")
			}
			// A control ping ahead of entry proves the replacement reader is
			// running while the previous handler remains paused.
			current.in <- transport.Frame{Opcode: transport.OpPing, Payload: []byte{7}}
			publicationThrough(t, current, transport.OpPong)
			current.in <- transport.Frame{Opcode: transport.OpEnterWorld, Payload: transport.EncodeEnterWorld(h.auth.Entry(testDivision, h.character.Name))}
			current.in <- transport.Frame{Opcode: publicationFence}
			wait.Consistently(t, 50*time.Millisecond, "resume must wait for committed receipt", func() bool {
				return len(current.out) == 0
			})
			unblock()
			frames := publicationThrough(t, current, publicationFence)
			publicationReceipt(t, frames, 49)
			receiptSeen := false
			for _, f := range frames {
				if f.Opcode == wire.OpItemUseResponse {
					receiptSeen = true
				}
				if f.Opcode == transport.OpEnterWorldResult && !receiptSeen {
					t.Fatal("bootstrap overtook the committed receipt")
				}
			}
			local := publicationBootstrapQuantity(t, frames)
			want := uint16(49)
			if refill {
				want = 50
			}
			if local != want {
				t.Fatalf("bootstrap = %d, want %d", local, want)
			}
			h.clock.Advance(time.Minute)
			current.in <- transport.Frame{Opcode: wire.OpItemUseRequest, Payload: h.request}
			current.in <- transport.Frame{Opcode: publicationFence}
			used := publicationReceipt(t, publicationThrough(t, current, publicationFence), local-1)
			if h.character.MissionInventory[0].StackCount != int64(used) {
				t.Fatal("authority and receipt disagreed")
			}
			t.Logf("ordered: delayed B5BD 49, bootstrap %d, next B5BD %d", local, used)
		})
	}
}

/*
================
TestItemUsePublishedBeforeResumeRemainsConsistent

Control: deliver the committed receipt before replacing the snapshot. Both
refill settings then admit the next decrement without a stale quantity.
================
*/
func TestItemUsePublishedBeforeResumeRemainsConsistent(t *testing.T) {
	for _, refill := range []bool{false, true} {
		t.Run(fmt.Sprintf("refill=%t", refill), func(t *testing.T) {
			h := newPublicationHarness(t, refill, 50)
			h.rt.Register(h.server.Hub)
			old, first := h.connect(t, nil)
			local := publicationBootstrapQuantity(t, h.enter(t, old))
			old.in <- transport.Frame{Opcode: wire.OpItemUseRequest, Payload: h.request}
			old.in <- transport.Frame{Opcode: publicationFence}
			used := publicationReceipt(t, publicationThrough(t, old, publicationFence), 49)
			if used != local-1 {
				t.Fatal("initial decrement disagreed")
			}
			current, resumed := h.connect(t, first.ResumeToken)
			if !resumed.Resumed || resumed.SessionID != first.SessionID {
				t.Fatal("did not resume the same session")
			}
			local = publicationBootstrapQuantity(t, h.enter(t, current))
			expected := uint16(49)
			if refill {
				expected = 50
			}
			if local != expected {
				t.Fatalf("bootstrap = %d, want %d", local, expected)
			}
			h.clock.Advance(time.Minute)
			current.in <- transport.Frame{Opcode: wire.OpItemUseRequest, Payload: h.request}
			current.in <- transport.Frame{Opcode: publicationFence}
			used = publicationReceipt(t, publicationThrough(t, current, publicationFence), local-1)
			if h.character.MissionInventory[0].StackCount != int64(used) {
				t.Fatal("authority and receipt disagreed")
			}
			t.Logf("ordered control: bootstrap %d, next B5BD %d; consumer agrees", local, used)
		})
	}
}
