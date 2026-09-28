/*
===========================================================================

e2e_chat_wire_test.go - native chat delivery, acknowledgement and privacy regressions.

===========================================================================
*/
package chat_test

// End-to-end exercise of the chat lane over the REAL transport with
// THREE live sessions against the REAL authority store (the friend-lane
// e2e precedent): a WebSocket client speaks the production frame
// protocol against a loopback transport.Server composed like server.go's
// gameplay wiring, asserting every 0xB367 / 0x3667 body BYTE-EXACT:
//
//	A all-chats      -> A gets 0xB367 {01 01 FF}; B and C each get
//	                    0x3667 {06, sender name, text} (beta cohort); A
//	                    gets NO 0x3667 (the ack presented A's line);
//	C (GM) all-chats -> the same named beta channel reaches the cohort;
//	A whispers C     -> C gets 0x3667 {02, "chatAlfa" ANSI, text}, A
//	                    gets the success ack;
//	A whispers B     -> B's persisted block list carries "CHATALFA"
//	                    (casing differs on purpose - the EqualFold
//	                    delivery match must fire): A gets 0xB367
//	                    SUCCESS and B receives NOTHING - the privacy
//	                    rule, both halves asserted;
//	A whispers ghost -> 0xB367 {02 03 02 FF} ("Cannot find [%s].");
//	A whispers self  -> success, nothing delivered anywhere;
//	party/guild/union without membership -> 0xB367 error 0x0A/0x0B/0x0B.

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/enterworld"
	wiretest "opensro.online/server/internal/game/internal"
	presence "opensro.online/server/internal/game/social"
	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/social/party"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

const (
	e2eChatNameA = "chatAlfa"
	e2eChatNameB = "chatBravo"
	e2eChatNameC = "chatCale"
)

// chatSkillSeeder satisfies the store's unconditional creation-seed
// invariant; chat never reads skills.
/*
================
chatSkillSeeder
================
*/
func chatSkillSeeder(raceKey string, learned []uint32) ([]uint32, error) {
	ids := []uint32{1, 2, 40, 70}
	have := make(map[uint32]bool, len(learned))
	for _, id := range learned {
		have[id] = true
	}
	missing := make([]uint32, 0, len(ids))
	for _, id := range ids {
		if !have[id] {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

/*
================
e2eInt64
================
*/
func e2eInt64(v int64) *int64 { return &v }

/*
================
chatServer
================
*/
type chatServer struct {
	srv       *transport.Server
	authority *store.Store
}

// startChatServer opens the authority store, creates the three division
// characters, and stands up the transport with the bootstrap + chat
// lanes composed like server.go: one deps pointer, the store door /
// presence facade assigned before the pointer-based Register shares, the
// exclusive world bind in OnWorldBound.
/*
================
startChatServer
================
*/
func startChatServer(t *testing.T, dir, divisionID string) chatServer {
	t.Helper()

	authority, err := store.Open(dir, store.Options{DefaultSkills: chatSkillSeeder})
	if err != nil {
		t.Fatalf("store.Open(%s): %v", dir, err)
	}
	t.Cleanup(authority.Close)

	for _, name := range []string{e2eChatNameA, e2eChatNameB, e2eChatNameC} {
		seed := &enterworld.Character{
			Name:          name,
			ModelCodename: "CHAR_CH_MAN_ADVENTURER",
			RaceIndex:     e2eInt64(enterworld.RaceChina),
			Gender:        e2eInt64(enterworld.GenderMale),
		}
		if err := authority.CreateCharacter(divisionID, "test-account", seed); err != nil {
			t.Fatalf("CreateCharacter(%s): %v", name, err)
		}
	}

	deps := &enterworld.Deps{
		Roster:     &enterworld.Roster{},
		Characters: authority.Characters(),
	}
	deps.ResolveDivisionID = enterworld.DevResolveDivisionIDFromCatalog(deps.Characters)
	deps.MutateCharacter = func(c *enterworld.Character, label string, fn func()) {
		authority.MutateCharacter(c, label, fn)
	}

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
	srv.Hub.SetHelloAuth(func([]byte) (transport.AdmissionIdentity, error) {
		return transport.AdmissionIdentity{AccountID: "test-account", ShardID: "global-official"}, nil
	})
	entryauth.NewAuthenticatedSessionFixture(t, srv.Hub)

	presence := presence.NewDirectory(srv.Hub)
	deps.OnWorldBound = func(s *transport.Session, divisionID string, character *enterworld.Character) {
		key := divisionID + ":" + strings.ToLower(character.Name)
		if old, replaced := srv.Hub.BindExclusive(key, s); replaced {
			old.ClearGameplayContext()
		}
	}

	enterworld.Register(srv.Hub, deps)
	chat.Register(srv.Hub, deps, presence, party.NewRegistry())
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	})
	return chatServer{srv: srv, authority: authority}
}

// mutateCharacter runs fn on the named live store record through the
// commit door (the test's stand-in for the 0x766F block mutate and the
// SRO_GM_CHARACTERS reconcile).
/*
================
mutateCharacter
================
*/
func mutateCharacter(
	t *testing.T,
	authority *store.Store,
	divisionID string,
	name string,
	fn func(*enterworld.Character),
) {
	t.Helper()
	for _, character := range authority.Characters().CharactersForDivision(divisionID) {
		if character != nil && character.Name == name {
			authority.MutateCharacter(character, "test-seed", func() { fn(character) })
			return
		}
	}
	t.Fatalf("character %q not in the division", name)
}

/*
================
dialWS
================
*/
func dialWS(t *testing.T, srv *transport.Server) *websocket.Conn {
	t.Helper()
	url := fmt.Sprintf("ws://%s%s", srv.WSAddr(), transport.PathWS)
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("websocket dial %s: %v", url, err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

/*
================
sendFrame
================
*/
func sendFrame(t *testing.T, c *websocket.Conn, opcode uint16, payload []byte) {
	t.Helper()
	f := transport.Frame{Opcode: opcode, Payload: payload}
	if err := c.WriteMessage(websocket.BinaryMessage, f.Encode()); err != nil {
		t.Fatalf("writing frame 0x%04X: %v", opcode, err)
	}
}

/*
================
nextFrame
================
*/
func nextFrame(t *testing.T, c *websocket.Conn, what string) transport.Frame {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		typ, data, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("%s: reading ws message (frame never arrived?): %v", what, err)
		}
		if typ != websocket.BinaryMessage {
			t.Fatalf("ws message type = %d, want binary", typ)
		}
		f, err := transport.DecodeFrame(data)
		if err != nil {
			t.Fatalf("decoding ws frame: %v", err)
		}
		if f.Opcode == transport.OpPing || f.Opcode == transport.OpPong {
			continue
		}
		return f
	}
}

/*
================
expectFrame
================
*/
func expectFrame(t *testing.T, c *websocket.Conn, opcode uint16, what string) []byte {
	t.Helper()
	f := nextFrame(t, c, what)
	if f.Opcode != opcode {
		t.Fatalf("%s: next frame = 0x%04X payload % X, want opcode 0x%04X", what, f.Opcode, f.Payload, opcode)
	}
	return f.Payload
}

/*
================
expectExact
================
*/
func expectExact(t *testing.T, c *websocket.Conn, opcode uint16, want []byte, what string) {
	t.Helper()
	got := expectFrame(t, c, opcode, what)
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: 0x%04X body = % X, want % X", what, opcode, got, want)
	}
}

