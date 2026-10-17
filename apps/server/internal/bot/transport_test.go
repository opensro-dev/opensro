package bot_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

// TestAdmitEntersTheWorldOverWebSocket proves the phase-1 client: a real
// WebSocket connection, the ordinary HELLO and EnterWorld authentication
// hooks, and the GameReady edge to world-ready.
func TestAdmitEntersTheWorldOverWebSocket(t *testing.T) {
	name := bot.CharacterName(1)
	character := &enterworld.Character{ID: 7, Name: name, ModelCodename: "CHAR_CH_MAN_ADVENTURER"}
	source := enterworld.StaticCharacterSource{enterworld.DefaultDivisionID: {character}}
	bound := make(chan struct{}, 1)
	deps := &enterworld.Deps{
		Roster:            &enterworld.Roster{},
		Characters:        source,
		Items:             enterworld.NewTextdataItems(filepath.Join(t.TempDir(), "missing-textdata")),
		ResolveDivisionID: enterworld.DevResolveDivisionIDFromCatalog(source),
		PlayerBaseStats:   func(*enterworld.Character) (wire.BaseStats, error) { return wire.BaseStats{}, nil },
		OnWorldBound:      func(*transport.Session, string, *enterworld.Character) { bound <- struct{}{} },
		SceneReferenceFrames: func() []wire.Frame {
			return []wire.Frame{{Opcode: 14, Payload: []byte(`{"version":1,"items":[]}`)}}
		},
	}
	srv, err := transport.NewServer(transport.Config{
		WTAddr:      "127.0.0.1:0",
		WSAddr:      "127.0.0.1:0",
		CertDir:     t.TempDir(),
		GracePeriod: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	}()
	srv.Hub.SetHelloAuth(func(token []byte) (transport.AdmissionIdentity, error) {
		if string(token) != "admission-1" {
			return transport.AdmissionIdentity{}, fmt.Errorf("unexpected admission ticket")
		}
		return transport.AdmissionIdentity{AccountID: bot.AccountID(1), ShardID: enterworld.DefaultDivisionID}, nil
	})
	entry := entryauth.NewAuthenticatedEntryFixture(t, "0", name)
	_ = entryauth.NewAuthenticatedSessionFixture(t, srv.Hub)
	enterworld.Register(srv.Hub, deps)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	url := fmt.Sprintf("ws://%s%s", srv.WSAddr(), transport.PathWS)
	socket, err := bot.DialWebSocket(ctx, url, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()

	welcome, _, _, _, _, err := bot.Admit(ctx, socket, "admission-1", string(entry.AuthToken), "0", name, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if welcome.Resumed {
		t.Fatal("a fresh bot session was marked resumed")
	}
	select {
	case <-bound:
	case <-time.After(3 * time.Second):
		t.Fatal("the bot did not reach the world-bound hook")
	}
	sessions := srv.Hub.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	if !sessions[0].WorldReady() {
		t.Fatal("the bot session is not world-ready after game-ready")
	}
}
