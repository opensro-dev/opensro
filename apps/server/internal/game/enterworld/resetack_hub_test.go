/*
===========================================================================

resetack_hub_test.go - the 0x36DD reset acknowledgement, taken silently

===========================================================================
*/
package enterworld_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

/*
================
TestResetClientAckIsTakenSilently

Drives the client's reset acknowledgement (0x36DD, the empty answer to
the bootstrap's 0x3369) through the REAL registration path at the moment
production sends it: after the enter-world finalize, before game-ready.
The native server ignores the ack, so the pin is threefold: nothing is
written back, the frame never reaches the unhandled-opcode counter
(#2147), and the lifecycle carries on to game-ready undisturbed. This
test FAILS if enterworld.Register stops wiring the 0x36DD handler.
================
*/
func TestResetClientAckIsTakenSilently(t *testing.T) {
	t.Parallel()
	character := &enterworld.Character{ID: 11, Name: "ResetAck", ModelCodename: "CHAR_CH_MAN_ADVENTURER"}
	source := enterworld.StaticCharacterSource{enterworld.DefaultDivisionID: {character}}
	bound := make(chan struct{}, 1)
	deps := &enterworld.Deps{
		Roster:            &enterworld.Roster{},
		Characters:        source,
		Items:             enterworld.NewTextdataItems(filepath.Join(t.TempDir(), "missing-textdata")),
		ResolveDivisionID: enterworld.DevResolveDivisionIDFromCatalog(source),
		PlayerBaseStats: func(*enterworld.Character) (wire.BaseStats, error) {
			return wire.BaseStats{}, nil
		},
		OnWorldBound: func(*transport.Session, string, *enterworld.Character) {
			bound <- struct{}{}
		},
	}
	srv, err := transport.NewServer(transport.Config{CertDir: t.TempDir(), GracePeriod: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
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
	barrier := make(chan struct{}, 1)
	srv.Hub.Handle(0x7fff, func(*transport.Session, uint16, []byte) { barrier <- struct{}{} })
	conn := newFakeConn()
	go srv.Hub.AcceptConn(conn)
	conn.inbound <- transport.Frame{Opcode: transport.OpHello, Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte("test-admission")})}
	waitFor(t, "WELCOME", func() bool {
		for _, frame := range conn.frames() {
			if frame.Opcode == transport.OpWelcome {
				return true
			}
		}
		return false
	})
	conn.inbound <- transport.Frame{Opcode: transport.OpEnterWorld, Payload: transport.EncodeEnterWorld(
		authenticatedSession.Entry("0", character.Name),
	)}
	waitFor(t, "enter-world finalize", func() bool {
		for _, frame := range conn.frames() {
			if frame.Opcode == enterworld.OpcodeObjectListFinalize {
				return true
			}
		}
		return false
	})

	// The ack, then a barrier frame: dispatch is serial per session, so
	// the barrier firing proves the ack was dispatched before it.
	before := len(conn.frames())
	conn.inbound <- transport.Frame{Opcode: enterworld.OpcodeResetClientAck}
	conn.inbound <- transport.Frame{Opcode: 0x7fff}
	select {
	case <-barrier:
	case <-time.After(3 * time.Second):
		t.Fatal("barrier frame was not dispatched after the reset ack")
	}
	waitFrameQuiescence(t, "reset ack silence", conn)
	if got := len(conn.frames()); got != before {
		t.Fatalf("reset ack produced %d frame(s), want silence: %v", got-before, conn.frames()[before:])
	}
	if got := srv.Hub.Metrics().UnhandledOpcodes["0x36DD"]; got != 0 {
		t.Fatalf("unhandled 0x36DD count = %d, want 0: the ack must be taken, not counted", got)
	}

	// The lifecycle is undisturbed: game-ready still binds the session.
	conn.inbound <- transport.Frame{Opcode: enterworld.OpcodeGameReady}
	select {
	case <-bound:
	case <-time.After(3 * time.Second):
		t.Fatal("WorldBound did not run after the reset ack and game-ready")
	}
}
