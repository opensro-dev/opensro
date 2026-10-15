package bot_test

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

// botConn drives the real Hub handshake and dispatch without a socket. It is
// the in-memory transport.Conn of the phase-0 test bot (option B stays in the
// Go test only; the production form is option A, an external service).
type botConn struct {
	inbound chan transport.Frame
	closed  chan struct{}
	once    sync.Once

	mu      sync.Mutex
	written []transport.Frame
}

func newBotConn() *botConn {
	return &botConn{inbound: make(chan transport.Frame, 16), closed: make(chan struct{})}
}

func (c *botConn) ReadFrame(ctx context.Context) (transport.Frame, error) {
	select {
	case f := <-c.inbound:
		return f, nil
	case <-ctx.Done():
		return transport.Frame{}, ctx.Err()
	case <-c.closed:
		return transport.Frame{}, io.EOF
	}
}

func (c *botConn) WriteFrame(f transport.Frame) error {
	c.mu.Lock()
	c.written = append(c.written, f)
	c.mu.Unlock()
	return nil
}

func (c *botConn) SupportsUnreliable() bool { return false }
func (c *botConn) WriteUnreliable(transport.Frame) error {
	return errors.New("botConn: no datagrams")
}
func (c *botConn) Close(string) error { c.once.Do(func() { close(c.closed) }); return nil }
func (c *botConn) Kind() string       { return "websocket" }
func (c *botConn) RemoteAddr() string { return "bot:0" }

func (c *botConn) frames() []transport.Frame {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]transport.Frame, len(c.written))
	copy(out, c.written)
	return out
}

func (c *botConn) hasOpcode(opcode uint16) bool {
	for _, f := range c.frames() {
		if f.Opcode == opcode {
			return true
		}
	}
	return false
}

// TestBotAdmitsThroughTheOrdinaryPath proves that one bot enters the world by
// the ordinary admission path: HELLO -> WELCOME, EnterWorld, GameReady. The
// bot connection is an in-memory transport.Conn and the Hub uses the real
// HELLO and EnterWorld authentication hooks.
func TestBotAdmitsThroughTheOrdinaryPath(t *testing.T) {
	t.Setenv(bot.EnvBotPlane, "1")

	name := bot.CharacterName(1)
	character := &enterworld.Character{
		ID:            7,
		Name:          name,
		ModelCodename: "CHAR_CH_MAN_ADVENTURER",
	}
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
		SceneReferenceFrames: func() []wire.Frame {
			return []wire.Frame{{Opcode: 14, Payload: []byte(`{"version":1,"items":[]}`)}}
		},
	}
	srv, err := transport.NewServer(transport.Config{
		CertDir:     t.TempDir(),
		GracePeriod: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.Hub.SetHelloAuth(func([]byte) (transport.AdmissionIdentity, error) {
		return transport.AdmissionIdentity{
			AccountID: bot.AccountID(1),
			ShardID:   enterworld.DefaultDivisionID,
		}, nil
	})
	authenticated := entryauth.NewAuthenticatedSessionFixture(t, srv.Hub)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	}()
	enterworld.Register(srv.Hub, deps)

	conn := newBotConn()
	go srv.Hub.AcceptConn(conn)

	conn.inbound <- transport.Frame{
		Opcode:  transport.OpHello,
		Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte("bot-admission")}),
	}
	wait.Eventually(t, 3*time.Second, "WELCOME", func() bool {
		return conn.hasOpcode(transport.OpWelcome)
	})

	conn.inbound <- transport.Frame{
		Opcode:  transport.OpEnterWorld,
		Payload: transport.EncodeEnterWorld(authenticated.Entry("0", name)),
	}
	wait.Eventually(t, 3*time.Second, "enter-world finalize", func() bool {
		return conn.hasOpcode(enterworld.OpcodeObjectListFinalize)
	})

	conn.inbound <- transport.Frame{Opcode: enterworld.OpcodeGameReady}
	select {
	case <-bound:
	case <-time.After(3 * time.Second):
		t.Fatal("the bot did not reach the world-bound hook")
	}
	wait.Eventually(t, 3*time.Second, "game-ready vitals", func() bool {
		return conn.hasOpcode(enterworld.OpcodeVitalsUpdate)
	})

	sessions := srv.Hub.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	if !sessions[0].WorldReady() {
		t.Fatal("the bot session is not world-ready after game-ready")
	}
}

// TestBotAdmissionRefusesWhenSwitchIsOff pins the operator switch on the
// admission side: no bot function starts while SRO_BOT_PLANE is off.
func TestBotAdmissionRefusesWhenSwitchIsOff(t *testing.T) {
	t.Setenv(bot.EnvBotPlane, "")
	if err := bot.RequireEnabled(); !errors.Is(err, bot.ErrDisabled) {
		t.Fatalf("RequireEnabled off = %v, want ErrDisabled", err)
	}
}
