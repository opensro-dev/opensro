/*
===========================================================================

account_overlap_test.go - one account, one character in the world (#3137)

The gateway refuses a second login for an already-connected account
(UIO_MSG_ERROR_OVERLAP). The port's session cap deliberately lets one
account hold several transport sessions (browser tabs, resume grace),
so the rule bites at world entry: a second session of the account
entering a different character is answered with the overlap refusal,
while the same character stays replaceable and the seat frees when
the holder leaves.

===========================================================================
*/
package enterworld_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

/*
================
TestSecondCharacterOfOneAccountCannotEnterTheWorld
================
*/
func TestSecondCharacterOfOneAccountCannotEnterTheWorld(t *testing.T) {
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
		return transport.AdmissionIdentity{AccountID: "overlap-account", ShardID: "global-official"}, nil
	})
	fixture := entryauth.NewAuthenticatedSessionFixture(t, srv.Hub)
	alpha := &enterworld.Character{ID: 3, Name: "alpha", ModelCodename: "CHAR_CH_MAN_ADVENTURER"}
	beta := &enterworld.Character{ID: 4, Name: "beta", ModelCodename: "CHAR_CH_MAN_ADVENTURER"}
	source := enterworld.StaticCharacterSource{enterworld.DefaultDivisionID: {alpha, beta}}
	deps := &enterworld.Deps{
		Roster: &enterworld.Roster{}, Characters: source,
		Items:             enterworld.NewTextdataItems(filepath.Join(t.TempDir(), "absent")),
		ResolveDivisionID: enterworld.DevResolveDivisionIDFromCatalog(source),
		EntrySkills:       func(string, string) []enterworld.EntrySkill { return []enterworld.EntrySkill{{ID: 1, Status: 2}} },
	}
	enterworld.RegisterEnterWorld(srv.Hub, deps)

	connect := func() *fakeConn {
		conn := newFakeConn()
		go srv.Hub.AcceptConn(conn)
		conn.inbound <- transport.Frame{Opcode: transport.OpHello, Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte("test")})}
		waitFor(t, "welcome", func() bool {
			for _, f := range conn.frames() {
				if f.Opcode == transport.OpWelcome {
					return true
				}
			}
			return false
		})
		return conn
	}
	enter := func(conn *fakeConn, name string) transport.EnterWorldResult {
		t.Helper()
		before := len(conn.frames())
		conn.inbound <- transport.Frame{Opcode: transport.OpEnterWorld, Payload: transport.EncodeEnterWorld(fixture.Entry("0", name))}
		var result transport.EnterWorldResult
		waitFor(t, "enter "+name, func() bool {
			for _, f := range conn.frames()[before:] {
				if f.Opcode != transport.OpEnterWorldResult {
					continue
				}
				decoded, err := transport.DecodeEnterWorldResult(f.Payload)
				if err != nil {
					t.Fatal("enter world result did not decode", err)
				}
				result = decoded
				return true
			}
			return false
		})
		return result
	}

	first := connect()
	if result := enter(first, "alpha"); !result.OK {
		t.Fatalf("first character refused entry: %+v", result)
	}

	second := connect()
	if result := enter(second, "beta"); result.OK || result.NativeErrorCode != 0x11 {
		t.Fatalf("second character of the account entered the world: %+v, want the overlap refusal", result)
	}
	if result := enter(second, "alpha"); !result.OK {
		t.Fatalf("the same character no longer replaces its session: %+v", result)
	}

	third := connect()
	if result := enter(third, "beta"); result.OK || result.NativeErrorCode != 0x11 {
		t.Fatalf("third session entered a second character while the seat moved: %+v", result)
	}
	// The holder leaves: the seat frees and another character may enter.
	if err := second.Close("entry finished"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "holder retired", func() bool { return len(srv.Hub.Sessions()) == 2 })
	if result := enter(third, "beta"); !result.OK {
		t.Fatalf("another character still refused after the holder left: %+v", result)
	}
}
