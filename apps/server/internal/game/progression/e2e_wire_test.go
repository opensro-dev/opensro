/*
===========================================================================

e2e_wire_test.go - progression end to end over the wire

===========================================================================
*/

package progression_test

// End-to-end exercise of the character-progression plane over the REAL
// transport against the REAL authority store (gapclose-wave LANE-1).
//
// Every prior test of this plane calls the Runtime handlers directly
// (runtime_test.go, skilllearn_test.go, door_persistence_test.go) - none
// drives AcceptConn -> Hub.dispatch -> hubHandler -> store door, and the
// live server log has never carried a progression opcode. This test closes
// that verification gap: a WebSocket client speaks the production frame
// protocol (HELLO/WELCOME, OpEnterWorld bind, then the progression
// requests) against a loopback transport.Server composed EXACTLY like
// server.go's gameplay wiring (one *enterworld.Deps, the store commit
// door and the textdata tables assigned BEFORE enterworld.Register /
// progression.Register retain its pointer), and asserts the full observable
// consequence of each operation:
//
//	0x727A +STR   -> 0xB27A [01] ack (the ack alone decrements the client's
//	                 remaining count - sub_75ba00), then the 0x343C block
//	                 whose APPLIED +0x20/+0x22 words carry the raised STR
//	                 (never the +0x14/+0x16 trap pair), and NO 0x30B3
//	                 type-3 rides the burst (it would double-apply the
//	                 spend);
//	0x7165 train  -> 0xB165 [01][masteryId][newLevel], then the 0x30B3
//	                 type-2 ABSOLUTE skill-point refresh (notify=0);
//	0x72CB learn  -> 0xB2CB [01][skillId], then the same 0x30B3 type-2
//	                 refresh; the learned skill then appears in the next
//	                 0x32B3 char-data (production never re-emits 0x32B3
//	                 inside the learn burst - the client's own ack handler
//	                 sub_75bb20 inserts the skill locally);
//	refusals      -> points-exhausted 0xB27A [02][02], insufficient-SP
//	                 0xB165 [02][02] (07:02) and 0xB2CB [02][0a] (05:0a),
//	                 duplicate-group 0xB2CB [02][01], each with NO
//	                 follow-up frame and NO persisted change;
//	persistence   -> the store is CLOSED and REOPENED under a second
//	                 transport server; a fresh EnterWorld's 0x32B3 carries
//	                 the trained mastery level, the learned skills and the
//	                 spent pools, the 0x0007 blob snapshot carries the
//	                 raised STR, and a post-reboot 0x727A still refuses
//	                 (spent points never come back across a restart).
//
// The scenario constants are pinned to the SHIPPED v1.150 tables (read
// off extracted/Media_extracted/server_dep/silkroad/textdata; the
// preflight below re-asserts them through the same TextdataLevels/
// TextdataSkills loaders production uses, so a re-extraction that moves a
// column fails loudly HERE instead of surfacing as a confusing wire
// mismatch):
//
//	leveldata.txt col 2:  training a mastery TO level 7 costs 5 SP,
//	                      TO level 8 costs 6 SP;
//	skilldata id 1: SKILL_PUNCH_01          grp 172 lvl 1, no reqs, 0 SP;
//	skilldata id 3: SKILL_CH_SWORD_SMASH_A_01 grp 174 lvl 1,
//	                      mastery 257@5, 2 SP;
//	skilldata id 6: SKILL_CH_SWORD_CHAIN_A_1S_01 grp 177 lvl 1,
//	                      mastery 257@7, 5 SP.
//
// The seed (CH character, Bicheon 257 pre-trained to 6, 8 SP, 1 stat
// point) is chosen so one scripted session witnesses every success AND
// every refusal deterministically: train 6->7 spends 5 (SP 8->3), train
// 7->8 refuses (6 > 3), learn skill 3 spends 2 (SP 3->1), learn skill 6
// refuses (5 > 1), relearn skill 3 refuses (group already learned - the
// refusal the client's success-ack assert @0x0075bb71 demands), learn
// skill 1 is free (SP stays 1).
//
// Deliberate deviations from server.go's full composition, none of which
// touch this plane: no action/movement (their handlers are other lanes'
// opcodes), no simulation ticker (progression has no tick leg; its absence also
// means any frame this test receives was CAUSED by a request, so the
// strict frame-order assertions can prove "nothing else rode the burst"),
// and no OnWorldBound exclusive bind (single client, no eviction to
// arbitrate). The rate limiter runs at the PRODUCTION default (25/s,
// burst 75); the whole script is ~16 dispatched frames.
//
// The LEVELLING path (levelup wave, LANE-5) has its own scripted test
// below - TestLevellingPathEndToEndOverWire - proving the grant-exp ->
// level-up -> grown-maxima -> new-stat-points sequence over the same
// real internal/transport/store composition, including the 0xDE01 dev-trigger
// registration gate and persistence across a store reopen.

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	wiretest "opensro.online/server/internal/game/internal"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/testsupport/licensed"
	"opensro.online/server/internal/transport"
)

