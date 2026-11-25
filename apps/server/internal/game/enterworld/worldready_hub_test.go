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

// The visibility provider must not exist while EnterWorld is still being
// consumed. A 0x30D7 sent in that window is a valid reliable frame delivered
// to a client whose CICUser/scene admission is not live yet; the ticker then
// latches it as shown and never retries it.
func TestWorldBoundWaitsForGameReady(t *testing.T) {
	t.Parallel()
	character := &enterworld.Character{ID: 7, Name: "PeerReady", ModelCodename: "CHAR_CH_MAN_ADVENTURER"}
	source := enterworld.StaticCharacterSource{enterworld.DefaultDivisionID: {character}}
	bound := make(chan struct{}, 2)
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
		SceneReferenceFrames: func() []wire.Frame {
			return []wire.Frame{{Opcode: 14, Payload: []byte(`{"version":1,"items":[]}`)}}
		},
		// The seed travels under its digest (#4421): once per session.
		SeedDigest: "test-seed-digest",
		// The region weather rides every finished scene too (action/weather.go).
		SceneWeatherFrames: func(_ string, c *enterworld.Character) []wire.Frame {
			if c == nil || c.Name != character.Name {
				return nil
			}
			return []wire.Frame{{Opcode: 0x3bde, Payload: []byte{2, 77}}}
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
	select {
	case <-bound:
		t.Fatal("WorldBound ran before the client announced game-ready")
	default:
	}

	conn.inbound <- transport.Frame{Opcode: enterworld.OpcodeGameReady}
	select {
	case <-bound:
	case <-time.After(3 * time.Second):
		t.Fatal("WorldBound did not run after game-ready")
	}
	waitFor(t, "game-ready vitals", func() bool {
		for _, frame := range conn.frames() {
			if frame.Opcode == enterworld.OpcodeVitalsUpdate {
				return true
			}
		}
		return false
	})
	session := srv.Hub.Sessions()[0]
	seedCount := func() int {
		n := 0
		for _, f := range conn.frames() {
			if f.Opcode == 14 {
				n++
			}
		}
		return n
	}
	weatherCount := func() int {
		n := 0
		for _, f := range conn.frames() {
			if f.Opcode == 0x3bde && len(f.Payload) == 2 && f.Payload[0] == 2 && f.Payload[1] == 77 {
				n++
			}
		}
		return n
	}
	waitFor(t, "initial scene references", func() bool { return seedCount() == 1 })
	waitFor(t, "initial scene weather", func() bool { return weatherCount() == 1 })
	if err := session.SendSceneReset([]transport.Frame{{Opcode: enterworld.OpcodeResetClient, Payload: []byte{1, 2}}}); err != nil {
		t.Fatal(err)
	}
	if _, active := session.SceneRevision(); active {
		t.Fatal("reentry visible before ready")
	}
	conn.inbound <- transport.Frame{Opcode: enterworld.OpcodeGameReady}
	waitFor(t, "reentry ready", func() bool { _, active := session.SceneRevision(); return active })
	// The commerce seed is a session constant (#4421): a reentry does
	// not republish it; the weather snapshot still travels per entry.
	if n := seedCount(); n != 1 {
		t.Fatalf("reentry republished the commerce seed: %d publishes, want 1", n)
	}
	waitFor(t, "reentry scene weather", func() bool { return weatherCount() == 2 })
	select {
	case <-bound:
		t.Fatal("scene reentry repeated session lifetime hooks")
	default:
	}
	// A second travel rebuild keeps the same session dictionary (#4421);
	// session hooks remain one-shot and duplicate readiness cannot
	// duplicate publication.
	if err := session.SendSceneReset([]transport.Frame{{Opcode: enterworld.OpcodeResetClient, Payload: []byte{1, 2}}}); err != nil {
		t.Fatal(err)
	}
	conn.inbound <- transport.Frame{Opcode: enterworld.OpcodeGameReady}
	waitFor(t, "second reentry weather", func() bool { return weatherCount() == 3 })
	if n := seedCount(); n != 1 {
		t.Fatalf("second reentry republished the commerce seed: %d publishes, want 1", n)
	}
	conn.inbound <- transport.Frame{Opcode: enterworld.OpcodeGameReady}
	conn.inbound <- transport.Frame{Opcode: 0x7fff}
	select {
	case <-barrier:
	case <-time.After(time.Second):
		t.Fatal("duplicate-ready barrier")
	}
	if seedCount() != 1 || weatherCount() != 3 {
		t.Fatal("duplicate readiness replayed scene references or weather")
	}
	select {
	case <-bound:
		t.Fatal("travel repeated session hooks")
	default:
	}

	// A transport resume can replace bootstrap while a prior scene is loading.
	// BindCharacter clears the provider, so this admission must run WorldBound.
	session.BindCharacter(enterworld.DefaultDivisionID, character.Name, 0)
	if err := session.SendSceneReset([]transport.Frame{{Opcode: enterworld.OpcodeResetClient, Payload: []byte{1, 2}}}); err != nil {
		t.Fatal(err)
	}
	// The replacement entry states no constants digest, so its answer is
	// full and the commerce seed publishes again at game-ready (#4421).
	conn.inbound <- transport.Frame{Opcode: transport.OpEnterWorld, Payload: transport.EncodeEnterWorld(
		authenticatedSession.Entry("0", character.Name),
	)}
	waitFor(t, "replacement enter-world finalize", func() bool {
		for _, frame := range conn.frames() {
			if frame.Opcode == enterworld.OpcodeObjectListFinalize {
				return true
			}
		}
		return false
	})
	conn.inbound <- transport.Frame{Opcode: enterworld.OpcodeGameReady}
	select {
	case <-bound:
	case <-time.After(time.Second):
		t.Fatal("replacement admission did not reinstall world provider")
	}
	waitFor(t, "replacement scene references", func() bool { return seedCount() == 2 })
	waitFor(t, "replacement scene weather", func() bool { return weatherCount() == 4 })

}
