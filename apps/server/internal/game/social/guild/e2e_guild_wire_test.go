package guild_test

// End-to-end exercise of the guild seed over the REAL transport against
// the REAL authority store (the community e2e_letter_wire_test.go
// composition, TWO live sessions for the presence-derived offline flags
// plus a server reboot for the persistence witness):
//
//	store seed   -> a guild + two memberships installed directly through
//	                the store door (the honest Phase C state source -
//	                no create opcode exists in this lane) and the
//	                GuildID FK set on both character records;
//	B enters     -> B's stream carries the 0x32C4 seed AFTER the letter
//	                seed (both members offline at B's compose time: the
//	                presence bind lands after the frames are composed);
//	A enters     -> A's 0x32C4 reads B ONLINE (flag 0) and A itself
//	                offline (flag 1), byte-equal to the hand-rolled
//	                oracle;
//	reboot       -> store + server torn down and reopened: the guild
//	                TABLES survive, and A's fresh enter-world carries
//	                the 0x32C4 again (now all-offline).
//
// Guild E2E tests intentionally stay package-serial. Each owns real TCP/UDP
// listeners and a SQLite authority; running all seven concurrently made their
// fixed wire deadlines measure scheduler saturation instead of correctness.

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
	"opensro.online/server/internal/game/enterworld"
	wiretest "opensro.online/server/internal/game/internal"
	presence "opensro.online/server/internal/game/social"
	"opensro.online/server/internal/game/social/community"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

const (
	guildE2EDivision = "global-official"
	guildE2ENameA    = "e2eGldAlfa"
	guildE2ENameB    = "e2eGldBravo"
	guildE2EGuildID  = int64(1)
)

type guildE2EServer struct {
	srv       *transport.Server
	authority *store.Store
}