const (
	e2eDivision = "global-official"
	// 11 chars: inside the native 2..12 creation window store.
	// CreateCharacter now enforces (levelup wave, LANE-4).
	e2eCharName = "e2eWireTest"

	// bicheonMastery is the first CH racial mastery id (shipped
	// skillmasterydata; the same id every progression unit test trains).
	bicheonMastery uint32 = 257

	// Shipped skilldata ids (see the header comment for the pinned rows).
	skillPunchID    uint32 = 1
	skillSmashA1ID  uint32 = 3
	skillChainA1SID uint32 = 6
)

/*
==================
shippedTextdataDir

shippedTextdataDir resolves the extracted v1.150 textdata tree the
production Deps read (the config-owned GameDataPaths textdata root, from
this package's directory). Missing tree = skip, the parity_full_test
posture: the tables ARE the authority data and substituting a fixture
here would exercise a different server than the one deployed.
==================
*/
func shippedTextdataDir(t *testing.T) string {
	t.Helper()
	dir := licensed.RetailTextdataDir(t)
	if _, err := os.Stat(filepath.Join(dir, "leveldata.txt")); err != nil {
		t.Skipf("shipped textdata not present in this checkout: %v", err)
	}
	return dir
}

// e2eServer is one live composition: real store, real transport, the
// server.go deps wiring for the bootstrap + progression lanes.
/*
================
e2eServer
================
*/
type e2eServer struct {
	srv       *transport.Server
	authority *store.Store
}

/*
==================
startProgressionServer

startProgressionServer opens the authority store in dir and stands up
the transport with the production composition of this plane. seed (nil
on a reopen) is created through store.CreateCharacter, i.e. the same
creation door the agent API uses.
==================
*/
func startProgressionServer(t *testing.T, dir string, seed *enterworld.Character) e2eServer {
	t.Helper()

	textdataDir := shippedTextdataDir(t)
	textdata, err := enterworld.LoadTextdataCatalogs(textdataDir)
	if err != nil {
		t.Fatalf("authoritative textdata readiness: %v", err)
	}
	authority, err := store.Open(dir, store.Options{
		DefaultSkills: enterworld.DefaultSkillSeeder(textdata.Skills),
	})
	if err != nil {
		t.Fatalf("store.Open(%s): %v", dir, err)
	}
	t.Cleanup(authority.Close)

	if seed != nil && len(authority.Characters().CharactersForDivision(e2eDivision)) == 0 {
		if err := authority.CreateCharacter(e2eDivision, "test-account", seed); err != nil {
			t.Fatalf("CreateCharacter: %v", err)
		}
	}

	// Mirror server.go: ONE deps pointer, with the store-owned collaborators
	// and the authority tables attached to the shared pointer before Register
	// captures below. Roster degrades to the empty fallback exactly like
	// a missing roster.json in production (visual loadout is not this
	// plane's concern).
	deps := &enterworld.Deps{
		Roster:       &enterworld.Roster{},
		Characters:   authority.Characters(),
		Items:        textdata.Items,
		Levels:       textdata.Levels,
		Skills:       textdata.Skills,
		MagicOptions: textdata.MagicOptions,
	}
	deps.ResolveDivisionID = enterworld.DevResolveDivisionIDFromCatalog(deps.Characters)
	deps.PlayerBaseStats = func(character *enterworld.Character) (wire.BaseStats, error) {
		return combat.PlayerBaseStats(character, combat.Catalogs{Items: deps.Items, Skills: deps.Skills})
	}
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
	enterworld.Register(srv.Hub, deps)
	progression.Register(srv.Hub, deps)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shutdownServer(t, srv) })
	return e2eServer{srv: srv, authority: authority}
}

/*
================
shutdownServer
================
*/
func shutdownServer(t *testing.T, srv *transport.Server) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

// --- WebSocket client (the production WebSocket protocol) -----------------

