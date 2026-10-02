/*
===========================================================================

e2e_invite_wire_test.go - guild and party invitation transport contracts

Exercises the real authority and ordered wire replies, including cross-session
barriers where a silent request must complete before another player acts.

===========================================================================
*/
package guild_test

// End-to-end exercise of the guild INVITE HANDSHAKE (T55) over the REAL
// transport against the REAL authority store, with the party lane
// registered exactly like wiring.go - the 0x3393 consent rides party's
// shared hub registration and routes to the guild arm by pending-invite
// ownership:
//
//	invite      -> E's 0x73AD prompts F with the byte-exact 0x3393
//	               {05, u32 inviterGid}; self-invite and an offline
//	               target refuse silently (pending table stays empty);
//	disconnect  -> F closing mid-prompt drops the pending invitation;
//	               a consent from F's NEXT session is a no-op;
//	refuse      -> the pinned {02 16} consumes the invitation, commits
//	               nothing, and the wire stays silent (proven by F's
//	               next elicited frame);
//	accept      -> {01 01} commits through the atomic AddGuildMemberAs
//	               door: F receives the full 0x32C4 block and E the
//	               0x3B29 subOp-2 join row, both byte-asserted;
//	stale       -> a second {01 01} finds no pending and moves nothing;
//	reboot      -> the joined membership persists: F's fresh
//	               enter-world carries the two-member 0x32C4.
//
// A second test pins the shared-opcode COEXISTENCE with party: the
// cross-lane prompt dismissal in both directions (the sub_5c8210
// mirror), a byte-identical {01 01} routing to the party lane while
// party holds the pending, and to the guild lane while guild does.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/enterworld"
	presence "opensro.online/server/internal/game/social"
	"opensro.online/server/internal/game/social/community"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/social/party"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

const (
	invE2ENameE = "e2eGldEcho"
	invE2ENameF = "e2eGldFox"
	invE2ENameG = "e2eGldGolf"
	invE2ENameH = "e2eGldHotel"
	invE2ENameI = "e2eGldIndia"
)

/*
================
inviteE2EServer
================
*/
type inviteE2EServer struct {
	srv       *transport.Server
	authority *store.Store
	partyRt   *party.Runtime
	invites   *guild.InviteRuntime
}

// startInviteServer stands the transport up with the bootstrap, party
// AND guild lanes composed like wiring.go: the party runtime owns the
// shared 0x3393 registration, the guild invite runtime hooks in as its
// consent arm, the cross-lane dismissal points back at the party
// registry, and both lanes' stale-drop hooks ride OnWorldBound /
// OnSessionClose.
/*
================
startInviteServer
================
*/
func startInviteServer(t *testing.T, dir string, seeds []*enterworld.Character) inviteE2EServer {
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

	partyRt := party.NewRuntime(deps, directory)
	// Everyone stands together: these suites test invitation lanes, not reach.
	partyRt.UseLivePose(func(string, *enterworld.Character) simulation.Spawn {
		return simulation.Spawn{RegionID: 0x6A48, X: 900, Z: 900}
	})
	invites := guild.NewInviteRuntime(deps, directory)
	partyRt.AddConsentArm(invites)
	invites.PeerPending = partyRt.Registry().HasPendingInviteFor

	deps.OnWorldBound = func(s *transport.Session, divisionID string, character *enterworld.Character) {
		srv.Hub.BindExclusive(presence.BindKey(divisionID, character.Name), s)
		partyRt.WorldBound(divisionID, character)
		invites.WorldBound(divisionID, character)
	}
	srv.Hub.OnSessionClose(func(s *transport.Session, _ error) {
		partyRt.SessionClosed(s)
		invites.SessionClosed(s)
	})

	enterworld.Register(srv.Hub, deps)
	partyRt.Register(srv.Hub)
	guild.Register(srv.Hub, deps, directory)
	invites.Register(srv.Hub)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { guildShutdownServer(t, srv) })
	return inviteE2EServer{srv: srv, authority: authority, partyRt: partyRt, invites: invites}
}

