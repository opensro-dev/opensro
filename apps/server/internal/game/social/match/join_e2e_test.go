/*
===========================================================================

join_e2e_test.go - party matching consent and native roster wire verification.

===========================================================================
*/
package match_test

// End-to-end exercise of the match-JOIN owner-approval handshake over
// the REAL transport (the e2e_wire_test.go harness; this suite composes
// the match runtime WITH its party/mentor seams exactly like wiring.go),
// three WebSocket clients speaking the production frame protocol:
//
//	stale entry join      -> 0xB5BF {01 00} refused (pinned detail arm);
//	join                  -> the OWNER gets the 0x75BF notify byte-exact
//	                         (requestId/entryId echo pair, purpose, the
//	                         joiner's sub_75db30 masked member record);
//	displacement          -> a second joiner displaces the first, who is
//	                         acked {01 02} no-reply (the owner pane holds
//	                         exactly one request - sub_63cbc0 overwrites);
//	owner refuse (ans 0)  -> joiner {01 00};
//	owner no-reply (ans 2)-> joiner {01 02};
//	owner disconnect      -> the parked joiner is acked {01 02} and the
//	                         owner's listings purge (a later join of the
//	                         dead listing refuses as stale - the "owner
//	                         offline" case collapses into stale-id
//	                         BECAUSE disconnect purges the board);
//	own listing           -> {01 00};
//	owner accept, partyless -> the party FORMS through internal/game/social/party
//	                         (optionBits = the listing's type bits):
//	                         both sides get the pinned 0xB0D5 + 0x35D6
//	                         seeds byte-exact, THEN the joiner {01 01};
//	owner accept, partied -> the joiner JOINS: sitting members get the
//	                         0x3E58 type-2 row, the joiner the seeds +
//	                         {01 01};
//	already partied       -> {01 00} (precheck through the party seam);
//	mentor join           -> the owner gets the 0x7592 notify byte-exact
//	                         (echo pair, "%d(%d)" level pair, RefObjID,
//	                         name); accept commits the camp through
//	                         internal/game/social/mentor's atomic doors: the joiner gets
//	                         the 0x3AC5 status-10 sub-1 seed + 0xB592
//	                         {01 01}, the master (fresh camp) their own
//	                         seed, and the training-camp STORE holds the
//	                         membership;
//	already camped        -> {01 00} (precheck through the mentor seam);
//	reboot                -> pending join requests die with the process:
//	                         the parked request's answer drops SILENTLY
//	                         on the fresh server (game-ready barrier).

