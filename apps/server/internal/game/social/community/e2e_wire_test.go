package community_test

// End-to-end exercise of the community lane over the REAL transport
// against the REAL authority store (the progression e2e_wire_test.go
// precedent): a WebSocket client speaks the production frame protocol
// against a loopback transport.Server composed like server.go's gameplay
// wiring (ONE *enterworld.Deps; the store door and the community
// seam assigned BEFORE the pointer-based Register shares), asserting:
//
//	enter-world  -> the 7 bootstrap frames in their frozen order, the
//	                0x32B3 chunk carrying an EMPTY chunk C (u8 0
//	                immediately before the 01 00 01 00 00 00 payload
//	                tail = fortress sentinel + event count + mission
//	                mode), then the community seeds: 0x3769 [00] and
//	                0xB3CD [01 00];
//	0x766F reg   -> the 0xB66F ack {mode 1, result 0, u16 len + name}
//	                rides the wire (sub_771550 applies the name to the
//	                blocking panel LIVE) and the store persists it;
//	0x766F dup   -> 0xB66F {1, 1} ("Name already exists."), no change;
//	0x766F unknown> 0xB66F {1, 2} ("User does not exist."), no change;
//	0x7164       -> SILENT refusal (live handler; unknown target name);
//	reboot       -> the store reopens with the blocked name PERSISTED,
//	                and a fresh EnterWorld's 0x32B3 chunk C carries it
//	                (the enter-world reflection channel - sub_77ad10
//	                reads exactly these bytes into the whisper panel);
//	0x766F cancel-> 0xB66F {2, 0, name}; the store view drops the name;
//	cancel miss  -> 0xB66F {2, 2} (the v1.188 GameServer remove-miss
//	                remap, MEDIUM confidence - the evidence note sits on
//	                the handler's cancel arm).

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/enterworld"
	wiretest "opensro.online/server/internal/game/internal"
	"opensro.online/server/internal/game/item/wire"
	presence "opensro.online/server/internal/game/social"
	"opensro.online/server/internal/game/social/community"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

const (
	e2eDivision = "global-official"
	// 12 chars: the top of the native 2..12 creation window.
	e2eCharName = "e2eCommunity"
)

type e2eServer struct {
	srv       *transport.Server
	authority *store.Store
}