// invE2EU32 renders one little-endian u32 payload (hand-rolled).
/*
================
invE2EU32
================
*/
func invE2EU32(v uint32) []byte {
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.LittleEndian, v)
	return buf.Bytes()
}

// invE2EPartyInvitePayload hand-rolls the 0x70D5 body
// {u32 targetGid, u8 optionBits}.
/*
================
invE2EPartyInvitePayload
================
*/
func invE2EPartyInvitePayload(targetGid uint32, optionBits uint8) []byte {
	return append(invE2EU32(targetGid), optionBits)
}

// invE2EPromptOracle hand-rolls the 0x3393 type-5 prompt
// {u8 5, u32 inviterGid}.
/*
================
invE2EPromptOracle
================
*/
func invE2EPromptOracle(inviterGid uint32) []byte {
	return append([]byte{0x05}, invE2EU32(inviterGid)...)
}

// invE2EMember is one hand-rolled member row for the block/join oracles.
type invE2EMember struct {
	jid     uint32
	name    string
	grade   uint8
	perm    uint32
	offline uint8
}

// invE2EMemberRow appends one member row in the pinned 0x32C4 /
// 0x3B29-subOp-2 order (level 1 - the seeded e2e characters persist no
// level - donated 0, dwords 0, empty grantName, refObjId 1907 =
// CHAR_CH_MAN_ADVENTURER male fallback, fortress 0).
/*
================
invE2EMemberRow
================
*/
func invE2EMemberRow(oracle *oracle32C4, member invE2EMember) {
	oracle.u32(member.jid)
	oracle.str(member.name)
	oracle.u8(member.grade)
	oracle.u8(1)
	oracle.u32(0)
	oracle.u32(member.perm)
	oracle.u32(0)
	oracle.u32(0)
	oracle.u32(0)
	oracle.str("")
	oracle.u32(1907)
	oracle.u8(0)
	oracle.u8(member.offline)
}

// invE2EBlockOracle hand-rolls the whole guild block with the notice
// fields parameterized (the notice-edit elicitation in the flow below
// mutates them, so the reboot oracle cannot hardcode empties the way
// mutE2EBlockOracle does).
/*
================
invE2EBlockOracle
================
*/
func invE2EBlockOracle(guildID int64, guildName, subject, contents string, members []invE2EMember) []byte {
	oracle := &oracle32C4{}
	oracle.u32(uint32(guildID))
	oracle.str(guildName)
	oracle.u8(1)
	oracle.u32(0)
	oracle.str(subject)
	oracle.str(contents)
	oracle.u32(0)
	oracle.u8(0)
	oracle.u8(uint8(len(members)))
	for _, member := range members {
		invE2EMemberRow(oracle, member)
	}
	oracle.u8(0)
	return oracle.buf.Bytes()
}

// invE2EJoinOracle hand-rolls the 0x3B29 subOp-2 join push.
/*
================
invE2EJoinOracle
================
*/
func invE2EJoinOracle(member invE2EMember) []byte {
	oracle := &oracle32C4{}
	oracle.u8(2)
	invE2EMemberRow(oracle, member)
	return oracle.buf.Bytes()
}

// invE2EAwaitPending polls the invite runtime's pending count toward
// want (the disconnect drop rides the async close hook).
/*
================
invE2EAwaitPending
================
*/
func invE2EAwaitPending(t *testing.T, invites *guild.InviteRuntime, want int, what string) {
	t.Helper()
	wait.Eventually(t, 5*time.Second, fmt.Sprintf("%s: pending invites to reach %d", what, want), func() bool {
		return invites.PendingInviteCount() == want
	})
}