import (
	"opensro.online/server/internal/domain"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/enterworld"
	presence "opensro.online/server/internal/game/social"
	"opensro.online/server/internal/game/social/match"
	"opensro.online/server/internal/game/social/mentor"
	"opensro.online/server/internal/game/social/party"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

const (
	e2eMiraName = "e2eJoinMira" // owner: party leader-to-be + camp master (level 60)
	e2eJackName = "e2eJoinJack" // first joiner (level 20 - inside the student band)
	e2eKateName = "e2eJoinKate" // second joiner (level 25)
)

// Creation order pins the store IDs 1..3, so the world gids are the
// 100000+ID band enterworld.ObjectIDForCharacter latches.
const (
	gidMira uint32 = 100001
	gidJack uint32 = 100002
	gidKate uint32 = 100003
)

/*
================
joinServer
================
*/
type joinServer struct {
	srv       *transport.Server
	authority *store.Store
	deps      *enterworld.Deps
	runtime   *match.Runtime
	partyRt   *party.Runtime
	mentorRt  *mentor.InviteRuntime
}

// startJoinServer stands up the transport with the bootstrap + match +
// party + mentor lanes composed like wiring.go: one deps pointer (store
// collaborators - including the training-camp door - assigned BEFORE
// the pointer-based Registers share it), the presence facade, the match
// runtime's party/mentor seams pointed at the owning runtimes, and the
// stale-state purges on OnWorldBound / OnSessionClose.
/*
================
startJoinServer
================
*/
func startJoinServer(t *testing.T, dir string, seeds []*enterworld.Character) joinServer {
	t.Helper()

	authority, err := store.Open(dir, store.Options{DefaultSkills: matchSkillSeeder})
	if err != nil {
		t.Fatalf("store.Open(%s): %v", dir, err)
	}
	t.Cleanup(authority.Close)

	if len(authority.Characters().CharactersForDivision(e2eDivision)) == 0 {
		for _, seed := range seeds {
			if err := authority.CreateCharacter(e2eDivision, "test-account", seed); err != nil {
				t.Fatalf("CreateCharacter(%s): %v", seed.Name, err)
			}
		}
	}

	deps := &enterworld.Deps{
		Roster:        &enterworld.Roster{},
		Characters:    authority.Characters(),
		TrainingCamps: authority.TrainingCamps(),
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
	partyRt := party.NewRuntime(deps, presence)
	// Everyone stands together: these suites test invitation lanes, not reach.
	partyRt.UseLivePose(func(string, *enterworld.Character) simulation.Spawn {
		return simulation.Spawn{RegionID: 0x6A48, X: 900, Z: 900}
	})
	mentorRt := mentor.NewInviteRuntime(deps, presence)
	partyRt.AddConsentArm(mentorRt)

	runtime := match.NewRuntime(deps, presence)
	runtime.MemberCountFor = func(divisionID, name string) int {
		if snapshot, ok := partyRt.Registry().PartyOf(divisionID, name); ok {
			return len(snapshot.Members)
		}
		return 1
	}
	// The wiring.go match-JOIN seams, verbatim.
	runtime.PartyListingAuthority = partyRt.ListingAuthority
	runtime.PartyJoinPrecheck = partyRt.MatchJoinPrecheck
	runtime.CommitPartyJoin = partyRt.AdmitMatchJoin
	runtime.PartyMemberInfoFor = partyRt.MaskedMemberInfoFor
	runtime.MentorJoinPrecheck = mentorRt.MatchJoinPrecheck
	runtime.CommitMentorJoin = mentorRt.CommitMatchJoin

	deps.OnWorldBound = func(s *transport.Session, divisionID string, character *enterworld.Character) {
		key := divisionID + ":" + strings.ToLower(character.Name)
		if old, replaced := srv.Hub.BindExclusive(key, s); replaced {
			old.ClearGameplayContext()
		}
		if s.Evicted() {
			return
		}
		partyRt.WorldBound(s, divisionID, character)
		runtime.WorldBound(divisionID, character)
		mentorRt.WorldBound(s, divisionID, character)
	}
	srv.Hub.OnSessionClose(func(s *transport.Session, _ error) {
		partyRt.SessionClosed(s)
		runtime.SessionClosed(s)
		mentorRt.SessionClosed(s)
	})

	enterworld.Register(srv.Hub, deps)
	partyRt.Register(srv.Hub)
	runtime.Register(srv.Hub)
	mentorRt.Register(srv.Hub)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shutdownServer(t, srv) })
	return joinServer{srv: srv, authority: authority, deps: deps, runtime: runtime, partyRt: partyRt, mentorRt: mentorRt}
}

// joinAnswer renders the sub_6fe370 / sub_6fe690 owner-answer body.
/*
================
joinAnswer
================
*/
func joinAnswer(requestID, entryID uint32, answer uint8) []byte {
	return concat(u32le(requestID), u32le(entryID), []byte{answer})
}

// chinaRow is the expected masked member row for this suite's fresh
// China-male seeds (the party e2e's chinaMaleRow with the level
// parameterized): model 1907 fallback, full vitals 0xAA, the China
// start profile spawn.
/*
================
chinaRow
================
*/
func chinaRow(gid uint32, name string, level uint8) party.MemberRow {
	return party.MemberRow{
		MemberID:      gid,
		Name:          name,
		ModelRefID:    1907,
		Level:         level,
		StatusNibbles: 0xAA,
		Region:        0x62A8,
		PosX:          960,
		PosY:          20,
		PosZ:          458,
		War:           domain.DefaultWorldInstance,
	}
}

// expectJoinSeed asserts the enter-a-party pair the accepted join
// commits toward one session: 0xB0D5 result-1 with the receiver's OWN
// gid, then the 0x35D6 settings+roster bulk.
/*
================
expectJoinSeed
================
*/
func expectJoinSeed(t *testing.T, c *websocket.Conn, ownGid, leaderGid uint32, optionBits uint8, rows []party.MemberRow, what string) {
	t.Helper()
	expectExactFrame(t, c, party.OpCreatePartyAck, party.EncodeCreatePartyAckB0D5(ownGid), what+" 0xB0D5 ack")
	expectExactFrame(t, c, party.OpPartyInfo, party.EncodePartyInfo35D6(leaderGid, optionBits, rows), what+" 0x35D6 bulk")
}

