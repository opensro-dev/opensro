/*
===========================================================================

gatefree_encode_test.go - the entry encode runs with the gate released

Building the entry snapshot is what the publication gate orders
against tick commits; encoding it reads only the detached result.
The handler releases the gate between the two (#2294), so an entry's
encode no longer keeps a login wave - and the tick behind it -
queued on the gate.

===========================================================================
*/
package enterworld_test

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

/*
================
TestEnterWorldReleasesTheGateBeforeEncoding

While the encoder runs, the division publication gate must be free:
the snapshot is already built, and nothing the encode does needs the
gate. The fake encoder tries the gate itself, so the answer needs no
timing: a handler that encodes under the gate fails its TryLock.
================
*/
func TestEnterWorldReleasesTheGateBeforeEncoding(t *testing.T) {
	srv, err := transport.NewServer(transport.Config{CertDir: t.TempDir(), GracePeriod: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()
	srv.Hub.SetHelloAuth(func([]byte) (transport.AdmissionIdentity, error) {
		return transport.AdmissionIdentity{AccountID: "test-account", ShardID: "global-official"}, nil
	})
	authenticated := entryauth.NewAuthenticatedSessionFixture(t, srv.Hub)
	c := &enterworld.Character{ID: 3, Name: "asd2", ModelCodename: "CHAR_CH_MAN_ADVENTURER"}
	source := enterworld.StaticCharacterSource{enterworld.DefaultDivisionID: {c}}
	var gateMu sync.Mutex
	var encoded, gateFree atomic.Bool
	deps := &enterworld.Deps{
		Roster: &enterworld.Roster{}, Characters: source,
		Items:             enterworld.NewTextdataItems(filepath.Join(t.TempDir(), "absent")),
		ResolveDivisionID: enterworld.DevResolveDivisionIDFromCatalog(source),
		LockPublication: func(string, string) func() {
			gateMu.Lock()
			return gateMu.Unlock
		},
		EncodeEntryPayload: func(*enterworld.BootstrapResult) ([]byte, error) {
			if gateMu.TryLock() {
				gateFree.Store(true)
				gateMu.Unlock()
			}
			encoded.Store(true)
			return []byte("entry-payload"), nil
		},
	}
	enterworld.RegisterEnterWorld(srv.Hub, deps)
	conn := newFakeConn()
	go srv.Hub.AcceptConn(conn)
	conn.inbound <- transport.Frame{Opcode: transport.OpHello, Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte("test")})}
	wait.Eventually(t, 10*time.Second, "welcome", func() bool {
		for _, f := range conn.frames() {
			if f.Opcode == transport.OpWelcome {
				return true
			}
		}
		return false
	})
	conn.inbound <- transport.Frame{Opcode: transport.OpEnterWorld, Payload: transport.EncodeEnterWorld(authenticated.Entry("0", c.Name))}
	wait.Eventually(t, 10*time.Second, "the enter-world answer", func() bool {
		for _, f := range conn.frames() {
			if f.Opcode == transport.OpEnterWorldResult {
				return true
			}
		}
		return false
	})
	if !encoded.Load() {
		t.Fatal("the entry never reached its encoder")
	}
	if !gateFree.Load() {
		t.Fatal("the publication gate was still held while the entry encoded")
	}
}
