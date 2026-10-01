package action_test

// End-to-end exercise of the NPC-select talk grant over the REAL
// transport against the REAL authority store (the guild e2e composition:
// store seed, live WebSocket, hand-rolled byte oracles):
//
//	enter world  -> the 0x0006 bind with NPC spawns ENABLED, so the
//	               object list carries the roster create row(s) between
//	               start and finalize;
//	self select  -> 0x745A with the player's own gid: recorded, SILENT;
//	ground select-> 0x745A with a live division drop's gid: recorded,
//	               SILENT (both silences proven by stream order below);
//	NPC select   -> 0x745A with the roster row's stable ObjectID: the
//	               VERY NEXT frame on the session's reliable ordered
//	               stream is the exact 0xB45A talk grant - result 1, the
//	               gid, vitalsMask 0, NPC_EU_SMITH's capability word
//	               0x03, npcExtra 0 - implemented shop|talk rows only,
//	               byte-equal to the hand-rolled
//	               oracle. Had the player or ground grant emitted
//	               anything, it would have arrived first and failed the
//	               opcode assertion.

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	wiretest "opensro.online/server/internal/game/internal"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

const (
	selectE2EDivision = "global-official"
	selectE2EName     = "e2eNpcSelect"
)

// selectE2ESkillSeeder is this suite's stand-in for
// enterworld.DefaultSkillSeeder (the store's unconditional creation-seed
// invariant refuses an unseeded CreateCharacter): the same racial id
// sets, without a textdata dependency. The select plane never reads
// skills - the seeder exists only to satisfy the store's invariant.
func selectE2ESkillSeeder(raceKey string, learned []uint32) ([]uint32, error) {
	ids := []uint32{1, 7127, 7128, 7129, 7909, 7910, 8454, 9069, 9606, 9970}
	if raceKey == enterworld.RaceKeyChina {
		ids = []uint32{1, 2, 40, 70}
	}
	missing := make([]uint32, 0, len(ids))
	have := map[uint32]bool{}
	for _, id := range learned {
		have[id] = true
	}
	for _, id := range ids {
		if !have[id] {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

type selectE2EServer struct {
	srv       *transport.Server
	authority *store.Store
	actions   *action.Runtime
}

// startSelectServer opens the authority store in dir and stands up the
// transport with the bootstrap + action lanes composed like server.go:
// one deps pointer, the item runtime built BEFORE enterworld.Register so
// both capture the same instance, and NPC spawns enabled on BOTH sides
// of the seam - the object-list rows (bootstrap) and the 0x745A liveness
// gate (action) must agree or the click targets a gid the other half
// denies.
func startSelectServer(t *testing.T, dir string, seeds []*enterworld.Character) selectE2EServer {
	t.Helper()

	authority, err := store.Open(dir, store.Options{DefaultSkills: selectE2ESkillSeeder})
	if err != nil {
		t.Fatalf("store.Open(%s): %v", dir, err)
	}
	t.Cleanup(authority.Close)

	existing := map[string]bool{}
	for _, c := range authority.Characters().CharactersForDivision(selectE2EDivision) {
		existing[c.Name] = true
	}
	for _, seed := range seeds {
		if seed == nil || existing[seed.Name] {
			continue
		}
		if err := authority.CreateCharacter(selectE2EDivision, "test-account", seed); err != nil {
			t.Fatalf("CreateCharacter(%s): %v", seed.Name, err)
		}
	}

	deps := &enterworld.Deps{
		Roster:     &enterworld.Roster{},
		Characters: authority.Characters(),
		PlayerBaseStats: func(*enterworld.Character) (wire.BaseStats, error) {
			return wire.BaseStats{}, nil
		},
	}
	deps.ResolveDivisionID = enterworld.DevResolveDivisionIDFromCatalog(deps.Characters)
	deps.MutateCharacter = func(c *enterworld.Character, label string, fn func()) {
		authority.MutateCharacter(c, label, fn)
	}
	deps.Letters = authority.Letters()
	deps.Guilds = authority.Guilds()

	// The fixture roster stands at the player, inside the native hit range.
	npcSpawns := enterworld.NpcSpawnConfig{Enabled: true, AtPlayer: true, Roster: simulation.DefaultNpcRoster()}
	deps.NpcSpawns = npcSpawns
	deps.ObjectListRows = func(divisionID string, character *enterworld.Character, entry *enterworld.LocalPlayerEntry) []enterworld.Packet {
		return npcSpawns.NpcObjectListRows(entry)
	}
	deps.RefObjSnapshot = npcSpawns.NpcRefObjSnapshot

	actions := action.NewRuntime(deps, nil)
	actions.NpcSpawn = npcSpawns

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
	actions.Register(srv.Hub)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	})
	return selectE2EServer{srv: srv, authority: authority, actions: actions}
}

func selectDialWS(t *testing.T, srv *transport.Server) *websocket.Conn {
	t.Helper()
	url := fmt.Sprintf("ws://%s%s", srv.WSAddr(), transport.PathWS)
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("websocket dial %s: %v", url, err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func selectSendFrame(t *testing.T, c *websocket.Conn, opcode uint16, payload []byte) {
	t.Helper()
	f := transport.Frame{Opcode: opcode, Payload: payload}
	if err := c.WriteMessage(websocket.BinaryMessage, f.Encode()); err != nil {
		t.Fatalf("writing frame 0x%04X: %v", opcode, err)
	}
}

func selectNextFrame(t *testing.T, c *websocket.Conn, what string) transport.Frame {
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

func selectExpectFrame(t *testing.T, c *websocket.Conn, opcode uint16, what string) []byte {
	t.Helper()
	f := selectNextFrame(t, c, what)
	if f.Opcode != opcode {
		t.Fatalf("%s: next frame = 0x%04X payload % X, want opcode 0x%04X", what, f.Opcode, f.Payload, opcode)
	}
	return f.Payload
}

func selectHelloWS(t *testing.T, c *websocket.Conn) {
	t.Helper()
	selectSendFrame(t, c, transport.OpHello, transport.EncodeHello(transport.Hello{AdmissionToken: []byte("test-admission")}))
	payload := selectExpectFrame(t, c, transport.OpWelcome, "handshake")
	if _, err := transport.DecodeWelcome(payload); err != nil {
		t.Fatalf("decoding WELCOME: %v", err)
	}
}

// selectEnterWorld performs the 0x0006 bind as name and consumes the
// frozen bootstrap sequence WITH the NPC roster riding the object list:
// one 0x3417 create row per roster NPC between start and finalize (the
// row bytes themselves are pinned by bootstrap's objectlist tests).
func selectEnterWorld(t *testing.T, c *websocket.Conn, name string) {
	t.Helper()
	selectSendFrame(t, c, transport.OpEnterWorld, transport.EncodeEnterWorld(
		entryauth.NewAuthenticatedEntryFixture(t, selectE2EDivision, name),
	))
	result, err := transport.DecodeEnterWorldResult(selectExpectFrame(t, c, transport.OpEnterWorldResult, "enter world "+name))
	if err != nil {
		t.Fatalf("decoding 0x0007: %v", err)
	}
	if !result.OK {
		t.Fatalf("enter world %s refused: nativeErrorCode=%#x", name, result.NativeErrorCode)
	}
	selectExpectFrame(t, c, enterworld.OpcodeResetClient, "bootstrap[0]")
	selectExpectFrame(t, c, enterworld.OpcodeMyCharacterData, "bootstrap[1]")
	selectExpectFrame(t, c, enterworld.OpcodeMyCharacterChunk, "bootstrap[2]")
	selectExpectFrame(t, c, enterworld.OpcodeMyCharacterFlush, "bootstrap[3]")
	selectExpectFrame(t, c, enterworld.OpcodeServerClockGidLatch, "bootstrap[4]")
	selectExpectFrame(t, c, enterworld.OpcodeObjectListStart, "object list start")
	for index := range simulation.DefaultNpcRoster() {
		selectExpectFrame(t, c, enterworld.OpcodeObjectListChunk, fmt.Sprintf("NPC create row %d", index))
	}
	selectExpectFrame(t, c, enterworld.OpcodeObjectListFinalize, "object list finalize")
	wiretest.ActivateWorld(t, c, "enter world "+name)
}

// selectE2ECharacter resolves a live store record by name.
func selectE2ECharacter(t *testing.T, authority *store.Store, name string) *enterworld.Character {
	t.Helper()
	var found *enterworld.Character
	authority.ReadCharacters(selectE2EDivision, func(characters []*enterworld.Character) {
		for _, c := range characters {
			if c != nil && c.Name == name {
				found = c
				return
			}
		}
	})
	if found == nil {
		t.Fatalf("character %s not in the store", name)
	}
	return found
}

// selectGidBody hand-rolls the 4-byte 0x745A body.
func selectGidBody(gid uint32) []byte {
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, gid)
	return out
}

// TestNpcSelectTalkGrantEndToEndOverWire is the live-play proof for the
// NPC talk window's opening frame: the roster-NPC 0x745A elicits the
// exact 0xB45A bytes, and neither a player nor a ground-drop selection
// leaks one.
func TestNpcSelectTalkGrantEndToEndOverWire(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "authority")

	race := func() *int64 { v := enterworld.RaceChina; return &v }
	gender := func() *int64 { v := enterworld.GenderMale; return &v }
	seeds := []*enterworld.Character{
		{Name: selectE2EName, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender()},
	}
	server := startSelectServer(t, dir, seeds)
	character := selectE2ECharacter(t, server.authority, selectE2EName)

	conn := selectDialWS(t, server.srv)
	selectHelloWS(t, conn)
	selectEnterWorld(t, conn, selectE2EName)

	// A live division drop, seeded through the SAME registry instance the
	// handler's liveness gate reads (the registry locks itself).
	drop := server.actions.Ground.Add(selectE2EDivision, grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02"})

	// Player and ground selections: granted (recorded) but SILENT. The
	// proof of silence is stream order - any frame either grant emitted
	// would arrive before the NPC grant's 0xB45A below and fail its
	// opcode assertion.
	selectSendFrame(t, conn, wire.OpObjectSelectRequest, selectGidBody(enterworld.ObjectIDForCharacter(character)))
	selectSendFrame(t, conn, wire.OpObjectSelectRequest, selectGidBody(drop.Gid))

	npcGid := server.actions.NpcRoster[0].ObjectID
	selectSendFrame(t, conn, wire.OpObjectSelectRequest, selectGidBody(npcGid))

	got := selectExpectFrame(t, conn, wire.OpObjectSelectResult, "NPC talk grant")
	want := []byte{0x01}
	want = binary.LittleEndian.AppendUint32(want, npcGid)
	want = append(want, 0x00)
	want = binary.LittleEndian.AppendUint32(want, 0x03) // NPC_EU_SMITH: implemented shop|talk
	want = append(want, 0x00)
	if !bytes.Equal(got, want) {
		t.Fatalf("0xB45A payload\n got % X\nwant % X", got, want)
	}

	// All three selections recorded; the last one wins the store slot.
	if gid, ok := server.actions.Selected.Get(selectE2EDivision, selectE2EName); !ok || gid != npcGid {
		t.Errorf("selection store = %d/%v, want the NPC gid %d", gid, ok, npcGid)
	}

	if dropped := server.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s)", dropped)
	}
	selectSendFrame(t, conn, transport.OpBye, []byte{transport.ByeReasonNormal})
}
