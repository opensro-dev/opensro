package mentor_test

// End-to-end exercise of the mentor/TC INVITE HANDSHAKE (T62) over the
// REAL transport against the REAL authority store, with the party AND
// guild lanes registered exactly like wiring.go - the 0x3393 consent
// rides party's shared hub registration and routes to the mentor arm by
// pending-invite ownership:
//
//	invite      -> M's 0x76B1 prompts S with the byte-exact 0x3393
//	               {09, u32 inviterGid, u16-len name}; self-invite, an
//	               offline target, an over-band target and a
//	               below-master-band inviter refuse silently (pending
//	               table stays empty);
//	disconnect  -> S closing mid-prompt drops the pending invitation;
//	               a consent from S's NEXT session is a no-op;
//	refuse      -> the pinned {02 00} (sub_68c4c0 case 7 - NOT guild's
//	               {02 16}) consumes the invitation, commits nothing,
//	               and the INVITER receives the byte-exact 0x3AC5
//	               {10, 2, 0x11} rejection notice - the lane's ONE
//	               pinned refusal carrier;
//	inviter gone-> M disconnecting mid-prompt invalidates S's accept
//	               (the consent-time re-validation);
//	accept      -> {01 01} commits through the ATOMIC AdmitStudent door
//	               (the camp is born with its first accepted
//	               invitation): S receives their 0x3AC5 status-10 sub-1
//	               seed and M - whose client was campless until this
//	               commit - their own seed, both byte-asserted;
//	re-invite   -> a camped target refuses silently;
//	second join -> O's accept rides AdmitStudent: O gets the seed,
//	               M and S the status-2 join row, all byte-asserted;
//	stale       -> a second {01 01} finds no pending and moves nothing;
//	reboot      -> the committed membership persists: S's fresh
//	               enter-world carries the three-member 0x3AC5 seed
//	               (the WorldBound hook).
//
// A second test pins the shared-opcode COEXISTENCE with party AND guild:
// the cross-lane prompt dismissal in all directions, then byte-identical
// {01 01} accepts routing by pending ownership - to the mentor lane
// while mentor holds the pending, to the guild lane while guild does,
// and to the party lane while party does.