/*
================
dialProgressionWS
================
*/
func dialProgressionWS(t *testing.T, srv *transport.Server) *websocket.Conn {
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
==================
nextFrame

nextFrame reads the next game frame, skipping only transport keepalive.
Every other frame is returned as-is, so a stray emission fails the
caller's opcode assertion instead of being silently absorbed.
==================
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
==================
expectFrame

expectFrame asserts the NEXT frame on the wire is opcode and returns its
payload. The strictness is the point: dispatch is serial per session and
all responses ride one ordered reliable queue, so "the next frame is the
one the contract promises" is exactly the no-stray-emission proof.
==================
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
helloProgressionWS
================
*/
func helloProgressionWS(t *testing.T, c *websocket.Conn) {
	t.Helper()
	sendFrame(t, c, transport.OpHello, transport.EncodeHello(transport.Hello{AdmissionToken: []byte("test-admission")}))
	payload := expectFrame(t, c, transport.OpWelcome, "handshake")
	if _, err := transport.DecodeWelcome(payload); err != nil {
		t.Fatalf("decoding WELCOME: %v", err)
	}
}

// --- EnterWorld + char-data parsing ----------------------------------------

/*
==================
charData

charData is the subset of the 0x32B3 chunk this lane asserts, parsed
with BuildLocalPlayerEntryPayload's exact field order (internal/game/enterworld/
wire.go; itself byte-pinned to the native reader sub_863880/sub_866e50).
==================
*/
type charData struct {
	Level       uint8
	MaxLevel    uint8
	Exp         uint64
	SkillExp    uint32
	SkillPoints uint32
	StatPoints  uint16
	CurrentHP   uint32
	CurrentMP   uint32
	Masteries   map[uint32]uint8
	Skills      []uint32
	// The 0x32B3 quest block's three section id lists (sub_8673d0):
	// walked so a populated section cannot hide behind the skill pins.
	CompletedQuests []uint32
	ActiveQuests    []uint32
	TrackedQuests   []uint32
}

/*
================
parseCharData
================
*/
func parseCharData(t *testing.T, payload []byte) charData {
	t.Helper()
	r := wire.NewReader(payload)
	fail := func(what string, err error) {
		if err != nil {
			t.Fatalf("0x32B3 parse: %s: %v", what, err)
		}
	}
	skip := func(n int, what string) {
		_, err := r.Bytes(n)
		fail(what, err)
	}

	// Base runtime block, in writer order: modelRef u32, bodyShape u8,
	// level u8, maxLevel u8, exp u64, skillExp u32, gold u64. The
	// level/exp fields are parsed (not skipped) since the levelup wave:
	// the levelling e2e asserts the persisted walk through them.
	skip(4+1, "modelRef + bodyShape")
	out := charData{Masteries: map[uint32]uint8{}}
	var err error
	out.Level, err = r.U8()
	fail("level", err)
	out.MaxLevel, err = r.U8()
	fail("maxLevel", err)
	out.Exp, err = r.U64()
	fail("exp", err)
	out.SkillExp, err = r.U32()
	fail("skillExp", err)
	skip(8, "gold")
	out.SkillPoints, err = r.U32()
	fail("skillPoints", err)
	out.StatPoints, err = r.U16()
	fail("statPoints", err)
	skip(1+4, "berserk + wireValue844")
	out.CurrentHP, err = r.U32()
	fail("currentHp", err)
	out.CurrentMP, err = r.U32()
	fail("currentMp", err)
	skip(1+1+2+4, "pk block")

	// Inventory block: u8 capacity, u8 count, per item u8 slot + the
	// VARIABLE-length CSOItem equipment body (17 fixed bytes + u8
	// magicParamCount + count x u64 - BuildEquipmentItemBody /
	// sub_78b1b0).
	skipItemBody := func(what string) {
		skip(4+1+8+4, what+" body head")
		magicCount, err := r.U8()
		fail(what+" magic count", err)
		skip(int(magicCount)*8, what+" magic params")
	}
	skip(1, "inventory capacity")
	invCount, err := r.U8()
	fail("inventory count", err)
	for i := 0; i < int(invCount); i++ {
		skip(1, "inventory slot")
		skipItemBody("inventory item")
	}

	// Avatar block: u8 capacity + u8 count + count x [u8 slot + the same
	// variable CSOItem body] (sub_8675f0 avatar loop).
	skip(1, "avatar capacity")
	avatarCount, err := r.U8()
	fail("avatar count", err)
	for i := 0; i < int(avatarCount); i++ {
		skip(1, "avatar slot")
		skipItemBody("avatar item")
	}

	// Mastery list: u8 count, per row marker 1 + u32 id + u8 level,
	// terminated by marker 2 (sub_866e50's first loop).
	masteryCount, err := r.U8()
	fail("mastery count", err)
	for i := 0; i < int(masteryCount); i++ {
		marker, err := r.U8()
		fail("mastery marker", err)
		if marker != 1 {
			t.Fatalf("0x32B3 mastery row %d marker = %d, want 1", i, marker)
		}
		id, err := r.U32()
		fail("mastery id", err)
		level, err := r.U8()
		fail("mastery level", err)
		out.Masteries[id] = level
	}
	terminator, err := r.U8()
	fail("mastery terminator", err)
	if terminator != 2 {
		t.Fatalf("0x32B3 mastery terminator = %d, want 2", terminator)
	}

	// Skill list: u8 count, per row marker 1 + u32 id + u8 value(1),
	// terminated by marker 2 (sub_866e50's second loop).
	skillCount, err := r.U8()
	fail("skill count", err)
	for i := 0; i < int(skillCount); i++ {
		marker, err := r.U8()
		fail("skill marker", err)
		if marker != 1 {
			t.Fatalf("0x32B3 skill row %d marker = %d, want 1", i, marker)
		}
		id, err := r.U32()
		fail("skill id", err)
		value, err := r.U8()
		fail("skill value", err)
		if value != 1 {
			t.Fatalf("0x32B3 skill row %d value byte = %d, want the native ctor default 1", i, value)
		}
		out.Skills = append(out.Skills, id)
	}
	terminator, err = r.U8()
	fail("skill terminator", err)
	if terminator != 2 {
		t.Fatalf("0x32B3 skill terminator = %d, want 2", terminator)
	}

	// Quest block (sub_8673d0's three counted sections; the composer
	// emits all three for real since the 2026-07-29 quest wave, so this
	// parser walks the full grammar instead of stopping blind after the
	// skills - a populated section cannot hide from the e2e assertions).
	// Section 1: completed refs.
	completedCount, err := r.U8()
	fail("completed quest count", err)
	for i := 0; i < int(completedCount); i++ {
		id, err := r.U32()
		fail("completed quest id", err)
		out.CompletedQuests = append(out.CompletedQuests, id)
	}
	// Section 2: active SQuestInfo records (sub_788210 flag-gated body).
	activeCount, err := r.U8()
	fail("active quest count", err)
	for i := 0; i < int(activeCount); i++ {
		id, err := r.U32()
		fail("active quest refId", err)
		out.ActiveQuests = append(out.ActiveQuests, id)
		skip(2, "active quest u08+u09")
		flags, err := r.U8()
		fail("active quest flags", err)
		if flags&0x04 != 0 {
			skip(4, "active quest progress")
		}
		if flags&0x08 != 0 {
			skip(1, "active quest u10")
		}
		if flags&0x10 != 0 {
			contentsCount, err := r.U8()
			fail("active quest contents count", err)
			for node := 0; node < int(contentsCount); node++ {
				skip(2, "contents tag+kind")
				strLen, err := r.U16()
				fail("contents string length", err)
				skip(int(strLen), "contents string")
				objectiveCount, err := r.U8()
				fail("contents objective count", err)
				if objectiveCount != 0xff {
					skip(int(objectiveCount)*4, "contents objective values")
				}
			}
		}
		if flags&0x40 != 0 {
			targetCount, err := r.U8()
			fail("active quest target count", err)
			skip(int(targetCount)*4, "active quest target ids")
		}
	}
	// Section 3: tracked records (optional u32 iff flags&0x02).
	trackedCount, err := r.U8()
	fail("tracked quest count", err)
	for i := 0; i < int(trackedCount); i++ {
		id, err := r.U32()
		fail("tracked quest refId", err)
		out.TrackedQuests = append(out.TrackedQuests, id)
		flags, err := r.U8()
		fail("tracked quest flags", err)
		skip(1+2+6, "tracked quest valueA+word+tail")
		if flags&0x02 != 0 {
			skip(4, "tracked quest optional")
		}
	}
	return out
}

/*
==================
blobCharacter

blobCharacter is the 0x0007 blob's character-snapshot subset this test
asserts (STR/INT ride the snapshot JSON and the 0x343C block, never the
0x32B3 chunk).
==================
*/
type blobCharacter struct {
	Strength  *int64   `json:"strength"`
	Intellect *int64   `json:"intellect"`
	Skills    []uint32 `json:"skills"`
}

/*
==================
enterWorld

enterWorld performs the 0x0006 bind and consumes the full bootstrap
frame sequence in its frozen order, returning the parsed 0x32B3 chunk
and the blob's character snapshot.
==================
*/
func enterWorld(t *testing.T, c *websocket.Conn) (charData, blobCharacter) {
	t.Helper()
	return enterWorldAs(t, c, e2eCharName)
}

// enterWorldAs is enterWorld for an explicit character name (the levelling
// e2e binds its own level-1 character).
/*
================
enterWorldAs
================
*/
func enterWorldAs(t *testing.T, c *websocket.Conn, charName string) (charData, blobCharacter) {
	t.Helper()
	sendFrame(t, c, transport.OpEnterWorld, transport.EncodeEnterWorld(
		entryauth.NewAuthenticatedEntryFixture(t, e2eDivision, charName),
	))

	result, err := transport.DecodeEnterWorldResult(expectFrame(t, c, transport.OpEnterWorldResult, "enter world"))
	if err != nil {
		t.Fatalf("decoding 0x0007: %v", err)
	}
	if !result.OK {
		t.Fatalf("enter world refused: nativeErrorCode=%#x", result.NativeErrorCode)
	}
	var blob struct {
		V         int `json:"v"`
		Bootstrap struct {
			Character blobCharacter `json:"character"`
		} `json:"bootstrap"`
	}
	if err := json.Unmarshal(result.Blob, &blob); err != nil {
		t.Fatalf("0x0007 blob does not parse: %v", err)
	}

	expectFrame(t, c, enterworld.OpcodeResetClient, "bootstrap[0]")
	expectFrame(t, c, enterworld.OpcodeMyCharacterData, "bootstrap[1]")
	chunk := expectFrame(t, c, enterworld.OpcodeMyCharacterChunk, "bootstrap[2]")
	expectFrame(t, c, enterworld.OpcodeMyCharacterFlush, "bootstrap[3]")
	expectFrame(t, c, enterworld.OpcodeServerClockGidLatch, "bootstrap[4]")
	expectFrame(t, c, enterworld.OpcodeObjectListStart, "bootstrap[5]")
	expectFrame(t, c, enterworld.OpcodeObjectListFinalize, "bootstrap[6]")
	wiretest.ActivateWorld(t, c, "enter world "+charName)
	return parseCharData(t, chunk), blob.Bootstrap.Character
}

// --- payload assertions ------------------------------------------------------

/*
==================
assertStatBlock

assertStatBlock checks the complete 0x343C projection: the first eight
fields feed the Character pane, while +0x18..+0x22 carry maxima and the
applied STR/INT words.
==================
*/
func assertStatBlock(t *testing.T, payload []byte, wantStr, wantInt uint16, wantMaxHP, wantMaxMP uint32) {
	t.Helper()
	if len(payload) != wire.BaseStatsSize {
		t.Fatalf("0x343C block = %d bytes, want %d", len(payload), wire.BaseStatsSize)
	}
	phyMin := binary.LittleEndian.Uint32(payload[0x00:])
	phyMax := binary.LittleEndian.Uint32(payload[0x04:])
	magMin := binary.LittleEndian.Uint32(payload[0x08:])
	magMax := binary.LittleEndian.Uint32(payload[0x0c:])
	if phyMin == 0 || phyMax < phyMin || magMin == 0 || magMax < magMin {
		t.Fatalf("0x343C attack projection = phy %d..%d mag %d..%d, want nonzero ordered ranges", phyMin, phyMax, magMin, magMax)
	}
	for offset := 0x10; offset <= 0x16; offset += 2 {
		if got := binary.LittleEndian.Uint16(payload[offset:]); got == 0 {
			t.Fatalf("0x343C combat word +0x%02X is zero", offset)
		}
	}
	if got := binary.LittleEndian.Uint32(payload[0x18:]); got != wantMaxHP {
		t.Fatalf("0x343C maxHP = %d, want %d", got, wantMaxHP)
	}
	if got := binary.LittleEndian.Uint32(payload[0x1c:]); got != wantMaxMP {
		t.Fatalf("0x343C maxMP = %d, want %d", got, wantMaxMP)
	}
	if got := binary.LittleEndian.Uint16(payload[0x20:]); got != wantStr {
		t.Fatalf("0x343C applied STR word (+0x20) = %d, want %d", got, wantStr)
	}
	if got := binary.LittleEndian.Uint16(payload[0x22:]); got != wantInt {
		t.Fatalf("0x343C applied INT word (+0x22) = %d, want %d", got, wantInt)
	}
}

/*
==================
assertSkillPointsRefresh

assertSkillPointsRefresh checks a 0x30B3 payload is the type-2 ABSOLUTE
skill-point refresh with notify=0 (the purchase posture: the op's own
ack is the player-visible feedback).
==================
*/
func assertSkillPointsRefresh(t *testing.T, payload []byte, wantSP uint32, what string) {
	t.Helper()
	if len(payload) != 6 || payload[0] != wire.PointsTypeSkill {
		t.Fatalf("%s: 0x30B3 payload = % X, want type 2 + u32 + notify", what, payload)
	}
	if got := binary.LittleEndian.Uint32(payload[1:]); got != wantSP {
		t.Fatalf("%s: 0x30B3 absolute SP = %d, want %d", what, got, wantSP)
	}
	if payload[5] != 0 {
		t.Fatalf("%s: 0x30B3 notify byte = %d, want 0 (silent refresh)", what, payload[5])
	}
}

/*
================
assertBytes
================
*/
func assertBytes(t *testing.T, got []byte, want []byte, what string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = % X, want % X", what, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s = % X, want % X", what, got, want)
		}
	}
}