// communitySkillSeeder is this package's stand-in for
// enterworld.DefaultSkillSeeder (the store's unconditional creation-seed
// invariant refuses an unseeded CreateCharacter): the same racial id
// sets, without a textdata dependency. Community tests never read
// skills - the seeder exists only to satisfy the store's creation
// invariant.
func communitySkillSeeder(raceKey string, learned []uint32) ([]uint32, error) {
	ids := []uint32{1, 7127, 7128, 7129, 7909, 7910, 8454, 9069, 9606, 9970}
	if raceKey == enterworld.RaceKeyChina {
		ids = []uint32{1, 2, 40, 70}
	}
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

// startCommunityServer opens the authority store in dir and stands up the
// transport with the bootstrap + community lanes composed like server.go:
// one deps pointer, store collaborators and the seed seam assigned before
// the registers capture it.
func startCommunityServer(t *testing.T, dir string, seed *enterworld.Character) e2eServer {
	t.Helper()

	authority, err := store.Open(dir, store.Options{DefaultSkills: communitySkillSeeder})
	if err != nil {
		t.Fatalf("store.Open(%s): %v", dir, err)
	}
	t.Cleanup(authority.Close)

	if seed != nil && len(authority.Characters().CharactersForDivision(e2eDivision)) == 0 {
		if err := authority.CreateCharacter(e2eDivision, "test-account", seed); err != nil {
			t.Fatalf("CreateCharacter: %v", err)
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
	// The presence-less seed builder: this suite drives ONE character
	// with an empty friend roster, so the division-aware seam gets the
	// nil-presence (all-offline) encoder.
	deps.CommunitySeedFramesFor = community.SeedFrames

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
	enterworld.Register(srv.Hub, deps)
	community.Register(srv.Hub, deps)
	// The friend mutators are live handlers now (friend.go); this suite
	// only proves their REFUSAL arms stay silent (unknown target), so
	// the presence facade rides without the OnWorldBound bind glue - the
	// friend e2e suite (e2e_friend_wire_test.go) owns the full wiring.
	community.RegisterFriend(srv.Hub, deps, presence.NewDirectory(srv.Hub))
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shutdownServer(t, srv) })
	return e2eServer{srv: srv, authority: authority}
}

func shutdownServer(t *testing.T, srv *transport.Server) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

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

func sendFrame(t *testing.T, c *websocket.Conn, opcode uint16, payload []byte) {
	t.Helper()
	f := transport.Frame{Opcode: opcode, Payload: payload}
	if err := c.WriteMessage(websocket.BinaryMessage, f.Encode()); err != nil {
		t.Fatalf("writing frame 0x%04X: %v", opcode, err)
	}
}

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

func expectFrame(t *testing.T, c *websocket.Conn, opcode uint16, what string) []byte {
	t.Helper()
	f := nextFrame(t, c, what)
	if f.Opcode != opcode {
		t.Fatalf("%s: next frame = 0x%04X payload % X, want opcode 0x%04X", what, f.Opcode, f.Payload, opcode)
	}
	return f.Payload
}

func helloWS(t *testing.T, c *websocket.Conn) {
	t.Helper()
	sendFrame(t, c, transport.OpHello, transport.EncodeHello(transport.Hello{AdmissionToken: []byte("test-admission")}))
	payload := expectFrame(t, c, transport.OpWelcome, "handshake")
	if _, err := transport.DecodeWelcome(payload); err != nil {
		t.Fatalf("decoding WELCOME: %v", err)
	}
}

// enterWorld performs the 0x0006 bind, consumes the frozen bootstrap
// sequence AND the two community seed frames, and returns the raw 0x32B3
// chunk for the chunk-C assertion.
func enterWorld(t *testing.T, c *websocket.Conn) []byte {
	t.Helper()
	sendFrame(t, c, transport.OpEnterWorld, transport.EncodeEnterWorld(
		entryauth.NewAuthenticatedEntryFixture(t, e2eDivision, e2eCharName),
	))
	result, err := transport.DecodeEnterWorldResult(expectFrame(t, c, transport.OpEnterWorldResult, "enter world"))
	if err != nil {
		t.Fatalf("decoding 0x0007: %v", err)
	}
	if !result.OK {
		t.Fatalf("enter world refused: nativeErrorCode=%#x", result.NativeErrorCode)
	}

	expectFrame(t, c, enterworld.OpcodeResetClient, "bootstrap[0]")
	expectFrame(t, c, enterworld.OpcodeMyCharacterData, "bootstrap[1]")
	chunk := expectFrame(t, c, enterworld.OpcodeMyCharacterChunk, "bootstrap[2]")
	expectFrame(t, c, enterworld.OpcodeMyCharacterFlush, "bootstrap[3]")
	expectFrame(t, c, enterworld.OpcodeServerClockGidLatch, "bootstrap[4]")
	expectFrame(t, c, enterworld.OpcodeObjectListStart, "bootstrap[5]")
	expectFrame(t, c, enterworld.OpcodeObjectListFinalize, "bootstrap[6]")

	// The community seeds ride immediately after the bootstrap sequence.
	if got := expectFrame(t, c, community.OpFriendRosterPush, "friend roster seed"); !bytes.Equal(got, []byte{0x00}) {
		t.Fatalf("0x3769 seed payload = % X, want the empty roster [00]", got)
	}
	if got := expectFrame(t, c, community.OpLetterListAnswer, "letter list seed"); !bytes.Equal(got, []byte{0x01, 0x00}) {
		t.Fatalf("0xB3CD seed payload = % X, want the empty list [01 00]", got)
	}
	wiretest.ActivateWorld(t, c, "enter world")
	// The exclusive bind and the world-bound hooks run at the tail of the
	// game-ready handler; drain it so callers see a bound session.
	gameReadyBarrier(t, c, "world-bound tail")
	return chunk
}

// enteredPayloadTail is the frozen 6-byte tail of the 0x32B3 chunk: the
// u32 0x10001 fortress-war sentinel, the u8 0 event count, the u8 0
// mission-mode flag. Chunk C (the whisper-block list) sits IMMEDIATELY
// before it - the position sub_77ad10 reads it at.
var enteredPayloadTail = []byte{0x01, 0x00, 0x01, 0x00, 0x00, 0x00}

// assertChunkC asserts the whisper-block chunk immediately before the
// frozen payload tail equals the expected names (u8 count, per name u16
// len + ANSI bytes).
func assertChunkC(t *testing.T, chunk []byte, names []string, what string) {
	t.Helper()
	wantChunk := []byte{byte(len(names))}
	for _, name := range names {
		wantChunk = append(wantChunk, byte(len(name)), byte(len(name)>>8))
		wantChunk = append(wantChunk, []byte(name)...)
	}
	want := append(wantChunk, enteredPayloadTail...)
	if len(chunk) < len(want) {
		t.Fatalf("%s: 0x32B3 chunk = %d bytes, shorter than chunk C + tail (%d)", what, len(chunk), len(want))
	}
	if got := chunk[len(chunk)-len(want):]; !bytes.Equal(got, want) {
		t.Fatalf("%s: 0x32B3 chunk-C tail = % X, want % X", what, got, want)
	}
}

func whisperBlockRequest(mode uint8, name string) []byte {
	writer := wire.NewWriter(3 + len(name))
	writer.U8(mode)
	writer.U16(uint16(len(name)))
	writer.Bytes([]byte(name))
	return writer.Payload()
}

// gameReadyBarrier proves NOTHING is queued using a non-mutating transport
// FIFO barrier; it must not replay world admission.
func gameReadyBarrier(t *testing.T, c *websocket.Conn, what string) {
	t.Helper()
	wiretest.AssertQueueDrained(t, c, what)
}

func e2eInt64(v int64) *int64 { return &v }

func readBlockedWhisperers(t *testing.T, authority *store.Store) []string {
	t.Helper()
	var blocked []string
	authority.ReadCharacters(e2eDivision, func(characters []*enterworld.Character) {
		for _, character := range characters {
			if character.Name == e2eCharName {
				blocked = append([]string{}, character.BlockedWhisperers...)
				return
			}
		}
		t.Fatalf("character %q not in the division", e2eCharName)
	})
	return blocked
}

// expectWhisperBlockAck asserts the next frame is 0xB66F with the exact
// body - the raw-byte contract the client agent pins sub_771550 against.
func expectWhisperBlockAck(t *testing.T, c *websocket.Conn, want []byte, what string) {
	t.Helper()
	got := expectFrame(t, c, community.OpWhisperBlockAck, what)
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: 0xB66F body = % X, want % X", what, got, want)
	}
}