import (
	"bytes"
	"context"
	"encoding/binary"
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
	"opensro.online/server/internal/game/social/community"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/social/mentor"
	"opensro.online/server/internal/game/social/party"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

const (
	campE2EDivision = "global-official"
	campE2ENameM    = "e2eTcMastr" // level 60 - the master band floor is 0x3c
	campE2ENameS    = "e2eTcStud"  // level 20 - inside the 0x28 student band
	campE2ENameO    = "e2eTcOwl"   // level 30 - the second student
	campE2ENameV    = "e2eTcVet"   // level 41 - OVER the 0x28 student band
	campE2ENameT    = "e2eTcTaru"  // never connects (the offline target)
	campE2ENameH    = "e2eTcHost"  // level 60 - the cross-dismissal master
	campE2ENameI    = "e2eTcIvy"   // level 20 - the cross-dismissal target
)

// campSkillSeeder satisfies the store's creation-seed invariant (the
// guildSkillSeeder twin - mentor tests never read skills).
func campSkillSeeder(raceKey string, learned []uint32) ([]uint32, error) {
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

type campE2EServer struct {
	srv       *transport.Server
	authority *store.Store
	partyRt   *party.Runtime
	guildInv  *guild.InviteRuntime
	mentorInv *mentor.InviteRuntime
}

// startCampServer stands the transport up with the bootstrap, party,
// guild AND mentor lanes composed like wiring.go: the party runtime owns
// the shared 0x3393 registration, the guild and mentor invite runtimes
// hook in as its consent arms, the cross-lane dismissal points in ALL
// directions, and every lane's stale-drop hooks ride OnWorldBound /
// OnSessionClose (mentor's WorldBound also seeds the 0x3AC5 camp state).
func startCampServer(t *testing.T, dir string, seeds []*enterworld.Character) campE2EServer {
	t.Helper()

	authority, err := store.Open(dir, store.Options{DefaultSkills: campSkillSeeder})
	if err != nil {
		t.Fatalf("store.Open(%s): %v", dir, err)
	}
	t.Cleanup(authority.Close)

	existing := map[string]bool{}
	for _, c := range authority.Characters().CharactersForDivision(campE2EDivision) {
		existing[c.Name] = true
	}
	for index, seed := range seeds {
		if seed == nil || existing[seed.Name] {
			continue
		}
		accountID := fmt.Sprintf("test-account-%d", index)
		if err := authority.CreateCharacter(campE2EDivision, accountID, seed); err != nil {
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
	deps.TrainingCamps = authority.TrainingCamps()

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

	partyRt := party.NewRuntime(deps, directory)
	// Everyone stands together: these suites test invitation lanes, not reach.
	partyRt.UseLivePose(func(string, *enterworld.Character) simulation.Spawn {
		return simulation.Spawn{RegionID: 0x6A48, X: 900, Z: 900}
	})
	guildInv := guild.NewInviteRuntime(deps, directory)
	mentorInv := mentor.NewInviteRuntime(deps, directory)
	partyRt.AddConsentArm(guildInv)
	partyRt.AddConsentArm(mentorInv)
	guildInv.PeerPending = func(divisionID, name string) bool {
		return partyRt.Registry().HasPendingInviteFor(divisionID, name) || mentorInv.HasPendingInvite(divisionID, name)
	}
	mentorInv.PeerPending = func(divisionID, name string) bool {
		return partyRt.Registry().HasPendingInviteFor(divisionID, name) || guildInv.HasPendingInvite(divisionID, name)
	}

	deps.OnWorldBound = func(s *transport.Session, divisionID string, character *enterworld.Character) {
		srv.Hub.BindExclusive(presence.BindKey(divisionID, character.Name), s)
		partyRt.WorldBound(s, divisionID, character)
		guildInv.WorldBound(divisionID, character)
		mentorInv.WorldBound(s, divisionID, character)
	}
	srv.Hub.OnSessionClose(func(s *transport.Session, _ error) {
		partyRt.SessionClosed(s)
		guildInv.SessionClosed(s)
		mentorInv.SessionClosed(s)
	})

	enterworld.Register(srv.Hub, deps)
	partyRt.Register(srv.Hub)
	guild.Register(srv.Hub, deps, directory, nil, nil)
	guildInv.Register(srv.Hub)
	mentorInv.Register(srv.Hub)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { campShutdownServer(t, srv) })
	return campE2EServer{srv: srv, authority: authority, partyRt: partyRt, guildInv: guildInv, mentorInv: mentorInv}
}

func campShutdownServer(t *testing.T, srv *transport.Server) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func campDialWS(t *testing.T, srv *transport.Server) *websocket.Conn {
	t.Helper()
	url := fmt.Sprintf("ws://%s%s", srv.WSAddr(), transport.PathWS)
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("websocket dial %s: %v", url, err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func campSendFrame(t *testing.T, c *websocket.Conn, opcode uint16, payload []byte) {
	t.Helper()
	f := transport.Frame{Opcode: opcode, Payload: payload}
	if err := c.WriteMessage(websocket.BinaryMessage, f.Encode()); err != nil {
		t.Fatalf("writing frame 0x%04X: %v", opcode, err)
	}
}

func campNextFrame(t *testing.T, c *websocket.Conn, what string) transport.Frame {
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

func campExpectFrame(t *testing.T, c *websocket.Conn, opcode uint16, what string) []byte {
	t.Helper()
	f := campNextFrame(t, c, what)
	if f.Opcode != opcode {
		t.Fatalf("%s: next frame = 0x%04X payload % X, want opcode 0x%04X", what, f.Opcode, f.Payload, opcode)
	}
	return f.Payload
}

func campHelloWS(t *testing.T, c *websocket.Conn) {
	t.Helper()
	campSendFrame(t, c, transport.OpHello, transport.EncodeHello(transport.Hello{AdmissionToken: []byte("test-admission")}))
	payload := campExpectFrame(t, c, transport.OpWelcome, "handshake")
	if _, err := transport.DecodeWelcome(payload); err != nil {
		t.Fatalf("decoding WELCOME: %v", err)
	}
}

// campConsumeEnterWorldTail asserts the 0x0007 result and consumes the
// frozen bootstrap sequence plus the friend + letter seeds (the
// guildEnterWorldSeeds shape).
func campConsumeEnterWorldTail(t *testing.T, c *websocket.Conn, name string) {
	t.Helper()
	result, err := transport.DecodeEnterWorldResult(campExpectFrame(t, c, transport.OpEnterWorldResult, "enter world "+name))
	if err != nil {
		t.Fatalf("decoding 0x0007: %v", err)
	}
	if !result.OK {
		t.Fatalf("enter world %s refused: nativeErrorCode=%#x", name, result.NativeErrorCode)
	}
	campExpectFrame(t, c, enterworld.OpcodeResetClient, "bootstrap[0]")
	campExpectFrame(t, c, enterworld.OpcodeMyCharacterData, "bootstrap[1]")
	campExpectFrame(t, c, enterworld.OpcodeMyCharacterChunk, "bootstrap[2]")
	campExpectFrame(t, c, enterworld.OpcodeMyCharacterFlush, "bootstrap[3]")
	campExpectFrame(t, c, enterworld.OpcodeServerClockGidLatch, "bootstrap[4]")
	campExpectFrame(t, c, enterworld.OpcodeObjectListStart, "bootstrap[5]")
	campExpectFrame(t, c, enterworld.OpcodeObjectListFinalize, "bootstrap[6]")
	if got := campExpectFrame(t, c, community.OpFriendRosterPush, "friend roster seed"); !bytes.Equal(got, []byte{0x00}) {
		t.Fatalf("0x3769 seed payload = % X, want the empty roster [00]", got)
	}
	if got := campExpectFrame(t, c, community.OpLetterListAnswer, "letter list seed"); !bytes.Equal(got, []byte{0x01, 0x00}) {
		t.Fatalf("0xB3CD seed payload = % X, want the empty list [01 00]", got)
	}
	wiretest.ActivateWorld(t, c, "enter world "+name)
}

// campEnterWorld performs the 0x0006 bind for a CAMPLESS character and
// consumes the whole enter-world tail.
func campEnterWorld(t *testing.T, c *websocket.Conn, name string) {
	t.Helper()
	campSendFrame(t, c, transport.OpEnterWorld, transport.EncodeEnterWorld(
		entryauth.NewAuthenticatedEntryFixture(t, campE2EDivision, name),
	))
	campConsumeEnterWorldTail(t, c, name)
	// The exclusive bind and the world-bound hooks run at the tail of the
	// game-ready handler; drain it so callers see a bound session.
	wiretest.AssertQueueDrained(t, c, "world-bound tail "+name)
}

// campEnterWorldWithSeed performs the 0x0006 bind for a persisted CAMP
// MEMBER. EnterWorld owns the result/bootstrap/community prefix; GameReady
// then queues its core burst before the one-shot WorldBound hook publishes
// the 0x3AC5 camp seed. Returns that post-admission seed payload.
func campEnterWorldWithSeed(t *testing.T, c *websocket.Conn, name string) []byte {
	t.Helper()
	campSendFrame(t, c, transport.OpEnterWorld, transport.EncodeEnterWorld(
		entryauth.NewAuthenticatedEntryFixture(t, campE2EDivision, name),
	))
	campConsumeEnterWorldTail(t, c, name)
	seed := campExpectFrame(t, c, mentor.OpTCStatus, "camp seed for "+name)
	// The camp seed is the world-bound hook's own frame; drain after it.
	wiretest.AssertQueueDrained(t, c, "world-bound tail "+name)
	return seed
}

// campE2ECharacter resolves a live store record by name.
func campE2ECharacter(t *testing.T, authority *store.Store, name string) *enterworld.Character {
	t.Helper()
	var found *enterworld.Character
	authority.ReadCharacters(campE2EDivision, func(characters []*enterworld.Character) {
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

// campU32 renders one little-endian u32 payload (hand-rolled).
func campU32(v uint32) []byte {
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.LittleEndian, v)
	return buf.Bytes()
}

// campStr renders one {u16 len}{ANSI bytes} wire string (hand-rolled).
func campStr(v string) []byte {
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.LittleEndian, uint16(len(v)))
	buf.WriteString(v)
	return buf.Bytes()
}

// campPromptOracle hand-rolls the 0x3393 type-9 prompt
// {u8 9, u32 inviterGid, u16-len name}.
func campPromptOracle(inviterGid uint32, inviterName string) []byte {
	body := append([]byte{0x09}, campU32(inviterGid)...)
	return append(body, campStr(inviterName)...)
}

// campE2EMember is one hand-rolled 0x3AC5 member row.
type campE2EMember struct {
	gid   uint32
	name  string
	kind  uint8
	level uint8
}

// campMemberRow appends one member row in the pinned sub_773c60 case-1 /
// sub_8290e0 read order.
func campMemberRow(buf *bytes.Buffer, member campE2EMember) {
	buf.Write(campU32(member.gid))
	buf.Write(campU32(member.gid))
	buf.Write(campU32(0))
	buf.Write(campStr(member.name))
	buf.WriteByte(member.kind)
	buf.WriteByte(0)
	buf.Write(make([]byte, 16))
	buf.WriteByte(member.level)
	buf.WriteByte(member.level)
	buf.Write(campU32(0))
	buf.WriteByte(0)
	buf.Write(campU32(0))
	buf.Write(make([]byte, 8))
	buf.Write(campStr(""))
}

// campSeedOracle hand-rolls the 0x3AC5 status-10 sub-1 seed for one
// receiver: {u8 10}{u8 1}{u32 localMemberId}{16-byte blob}{u8 0}{str ""}
// {str ""}{u8 count}{rows}.
func campSeedOracle(localMemberID uint32, members []campE2EMember) []byte {
	return campSeedWithNoticeOracle(localMemberID, members, "", "")
}

func campSeedWithNoticeOracle(localMemberID uint32, members []campE2EMember, subject, contents string) []byte {
	buf := &bytes.Buffer{}
	buf.WriteByte(10)
	buf.WriteByte(1)
	buf.Write(campU32(localMemberID))
	buf.Write(make([]byte, 16))
	buf.WriteByte(0)
	buf.Write(campStr(subject))
	buf.Write(campStr(contents))
	buf.WriteByte(uint8(len(members)))
	for _, member := range members {
		campMemberRow(buf, member)
	}
	return buf.Bytes()
}

// campJoinOracle hand-rolls the 0x3AC5 status-2 joining-member push.
func campJoinOracle(member campE2EMember) []byte {
	buf := &bytes.Buffer{}
	buf.WriteByte(2)
	campMemberRow(buf, member)
	return buf.Bytes()
}

// campGuildCreatePayload hand-rolls the 0x7663 body
// {u32 npcGid}{u16-len name} - the 13-char name is this suite's silent-
// refusal elicitation (the evidenced 0xB663 {02 18} answer proves the
// preceding frames were processed and emitted nothing).
func campGuildCreatePayload(name string) []byte {
	return append(campU32(0), campStr(name)...)
}

// campElicitGuildRefusal proves the stream carried NOTHING since the
// last asserted frame: the 13-char guild-create name answers the
// evidenced 0xB663 {02 18} refusal as the very next frame.
func campElicitGuildRefusal(t *testing.T, c *websocket.Conn, what string) {
	t.Helper()
	campSendFrame(t, c, guild.OpGuildCreateRequest, campGuildCreatePayload("ThirteenChars"))
	if got := campExpectFrame(t, c, guild.OpGuildCreateAck, what); !bytes.Equal(got, []byte{0x02, 0x18}) {
		t.Fatalf("%s: elicited 0xB663 = % X, want [02 18]", what, got)
	}
}

// campRoundTrip is an ordering barrier: a session dispatches its frames one at
// a time in its read loop (transport/session.go readLoop), so the PONG to a
// PING sent after a request arrives only once that request was handled. It
// reads raw frames (campNextFrame hides keepalive PING/PONG) and fails on any
// game frame before its PONG, which also proves the stream carried nothing.
func campRoundTrip(t *testing.T, c *websocket.Conn, what string) {
	t.Helper()
	marker := []byte("barrier")
	campSendFrame(t, c, transport.OpPing, marker)
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, data, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("%s: PONG never arrived: %v", what, err)
		}
		f, err := transport.DecodeFrame(data)
		if err != nil {
			t.Fatalf("%s: decoding ws frame: %v", what, err)
		}
		switch {
		case f.Opcode == transport.OpPong && bytes.Equal(f.Payload, marker):
			return
		case f.Opcode == transport.OpPing || f.Opcode == transport.OpPong:
			continue
		default:
			t.Fatalf("%s: frame 0x%04X payload % X arrived before the barrier", what, f.Opcode, f.Payload)
		}
	}
}

// campAwaitPending polls the mentor runtime's pending count toward want
// (the disconnect drop rides the async close hook).
func campAwaitPending(t *testing.T, invites *mentor.InviteRuntime, want int, what string) {
	t.Helper()
	wait.Eventually(t, 5*time.Second, fmt.Sprintf("%s: pending invites to reach %d", what, want), func() bool {
		return invites.PendingInviteCount() == want
	})
}

// campLevel builds a *int64 level seed.
func campLevel(v int64) *int64 { return &v }

// TestMentorInviteConsentEndToEndOverWire walks the whole handshake:
// silent invite refusals (self, offline, over-band target, below-band
// inviter), the disconnect-mid-prompt drop, the pinned {02 00} refuse
// with the byte-asserted 0x3AC5 {10,2,0x11} rejection notice toward the
// inviter, the inviter-disconnect consent invalidation, the {01 01}
// accept with the atomic camp CREATION and both byte-asserted seeds, the
// camped re-invite refusal, the second joiner's status-2 fan-out, the
// stale-consent no-op, and the reboot persistence of the membership.
func TestMentorInviteConsentEndToEndOverWire(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "authority")

	race := func() *int64 { v := enterworld.RaceChina; return &v }
	gender := func() *int64 { v := enterworld.GenderMale; return &v }
	seeds := []*enterworld.Character{
		{Name: campE2ENameM, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: campLevel(60)},
		{Name: campE2ENameS, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: campLevel(20)},
		{Name: campE2ENameO, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: campLevel(30)},
		{Name: campE2ENameV, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: campLevel(41)},
		{Name: campE2ENameT, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: campLevel(20)},
	}
	first := startCampServer(t, dir, seeds)
	master := campE2ECharacter(t, first.authority, campE2ENameM)
	stud := campE2ECharacter(t, first.authority, campE2ENameS)
	owl := campE2ECharacter(t, first.authority, campE2ENameO)
	vet := campE2ECharacter(t, first.authority, campE2ENameV)
	taru := campE2ECharacter(t, first.authority, campE2ENameT)
	gidM := enterworld.ObjectIDForCharacter(master)
	gidS := enterworld.ObjectIDForCharacter(stud)
	gidO := enterworld.ObjectIDForCharacter(owl)
	gidV := enterworld.ObjectIDForCharacter(vet)
	gidT := enterworld.ObjectIDForCharacter(taru)

	connM := campDialWS(t, first.srv)
	campHelloWS(t, connM)
	campEnterWorld(t, connM, campE2ENameM)
	connS := campDialWS(t, first.srv)
	campHelloWS(t, connS)
	campEnterWorld(t, connS, campE2ENameS)
	connV := campDialWS(t, first.srv)
	campHelloWS(t, connV)
	campEnterWorld(t, connV, campE2ENameV)

	// ---- silent invite refusals leave the pending table empty ----
	// Self-invite (the client refuses it before composing - the
	// sub_702c90 target != data_cedb54 check; a forged one refuses
	// server-side too), an OFFLINE target (T never connects), an
	// OVER-BAND target (V at 41 > 0x28), and a below-master-band
	// INVITER (S at 20 < 0x3c).
	campSendFrame(t, connM, mentor.OpTCInviteRequest, campU32(gidM))
	campSendFrame(t, connM, mentor.OpTCInviteRequest, campU32(gidT))
	campSendFrame(t, connM, mentor.OpTCInviteRequest, campU32(gidV))
	campSendFrame(t, connS, mentor.OpTCInviteRequest, campU32(gidO))
	// The next elicited answer proves all four were processed and
	// refused silently: nothing rode either stream meanwhile.
	campSendFrame(t, connM, mentor.OpTCInviteRequest, campU32(gidS))
	if got, want := campExpectFrame(t, connS, mentor.OpInvitationProposal, "S's type-9 prompt"), campPromptOracle(gidM, campE2ENameM); !bytes.Equal(got, want) {
		t.Fatalf("0x3393 prompt = % X, want % X", got, want)
	}
	if got := first.mentorInv.PendingInviteCount(); got != 1 {
		t.Fatalf("pending invites after the five 0x76B1 = %d, want 1 (four refused)", got)
	}

	// ---- disconnect mid-prompt: the invitation dies with S ----
	campSendFrame(t, connS, transport.OpBye, []byte{transport.ByeReasonNormal})
	connS.Close()
	campAwaitPending(t, first.mentorInv, 0, "after S's disconnect")

	// S's NEXT session answers the dead prompt: a no-op.
	connS2 := campDialWS(t, first.srv)
	campHelloWS(t, connS2)
	campEnterWorld(t, connS2, campE2ENameS)
	campSendFrame(t, connS2, mentor.OpInvitationProposal, []byte{0x01, 0x01})
	campElicitGuildRefusal(t, connS2, "post-stale-consent elicitation")
	if _, joined := first.authority.TrainingCamps().CampOfCharacter(campE2EDivision, stud.ID); joined {
		t.Fatalf("a consent to a dead prompt committed membership")
	}

	// ---- refuse: the pinned {02 00} consumes, commits nothing, and
	// the INVITER gets the byte-exact 0x3AC5 {10,2,0x11} notice ----
	campSendFrame(t, connM, mentor.OpTCInviteRequest, campU32(gidS))
	campExpectFrame(t, connS2, mentor.OpInvitationProposal, "S's second prompt")
	campSendFrame(t, connS2, mentor.OpInvitationProposal, []byte{0x02, 0x00})
	if got, want := campExpectFrame(t, connM, mentor.OpTCStatus, "M's rejection notice"), []byte{0x0A, 0x02, 0x11}; !bytes.Equal(got, want) {
		t.Fatalf("0x3AC5 rejection notice = % X, want % X", got, want)
	}
	if got := first.mentorInv.PendingInviteCount(); got != 0 {
		t.Fatalf("pending invites after the refuse = %d, want 0 (consumed)", got)
	}
	if _, joined := first.authority.TrainingCamps().CampOfCharacter(campE2EDivision, stud.ID); joined {
		t.Fatalf("the {02 00} refuse committed membership")
	}

	// ---- inviter disconnect mid-prompt: the accept is invalidated ----
	campSendFrame(t, connM, mentor.OpTCInviteRequest, campU32(gidS))
	campExpectFrame(t, connS2, mentor.OpInvitationProposal, "S's third prompt")
	campSendFrame(t, connM, transport.OpBye, []byte{transport.ByeReasonNormal})
	connM.Close()
	// The pending targets S and deliberately survives M's close (only
	// the consent-time re-validation refuses); wait for M's presence to
	// actually drop before S answers.
	wait.Eventually(t, 5*time.Second, "M's presence bind to drop after the close", func() bool {
		_, bound := first.srv.Hub.BoundSession(presence.BindKey(campE2EDivision, campE2ENameM))
		return !bound
	})
	campSendFrame(t, connS2, mentor.OpInvitationProposal, []byte{0x01, 0x01})
	campElicitGuildRefusal(t, connS2, "post-gone-inviter elicitation")
	if _, joined := first.authority.TrainingCamps().CampOfCharacter(campE2EDivision, stud.ID); joined {
		t.Fatalf("an accept whose inviter logged off committed membership")
	}

	// ---- accept: {01 01} commits through the ATOMIC AdmitStudent door ----
	connM2 := campDialWS(t, first.srv)
	campHelloWS(t, connM2)
	campEnterWorld(t, connM2, campE2ENameM)
	campSendFrame(t, connM2, mentor.OpTCInviteRequest, campU32(gidS))
	campExpectFrame(t, connS2, mentor.OpInvitationProposal, "S's fourth prompt")
	campSendFrame(t, connS2, mentor.OpInvitationProposal, []byte{0x01, 0x01})
	twoRoster := []campE2EMember{
		{gid: gidM, name: campE2ENameM, kind: mentor.MemberKindMaster, level: 60},
		{gid: gidS, name: campE2ENameS, kind: mentor.MemberKindStudent, level: 20},
	}
	if got, want := campExpectFrame(t, connS2, mentor.OpTCStatus, "the joiner's seed"), campSeedOracle(gidS, twoRoster); !bytes.Equal(got, want) {
		t.Fatalf("joiner's 0x3AC5 seed = % X, want % X", got, want)
	}
	// The commit CREATED the camp, so the master's campless client gets
	// its OWN seed (not a join row).
	if got, want := campExpectFrame(t, connM2, mentor.OpTCStatus, "the master's creation seed"), campSeedOracle(gidM, twoRoster); !bytes.Equal(got, want) {
		t.Fatalf("master's 0x3AC5 seed = % X, want % X", got, want)
	}
	campID, joined := first.authority.TrainingCamps().CampOfCharacter(campE2EDivision, stud.ID)
	if !joined || campID != master.ID {
		t.Fatalf("CampOfCharacter(S) = %d/%v, want %d/true (the master's char id)", campID, joined, master.ID)
	}
	if masterCamp, ok := first.authority.TrainingCamps().CampOfCharacter(campE2EDivision, master.ID); !ok || masterCamp != campID {
		t.Fatalf("CampOfCharacter(M) = %d/%v, want %d/true", masterCamp, ok, campID)
	}

	// ---- a camped target refuses silently ----
	campSendFrame(t, connM2, mentor.OpTCInviteRequest, campU32(gidS))
	campElicitGuildRefusal(t, connM2, "post-camped-re-invite elicitation")
	if got := first.mentorInv.PendingInviteCount(); got != 0 {
		t.Fatalf("pending invites after the camped re-invite = %d, want 0", got)
	}

	// Notice admission and publication use actual client-shaped requests.
	noticeRequest := func(subject, contents string) []byte { return append(campStr(subject), campStr(contents)...) }
	for _, tc := range []struct {
		conn *websocket.Conn
		body []byte
		code byte
	}{
		{connS2, noticeRequest("", ""), 0x16}, // permission before text validation
		{connM2, noticeRequest("", "body"), 0x17},
		{connM2, noticeRequest(strings.Repeat("a", 129), "body"), 0x17},
		{connM2, noticeRequest("title", strings.Repeat("b", 2049)), 0x17},
	} {
		campSendFrame(t, tc.conn, mentor.OpTCNoticeEditRequest, tc.body)
		if got := campExpectFrame(t, tc.conn, mentor.OpTCNoticeEditAck, "notice refusal"); !bytes.Equal(got, []byte{2, tc.code}) {
			t.Fatalf("notice refusal %x", got)
		}
	}
	// Native filter and malformed framing are silent; the next refusal acts as
	// an ordering barrier, so any spurious ack or broadcast fails this test.
	for _, body := range [][]byte{noticeRequest("SYSOBJECTS", "body"), noticeRequest("title", "quote'"), {1}} {
		campSendFrame(t, connM2, mentor.OpTCNoticeEditRequest, body)
		campElicitGuildRefusal(t, connM2, "silent notice barrier")
	}
	masterSession, ok := first.srv.Hub.BoundSession(presence.BindKey(campE2EDivision, campE2ENameM))
	if !ok {
		t.Fatal("master not bound")
	}
	masterSession.SetCommandRestriction(transport.CommandRestrictionChat, [8]uint16{2026, 1, 0, 1, 0, 0, 0, 0})
	campSendFrame(t, connM2, mentor.OpTCNoticeEditRequest, noticeRequest("title", "body"))
	if got := campExpectFrame(t, connM2, 0x36ea, "restricted notice"); !bytes.Equal(got, []byte{0, 0x5a, 4, 0, 0}) {
		t.Fatalf("restriction %x", got)
	}
	masterSession.ClearCommandRestriction(transport.CommandRestrictionChat)
	first.authority.MutateCharacter(master, "notice-test-dead", func() { hp := int64(0); master.CurrentHP = &hp })
	campSendFrame(t, connM2, mentor.OpTCNoticeEditRequest, noticeRequest("title", "body"))
	if got := campExpectFrame(t, connM2, mentor.OpTCNoticeEditAck, "dead author"); !bytes.Equal(got, []byte{2, 7}) {
		t.Fatalf("dead refusal %x", got)
	}
	first.authority.MutateCharacter(master, "notice-test-alive", func() { master.CurrentHP = nil })
	unchanged, _, _ := first.authority.TrainingCamps().Camp(campE2EDivision, campID)
	if unchanged.Subject != "" || unchanged.Contents != "" {
		t.Fatal("refused request changed notice")
	}
	noticeBody := noticeRequest("Caf\xe9", "Entry \x805.")
	campSendFrame(t, connM2, mentor.OpTCNoticeEditRequest, noticeBody)
	if got := campExpectFrame(t, connM2, mentor.OpTCNoticeEditAck, "notice success"); !bytes.Equal(got, []byte{1}) {
		t.Fatalf("notice ack %x", got)
	}
	for _, conn := range []*websocket.Conn{connM2, connS2} {
		if got := campExpectFrame(t, conn, mentor.OpTCStatus, "notice publication"); !bytes.Equal(got, append([]byte{7}, noticeBody...)) {
			t.Fatalf("notice push %x", got)
		}
	}

	// ---- second joiner: the join row fans to the sitting members ----
	connO := campDialWS(t, first.srv)
	campHelloWS(t, connO)
	campEnterWorld(t, connO, campE2ENameO)
	campSendFrame(t, connM2, mentor.OpTCInviteRequest, campU32(gidO))
	campExpectFrame(t, connO, mentor.OpInvitationProposal, "O's prompt")
	campSendFrame(t, connO, mentor.OpInvitationProposal, []byte{0x01, 0x01})
	owlRow := campE2EMember{gid: gidO, name: campE2ENameO, kind: mentor.MemberKindStudent, level: 30}
	threeRoster := append(append([]campE2EMember{}, twoRoster...), owlRow)
	if got, want := campExpectFrame(t, connO, mentor.OpTCStatus, "O's seed"), campSeedWithNoticeOracle(gidO, threeRoster, "Caf\xe9", "Entry \x805."); !bytes.Equal(got, want) {
		t.Fatalf("O's 0x3AC5 seed = % X, want % X", got, want)
	}
	if got, want := campExpectFrame(t, connM2, mentor.OpTCStatus, "M's join row"), campJoinOracle(owlRow); !bytes.Equal(got, want) {
		t.Fatalf("M's 0x3AC5 join row = % X, want % X", got, want)
	}
	if got, want := campExpectFrame(t, connS2, mentor.OpTCStatus, "S's join row"), campJoinOracle(owlRow); !bytes.Equal(got, want) {
		t.Fatalf("S's 0x3AC5 join row = % X, want % X", got, want)
	}

	// ---- stale consent: a second {01 01} finds no pending ----
	campSendFrame(t, connO, mentor.OpInvitationProposal, []byte{0x01, 0x01})
	campElicitGuildRefusal(t, connO, "post-stale-accept elicitation")
	if _, members, ok := first.authority.TrainingCamps().Camp(campE2EDivision, campID); !ok || len(members) != 3 {
		t.Fatalf("camp members after the stale consent = %d, want 3", len(members))
	}

	if dropped := first.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s)", dropped)
	}
	campSendFrame(t, connM2, transport.OpBye, []byte{transport.ByeReasonNormal})
	campSendFrame(t, connS2, transport.OpBye, []byte{transport.ByeReasonNormal})
	campSendFrame(t, connO, transport.OpBye, []byte{transport.ByeReasonNormal})
	campSendFrame(t, connV, transport.OpBye, []byte{transport.ByeReasonNormal})
	connM2.Close()
	connS2.Close()
	connO.Close()
	connV.Close()

	// ---- reboot: the committed membership persists and the fresh
	// enter-world reseeds the 0x3AC5 camp state ----
	campShutdownServer(t, first.srv)
	first.authority.Close()
	second := startCampServer(t, dir, nil)
	stud2 := campE2ECharacter(t, second.authority, campE2ENameS)
	if rebootCamp, ok := second.authority.TrainingCamps().CampOfCharacter(campE2EDivision, stud2.ID); !ok || rebootCamp != campID {
		t.Fatalf("post-reboot CampOfCharacter(S) = %d/%v, want %d/true", rebootCamp, ok, campID)
	}
	if second.mentorInv.PendingInviteCount() != 0 {
		t.Fatalf("pending invitations survived the reboot (session-scoped state must die with the process)")
	}
	connS3 := campDialWS(t, second.srv)
	campHelloWS(t, connS3)
	seed := campEnterWorldWithSeed(t, connS3, campE2ENameS)
	if want := campSeedWithNoticeOracle(gidS, threeRoster, "Caf\xe9", "Entry \x805."); !bytes.Equal(seed, want) {
		t.Fatalf("post-reboot 0x3AC5 seed = % X, want % X", seed, want)
	}
	campSendFrame(t, connS3, transport.OpBye, []byte{transport.ByeReasonNormal})
}

// TestMentorInvitePartyGuildOneProposalPerPlayerEndToEnd pins the native
// one-transaction-per-player rule (46F420) with the party AND guild
// lanes: while one lane's proposal waits the others refuse, and
// byte-identical {01 01} accepts route to the lane holding the only
// pending - MENTOR, then GUILD, then PARTY.
func TestMentorInvitePartyGuildOneProposalPerPlayerEndToEnd(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "authority")

	race := func() *int64 { v := enterworld.RaceChina; return &v }
	gender := func() *int64 { v := enterworld.GenderMale; return &v }
	seeds := []*enterworld.Character{
		{Name: campE2ENameH, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: campLevel(60)},
		{Name: campE2ENameI, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: campLevel(20)},
	}
	server := startCampServer(t, dir, seeds)
	host := campE2ECharacter(t, server.authority, campE2ENameH)
	ivy := campE2ECharacter(t, server.authority, campE2ENameI)
	gidH := enterworld.ObjectIDForCharacter(host)
	gidI := enterworld.ObjectIDForCharacter(ivy)

	connH := campDialWS(t, server.srv)
	campHelloWS(t, connH)
	campEnterWorld(t, connH, campE2ENameH)
	campSendFrame(t, connH, guild.OpGuildCreateRequest, campGuildCreatePayload("OakWard"))
	campExpectFrame(t, connH, guild.OpGuildCreateAck, "create ack")

	connI := campDialWS(t, server.srv)
	campHelloWS(t, connI)
	campEnterWorld(t, connI, campE2ENameI)

	// ---- a party proposal waits: the TC invite fails silently ----
	campSendFrame(t, connH, party.OpPartyInviteRequest, append(campU32(gidI), 0x00))
	campExpectFrame(t, connI, mentor.OpInvitationProposal, "party prompt")
	campSendFrame(t, connH, mentor.OpTCInviteRequest, campU32(gidI))
	// H's requests are handled in order, so an answered barrier on H proves
	// the TC invite was already dropped. Without it, I's refusal (another
	// connection) could clear the party proposal first and admit the TC
	// prompt, which CI observed as 0x3393 type 9 ahead of the refusal ack.
	campRoundTrip(t, connH, "TC invite processed barrier")
	if got := server.mentorInv.PendingInviteCount(); got != 0 {
		t.Fatalf("mentor pendings over a waiting party proposal = %d, want 0", got)
	}
	// I refuses; the next frame I sees is the refusal, not a TC prompt.
	campSendFrame(t, connI, mentor.OpInvitationProposal, []byte{0x02, 0x0c})
	if got := campExpectFrame(t, connI, party.OpPartyJoinAck, "I's refusal ack"); !bytes.Equal(got, []byte{2, 0x0c}) {
		t.Fatalf("I's 0xB452 = % X, want 02 0C", got)
	}
	campExpectFrame(t, connH, party.OpCreatePartyAck, "H's refusal ack")

	// ---- a TC proposal waits: guild fails silently, party with {2, 2} ----
	campSendFrame(t, connH, mentor.OpTCInviteRequest, campU32(gidI))
	if got, want := campExpectFrame(t, connI, mentor.OpInvitationProposal, "TC prompt"), campPromptOracle(gidH, campE2ENameH); !bytes.Equal(got, want) {
		t.Fatalf("TC prompt = % X, want % X", got, want)
	}
	campSendFrame(t, connH, guild.OpGuildInviteRequest, campU32(gidI))
	if got := server.guildInv.PendingInviteCount(); got != 0 {
		t.Fatalf("guild pendings over a waiting TC proposal = %d, want 0", got)
	}
	campSendFrame(t, connH, party.OpPartyInviteRequest, append(campU32(gidI), 0x00))
	if got := campExpectFrame(t, connH, party.OpCreatePartyAck, "H's busy ack"); !bytes.Equal(got, []byte{2, 2}) {
		t.Fatalf("H's 0xB0D5 = % X, want 02 02", got)
	}
	if got := campExpectFrame(t, connI, party.OpPartyJoinAck, "I's busy ack"); !bytes.Equal(got, []byte{2, 2}) {
		t.Fatalf("I's 0xB452 = % X, want 02 02", got)
	}
	if got := server.mentorInv.PendingInviteCount(); got != 1 {
		t.Fatalf("mentor pendings = %d, want 1 (untouched)", got)
	}

	// ---- {01 01} routes to MENTOR - the lane holding the pending ----
	campSendFrame(t, connI, mentor.OpInvitationProposal, []byte{0x01, 0x01})
	campExpectFrame(t, connI, mentor.OpTCStatus, "I's camp seed")
	campExpectFrame(t, connH, mentor.OpTCStatus, "H's creation seed")
	if _, joined := server.authority.Guilds().GuildOfCharacter(campE2EDivision, ivy.ID); joined {
		t.Fatalf("the mentor accept committed GUILD membership")
	}
	if campID, joined := server.authority.TrainingCamps().CampOfCharacter(campE2EDivision, ivy.ID); !joined || campID != host.ID {
		t.Fatalf("CampOfCharacter(I) = %d/%v, want %d/true", campID, joined, host.ID)
	}

	// ---- {01 01} routes to GUILD once guild holds the pending ----
	campSendFrame(t, connH, guild.OpGuildInviteRequest, campU32(gidI))
	campExpectFrame(t, connI, mentor.OpInvitationProposal, "guild prompt")
	campSendFrame(t, connI, mentor.OpInvitationProposal, []byte{0x01, 0x01})
	campExpectFrame(t, connI, guild.OpGuildInfo, "I's 0x32C4")
	campExpectFrame(t, connH, guild.OpGuildUpdatePush, "H's subOp-2")
	if gid, joined := server.authority.Guilds().GuildOfCharacter(campE2EDivision, ivy.ID); !joined || gid != 1 {
		t.Fatalf("GuildOfCharacter(I) = %d/%v, want 1/true", gid, joined)
	}

	// ---- {01 01} routes to PARTY once party holds the pending ----
	campSendFrame(t, connH, party.OpPartyInviteRequest, append(campU32(gidI), 0x00))
	campExpectFrame(t, connI, mentor.OpInvitationProposal, "second party prompt")
	campSendFrame(t, connI, mentor.OpInvitationProposal, []byte{0x01, 0x01})
	campExpectFrame(t, connI, party.OpCreatePartyAck, "I's party ack")
	campExpectFrame(t, connI, party.OpPartyInfo, "I's roster")
	campExpectFrame(t, connH, party.OpCreatePartyAck, "H's party ack")
	campExpectFrame(t, connH, party.OpPartyInfo, "H's roster")

	if dropped := server.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s)", dropped)
	}
	campSendFrame(t, connH, transport.OpBye, []byte{transport.ByeReasonNormal})
	campSendFrame(t, connI, transport.OpBye, []byte{transport.ByeReasonNormal})
}