/*
================
masteryRequest
================
*/
func masteryRequest(masteryID uint32, amount uint8) []byte {
	return wire.NewWriter(5).U32(masteryID).U8(amount).Payload()
}

/*
================
skillRequest
================
*/
func skillRequest(skillID uint32) []byte {
	return wire.NewWriter(4).U32(skillID).Payload()
}

/*
================
e2eInt64
================
*/
func e2eInt64(v int64) *int64 { return &v }

// --- the test ----------------------------------------------------------------

/*
================
TestProgressionPlaneEndToEndOverWire
================
*/
func TestProgressionPlaneEndToEndOverWire(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "authority")

	// Preflight: re-assert the shipped-table constants the script depends
	// on through the production loaders, so a moved column fails HERE.
	textdata := shippedTextdataDir(t)
	levels := enterworld.NewTextdataLevels(textdata)
	// Pricing reads the row of the level being LEFT (the client's own
	// tooltip/gate indexing): train 6->7 charges row 6, train 7->8
	// charges row 7.
	if cost, ok := levels.SkillPointCost(6); !ok || cost != 4 {
		t.Fatalf("shipped leveldata: row 6 = %d/%v SP, want 4/true (column moved?)", cost, ok)
	}
	if cost, ok := levels.SkillPointCost(7); !ok || cost != 5 {
		t.Fatalf("shipped leveldata: row 7 = %d/%v SP, want 5/true", cost, ok)
	}
	skills := enterworld.NewTextdataSkills(textdata)
	punch, ok := skills.SkillByID(skillPunchID)
	if !ok || punch.Group != 172 || punch.Level != 1 || punch.SPCost != 0 || punch.Masteries[0].ID != 0 {
		t.Fatalf("shipped skilldata row 1 = %+v/%v, want the requirement-free 0-SP PUNCH", punch, ok)
	}
	smashA, ok := skills.SkillByID(skillSmashA1ID)
	if !ok || smashA.Group != 174 || smashA.Level != 1 || smashA.SPCost != 2 ||
		smashA.Masteries[0] != (enterworld.SkillRequirement{ID: bicheonMastery, Level: 5}) {
		t.Fatalf("shipped skilldata row 3 = %+v/%v, want grp 174 lvl 1 sp 2 mastery 257@5", smashA, ok)
	}
	chainA, ok := skills.SkillByID(skillChainA1SID)
	if !ok || chainA.Group != 177 || chainA.Level != 1 || chainA.SPCost != 5 ||
		chainA.Masteries[0] != (enterworld.SkillRequirement{ID: bicheonMastery, Level: 7}) {
		t.Fatalf("shipped skilldata row 6 = %+v/%v, want grp 177 lvl 1 sp 5 mastery 257@7", chainA, ok)
	}

	// The seed: CreateCharacter fills Strength/Intellect with the creation
	// base (20/20) and respects an explicit mastery set, so Bicheon rides
	// pre-trained to 6 with the rest of the CH racial rows at the seed
	// level 0. The
	// 500/400 maxima are DELIBERATE decoys: maxima are derived from
	// level+stats now, so these persisted values must never surface on
	// the wire (the assertStatBlock below would catch them).
	seed := &enterworld.Character{
		Name:          e2eCharName,
		ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		RaceIndex:     e2eInt64(enterworld.RaceChina),
		Gender:        e2eInt64(enterworld.GenderMale),
		Level:         e2eInt64(40),
		StatPoints:    e2eInt64(1),
		SkillPoints:   e2eInt64(8),
		// The store adds the current-schema racial base skills at creation.
		Skills: []uint32{},
	}
	seed.Masteries = enterworld.DefaultMasteries(enterworld.RaceKeyChina)
	for i := range seed.Masteries {
		if seed.Masteries[i].ID == bicheonMastery {
			seed.Masteries[i].Level = 6
		}
	}

	// ---- session 1: the live exercise ----
	first := startProgressionServer(t, dir, seed)
	conn := dialProgressionWS(t, first.srv)
	helloProgressionWS(t, conn)

	baseline, blobBefore := enterWorld(t, conn)
	if baseline.SkillPoints != 8 || baseline.StatPoints != 1 {
		t.Fatalf("baseline pools = %d SP / %d stat points, want 8 / 1", baseline.SkillPoints, baseline.StatPoints)
	}
	if got := baseline.Masteries[bicheonMastery]; got != 6 {
		t.Fatalf("baseline Bicheon level = %d, want the seeded 6", got)
	}
	if len(baseline.Masteries) != 7 {
		t.Fatalf("baseline mastery rows = %d, want the 7 CH racial rows", len(baseline.Masteries))
	}
	wantBaselineSkills := []uint32{1, 2, 40, 70}
	if !reflect.DeepEqual(baseline.Skills, wantBaselineSkills) {
		t.Fatalf("baseline skill list = %v, want current-schema bases %v", baseline.Skills, wantBaselineSkills)
	}
	if blobBefore.Strength == nil || *blobBefore.Strength != enterworld.BaseStat {
		t.Fatalf("baseline blob strength = %v, want the creation base %d", blobBefore.Strength, enterworld.BaseStat)
	}

	// +STR (0x727A, empty body): the [01] ack moves the client's remaining
	// count, the 0x343C block moves the live STR word, and nothing else
	// rides the burst (the very next frame after the block is the NEXT
	// request's ack, so a 0x30B3 type-3 double-apply cannot hide).
	// The maxima are DERIVED from the raised stats, never the seeded
	// 500/400 history fields: level 40, STR 21 -> trunc(1.02^39*210) =
	// 454, INT 20 -> trunc(1.02^39*200) = 432 - the +STR moved the HP
	// maximum in the same block that carries the raised word.
	sendFrame(t, conn, wire.OpAllocStrRequest, nil)
	assertBytes(t, expectFrame(t, conn, wire.OpAllocStrResponse, "+STR ack"),
		[]byte{wire.ResultSuccess}, "+STR ack")
	assertStatBlock(t, expectFrame(t, conn, wire.OpBaseStats, "+STR stat block"),
		uint16(enterworld.BaseStat)+1, uint16(enterworld.BaseStat), 454, 432)

	// +STR with the pool exhausted: typed refusal, no follow-up frames.
	sendFrame(t, conn, wire.OpAllocStrRequest, nil)
	assertBytes(t, expectFrame(t, conn, wire.OpAllocStrResponse, "exhausted +STR ack"),
		[]byte{wire.ResultError, wire.ErrCodeStatAllocRefused}, "exhausted +STR refusal")

	// Mastery train 6->7 (0x7165): ack carries the POST-training level,
	// then the absolute SP refresh (8 - 4 = 4; the cost is row 6, the
	// level being left).
	sendFrame(t, conn, wire.OpMasteryLevelUpRequest, masteryRequest(bicheonMastery, 1))
	assertBytes(t, expectFrame(t, conn, wire.OpMasteryLevelUpResponse, "mastery ack"),
		wire.NewWriter(6).U8(wire.ResultSuccess).U32(bicheonMastery).U8(7).Payload(), "mastery train ack")
	assertSkillPointsRefresh(t, expectFrame(t, conn, wire.OpPointsUpdate, "mastery SP refresh"), 4, "mastery train")

	// Mastery train 7->8 costs row 7 = 5 > 4: the 07:02 insufficient-SP
	// refusal, alone.
	sendFrame(t, conn, wire.OpMasteryLevelUpRequest, masteryRequest(bicheonMastery, 1))
	assertBytes(t, expectFrame(t, conn, wire.OpMasteryLevelUpResponse, "mastery SP refusal"),
		[]byte{wire.ResultError, wire.ErrCodeMasterySkillPoints}, "mastery SP refusal")

	// Skill learn SMASH_A_01 (0x72CB): mastery 257@7 >= 5, SP 4 >= 2.
	// Ack echoes the id, then the absolute refresh (4 - 2 = 2).
	sendFrame(t, conn, wire.OpSkillLearnRequest, skillRequest(skillSmashA1ID))
	assertBytes(t, expectFrame(t, conn, wire.OpSkillLearnResponse, "skill learn ack"),
		wire.NewWriter(5).U8(wire.ResultSuccess).U32(skillSmashA1ID).Payload(), "skill learn ack")
	assertSkillPointsRefresh(t, expectFrame(t, conn, wire.OpPointsUpdate, "skill SP refresh"), 2, "skill learn")

	// Skill learn CHAIN_A_1S costs 5 > 2: the 05:0a insufficient-SP
	// refusal (its mastery gate 257@7 passes, so the SP code is the one
	// the client maps to a retail notice).
	sendFrame(t, conn, wire.OpSkillLearnRequest, skillRequest(skillChainA1SID))
	assertBytes(t, expectFrame(t, conn, wire.OpSkillLearnResponse, "skill SP refusal"),
		[]byte{wire.ResultError, wire.ErrCodeSkillLearnSP}, "skill SP refusal")

	// Re-learning the same skill: the duplicate refusal the client's
	// success-ack assert (@0x0075bb71) demands instead of a second [01].
	sendFrame(t, conn, wire.OpSkillLearnRequest, skillRequest(skillSmashA1ID))
	assertBytes(t, expectFrame(t, conn, wire.OpSkillLearnResponse, "duplicate learn refusal"),
		[]byte{wire.ResultError, 0x0c}, "duplicate learn refusal")

	// PUNCH is a required China base skill, so learning it again refuses
	// as a duplicate and cannot move the pool.
	sendFrame(t, conn, wire.OpSkillLearnRequest, skillRequest(skillPunchID))
	assertBytes(t, expectFrame(t, conn, wire.OpSkillLearnResponse, "base-skill duplicate refusal"),
		[]byte{wire.ResultError, 0x09}, "base-skill duplicate refusal")

	// A non-mutating FIFO barrier surfaces any stray response without
	// replaying the one-shot world-admission lifecycle.
	wiretest.AssertQueueDrained(t, conn, "final progression barrier")

	// The whole script must have run UNDER the production rate limiter
	// (25/s, burst 75): a dropped frame is silent by design, so a zero
	// throttle counter is the proof no request or response above was
	// clamped rather than answered.
	if dropped := first.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s); the script must stay under the production burst", dropped)
	}

	sendFrame(t, conn, transport.OpBye, []byte{transport.ByeReasonNormal})
	conn.Close()

	// ---- the reboot: close everything, reopen from disk ----
	shutdownServer(t, first.srv)
	first.authority.Close()

	second := startProgressionServer(t, dir, nil)

	// The store's own view first (the door-read contract): the spends
	// and the raised stat survived as PERSISTED state, not as the dead
	// process's memory.
	second.authority.ReadCharacters(e2eDivision, func(characters []*enterworld.Character) {
		if len(characters) != 1 {
			t.Fatalf("characters after reboot = %d, want 1", len(characters))
		}
		c := characters[0]
		if got := enterworld.CharacterStrength(c); got != enterworld.BaseStat+1 {
			t.Errorf("restored strength = %d, want %d", got, enterworld.BaseStat+1)
		}
		if got := enterworld.CharacterIntellect(c); got != enterworld.BaseStat {
			t.Errorf("restored intellect = %d, want the untouched %d", got, enterworld.BaseStat)
		}
	})

	// A fresh client on the fresh process: the 0x32B3 char-data now
	// carries everything the wire session bought, and the blob snapshot
	// carries the raised STR word.
	conn2 := dialProgressionWS(t, second.srv)
	helloProgressionWS(t, conn2)
	restored, blobAfter := enterWorld(t, conn2)

	if restored.SkillPoints != 2 {
		t.Fatalf("restored SP = %d, want 2 (8 - 4 mastery - 2 learn)", restored.SkillPoints)
	}
	if restored.StatPoints != 0 {
		t.Fatalf("restored stat points = %d, want 0 (spent points must not return across a restart)", restored.StatPoints)
	}
	if got := restored.Masteries[bicheonMastery]; got != 7 {
		t.Fatalf("restored Bicheon level = %d, want the trained 7", got)
	}
	wantRestoredSkills := []uint32{1, 2, 40, 70, skillSmashA1ID}
	if !reflect.DeepEqual(restored.Skills, wantRestoredSkills) {
		t.Fatalf("restored 0x32B3 skill list = %v, want %v", restored.Skills, wantRestoredSkills)
	}
	if blobAfter.Strength == nil || *blobAfter.Strength != enterworld.BaseStat+1 {
		t.Fatalf("restored blob strength = %v, want %d", blobAfter.Strength, enterworld.BaseStat+1)
	}
	if blobAfter.Intellect == nil || *blobAfter.Intellect != enterworld.BaseStat {
		t.Fatalf("restored blob intellect = %v, want %d", blobAfter.Intellect, enterworld.BaseStat)
	}

	// And the refusal is durable too: the reboot must not hand the spent
	// stat point back.
	sendFrame(t, conn2, wire.OpAllocStrRequest, nil)
	assertBytes(t, expectFrame(t, conn2, wire.OpAllocStrResponse, "post-reboot +STR ack"),
		[]byte{wire.ResultError, wire.ErrCodeStatAllocRefused}, "post-reboot +STR refusal")

	if dropped := second.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s) on the reopened server", dropped)
	}

	sendFrame(t, conn2, transport.OpBye, []byte{transport.ByeReasonNormal})
}

// --- the levelling path (levelup wave, LANE-5) ------------------------------

// e2eLevelCharName is the levelling test's own character (12 chars, the
// top of the native 2..12 creation window).