func TestCommunityLaneEndToEndOverWire(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")

	seed := &enterworld.Character{
		Name:          e2eCharName,
		ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		RaceIndex:     e2eInt64(enterworld.RaceChina),
		Gender:        e2eInt64(enterworld.GenderMale),
	}

	// ---- session 1: empty seeds, register, the 0xB66F acks ----
	first := startCommunityServer(t, dir, seed)

	// "Berk" must EXIST for the register to succeed (retail requires the
	// target in _CharNameList); provision it beside the actor.
	if err := first.authority.CreateCharacter(e2eDivision, "test-account", &enterworld.Character{
		Name:          "Berk",
		ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		RaceIndex:     e2eInt64(enterworld.RaceChina),
		Gender:        e2eInt64(enterworld.GenderMale),
	}); err != nil {
		t.Fatalf("CreateCharacter(Berk): %v", err)
	}

	conn := dialWS(t, first.srv)
	helloWS(t, conn)

	chunk := enterWorld(t, conn)
	assertChunkC(t, chunk, nil, "baseline")

	// Register "Berk": 0xB66F {1, 0, name} - sub_771550 applies the name
	// to the blocking panel live.
	sendFrame(t, conn, community.OpWhisperBlockRequest, whisperBlockRequest(community.WhisperBlockModeRegister, "Berk"))
	expectWhisperBlockAck(t, conn, []byte{0x01, 0x00, 0x04, 0x00, 'B', 'e', 'r', 'k'}, "register ack")
	gameReadyBarrier(t, conn, "post-register")
	if got := readBlockedWhisperers(t, first.authority); len(got) != 1 || got[0] != "Berk" {
		t.Fatalf("store block list after register = %v, want [Berk]", got)
	}

	// Duplicate register: 0xB66F {1, 1}, no state change.
	sendFrame(t, conn, community.OpWhisperBlockRequest, whisperBlockRequest(community.WhisperBlockModeRegister, "Berk"))
	expectWhisperBlockAck(t, conn, []byte{0x01, 0x01}, "duplicate ack")
	// Unknown target: 0xB66F {1, 2} ("User does not exist.").
	sendFrame(t, conn, community.OpWhisperBlockRequest, whisperBlockRequest(community.WhisperBlockModeRegister, "Cale"))
	expectWhisperBlockAck(t, conn, []byte{0x01, 0x02}, "unknown-target ack")
	// Friend-add refusal arm: "Cale" is not a division character, so the
	// live handler refuses silently (no refusal bytes are pinned).
	friendAdd := wire.NewWriter(6).U16(4).Bytes([]byte("Cale")).Payload()
	sendFrame(t, conn, community.OpFriendAddRequest, friendAdd)
	gameReadyBarrier(t, conn, "post-refusals")
	if got := readBlockedWhisperers(t, first.authority); len(got) != 1 || got[0] != "Berk" {
		t.Fatalf("store block list after refusals = %v, want [Berk]", got)
	}

	if dropped := first.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s)", dropped)
	}
	sendFrame(t, conn, transport.OpBye, []byte{transport.ByeReasonNormal})
	conn.Close()

	// ---- the reboot: the list is PERSISTED state ----
	shutdownServer(t, first.srv)
	first.authority.Close()

	second := startCommunityServer(t, dir, nil)
	if got := readBlockedWhisperers(t, second.authority); len(got) != 1 || got[0] != "Berk" {
		t.Fatalf("restored block list = %v, want [Berk]", got)
	}

	// ---- session 2: the chunk-C reseed carries the persisted name ----
	conn2 := dialWS(t, second.srv)
	helloWS(t, conn2)
	chunk2 := enterWorld(t, conn2)
	assertChunkC(t, chunk2, []string{"Berk"}, "post-reboot reseed")

	// Cancel removes it: 0xB66F {2, 0, name}, and the store view drops it.
	sendFrame(t, conn2, community.OpWhisperBlockRequest, whisperBlockRequest(community.WhisperBlockModeCancel, "Berk"))
	expectWhisperBlockAck(t, conn2, []byte{0x02, 0x00, 0x04, 0x00, 'B', 'e', 'r', 'k'}, "cancel ack")
	gameReadyBarrier(t, conn2, "post-cancel")
	if got := readBlockedWhisperers(t, second.authority); len(got) != 0 {
		t.Fatalf("store block list after cancel = %v, want empty", got)
	}

	// Cancel MISS: 0xB66F {2, 2} - the v1.188 GameServer remove-miss
	// remap (sub_435cd0 SQL 1 -> wire 2), MEDIUM confidence; the full
	// evidence note sits on the handler's cancel arm.
	sendFrame(t, conn2, community.OpWhisperBlockRequest, whisperBlockRequest(community.WhisperBlockModeCancel, "Berk"))
	expectWhisperBlockAck(t, conn2, []byte{0x02, 0x02}, "cancel-miss ack")
	gameReadyBarrier(t, conn2, "post-cancel-miss")
	if got := readBlockedWhisperers(t, second.authority); len(got) != 0 {
		t.Fatalf("store block list after cancel miss = %v, want empty", got)
	}

	if dropped := second.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s) on the reopened server", dropped)
	}
	sendFrame(t, conn2, transport.OpBye, []byte{transport.ByeReasonNormal})
}