/*
================
helloWS
================
*/
func helloWS(t *testing.T, c *websocket.Conn) {
	t.Helper()
	sendFrame(t, c, transport.OpHello, transport.EncodeHello(transport.Hello{AdmissionToken: []byte("test-admission")}))
	payload := expectFrame(t, c, transport.OpWelcome, "handshake")
	if _, err := transport.DecodeWelcome(payload); err != nil {
		t.Fatalf("decoding WELCOME: %v", err)
	}
}

// enterChatWorld performs the 0x0006 bind and consumes the frozen
// bootstrap sequence (no community seed seam is wired in this harness).
/*
================
enterChatWorld
================
*/
func enterChatWorld(
	t *testing.T,
	c *websocket.Conn,
	divisionID string,
	name string,
) {
	t.Helper()
	sendFrame(t, c, transport.OpEnterWorld, transport.EncodeEnterWorld(
		entryauth.NewAuthenticatedEntryFixture(t, divisionID, name),
	))
	result, err := transport.DecodeEnterWorldResult(expectFrame(t, c, transport.OpEnterWorldResult, "enter world "+name))
	if err != nil {
		t.Fatalf("decoding 0x0007 for %s: %v", name, err)
	}
	if !result.OK {
		t.Fatalf("enter world refused for %s: nativeErrorCode=%#x", name, result.NativeErrorCode)
	}
	expectFrame(t, c, enterworld.OpcodeResetClient, "bootstrap[0]")
	expectFrame(t, c, enterworld.OpcodeMyCharacterData, "bootstrap[1]")
	expectFrame(t, c, enterworld.OpcodeMyCharacterChunk, "bootstrap[2]")
	expectFrame(t, c, enterworld.OpcodeMyCharacterFlush, "bootstrap[3]")
	expectFrame(t, c, enterworld.OpcodeServerClockGidLatch, "bootstrap[4]")
	expectFrame(t, c, enterworld.OpcodeObjectListStart, "bootstrap[5]")
	expectFrame(t, c, enterworld.OpcodeObjectListFinalize, "bootstrap[6]")
	wiretest.ActivateWorld(t, c, "enter world "+name)
}