// TestGuildInviteConsentEndToEndOverWire walks the whole handshake:
// silent invite refusals, the disconnect-mid-prompt drop, the pinned
// {02 16} refuse, the {01 01} accept with both byte-asserted commit
// frames, the stale-consent no-op, the already-in-a-guild re-invite
// refusal, and the reboot persistence of the committed membership.
/*
================
TestGuildInviteConsentEndToEndOverWire
================
*/
func TestGuildInviteConsentEndToEndOverWire(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")

	race := func() *int64 { v := enterworld.RaceChina; return &v }
	gender := func() *int64 { v := enterworld.GenderMale; return &v }
	seeds := []*enterworld.Character{
		{Name: invE2ENameE, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender()},
		{Name: invE2ENameF, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender()},
		{Name: invE2ENameG, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender()},
	}
	first := startInviteServer(t, dir, seeds)
	echo := guildE2ECharacter(t, first.authority, invE2ENameE)
	fox := guildE2ECharacter(t, first.authority, invE2ENameF)
	golf := guildE2ECharacter(t, first.authority, invE2ENameG)
	gidE := enterworld.ObjectIDForCharacter(echo)
	gidF := enterworld.ObjectIDForCharacter(fox)
	gidG := enterworld.ObjectIDForCharacter(golf)

	// ---- E enters and creates the guild over the wire ----
	connE := guildDialWS(t, first.srv)
	guildHelloWS(t, connE)
	guildEnterWorldSeeds(t, connE, invE2ENameE)
	guildSendFrame(t, connE, guild.OpGuildCreateRequest, mutE2ECreatePayload(0, "IronPact"))
	wantAck := append([]byte{0x01}, invE2EBlockOracle(1, "IronPact", "", "", []invE2EMember{
		{jid: uint32(echo.ID), name: invE2ENameE, grade: 0, perm: 0xffffffff, offline: 0},
	})...)
	if got := guildExpectFrame(t, connE, guild.OpGuildCreateAck, "create ack"); !bytes.Equal(got, wantAck) {
		t.Fatalf("0xB663 = % X, want % X", got, wantAck)
	}

	connF := guildDialWS(t, first.srv)
	guildHelloWS(t, connF)
	guildEnterWorldSeeds(t, connF, invE2ENameF)

	// ---- silent invite refusals leave the pending table empty ----
	// Self-invite (the client refuses it before composing @0x00700ba7;
	// a forged one refuses server-side too), then an OFFLINE target
	// (G never connects - the prompt has no session to land on).
	guildSendFrame(t, connE, guild.OpGuildInviteRequest, invE2EU32(gidE))
	guildSendFrame(t, connE, guild.OpGuildInviteRequest, invE2EU32(gidG))
	// The next elicited answer proves both were processed and refused
	// silently: nothing rode either stream meanwhile.
	guildSendFrame(t, connE, guild.OpGuildInviteRequest, invE2EU32(gidF))
	if got, want := guildExpectFrame(t, connF, guild.OpInvitationProposal, "F's type-5 prompt"), invE2EPromptOracle(gidE); !bytes.Equal(got, want) {
		t.Fatalf("0x3393 prompt = % X, want % X", got, want)
	}
	if got := first.invites.PendingInviteCount(); got != 1 {
		t.Fatalf("pending invites after the three 0x73AD = %d, want 1 (self and offline refused)", got)
	}

	// ---- disconnect mid-prompt: the invitation dies with F ----
	guildSendFrame(t, connF, transport.OpBye, []byte{transport.ByeReasonNormal})
	connF.Close()
	invE2EAwaitPending(t, first.invites, 0, "after F's disconnect")

	// F's NEXT session answers the dead prompt: a no-op. The elicited
	// 0xB663 {02 18} (a 13-byte create name - the evidenced refusal
	// answer) proves the consent was processed and nothing else rode
	// F's stream.
	connF2 := guildDialWS(t, first.srv)
	guildHelloWS(t, connF2)
	guildEnterWorldSeeds(t, connF2, invE2ENameF)
	guildSendFrame(t, connF2, guild.OpInvitationProposal, []byte{0x01, 0x01})
	guildSendFrame(t, connF2, guild.OpGuildCreateRequest, mutE2ECreatePayload(0, "ThirteenChars"))
	if got := guildExpectFrame(t, connF2, guild.OpGuildCreateAck, "post-stale-consent elicitation"); !bytes.Equal(got, []byte{0x02, 0x18}) {
		t.Fatalf("elicited 0xB663 = % X, want [02 18]", got)
	}
	if _, joined := first.authority.Guilds().GuildOfCharacter(guildE2EDivision, fox.ID); joined {
		t.Fatalf("a consent to a dead prompt committed membership")
	}

	// ---- refuse: the pinned {02 16} consumes and commits nothing ----
	guildSendFrame(t, connE, guild.OpGuildInviteRequest, invE2EU32(gidF))
	if got, want := guildExpectFrame(t, connF2, guild.OpInvitationProposal, "F's second prompt"), invE2EPromptOracle(gidE); !bytes.Equal(got, want) {
		t.Fatalf("0x3393 prompt = % X, want % X", got, want)
	}
	guildSendFrame(t, connF2, guild.OpInvitationProposal, []byte{0x02, 0x16})
	guildSendFrame(t, connF2, guild.OpGuildCreateRequest, mutE2ECreatePayload(0, "ThirteenChars"))
	if got := guildExpectFrame(t, connF2, guild.OpGuildCreateAck, "post-refuse elicitation"); !bytes.Equal(got, []byte{0x02, 0x18}) {
		t.Fatalf("elicited 0xB663 = % X, want [02 18]", got)
	}
	if got := first.invites.PendingInviteCount(); got != 0 {
		t.Fatalf("pending invites after the refuse = %d, want 0 (consumed)", got)
	}
	if _, joined := first.authority.Guilds().GuildOfCharacter(guildE2EDivision, fox.ID); joined {
		t.Fatalf("the {02 16} refuse committed membership")
	}

	// ---- accept: {01 01} commits and pushes both pinned frames ----
	guildSendFrame(t, connE, guild.OpGuildInviteRequest, invE2EU32(gidF))
	guildExpectFrame(t, connF2, guild.OpInvitationProposal, "F's third prompt")
	guildSendFrame(t, connF2, guild.OpInvitationProposal, []byte{0x01, 0x01})
	joinedRow := invE2EMember{jid: uint32(fox.ID), name: invE2ENameF, grade: 0x0a, perm: 0, offline: 0}
	wantBlock := invE2EBlockOracle(1, "IronPact", "", "", []invE2EMember{
		{jid: uint32(echo.ID), name: invE2ENameE, grade: 0, perm: 0xffffffff, offline: 0},
		joinedRow,
	})
	if got := guildExpectFrame(t, connF2, guild.OpGuildInfo, "the joiner's 0x32C4"); !bytes.Equal(got, wantBlock) {
		t.Fatalf("joiner's 0x32C4 = % X, want % X", got, wantBlock)
	}
	if got, want := guildExpectFrame(t, connE, guild.OpGuildUpdatePush, "the sitting member's subOp-2"), invE2EJoinOracle(joinedRow); !bytes.Equal(got, want) {
		t.Fatalf("0x3B29 subOp-2 = % X, want % X", got, want)
	}
	if gid, joined := first.authority.Guilds().GuildOfCharacter(guildE2EDivision, fox.ID); !joined || gid != 1 {
		t.Fatalf("GuildOfCharacter(F) = %d/%v, want 1/true", gid, joined)
	}

	// ---- stale consent + already-in-a-guild re-invite: no-ops ----
	guildSendFrame(t, connF2, guild.OpInvitationProposal, []byte{0x01, 0x01})
	guildSendFrame(t, connE, guild.OpGuildInviteRequest, invE2EU32(gidF))
	// Elicit through the notice edit: E gets the 0xB77A ack and BOTH
	// members the subOp-5 push - F's next frame being that push proves
	// the stale consent and the re-invite emitted nothing to F.
	guildSendFrame(t, connE, guild.OpGuildNoticeEditRequest, mutatorNoticePayload("war notice", "march friday"))
	if got := guildExpectFrame(t, connE, guild.OpGuildNoticeEditAck, "notice ack"); !bytes.Equal(got, []byte{0x01}) {
		t.Fatalf("0xB77A = % X, want [01]", got)
	}
	guildExpectFrame(t, connE, guild.OpGuildUpdatePush, "E's subOp-5")
	guildExpectFrame(t, connF2, guild.OpGuildUpdatePush, "F's subOp-5")
	if got := first.invites.PendingInviteCount(); got != 0 {
		t.Fatalf("pending invites after the guilded re-invite = %d, want 0", got)
	}
	if _, members, ok := first.authority.Guilds().Guild(guildE2EDivision, 1); !ok || len(members) != 2 {
		t.Fatalf("guild members after the stale consent = %d, want 2", len(members))
	}

	if dropped := first.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s)", dropped)
	}
	guildSendFrame(t, connE, transport.OpBye, []byte{transport.ByeReasonNormal})
	guildSendFrame(t, connF2, transport.OpBye, []byte{transport.ByeReasonNormal})
	connE.Close()
	connF2.Close()

	// ---- reboot: the committed membership persists ----
	guildShutdownServer(t, first.srv)
	first.authority.Close()
	second := startInviteServer(t, dir, nil)
	fox2 := guildE2ECharacter(t, second.authority, invE2ENameF)
	echo2 := guildE2ECharacter(t, second.authority, invE2ENameE)
	connF3 := guildDialWS(t, second.srv)
	guildHelloWS(t, connF3)
	seed := guildEnterWorld(t, connF3, invE2ENameF)
	wantSeed := invE2EBlockOracle(1, "IronPact", "war notice", "march friday", []invE2EMember{
		{jid: uint32(echo2.ID), name: invE2ENameE, grade: 0, perm: 0xffffffff, offline: 1},
		// F itself is not yet bound at its own seed-compose time.
		{jid: uint32(fox2.ID), name: invE2ENameF, grade: 0x0a, perm: 0, offline: 1},
	})
	if !bytes.Equal(seed, wantSeed) {
		t.Fatalf("post-reboot 0x32C4 = % X, want % X", seed, wantSeed)
	}
	guildSendFrame(t, connF3, transport.OpBye, []byte{transport.ByeReasonNormal})
}