// campRow is the expected 0x3AC5 member wire row for one seed (empty
// location until a status-13 coord row would update it).
/*
================
campRow
================
*/
func campRow(gid uint32, name string, kind, level uint8) mentor.MemberWireRow {
	return mentor.MemberWireRow{MemberID: gid, Name: name, Kind: kind, LevelByte58: level, Level: level}
}

/*
================
TestMatchJoinHandshakeEndToEndOverWire
================
*/
func TestMatchJoinHandshakeEndToEndOverWire(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")

	seeds := []*enterworld.Character{
		{
			Name:          e2eMiraName,
			ModelCodename: "CHAR_CH_MAN_ADVENTURER",
			RaceIndex:     e2eInt64(enterworld.RaceChina),
			Gender:        e2eInt64(enterworld.GenderMale),
			Level:         e2eInt64(60),
		},
		{
			Name:          e2eJackName,
			Masteries:     []domain.CharacterMastery{{ID: 273, Level: 12}, {ID: 257, Level: 20}, {ID: 258, Level: 12}},
			ModelCodename: "CHAR_CH_MAN_ADVENTURER",
			RaceIndex:     e2eInt64(enterworld.RaceChina),
			Gender:        e2eInt64(enterworld.GenderMale),
			Level:         e2eInt64(20),
		},
		{
			Name:          e2eKateName,
			ModelCodename: "CHAR_CH_MAN_ADVENTURER",
			RaceIndex:     e2eInt64(enterworld.RaceChina),
			Gender:        e2eInt64(enterworld.GenderMale),
			Level:         e2eInt64(25),
		},
	}

	first := startJoinServer(t, dir, seeds)

	mira := dialWS(t, first.srv)
	helloWS(t, mira)
	enterWorld(t, mira, e2eMiraName)

	jack := dialWS(t, first.srv)
	helloWS(t, jack)
	enterWorld(t, jack, e2eJackName)

	kate := dialWS(t, first.srv)
	helloWS(t, kate)
	enterWorld(t, kate, e2eKateName)

	// ---- stale entry id: the pinned {01 00} refusal ----
	sendFrame(t, jack, match.OpPartyJoinRequest, u32le(99))
	expectExactFrame(t, jack, match.OpPartyJoinAck, []byte{1, 0}, "stale-entry join refusal")

	// ---- Mira lists a party (entry 1): typeBits 5 = exp-share |
	// join-anyone, purpose 0 ----
	sendFrame(t, mira, match.OpPartyRegisterRequest, partyRequest(0, 777, 5, 0, 1, 80, "mira party"))
	expectExactFrame(t, mira, match.OpPartyRegisterAck,
		concat([]byte{1}, u32le(1), u32le(777), []byte{5, 0, 1, 80}, wideStr("mira party")),
		"mira party register ack")

	// ---- Jack joins: Mira gets the 0x75BF notify byte-exact ----
	sendFrame(t, jack, match.OpPartyJoinRequest, u32le(1))
	jackRow := party.EncodeMaskedMemberRow(chinaRow(gidJack, e2eJackName, 20))
	expectExactFrame(t, mira, match.OpPartyJoinRequest,
		match.EncodePartyJoinNotify75BF(1, 1, match.PartyApplicant{Primary: 257, Secondary: 258, JobClass: 4}, jackRow),
		"owner notify for jack (request 1)")

	// ---- Kate's request DISPLACES Jack's: Jack is acked no-reply and
	// Mira gets the fresh notify ----
	sendFrame(t, kate, match.OpPartyJoinRequest, u32le(1))
	expectExactFrame(t, jack, match.OpPartyJoinAck, []byte{1, 2}, "jack displaced - no-reply ack")
	kateRow := party.EncodeMaskedMemberRow(chinaRow(gidKate, e2eKateName, 25))
	expectExactFrame(t, mira, match.OpPartyJoinRequest,
		match.EncodePartyJoinNotify75BF(2, 1, match.PartyApplicant{JobClass: 4}, kateRow),
		"owner notify for kate (request 2)")

	// ---- Mira REFUSES (answer 0) -> Kate {01 00} ----
	sendFrame(t, mira, match.OpPartyJoinAnswer, joinAnswer(2, 1, 0))
	expectExactFrame(t, kate, match.OpPartyJoinAck, []byte{1, 0}, "kate refused by the owner")

	// ---- Mira answers NO-REPLY (answer 2, the sub_63c340 arm) ----
	sendFrame(t, jack, match.OpPartyJoinRequest, u32le(1))
	expectExactFrame(t, mira, match.OpPartyJoinRequest,
		match.EncodePartyJoinNotify75BF(3, 1, match.PartyApplicant{Primary: 257, Secondary: 258, JobClass: 4}, jackRow),
		"owner notify for jack (request 3)")
	sendFrame(t, mira, match.OpPartyJoinAnswer, joinAnswer(3, 1, 2))
	expectExactFrame(t, jack, match.OpPartyJoinAck, []byte{1, 2}, "jack no-replied by the owner")

	// ---- owner disconnect with a PARKED request: the joiner is acked
	// no-reply and the dead owner's listing purges (a later join of it
	// refuses as stale - the owner-offline posture) ----
	sendFrame(t, kate, match.OpPartyRegisterRequest, partyRequest(0, 888, 0, 1, 1, 40, "kate party"))
	expectExactFrame(t, kate, match.OpPartyRegisterAck,
		concat([]byte{1}, u32le(2), u32le(888), []byte{0, 1, 1, 40}, wideStr("kate party")),
		"kate party register ack")
	sendFrame(t, jack, match.OpPartyJoinRequest, u32le(2))
	expectExactFrame(t, kate, match.OpPartyJoinRequest,
		match.EncodePartyJoinNotify75BF(4, 2, match.PartyApplicant{Primary: 257, Secondary: 258, JobClass: 4}, jackRow),
		"owner notify for jack toward kate (request 4)")
	sendFrame(t, kate, transport.OpBye, []byte{transport.ByeReasonNormal})
	kate.Close()
	expectExactFrame(t, jack, match.OpPartyJoinAck, []byte{1, 2}, "jack orphaned by kate's disconnect")
	sendFrame(t, jack, match.OpPartyJoinRequest, u32le(2))
	expectExactFrame(t, jack, match.OpPartyJoinAck, []byte{1, 0}, "kate's purged listing refuses as stale")

	// ---- own listing ----
	sendFrame(t, mira, match.OpPartyJoinRequest, u32le(1))
	expectExactFrame(t, mira, match.OpPartyJoinAck, []byte{1, 0}, "own-listing join refusal")

	// ---- ACCEPT, owner partyless: the party FORMS through internal/game/social/party
	// with the LISTING's type bits as its option bits ----
	sendFrame(t, jack, match.OpPartyJoinRequest, u32le(1))
	expectExactFrame(t, mira, match.OpPartyJoinRequest,
		match.EncodePartyJoinNotify75BF(5, 1, match.PartyApplicant{Primary: 257, Secondary: 258, JobClass: 4}, jackRow),
		"owner notify for jack (request 5)")
	sendFrame(t, mira, match.OpPartyJoinAnswer, joinAnswer(5, 1, 1))
	formedRows := []party.MemberRow{
		chinaRow(gidMira, e2eMiraName, 60),
		chinaRow(gidJack, e2eJackName, 20),
	}
	expectJoinSeed(t, mira, gidMira, gidMira, 5, formedRows, "mira (leader) formed seed")
	expectJoinSeed(t, jack, gidJack, gidMira, 5, formedRows, "jack (joiner) formed seed")
	expectExactFrame(t, jack, match.OpPartyJoinAck, []byte{1, 1}, "jack join complete")
	if snapshot, ok := first.partyRt.Registry().PartyOf(e2eDivision, e2eJackName); !ok || len(snapshot.Members) != 2 || snapshot.LeaderID != gidMira {
		t.Fatalf("party registry after the formed join = %+v (ok=%v), want mira-led pair", snapshot, ok)
	}

	// ---- already partied ----
	sendFrame(t, jack, match.OpPartyJoinRequest, u32le(1))
	expectExactFrame(t, jack, match.OpPartyJoinAck, []byte{1, 0}, "partied joiner refusal")

	// ---- ACCEPT, owner partied: Kate (reconnected) JOINS the existing
	// party - the sitting members get the 0x3E58 type-2 row ----
	kate2 := dialWS(t, first.srv)
	helloWS(t, kate2)
	enterWorld(t, kate2, e2eKateName)
	sendFrame(t, kate2, match.OpPartyJoinRequest, u32le(1))
	expectExactFrame(t, mira, match.OpPartyJoinRequest,
		match.EncodePartyJoinNotify75BF(6, 1, match.PartyApplicant{JobClass: 4}, kateRow),
		"owner notify for kate (request 6)")
	sendFrame(t, mira, match.OpPartyJoinAnswer, joinAnswer(6, 1, 1))
	kateJoinRow := party.EncodePartyJoin3E58(chinaRow(gidKate, e2eKateName, 25))
	expectExactFrame(t, mira, party.OpPartyUpdate, kateJoinRow, "mira sees kate's 0x3E58 join row")
	expectExactFrame(t, jack, party.OpPartyUpdate, kateJoinRow, "jack sees kate's 0x3E58 join row")
	expectJoinSeed(t, kate2, gidKate, gidMira, 5,
		append(formedRows, chinaRow(gidKate, e2eKateName, 25)), "kate (joiner) seed")
	expectExactFrame(t, kate2, match.OpPartyJoinAck, []byte{1, 1}, "kate join complete")

	// ---- mentor join: Mira (level 60 master) lists (entry 3), Jack
	// (level 20, campless) joins ----
	sendFrame(t, mira, match.OpMentorRegisterRequest, mentorRequest(0, 0, 2, "mira academy"))
	expectExactFrame(t, mira, match.OpMentorRegisterAck,
		concat([]byte{1}, u32le(3), u32le(0), []byte{2}, wideStr("mira academy"), u32le(0)),
		"mira mentor register ack")
	sendFrame(t, jack, match.OpMentorJoinRequest, u32le(3))
	expectExactFrame(t, mira, match.OpMentorJoinRequest,
		match.EncodeMentorJoinNotify7592(7, 3, 20, 1907, e2eJackName),
		"mentor owner notify for jack (request 7)")
	sendFrame(t, mira, match.OpMentorJoinAnswer, joinAnswer(7, 3, 1))
	campRows := []mentor.MemberWireRow{
		campRow(gidMira, e2eMiraName, mentor.MemberKindMaster, 60),
		campRow(gidJack, e2eJackName, mentor.MemberKindStudent, 20),
	}
	expectExactFrame(t, jack, mentor.OpTCStatus,
		mentor.EncodeCampSeed3AC5(gidJack, "", "", campRows), "jack's 0x3AC5 camp seed")
	expectExactFrame(t, jack, match.OpMentorJoinAck, []byte{1, 1}, "jack mentor join complete")
	expectExactFrame(t, mira, mentor.OpTCStatus,
		mentor.EncodeCampSeed3AC5(gidMira, "", "", campRows), "mira's 0x3AC5 seed (fresh camp)")
	var jackChar *enterworld.Character
	for _, c := range first.deps.Characters.CharactersForDivision(e2eDivision) {
		if c.Name == e2eJackName {
			jackChar = c
		}
	}
	if jackChar == nil {
		t.Fatal("jack missing from the store")
	}
	if campID, joined := first.authority.TrainingCamps().CampOfCharacter(e2eDivision, jackChar.ID); !joined || campID == 0 {
		t.Fatalf("training-camp store after the mentor join: campID=%d joined=%v, want a persisted membership", campID, joined)
	}

	// ---- already camped ----
	sendFrame(t, jack, match.OpMentorJoinRequest, u32le(3))
	expectExactFrame(t, jack, match.OpMentorJoinAck, []byte{1, 0}, "camped joiner refusal")

	// ---- reboot: a parked request dies with the process ----
	sendFrame(t, kate2, match.OpMentorJoinRequest, u32le(3))
	expectExactFrame(t, mira, match.OpMentorJoinRequest,
		match.EncodeMentorJoinNotify7592(8, 3, 25, 1907, e2eKateName),
		"mentor owner notify for kate (request 8)")
	if got := first.runtime.PendingJoinCount(); got != 1 {
		t.Fatalf("pending join count before the reboot = %d, want 1", got)
	}
	sendFrame(t, mira, transport.OpBye, []byte{transport.ByeReasonNormal})
	sendFrame(t, jack, transport.OpBye, []byte{transport.ByeReasonNormal})
	sendFrame(t, kate2, transport.OpBye, []byte{transport.ByeReasonNormal})
	mira.Close()
	jack.Close()
	kate2.Close()
	shutdownServer(t, first.srv)
	first.authority.Close()

	second := startJoinServer(t, dir, nil)
	if got := second.runtime.PendingJoinCount(); got != 0 {
		t.Fatalf("pending join count after the reboot = %d, want 0 (in-memory by design)", got)
	}
	mira2 := dialWS(t, second.srv)
	helloWS(t, mira2)
	// Mira is now a PERSISTED camp master. Complete EnterWorld and the
	// one-shot GameReady admission first; the mentor WorldBound hook then
	// publishes her 0x3AC5 roster behind the core ready burst.
	enterWorld(t, mira2, e2eMiraName)
	expectExactFrame(t, mira2, mentor.OpTCStatus,
		mentor.EncodeCampSeed3AC5(gidMira, "", "", campRows), "mira's post-reboot 0x3AC5 camp seed")
	// The pre-reboot answer drops SILENTLY: the table is empty and the
	// barrier proves nothing rode.
	sendFrame(t, mira2, match.OpMentorJoinAnswer, joinAnswer(8, 3, 1))
	gameReadyBarrier(t, mira2, "post-reboot stale answer")
	sendFrame(t, mira2, transport.OpBye, []byte{transport.ByeReasonNormal})
}