// gameReadyBarrier proves NOTHING is queued on the wire for this
// session: a transport FIFO barrier surfaces any stray frame (e.g. a whisper
// that must NOT have been delivered) without replaying world admission.
/*
================
gameReadyBarrier
================
*/
func gameReadyBarrier(t *testing.T, c *websocket.Conn, what string) {
	t.Helper()
	wiretest.AssertQueueDrained(t, c, what)
}

// chatRequestFrame hand-rolls the 0x7367 body exactly as the retail
// composer does: {u8 type, u8 second=0xFF, [whisper: u16 len + ANSI
// target], u16 wcharCount + UTF-16LE text}.
/*
================
chatRequestFrame
================
*/
func chatRequestFrame(chatType uint8, target, message string) []byte {
	frame := []byte{chatType, 0xFF}
	if chatType == chat.ChatTypeWhisper {
		frame = append(frame, byte(len(target)), byte(len(target)>>8))
		frame = append(frame, []byte(target)...)
	}
	return append(frame, sizedUTF16LE(message)...)
}

// sizedUTF16LE hand-rolls the sized wide text {u16 count, count*2 bytes}
// (BMP-only fixtures: one code unit per rune).
/*
================
sizedUTF16LE
================
*/
func sizedUTF16LE(message string) []byte {
	runes := []rune(message)
	out := []byte{byte(len(runes)), byte(len(runes) >> 8)}
	for _, r := range runes {
		out = append(out, byte(r), byte(uint16(r)>>8))
	}
	return out
}

// namedBroadcast hand-rolls the 0x3667 name-authored body {u8 type, u16
// len + ANSI name, sized wide text}.
/*
================
namedBroadcast
================
*/
func namedBroadcast(chatType uint8, name, message string) []byte {
	body := []byte{chatType, byte(len(name)), byte(len(name) >> 8)}
	body = append(body, []byte(name)...)
	return append(body, sizedUTF16LE(message)...)
}

/*
================
TestChatLaneEndToEndOverWire
================
*/
func TestChatLaneEndToEndOverWire(t *testing.T) {
	for _, divisionID := range []string{"global-official", "test"} {
		t.Run(divisionID, func(t *testing.T) {
			runChatLaneEndToEndOverWire(t, divisionID)
		})
	}
}