// startGuildServer opens the authority store in dir and stands up the
// transport with the bootstrap + guild lanes composed like server.go:
// one deps pointer; store collaborators, the guild door and the
// division-aware seed seam assigned before the registers capture it;
// OnWorldBound claiming the same presence bind key the offline flags
// derive through.
func startGuildServer(t *testing.T, dir string, seeds []*enterworld.Character) guildE2EServer {
	t.Helper()

	authority, err := store.Open(dir, store.Options{DefaultSkills: guildSkillSeeder})
	if err != nil {
		t.Fatalf("store.Open(%s): %v", dir, err)
	}
	t.Cleanup(authority.Close)

	existing := map[string]bool{}
	for _, c := range authority.Characters().CharactersForDivision(guildE2EDivision) {
		existing[c.Name] = true
	}
	for _, seed := range seeds {
		if seed == nil || existing[seed.Name] {
			continue
		}
		if err := authority.CreateCharacter(guildE2EDivision, "test-account", seed); err != nil {
			t.Fatalf("CreateCharacter(%s): %v", seed.Name, err)
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
	srv.Hub.SetHelloAuth(func([]byte) (transport.AdmissionIdentity, error) {
		return transport.AdmissionIdentity{AccountID: "test-account", ShardID: "global-official"}, nil
	})
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
	// The presence bind: the same key server.go's OnWorldBound claims -
	// without it the offline flags cannot see the online member.
	deps.OnWorldBound = func(s *transport.Session, divisionID string, character *enterworld.Character) {
		srv.Hub.BindExclusive(presence.BindKey(divisionID, character.Name), s)
	}
	enterworld.Register(srv.Hub, deps)
	guild.Register(srv.Hub, deps, directory, nil, nil)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { guildShutdownServer(t, srv) })
	return guildE2EServer{srv: srv, authority: authority}
}

func guildShutdownServer(t *testing.T, srv *transport.Server) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func guildDialWS(t *testing.T, srv *transport.Server) *websocket.Conn {
	t.Helper()
	url := fmt.Sprintf("ws://%s%s", srv.WSAddr(), transport.PathWS)
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("websocket dial %s: %v", url, err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func guildSendFrame(t *testing.T, c *websocket.Conn, opcode uint16, payload []byte) {
	t.Helper()
	f := transport.Frame{Opcode: opcode, Payload: payload}
	if err := c.WriteMessage(websocket.BinaryMessage, f.Encode()); err != nil {
		t.Fatalf("writing frame 0x%04X: %v", opcode, err)
	}
}

func guildNextFrame(t *testing.T, c *websocket.Conn, what string) transport.Frame {
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

func guildExpectFrame(t *testing.T, c *websocket.Conn, opcode uint16, what string) []byte {
	t.Helper()
	f := guildNextFrame(t, c, what)
	if f.Opcode != opcode {
		t.Fatalf("%s: next frame = 0x%04X payload % X, want opcode 0x%04X", what, f.Opcode, f.Payload, opcode)
	}
	return f.Payload
}

func guildHelloWS(t *testing.T, c *websocket.Conn) {
	t.Helper()
	guildSendFrame(t, c, transport.OpHello, transport.EncodeHello(transport.Hello{AdmissionToken: []byte("test-admission")}))
	payload := guildExpectFrame(t, c, transport.OpWelcome, "handshake")
	if _, err := transport.DecodeWelcome(payload); err != nil {
		t.Fatalf("decoding WELCOME: %v", err)
	}
}

// guildEnterWorld performs the 0x0006 bind as name, consumes the frozen
// bootstrap sequence and the friend + letter seeds, and returns the
// 0x32C4 guild seed payload - asserting it arrives immediately AFTER the
// letter seed (the pinned seed order friend, letter, guild).
func guildEnterWorld(t *testing.T, c *websocket.Conn, name string) []byte {
	t.Helper()
	guildEnterWorldBootstrap(t, c, name)
	seed := guildExpectFrame(t, c, guild.OpGuildInfo, "guild info seed")
	wiretest.ActivateWorld(t, c, "enter world "+name)
	// The exclusive bind and the world-bound hooks run at the tail of the
	// game-ready handler; drain it so callers see a bound session.
	wiretest.AssertQueueDrained(t, c, "world-bound tail "+name)
	return seed
}

// guildEnterWorldSeeds performs the 0x0006 bind as name and consumes the
// frozen bootstrap sequence plus the friend + letter seeds, stopping
// BEFORE any guild frame - the shared prefix of the in-guild entry
// (guildEnterWorld appends the 0x32C4 expectation) and the guildless
// entry (whose very next frame must NOT be a 0x32C4; the mutator e2e
// proves that by making the next frame an answer it elicits itself).
func guildEnterWorldSeeds(t *testing.T, c *websocket.Conn, name string) {
	t.Helper()
	guildEnterWorldBootstrap(t, c, name)
	wiretest.ActivateWorld(t, c, "enter world "+name)
	wiretest.AssertQueueDrained(t, c, "world-bound tail "+name)
}

// guildEnterWorldBootstrap consumes only the common enter-world prefix. The
// caller owns optional domain seeds, which must be consumed before crossing
// GameReady because they were queued as part of EnterWorld.
func guildEnterWorldBootstrap(t *testing.T, c *websocket.Conn, name string) {
	t.Helper()
	guildSendFrame(t, c, transport.OpEnterWorld, transport.EncodeEnterWorld(
		entryauth.NewAuthenticatedEntryFixture(t, guildE2EDivision, name),
	))
	result, err := transport.DecodeEnterWorldResult(guildExpectFrame(t, c, transport.OpEnterWorldResult, "enter world "+name))
	if err != nil {
		t.Fatalf("decoding 0x0007: %v", err)
	}
	if !result.OK {
		t.Fatalf("enter world %s refused: nativeErrorCode=%#x", name, result.NativeErrorCode)
	}
	guildExpectFrame(t, c, enterworld.OpcodeResetClient, "bootstrap[0]")
	guildExpectFrame(t, c, enterworld.OpcodeMyCharacterData, "bootstrap[1]")
	guildExpectFrame(t, c, enterworld.OpcodeMyCharacterChunk, "bootstrap[2]")
	guildExpectFrame(t, c, enterworld.OpcodeMyCharacterFlush, "bootstrap[3]")
	guildExpectFrame(t, c, enterworld.OpcodeServerClockGidLatch, "bootstrap[4]")
	guildExpectFrame(t, c, enterworld.OpcodeObjectListStart, "bootstrap[5]")
	guildExpectFrame(t, c, enterworld.OpcodeObjectListFinalize, "bootstrap[6]")
	if got := guildExpectFrame(t, c, community.OpFriendRosterPush, "friend roster seed"); !bytes.Equal(got, []byte{0x00}) {
		t.Fatalf("0x3769 seed payload = % X, want the empty roster [00]", got)
	}
	if got := guildExpectFrame(t, c, community.OpLetterListAnswer, "letter list seed"); !bytes.Equal(got, []byte{0x01, 0x00}) {
		t.Fatalf("0xB3CD seed payload = % X, want the empty list [01 00]", got)
	}
}

// guildE2ECharacter resolves a live store record by name.
func guildE2ECharacter(t *testing.T, authority *store.Store, name string) *enterworld.Character {
	t.Helper()
	var found *enterworld.Character
	authority.ReadCharacters(guildE2EDivision, func(characters []*enterworld.Character) {
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

// guildE2EOracle hand-rolls the expected 0x32C4 bytes for the seeded
// two-member guild with the given offline flags (A first, B second).
func guildE2EOracle(alfaID, bravoID int64, alfaOffline, bravoOffline uint8) []byte {
	oracle := &oracle32C4{}
	oracle.u32(uint32(guildE2EGuildID))
	oracle.str("WallWatch")
	oracle.u8(1)
	oracle.u32(5000)
	oracle.str("e2e subject")
	oracle.str("e2e contents")
	oracle.u32(0x1234)
	oracle.u8(0)
	oracle.u8(2)
	oracle.u32(uint32(500000 + alfaID))
	oracle.str(guildE2ENameA)
	oracle.u8(0)
	oracle.u8(12)
	oracle.u32(300)
	oracle.u32(0xffffffff)
	oracle.u32(0)
	oracle.u32(0)
	oracle.u32(0)
	oracle.str("Warden")
	oracle.u32(1907)
	oracle.u8(0)
	oracle.u8(alfaOffline)
	oracle.u32(uint32(500000 + bravoID))
	oracle.str(guildE2ENameB)
	oracle.u8(3)
	oracle.u8(6)
	oracle.u32(80)
	oracle.u32(0)
	oracle.u32(0)
	oracle.u32(0)
	oracle.u32(0)
	oracle.str("")
	oracle.u32(1907)
	oracle.u8(0)
	oracle.u8(bravoOffline)
	oracle.u8(0)
	return oracle.buf.Bytes()
}

func TestGuildSeedEndToEndOverWire(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")

	race := func() *int64 { v := enterworld.RaceChina; return &v }
	gender := func() *int64 { v := enterworld.GenderMale; return &v }
	level := func(value int64) *int64 { return &value }
	seeds := []*enterworld.Character{
		{Name: guildE2ENameA, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: level(12)},
		{Name: guildE2ENameB, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: level(6)},
	}

	first := startGuildServer(t, dir, seeds)

	// Store-seed the guild + memberships through the atomic topology
	// doors, which move member rows and character FKs together.
	alfa := guildE2ECharacter(t, first.authority, guildE2ENameA)
	bravo := guildE2ECharacter(t, first.authority, guildE2ENameB)
	guildID, err := first.authority.Guilds().CreateGuild(guildE2EDivision, enterworld.GuildRecord{
		Name:           "WallWatch",
		Level:          1,
		GP:             5000,
		NoticeSubject:  "e2e subject",
		NoticeContents: "e2e contents",
		CrestParam:     0x1234,
		Byte10:         0,
	}, enterworld.GuildMemberRecord{
		CharID: alfa.ID, JID: uint32(500000 + alfa.ID), Name: guildE2ENameA, Grade: 0, Level: 12, DonatedGP: 300, PermMask: 0xffffffff, GrantName: "Warden", RefObjID: 1907,
	}, alfa)
	if err != nil || guildID != guildE2EGuildID {
		t.Fatalf("fixture guild create = %d/%v, want %d/nil", guildID, err, guildE2EGuildID)
	}
	if !addGuildMemberForTest(first.authority.Guilds(), guildE2EDivision, guildID, alfa.ID, enterworld.GuildMemberRecord{
		CharID: bravo.ID, JID: uint32(500000 + bravo.ID), Name: guildE2ENameB, Grade: 3, Level: 6, DonatedGP: 80, RefObjID: 1907,
	}) {
		t.Fatal("fixture member join refused")
	}

	// ---- session pair: B enters first, then A reads B online ----
	connBravo := guildDialWS(t, first.srv)
	guildHelloWS(t, connBravo)
	if got, want := guildEnterWorld(t, connBravo, guildE2ENameB), guildE2EOracle(alfa.ID, bravo.ID, 1, 1); !bytes.Equal(got, want) {
		t.Fatalf("B's 0x32C4 seed = % X, want the all-offline oracle % X", got, want)
	}

	connAlfa := guildDialWS(t, first.srv)
	guildHelloWS(t, connAlfa)
	if got, want := guildEnterWorld(t, connAlfa, guildE2ENameA), guildE2EOracle(alfa.ID, bravo.ID, 1, 0); !bytes.Equal(got, want) {
		t.Fatalf("A's 0x32C4 seed = % X, want the B-online oracle % X", got, want)
	}

	if dropped := first.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s)", dropped)
	}
	guildSendFrame(t, connAlfa, transport.OpBye, []byte{transport.ByeReasonNormal})
	guildSendFrame(t, connBravo, transport.OpBye, []byte{transport.ByeReasonNormal})
	connAlfa.Close()
	connBravo.Close()

	// ---- the reboot: the guild TABLES and the GuildID FK persist ----
	guildShutdownServer(t, first.srv)
	first.authority.Close()

	second := startGuildServer(t, dir, nil)
	connAlfa2 := guildDialWS(t, second.srv)
	guildHelloWS(t, connAlfa2)
	alfa2 := guildE2ECharacter(t, second.authority, guildE2ENameA)
	bravo2 := guildE2ECharacter(t, second.authority, guildE2ENameB)
	if got, want := guildEnterWorld(t, connAlfa2, guildE2ENameA), guildE2EOracle(alfa2.ID, bravo2.ID, 1, 1); !bytes.Equal(got, want) {
		t.Fatalf("post-reboot 0x32C4 seed = % X, want the all-offline oracle % X", got, want)
	}

	if dropped := second.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s) on the reopened server", dropped)
	}
	guildSendFrame(t, connAlfa2, transport.OpBye, []byte{transport.ByeReasonNormal})
}

// ---- the consent-free mutator e2e ----

const (
	guildMutE2ENameC = "e2eGldChrly"
	guildMutE2ENameD = "e2eGldDlta"
	guildMutE2EJIDD  = uint32(600000) // + the store id; distinct from uint32(id) to pin the STORED jid
)

// mutE2ESized appends {u16 len}{ANSI} with encoding/binary (hand-rolled,
// never the production writers).
func mutE2ESized(buf *bytes.Buffer, value string) {
	binary.Write(buf, binary.LittleEndian, uint16(len(value)))
	buf.WriteString(value)
}

func mutE2ECreatePayload(selectedTargetGid uint32, name string) []byte {
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.LittleEndian, selectedTargetGid)
	mutE2ESized(buf, name)
	return buf.Bytes()
}

func mutE2EKickPayload(name string) []byte {
	buf := &bytes.Buffer{}
	mutE2ESized(buf, name)
	return buf.Bytes()
}

// mutE2EMember is one hand-rolled member row for the block oracle.
type mutE2EMember struct {
	jid     uint32
	name    string
	grade   uint8
	perm    uint32
	offline uint8
}

// mutE2EBlockOracle hand-rolls the guild block for a freshly created
// guild carrying the documented DECISION initial values (level 1, GP 0,
// empty notice, crest 0, byte10 0; members level 1 - the seeded e2e
// characters persist no level - donated 0, dwords 0, empty grantName,
// refObjId 1907 = the CHAR_CH_MAN_ADVENTURER male fallback, fortress 0).
func mutE2EBlockOracle(guildID int64, guildName string, members []mutE2EMember) []byte {
	oracle := &oracle32C4{}
	oracle.u32(uint32(guildID))
	oracle.str(guildName)
	oracle.u8(1)  // guild level
	oracle.u32(0) // GP
	oracle.str("")
	oracle.str("")
	oracle.u32(0) // crestParam
	oracle.u8(0)  // byte10
	oracle.u8(uint8(len(members)))
	for _, member := range members {
		oracle.u32(member.jid)
		oracle.str(member.name)
		oracle.u8(member.grade)
		oracle.u8(1) // level
		oracle.u32(0)
		oracle.u32(member.perm)
		oracle.u32(0)
		oracle.u32(0)
		oracle.u32(0)
		oracle.str("")
		oracle.u32(1907)
		oracle.u8(0) // fortressRole
		oracle.u8(member.offline)
	}
	oracle.u8(0) // voteCount
	return oracle.buf.Bytes()
}

// TestGuildMutatorsEndToEndOverWire walks the consent-free mutators over
// the real transport and the real authority store:
//
//	C refused    -> a SYNTHETIC 0x7663 with a 13-byte name answers the
//	                evidenced 0xB663 {2, 0x18} refusal on the wire;
//	C creates    -> a SYNTHETIC 0x7663 (no client trigger exists - the
//	                NPC-chat entry point is not folded), answered by
//	                0xB663 {1}+block matching the hand-rolled oracle;
//	reboot       -> C's fresh enter-world carries the created guild's
//	                0x32C4 seed (watermark + rows + FK persisted);
//	kick         -> with D seeded in and ONLINE, C's 0x74B1 fans the
//	                0x3B29 subOp-3 frame to BOTH live members (one frame
//	                serves everyone - the client branches by jid);
//	D relogs     -> NO 0x32C4 rides D's seeds (FK cleared), proven by
//	                D's next elicited frame being the 0xB663 answer to a
//	                fresh create - which also pins the watermark: the
//	                new guild allocates id 2, never a reissue of 1.
func TestGuildMutatorsEndToEndOverWire(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")

	race := func() *int64 { v := enterworld.RaceChina; return &v }
	gender := func() *int64 { v := enterworld.GenderMale; return &v }
	seeds := []*enterworld.Character{
		{Name: guildMutE2ENameC, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender()},
		{Name: guildMutE2ENameD, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender()},
	}
	first := startGuildServer(t, dir, seeds)
	charlie := guildE2ECharacter(t, first.authority, guildMutE2ENameC)

	// ---- C enters guildless and creates over the wire ----
	connCharlie := guildDialWS(t, first.srv)
	guildHelloWS(t, connCharlie)
	guildEnterWorldSeeds(t, connCharlie, guildMutE2ENameC)

	// The evidenced refusal ANSWERS on the wire: a 13-byte name elicits
	// 0xB663 {u8 2}{u8 0x18} (INVALID_GUILDNAME_LEN, the pinned
	// trigger/code pair - errors.go) before the valid create succeeds.
	guildSendFrame(t, connCharlie, guild.OpGuildCreateRequest, mutE2ECreatePayload(0, "ThirteenChars"))
	if got := guildExpectFrame(t, connCharlie, guild.OpGuildCreateAck, "refused create ack"); !bytes.Equal(got, []byte{0x02, 0x18}) {
		t.Fatalf("refused 0xB663 = % X, want [02 18]", got)
	}

	guildSendFrame(t, connCharlie, guild.OpGuildCreateRequest, mutE2ECreatePayload(0, "SteelBanner"))
	ack := guildExpectFrame(t, connCharlie, guild.OpGuildCreateAck, "create ack")
	wantAck := append([]byte{0x01}, mutE2EBlockOracle(1, "SteelBanner", []mutE2EMember{
		{jid: uint32(charlie.ID), name: guildMutE2ENameC, grade: 0, perm: 0xffffffff, offline: 0},
	})...)
	if !bytes.Equal(ack, wantAck) {
		t.Fatalf("0xB663 = % X, want the oracle % X", ack, wantAck)
	}
	guildSendFrame(t, connCharlie, transport.OpBye, []byte{transport.ByeReasonNormal})
	connCharlie.Close()

	// ---- reboot: watermark + rows + FK persisted ----
	guildShutdownServer(t, first.srv)
	first.authority.Close()
	second := startGuildServer(t, dir, nil)
	charlie2 := guildE2ECharacter(t, second.authority, guildMutE2ENameC)
	delta2 := guildE2ECharacter(t, second.authority, guildMutE2ENameD)

	connCharlie2 := guildDialWS(t, second.srv)
	guildHelloWS(t, connCharlie2)
	seed := guildEnterWorld(t, connCharlie2, guildMutE2ENameC)
	wantSeed := mutE2EBlockOracle(1, "SteelBanner", []mutE2EMember{
		// C itself is not yet bound at its own seed-compose time.
		{jid: uint32(charlie2.ID), name: guildMutE2ENameC, grade: 0, perm: 0xffffffff, offline: 1},
	})
	if !bytes.Equal(seed, wantSeed) {
		t.Fatalf("post-reboot 0x32C4 = % X, want the created guild % X", seed, wantSeed)
	}

	// ---- seed D into the guild through the store doors ----
	const guildID = int64(1)
	deltaJID := guildMutE2EJIDD + uint32(delta2.ID)
	if !addGuildMemberForTest(second.authority.Guilds(), guildE2EDivision, guildID, charlie2.ID, enterworld.GuildMemberRecord{
		CharID: delta2.ID, JID: deltaJID, Name: guildMutE2ENameD, Grade: 3, Level: 1, RefObjID: 1907,
	}) {
		t.Fatal("fixture member join refused")
	}

	// ---- D enters ONLINE, then C kicks D: both receive subOp 3 ----
	connDelta := guildDialWS(t, second.srv)
	guildHelloWS(t, connDelta)
	deltaSeed := guildEnterWorld(t, connDelta, guildMutE2ENameD)
	wantDeltaSeed := mutE2EBlockOracle(1, "SteelBanner", []mutE2EMember{
		{jid: uint32(charlie2.ID), name: guildMutE2ENameC, grade: 0, perm: 0xffffffff, offline: 0},
		{jid: deltaJID, name: guildMutE2ENameD, grade: 3, perm: 0, offline: 1},
	})
	if !bytes.Equal(deltaSeed, wantDeltaSeed) {
		t.Fatalf("D's 0x32C4 = % X, want the two-member guild % X", deltaSeed, wantDeltaSeed)
	}

	// 516EA0 checks session chat restriction before even decoding or checking
	// guild permission. Both a master and a member receive only 36EA.
	for _, actor := range []struct {
		name string
		conn *websocket.Conn
	}{{guildMutE2ENameC, connCharlie2}, {guildMutE2ENameD, connDelta}} {
		session, ok := second.srv.Hub.BoundSession(presence.BindKey(guildE2EDivision, actor.name))
		if !ok {
			t.Fatal("guild actor not bound")
		}
		session.SetCommandRestriction(transport.CommandRestrictionChat, [8]uint16{2026, 1, 0, 1, 0, 0, 0, 0})
		for _, payload := range [][]byte{mutatorNoticePayload("Changed", "Body"), nil, mutatorNoticePayload("", "")} {
			guildSendFrame(t, actor.conn, guild.OpGuildNoticeEditRequest, payload)
			if got := guildExpectFrame(t, actor.conn, 0x36ea, "restricted guild notice"); !bytes.Equal(got, []byte{0, 0x5a, 4, 0, 0}) {
				t.Fatalf("restriction %x", got)
			}
		}
		session.ClearCommandRestriction(transport.CommandRestrictionChat)
	}
	if record, _, ok := second.authority.Guilds().Guild(guildE2EDivision, guildID); !ok || record.NoticeSubject != "" || record.NoticeContents != "" {
		t.Fatal("restricted request mutated guild notice")
	}
	// Refusal is actor-only and uses B77A, not the inventory error channel.
	guildSendFrame(t, connDelta, guild.OpGuildNoticeEditRequest, mutatorNoticePayload("", ""))
	if got := guildExpectFrame(t, connDelta, guild.OpGuildNoticeEditAck, "permission precedes empty text"); !bytes.Equal(got, []byte{2, 0x1e}) {
		t.Fatalf("notice permission reply = % X", got)
	}
	guildSendFrame(t, connCharlie2, guild.OpGuildNoticeEditRequest, mutatorNoticePayload("", "body"))
	if got := guildExpectFrame(t, connCharlie2, guild.OpGuildNoticeEditAck, "authorized empty subject"); !bytes.Equal(got, []byte{2, 0x22}) {
		t.Fatalf("notice subject reply = % X", got)
	}
	guildSendFrame(t, connCharlie2, guild.OpGuildKickRequest, mutE2EKickPayload(guildMutE2ENameD))
	wantKick := []byte{0x03, byte(deltaJID), byte(deltaJID >> 8), byte(deltaJID >> 16), byte(deltaJID >> 24), 0x02}
	if got := guildExpectFrame(t, connCharlie2, guild.OpGuildUpdatePush, "kicker's subOp 3"); !bytes.Equal(got, wantKick) {
		t.Fatalf("kicker's 0x3B29 = % X, want % X", got, wantKick)
	}
	if got := guildExpectFrame(t, connDelta, guild.OpGuildUpdatePush, "kicked member's subOp 3"); !bytes.Equal(got, wantKick) {
		t.Fatalf("kicked member's 0x3B29 = % X, want the SAME frame % X", got, wantKick)
	}
	guildSendFrame(t, connDelta, transport.OpBye, []byte{transport.ByeReasonNormal})
	connDelta.Close()

	// ---- D relogs: NO 0x32C4, and a fresh create answers as guild 2 ----
	connDelta2 := guildDialWS(t, second.srv)
	guildHelloWS(t, connDelta2)
	guildEnterWorldSeeds(t, connDelta2, guildMutE2ENameD)
	guildSendFrame(t, connDelta2, guild.OpGuildCreateRequest, mutE2ECreatePayload(0, "DeltaBanner"))
	// The very NEXT frame is the create ack: no 0x32C4 rode the seeds
	// (guildExpectFrame fails loud on any other opcode), the kicked FK is
	// clear (a linked character's create refuses), and the watermark
	// allocates 2 - id 1 is never reissued.
	ack2 := guildExpectFrame(t, connDelta2, guild.OpGuildCreateAck, "post-kick create ack")
	wantAck2 := append([]byte{0x01}, mutE2EBlockOracle(2, "DeltaBanner", []mutE2EMember{
		{jid: uint32(delta2.ID), name: guildMutE2ENameD, grade: 0, perm: 0xffffffff, offline: 0},
	})...)
	if !bytes.Equal(ack2, wantAck2) {
		t.Fatalf("post-kick 0xB663 = % X, want the oracle % X", ack2, wantAck2)
	}

	if dropped := second.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s)", dropped)
	}
	guildSendFrame(t, connCharlie2, transport.OpBye, []byte{transport.ByeReasonNormal})
	guildSendFrame(t, connDelta2, transport.OpBye, []byte{transport.ByeReasonNormal})
}
