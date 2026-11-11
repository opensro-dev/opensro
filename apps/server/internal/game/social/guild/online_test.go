package guild_test

// The #3208 presence pins, over the real transport against the real
// authority store (the startGuildServer composition plus the two
// production lifecycle tails wired the way wiring_sessions.go wires
// them):
//
//	Alfa enters     -> Alfa receives her own online delta: her seed
//	                   encoded her row offline (the seed is composed
//	                   before the bind), and the self leg corrects it.
//	Berk enters     -> both members receive Berk's online delta,
//	                   naming his STORED member jid (700077, not his
//	                   character id): Alfa's seeded row flips online.
//	Berk closes     -> Alfa receives Berk's offline delta.
//	Berk re-enters, then enters again on a second connection (the
//	first session is replaced). The stale connection closes, then
//	the live one: Alfa receives exactly ONE offline delta - the
//	replaced session's close hook sends nothing.

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	wiretest "opensro.online/server/internal/game/internal"
	presence "opensro.online/server/internal/game/social"
	"opensro.online/server/internal/game/social/community"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

// startOnlineServer stands up the two-member NightWatch fixture
// (newTwoMemberGuildFixture) behind a real transport whose
// OnWorldBound and OnSessionClose hooks call the guild presence
// tails, as wiring_sessions.go does, and returns the server and both
// members' stored wire jids.
func startOnlineServer(t *testing.T) (*transport.Server, uint32, uint32) {
	t.Helper()
	_, authority, alfa, _, guildID := newTwoMemberGuildFixture(t)

	deps := &enterworld.Deps{
		Roster:     &enterworld.Roster{},
		Characters: authority.Characters(),
	}
	deps.ResolveDivisionID = enterworld.DevResolveDivisionIDFromCatalog(deps.Characters)
	deps.MutateCharacter = func(c *enterworld.Character, label string, fn func()) {
		authority.MutateCharacter(c, label, fn)
	}
	deps.Letters = authority.Letters()
	deps.Guilds = authority.Guilds()

	srv, err := transport.NewServer(transport.Config{
		WTAddr:            "127.0.0.1:0",
		WSAddr:            "127.0.0.1:0",
		CertDir:           t.TempDir(),
		HelloTimeout:      5 * time.Second,
		GracePeriod:       10 * time.Second,
		KeepaliveInterval: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		OutboundQueue:     64,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.Hub.SetHelloAuth(entryauth.UniqueAccountHelloAuth())
	entryauth.NewAuthenticatedSessionFixture(t, srv.Hub)
	directory := presence.NewDirectory(srv.Hub)
	communitySeeds := community.SeedFramesFunc(directory, deps.Letters)
	deps.CommunitySeedFramesFor = func(divisionID string, character *enterworld.Character) []enterworld.Packet {
		return guild.AppendSeedFrame(
			communitySeeds(divisionID, character),
			deps.Guilds,
			directory,
			divisionID,
			character,
		)
	}
	deps.OnWorldBound = func(s *transport.Session, divisionID string, character *enterworld.Character) {
		if old, replaced := srv.Hub.BindExclusive(presence.BindKey(divisionID, character.Name), s); replaced {
			old.ClearGameplayContext()
		}
		if s.Evicted() {
			return
		}
		guild.WorldBound(deps, directory, divisionID, character)
	}
	srv.Hub.OnSessionClose(func(s *transport.Session, _ error) {
		guild.SessionClosed(deps, directory, s)
		guild.ForgetSession(s)
	})
	deps.PublishEntry = guild.EntryPublisher(func() domain.GuildStore { return deps.Guilds })
	enterworld.Register(srv.Hub, deps)
	guild.Register(srv.Hub, deps, directory, nil, nil)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { guildShutdownServer(t, srv) })

	var alfaJID uint32
	_, members, ok := authority.Guilds().Guild(mutatorDivision, guildID)
	if !ok {
		t.Fatal("fixture guild missing")
	}
	for _, member := range members {
		if member.CharID == alfa.ID {
			alfaJID = member.JID
		}
	}
	if alfaJID == 0 {
		t.Fatal("fixture leader has no member jid")
	}
	return srv, alfaJID, mutatorBerkJID
}

// presenceDeltaOracle hand-rolls the subOp-6 &0x02 frame body:
// {u8 6}{u32 jid}{u8 0x02}{u8 offline}.
func presenceDeltaOracle(jid uint32, offline uint8) []byte {
	buf := &bytes.Buffer{}
	buf.WriteByte(6)
	binary.Write(buf, binary.LittleEndian, jid)
	buf.WriteByte(0x02)
	buf.WriteByte(offline)
	return buf.Bytes()
}

func expectPresenceDelta(t *testing.T, c *websocket.Conn, jid uint32, offline uint8, what string) {
	t.Helper()
	got := guildExpectFrame(t, c, guild.OpGuildUpdatePush, what)
	if want := presenceDeltaOracle(jid, offline); !bytes.Equal(got, want) {
		t.Fatalf("%s: 0x3B29 payload = % X, want % X", what, got, want)
	}
}

// expectPresenceSilence fails when any game frame reaches the client
// inside the window. It is only ever a test's LAST read on a
// connection: a gorilla read that times out corrupts the connection
// for every later read.
func expectPresenceSilence(t *testing.T, c *websocket.Conn, what string) {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	for {
		typ, data, err := c.ReadMessage()
		if err != nil {
			return
		}
		if typ != websocket.BinaryMessage {
			continue
		}
		f, err := transport.DecodeFrame(data)
		if err != nil {
			t.Fatalf("%s: decoding ws frame: %v", what, err)
		}
		if f.Opcode == transport.OpPing || f.Opcode == transport.OpPong {
			continue
		}
		t.Fatalf("%s: unexpected frame 0x%04X payload % X", what, f.Opcode, f.Payload)
	}
}

// guildBye sends the client's goodbye frame and closes the socket:
// the session tears down at once instead of riding the transport
// resume grace a dropped socket gets (the friend wire test's
// disconnect shape).
func guildBye(t *testing.T, c *websocket.Conn) {
	t.Helper()
	guildSendFrame(t, c, transport.OpBye, []byte{transport.ByeReasonNormal})
	c.Close()
}

// enterOnlineWorld enters one member and consumes the self delta the
// bind tail queues behind the seed; reading it is the barrier that
// the tail ran (the harness's queue drain cannot serve here - the
// self delta is itself a queued frame).
func enterOnlineWorld(t *testing.T, c *websocket.Conn, name string, selfJID uint32) {
	t.Helper()
	guildEnterWorldBootstrap(t, c, name)
	guildExpectFrame(t, c, guild.OpGuildInfo, "guild info seed "+name)
	wiretest.ActivateWorld(t, c, "enter world "+name)
	expectPresenceDelta(t, c, selfJID, 0, "the binder's own online delta")
}

func TestGuildPresenceFollowsBindAndClose(t *testing.T) {
	srv, alfaJID, berkJID := startOnlineServer(t)

	alfaConn := guildDialWS(t, srv)
	guildHelloWS(t, alfaConn)
	enterOnlineWorld(t, alfaConn, "Alfa", alfaJID)

	berkConn := guildDialWS(t, srv)
	guildHelloWS(t, berkConn)
	enterOnlineWorld(t, berkConn, "Berk", berkJID)
	expectPresenceDelta(t, alfaConn, berkJID, 0, "the sitting member sees Berk online")

	guildBye(t, berkConn)
	expectPresenceDelta(t, alfaConn, berkJID, 1, "the sitting member sees Berk offline")

	berkConn2 := guildDialWS(t, srv)
	guildHelloWS(t, berkConn2)
	enterOnlineWorld(t, berkConn2, "Berk", berkJID)
	expectPresenceDelta(t, alfaConn, berkJID, 0, "the re-entry online delta")

	berkConn3 := guildDialWS(t, srv)
	guildHelloWS(t, berkConn3)
	enterOnlineWorld(t, berkConn3, "Berk", berkJID)
	expectPresenceDelta(t, alfaConn, berkJID, 0, "the replacement online delta")

	guildBye(t, berkConn2)
	guildBye(t, berkConn3)
	expectPresenceDelta(t, alfaConn, berkJID, 1, "the live session's close sends the offline delta")
	// A replaced session's close hook must stay silent: had it sent
	// its own offline delta, a second one would be queued now (ahead
	// of or behind the live close's), and this last read would meet
	// it.
	expectPresenceSilence(t, alfaConn, "a replaced session's close sends nothing")
}