/*
================
runChatLaneEndToEndOverWire
================
*/
func runChatLaneEndToEndOverWire(t *testing.T, divisionID string) {
	server := startChatServer(
		t,
		filepath.Join(t.TempDir(), "authority"),
		divisionID,
	)

	// Bravo blocks "CHATALFA" - the casing DIFFERS from the stored
	// "chatAlfa" on purpose: the delivery-side match folds case exactly
	// like the register path (whisperblock.go). Cale is a GM (the
	// SRO_GM_CHARACTERS reconcile's effect on the record).
	mutateCharacter(t, server.authority, divisionID, e2eChatNameB, func(c *enterworld.Character) {
		c.BlockedWhisperers = []string{"CHATALFA"}
	})
	mutateCharacter(t, server.authority, divisionID, e2eChatNameC, func(c *enterworld.Character) {
		c.GMPrivilege = true
	})

	connA := dialWS(t, server.srv)
	helloWS(t, connA)
	enterChatWorld(t, connA, divisionID, e2eChatNameA)
	connB := dialWS(t, server.srv)
	helloWS(t, connB)
	enterChatWorld(t, connB, divisionID, e2eChatNameB)
	connC := dialWS(t, server.srv)
	helloWS(t, connC)
	enterChatWorld(t, connC, divisionID, e2eChatNameC)

	// Restriction admission precedes decode, routing, success acknowledgments and
	// all fan-out. The deliberately old enabled date must not expire locally.
	for _, actor := range []struct {
		name string
		conn *websocket.Conn
	}{{e2eChatNameA, connA}, {e2eChatNameC, connC}} {
		session, ok := server.srv.Hub.BoundSession(presence.BindKey(divisionID, actor.name))
		if !ok {
			t.Fatal("chat actor not bound")
		}
		session.SetCommandRestriction(transport.CommandRestrictionChat, [8]uint16{2026, 1, 0, 1, 0, 0, 0, 0})
		for _, kind := range []uint8{chat.ChatTypeAll, chat.ChatTypeWhisper, chat.ChatTypeParty, chat.ChatTypeGuild, chat.ChatTypeUnion, chat.ChatTypeGM, 255} {
			sendFrame(t, actor.conn, chat.OpChatRequest, chatRequestFrame(kind, e2eChatNameB, "blocked"))
			expectExact(t, actor.conn, 0x36ea, []byte{0, 0x5a, 4, 0, 0}, "restricted chat")
		}
		sendFrame(t, actor.conn, chat.OpChatRequest, nil)
		expectExact(t, actor.conn, 0x36ea, []byte{0, 0x5a, 4, 0, 0}, "restriction before malformed body")
		session.ClearCommandRestriction(transport.CommandRestrictionChat)
		gameReadyBarrier(t, actor.conn, "no chat ack alongside restriction")
	}
	// A trade-only restriction must not block chat. Subsequent peer reads also
	// prove that none of the blocked requests leaked a division/target delivery.
	sessionA, _ := server.srv.Hub.BoundSession(presence.BindKey(divisionID, e2eChatNameA))
	sessionA.SetCommandRestriction(transport.CommandRestrictionTrade, [8]uint16{2026, 1, 0, 1, 0, 0, 0, 0})
	// ---- All-chat: division cohort, sender excluded ----
	sendFrame(t, connA, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeAll, "", "hey"))
	expectExact(t, connA, chat.OpChatAck, []byte{0x01, 0x01, 0xFF}, "all-chat ack to A")
	expectExact(t, connB, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "hey"), "all-chat to B")
	expectExact(t, connC, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "hey"), "all-chat to C")
	// A must NOT receive its own broadcast (the ack presented it).
	gameReadyBarrier(t, connA, "post-all-chat A")

	// ---- GM speaker: the broadcast type byte is FORCED to 3 ----
	sendFrame(t, connC, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeGM, "", "gm"))
	expectExact(t, connC, chat.OpChatAck, []byte{0x01, 0x03, 0xFF}, "GM all-chat ack to C")
	expectExact(t, connA, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameC, "gm"), "GM all-chat to A")
	expectExact(t, connB, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameC, "gm"), "GM all-chat to B")

	// ---- Whisper delivered: ANSI sender name + text ----
	sendFrame(t, connA, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeWhisper, e2eChatNameC, "yo"))
	expectExact(t, connA, chat.OpChatAck, []byte{0x01, 0x02, 0xFF}, "whisper ack to A")
	expectExact(t, connC, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeWhisper, e2eChatNameA, "yo"), "whisper to C")
	gameReadyBarrier(t, connB, "post-whisper B (bystander)")

	// ---- Whisper BLOCKED: sender sees SUCCESS, target gets NOTHING ----
	sendFrame(t, connA, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeWhisper, e2eChatNameB, "psst"))
	expectExact(t, connA, chat.OpChatAck, []byte{0x01, 0x02, 0xFF}, "blocked-whisper ack to A (MUST be success)")
	gameReadyBarrier(t, connB, "post-blocked-whisper B (MUST be empty)")

	// ---- Whisper to a nonexistent name: error 3 ----
	sendFrame(t, connA, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeWhisper, "ghost", "hello"))
	expectExact(t, connA, chat.OpChatAck, []byte{0x02, 0x03, 0x02, 0xFF}, "ghost-whisper ack to A")

	// ---- Whisper to self: success, no delivery anywhere ----
	sendFrame(t, connA, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeWhisper, e2eChatNameA, "me"))
	expectExact(t, connA, chat.OpChatAck, []byte{0x01, 0x02, 0xFF}, "self-whisper ack to A")
	gameReadyBarrier(t, connA, "post-self-whisper A")

	// ---- Membership gates: party 0x0A, guild 0x0B, union 0x0B ----
	sendFrame(t, connA, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeParty, "", "party?"))
	expectExact(t, connA, chat.OpChatAck, []byte{0x02, 0x0A, 0x04, 0xFF}, "partyless ack to A")
	sendFrame(t, connA, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeGuild, "", "guild?"))
	expectExact(t, connA, chat.OpChatAck, []byte{0x02, 0x0B, 0x05, 0xFF}, "guildless ack to A")
	sendFrame(t, connA, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeUnion, "", "union?"))
	expectExact(t, connA, chat.OpChatAck, []byte{0x02, 0x0B, 0x0B, 0xFF}, "unionless ack to A")

	// Nothing stray anywhere; the transport rate limiter never fired.
	gameReadyBarrier(t, connB, "final B")
	gameReadyBarrier(t, connC, "final C")
	if dropped := server.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s)", dropped)
	}
	sendFrame(t, connA, transport.OpBye, []byte{transport.ByeReasonNormal})
	sendFrame(t, connB, transport.OpBye, []byte{transport.ByeReasonNormal})
	sendFrame(t, connC, transport.OpBye, []byte{transport.ByeReasonNormal})
}