// TestGuildInvitePartyOneProposalPerPlayerEndToEnd pins the native
// one-transaction-per-player rule (46F420) across the shared 0x3393: a
// waiting party proposal makes the guild invite fail silently, a waiting
// guild proposal makes the party invite fail with {2, 2} on B0D5 and
// B452, and byte-identical {01 01} answers route to the lane that holds
// the only pending.
/*
================
TestGuildInvitePartyOneProposalPerPlayerEndToEnd
================
*/
func TestGuildInvitePartyOneProposalPerPlayerEndToEnd(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")

	race := func() *int64 { v := enterworld.RaceChina; return &v }
	gender := func() *int64 { v := enterworld.GenderMale; return &v }
	seeds := []*enterworld.Character{
		{Name: invE2ENameH, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender()},
		{Name: invE2ENameI, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender()},
	}
	server := startInviteServer(t, dir, seeds)
	hotel := guildE2ECharacter(t, server.authority, invE2ENameH)
	india := guildE2ECharacter(t, server.authority, invE2ENameI)
	gidH := enterworld.ObjectIDForCharacter(hotel)
	gidI := enterworld.ObjectIDForCharacter(india)

	connH := guildDialWS(t, server.srv)
	guildHelloWS(t, connH)
	guildEnterWorldSeeds(t, connH, invE2ENameH)
	guildSendFrame(t, connH, guild.OpGuildCreateRequest, mutE2ECreatePayload(0, "OakWard"))
	guildExpectFrame(t, connH, guild.OpGuildCreateAck, "create ack")

	connI := guildDialWS(t, server.srv)
	guildHelloWS(t, connI)
	guildEnterWorldSeeds(t, connI, invE2ENameI)

	// ---- a party proposal waits: the guild invite fails silently ----
	guildSendFrame(t, connH, party.OpPartyInviteRequest, invE2EPartyInvitePayload(gidI, 0))
	if got, want := guildExpectFrame(t, connI, guild.OpInvitationProposal, "party prompt"), append(append([]byte{0x02}, invE2EU32(gidH)...), 0); !bytes.Equal(got, want) {
		t.Fatalf("party prompt = % X, want % X", got, want)
	}
	guildSendFrame(t, connH, guild.OpGuildInviteRequest, invE2EU32(gidI))
	// H's silent invite must finish before I can release the party proposal.
	// WebSocket writes on two sessions do not establish server dispatch order.
	guildSendFrame(t, connH, guild.OpGuildNoticeEditRequest, mutatorNoticePayload("", "body"))
	if got := guildExpectFrame(t, connH, guild.OpGuildNoticeEditAck, "H's dispatch barrier"); !bytes.Equal(got, []byte{2, 0x22}) {
		t.Fatalf("empty notice refusal = % X, want 02 22", got)
	}
	if got := server.invites.PendingInviteCount(); got != 0 {
		t.Fatalf("guild pendings over a waiting party proposal = %d, want 0", got)
	}
	if got := server.partyRt.Registry().PendingInviteCount(); got != 1 {
		t.Fatalf("party pendings = %d, want 1 (untouched)", got)
	}

	// I refuses; the next frame I sees is the refusal, not a guild prompt.
	guildSendFrame(t, connI, guild.OpInvitationProposal, []byte{0x02, 0x0c})
	if got := guildExpectFrame(t, connI, party.OpPartyJoinAck, "I's refusal ack"); !bytes.Equal(got, []byte{2, 0x0c}) {
		t.Fatalf("I's 0xB452 = % X, want 02 0C", got)
	}
	guildExpectFrame(t, connH, party.OpCreatePartyAck, "H's refusal ack")

	// ---- a guild proposal waits: the party invite fails with {2, 2} ----
	guildSendFrame(t, connH, guild.OpGuildInviteRequest, invE2EU32(gidI))
	if got, want := guildExpectFrame(t, connI, guild.OpInvitationProposal, "guild prompt"), invE2EPromptOracle(gidH); !bytes.Equal(got, want) {
		t.Fatalf("guild prompt = % X, want % X", got, want)
	}
	guildSendFrame(t, connH, party.OpPartyInviteRequest, invE2EPartyInvitePayload(gidI, 0))
	if got := guildExpectFrame(t, connH, party.OpCreatePartyAck, "H's busy ack"); !bytes.Equal(got, []byte{2, 2}) {
		t.Fatalf("H's 0xB0D5 = % X, want 02 02", got)
	}
	if got := guildExpectFrame(t, connI, party.OpPartyJoinAck, "I's busy ack"); !bytes.Equal(got, []byte{2, 2}) {
		t.Fatalf("I's 0xB452 = % X, want 02 02", got)
	}
	if got := server.partyRt.Registry().PendingInviteCount(); got != 0 {
		t.Fatalf("party pendings over a waiting guild proposal = %d, want 0", got)
	}

	// ---- {01 01} routes to GUILD, the only lane holding a pending ----
	guildSendFrame(t, connI, guild.OpInvitationProposal, []byte{0x01, 0x01})
	guildExpectFrame(t, connI, guild.OpGuildInfo, "I's 0x32C4")
	guildExpectFrame(t, connH, guild.OpGuildUpdatePush, "H's subOp-2")
	if gid, joined := server.authority.Guilds().GuildOfCharacter(guildE2EDivision, india.ID); !joined || gid != 1 {
		t.Fatalf("GuildOfCharacter(I) = %d/%v, want 1/true", gid, joined)
	}

	if dropped := server.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s)", dropped)
	}
	guildSendFrame(t, connH, transport.OpBye, []byte{transport.ByeReasonNormal})
	guildSendFrame(t, connI, transport.OpBye, []byte{transport.ByeReasonNormal})
}
