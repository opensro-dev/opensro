package quest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"opensro.online/server/internal/testsupport/licensed"
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

// In-memory connection, real hub/session/handler/runtime. The test binds the
// character server-side at the seam normally owned by EnterWorld.
type questTestConn struct {
	in, out chan transport.Frame
	done    chan struct{}
	once    sync.Once
}

func (c *questTestConn) ReadFrame(ctx context.Context) (transport.Frame, error) {
	select {
	case f := <-c.in:
		return f, nil
	case <-ctx.Done():
		return transport.Frame{}, ctx.Err()
	case <-c.done:
		return transport.Frame{}, io.EOF
	}
}
func (c *questTestConn) WriteFrame(f transport.Frame) error {
	select {
	case c.out <- f:
		return nil
	case <-c.done:
		return io.EOF
	}
}
func (c *questTestConn) SupportsUnreliable() bool                { return false }
func (c *questTestConn) WriteUnreliable(f transport.Frame) error { return c.WriteFrame(f) }
func (c *questTestConn) Close(string) error                      { c.once.Do(func() { close(c.done) }); return nil }
func (c *questTestConn) Kind() string                            { return "websocket" }
func (c *questTestConn) RemoteAddr() string                      { return "quest-test" }
func (c *questTestConn) read(t *testing.T) transport.Frame {
	t.Helper()
	select {
	case f := <-c.out:
		return f
	case <-time.After(3 * time.Second):
		t.Fatal("quest reply missing")
		return transport.Frame{}
	}
}

func TestQuestRefusalsThroughRegisteredHub(t *testing.T) {
	licensed.RequireGameData(t)
	rt := testRuntime(t)
	character := questCharacter()
	if _, err := rt.StartQuest(character, "QTUTORIAL_CH"); err != nil {
		t.Fatal(err)
	}
	rt.deps.(*enterworld.Deps).Characters = enterworld.StaticCharacterSource{"quest-test": {character}}
	srv, err := transport.NewServer(transport.Config{CertDir: t.TempDir(), KeepaliveInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	})
	srv.Hub.SetHelloAuth(func([]byte) (transport.AdmissionIdentity, error) {
		return transport.AdmissionIdentity{AccountID: "quest-test", ShardID: "global-official"}, nil
	})
	entryauth.NewAuthenticatedSessionFixture(t, srv.Hub)
	Register(srv.Hub, rt)
	connect := func() *questTestConn {
		c := &questTestConn{in: make(chan transport.Frame, 16), out: make(chan transport.Frame, 16), done: make(chan struct{})}
		t.Cleanup(func() { c.Close("test complete") })
		go srv.Hub.AcceptConn(c)
		c.in <- transport.Frame{Opcode: transport.OpHello, Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte("quest-test")})}
		f := c.read(t)
		if f.Opcode != transport.OpWelcome {
			t.Fatalf("expected welcome, got %04x", f.Opcode)
		}
		welcome, err := transport.DecodeWelcome(f.Payload)
		if err != nil {
			t.Fatal(err)
		}
		s, ok := srv.Hub.Session(welcome.SessionID)
		if !ok {
			t.Fatal("session missing")
		}
		s.BindCharacter("quest-test", character.Name, 0)
		return c
	}
	requester, peer := connect(), connect()
	before, err := json.Marshal(character)
	if err != nil {
		t.Fatal(err)
	}
	// Repeated refusals must remain answerable, must never pay/remove anything,
	// and must not leak the private reply to other sessions in the division.
	for cycle := 0; cycle < 2; cycle++ {
		for _, tc := range []struct {
			op, ack uint16
			payload []byte
		}{
			{OpQuestGiveUpRequest, 0xB1EB, []byte{2}},         // malformed request
			{OpQuestGiveUpRequest, 0xB1EB, u32le(0xffffffff)}, // unknown definition
			{OpQuestGiveUpRequest, 0xB1EB, u32le(29)},         // reward-only kind
			{OpQuestRewardRequest, 0xB29A, []byte{2}},
			{OpQuestRewardRequest, 0xB29A, u32le(2)},  // wrong kind
			{OpQuestRewardRequest, 0xB29A, u32le(29)}, // not active
		} {
			requester.in <- transport.Frame{Opcode: tc.op, Payload: tc.payload}
			f := requester.read(t)
			if f.Opcode != tc.ack || !bytes.Equal(f.Payload, []byte{2, 0}) {
				t.Fatalf("refusal = %04x %x", f.Opcode, f.Payload)
			}
			after, err := json.Marshal(character)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("refusal mutated character")
			}
		}
	}
	if _, err := rt.StartQuest(character, "QSP_ALL_POTION_1"); err != nil {
		t.Fatal(err)
	}
	before, err = json.Marshal(character)
	if err != nil {
		t.Fatal(err)
	}
	requester.in <- transport.Frame{Opcode: OpQuestRewardRequest, Payload: u32le(29)}
	if f := requester.read(t); f.Opcode != 0xB29A || !bytes.Equal(f.Payload, []byte{2, 0}) {
		t.Fatalf("incomplete objective refusal = %04x %x", f.Opcode, f.Payload)
	}
	after, err := json.Marshal(character)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("incomplete objective refusal mutated character")
	}
	requester.in <- transport.Frame{Opcode: OpQuestGiveUpRequest, Payload: u32le(2)}
	f := requester.read(t)
	if f.Opcode != 0x31ED || !bytes.Equal(f.Payload, []byte{4, 2, 0, 0, 0}) {
		t.Fatalf("success = %04x %x", f.Opcode, f.Payload)
	}
	if len(character.ActiveQuests) != 1 || character.ActiveQuests[0].RefID != 29 || len(character.CompletedQuestIds) != 0 {
		t.Fatal("abandon did not preserve completion semantics")
	}
	// An ordered barrier on the acting session proves the previous handler has
	// finished fan-out; checking the peer before that would have a race.
	const barrier = 0x7FFE
	srv.Hub.Handle(barrier, func(s *transport.Session, _ uint16, _ []byte) { _ = s.Send(barrier, nil) })
	requester.in <- transport.Frame{Opcode: barrier}
	if requester.read(t).Opcode != barrier {
		t.Fatal("unexpected extra reply")
	}
	peer.in <- transport.Frame{Opcode: barrier}
	if f := peer.read(t); f.Opcode != barrier {
		t.Fatalf("private quest reply reached peer: %04x", f.Opcode)
	}
}