/*
================
TestPartyApprovalRevalidatesDeletedAndModifiedListing
================
*/
func TestPartyApprovalRevalidatesDeletedAndModifiedListing(t *testing.T) {
	seeds := []*enterworld.Character{
		{Name: e2eMiraName, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: e2eInt64(enterworld.RaceChina), Gender: e2eInt64(enterworld.GenderMale), Level: e2eInt64(60)},
		{Name: e2eJackName, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: e2eInt64(enterworld.RaceChina), Gender: e2eInt64(enterworld.GenderMale), Level: e2eInt64(20)},
	}
	server := startJoinServer(t, filepath.Join(t.TempDir(), "authority"), seeds)
	owner := dialWS(t, server.srv)
	helloWS(t, owner)
	enterWorld(t, owner, e2eMiraName)
	joiner := dialWS(t, server.srv)
	helloWS(t, joiner)
	enterWorld(t, joiner, e2eJackName)
	member := party.EncodeMaskedMemberRow(chinaRow(gidJack, e2eJackName, 20))
	for _, invalid := range [][]byte{partyRequest(0, 0, 8, 0, 1, 90, "x"), partyRequest(0, 0, 0, 0, 0, 90, "x"), partyRequest(0, 0, 0, 0, 40, 20, "x"), partyRequest(0, 0, 0, 0, 1, 90, "")} {
		sendFrame(t, owner, match.OpPartyRegisterRequest, invalid)
		expectExactFrame(t, owner, match.OpPartyRegisterAck, []byte{2, 2}, "invalid registration")
	}
	for _, id := range []uint32{1, 2} {
		sendFrame(t, owner, match.OpPartyRegisterRequest, partyRequest(0, 0, 3, 0, 1, 90, "Hunting"))
		expectExactFrame(t, owner, match.OpPartyRegisterAck, concat([]byte{1}, u32le(id), u32le(0), []byte{3, 0, 1, 90}, wideStr("Hunting")), "registration")
		sendFrame(t, joiner, match.OpPartyJoinRequest, u32le(id))
		expectExactFrame(t, owner, match.OpPartyJoinRequest, match.EncodePartyJoinNotify75BF(id, id, match.PartyApplicant{JobClass: 4}, member), "notify")
		if id == 1 {
			sendFrame(t, owner, match.OpPartyDeleteRequest, u32le(id))
			expectExactFrame(t, owner, match.OpPartyDeleteAck, concat([]byte{1}, u32le(id)), "delete while approval pending")
		} else {
			sendFrame(t, owner, match.OpPartyModifyRequest, partyRequest(id, 0, 3, 0, 40, 90, "Hunting"))
			expectExactFrame(t, owner, match.OpPartyModifyAck, concat([]byte{1}, u32le(id), u32le(0), []byte{3, 0, 40, 90}, wideStr("Hunting")), "change eligibility while approval pending")
		}
		sendFrame(t, owner, match.OpPartyJoinAnswer, joinAnswer(id, id, 1))
		expectExactFrame(t, joiner, match.OpPartyJoinAck, []byte{1, 0}, "stale approval refused")
		gameReadyBarrier(t, owner, "no owner roster mutation")
		gameReadyBarrier(t, joiner, "no joiner roster mutation")
	}
	sendFrame(t, joiner, match.OpPartyJoinRequest, u32le(2))
	// The level gate names its reason: UIIT_MSG_PARTYMATCH_JOIN_ERROR_LEVEL.
	expectExactFrame(t, joiner, match.OpPartyJoinAck, match.EncodeJoinError(match.JoinErrorLevel), "out-of-band level refuses before prompt")
	if server.runtime.PendingJoinCount() != 0 {
		t.Fatal("refused approvals retained pending joins")
	}
}
