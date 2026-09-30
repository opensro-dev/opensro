/*
===========================================================================

e2e_wire_test.go - real-transport party consent, membership and roster delivery.

===========================================================================
*/
package party_test

// End-to-end exercise of the PARTY lane over the REAL transport with
// THREE live sessions (the friend lane's multi-session pattern - a party
// inherently spans sessions) plus a never-entering fourth character for
// the offline-target refusal. Every invite is the CONSENT HANDSHAKE: the
// proposal answers the target with the byte-exact 0x3393 {01, inviterGid}
// prompt (the sub_7644e0 party arm) and commits nothing until the
// target's 0x3393 {01 01} accept:
//
//	stray consents        -> SILENT (no outstanding invitation, and an
//	                         unshipped type-5 guild consent);
//	A invites B (0x70D5)  -> B gets the PROMPT; B REFUSES {02 0C} ->
//	                         nothing emitted anywhere, no party forms;
//	A re-invites B        -> B gets the prompt; B ACCEPTS {01 01} ->
//	                         BOTH get 0xB0D5 result=1 {own gid} + the
//	                         0x35D6 flags=3 settings+roster bulk; the
//	                         third session C receives NOTHING (barrier);
//	duplicate consent     -> SILENT (the pending invitation was consumed);
//	non-leader 0x751A     -> SILENT (join-anyone off);
//	A extends with C      -> C gets the prompt, accepts -> C gets the
//	   (0x751A)              0xB0D5 + 0x35D6 seed, A and B get 0x3E58
//	                         type-2 JOIN {C's masked row};
//	A banishes C (0x7664) -> type-3 BOOTED {gid C} to all three;
//	A (leader) leaves     -> type-1 BROKEN to A and B (the native
//	   (0x704F)              leader-leave dissolve); registry empty;
//	join-anyone reform    -> B leads with option bit 0x4; the NON-leader
//	                         A extends with C successfully (prompt +
//	                         accept both hops);
//	C (member) leaves     -> type-3 SECEDE {gid C} to all three, no
//	                         dissolve (two remain);
//	A disconnects         -> B gets type-3 LOGOUT {gid A} + type-1
//	                         BROKEN (the departure dropped the roster
//	                         below two);
//	reboot                -> the registry is EMPTY (parties AND pending
//	                         invitations): in-memory party state dying
//	                         with the process is the design; a fresh
//	                         0x704F refuses silently.
//
// The server composition mirrors server.go: ONE *enterworld.Deps,
// the presence facade over the Hub's exclusive bindings, BindExclusive
// claimed in OnWorldBound with the party stale-membership drop on its
// tail, and the party disconnect hook on OnSessionClose.

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
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	wiretest "opensro.online/server/internal/game/internal"
	presence "opensro.online/server/internal/game/social"
	"opensro.online/server/internal/game/social/party"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

const (
	e2eDivision = "global-official"
	e2eNameA    = "partyAlfa"
	e2eNameB    = "partyBravo"
	e2eNameC    = "partyCharlie"
	e2eNameD    = "partyDelta"
)

// Creation order pins the store IDs 1..4, so the world gids are the
// 100000+ID band enterworld.ObjectIDForCharacter latches.
const (
	gidA uint32 = 100001
	gidB uint32 = 100002
	gidC uint32 = 100003
	gidD uint32 = 100004
)

/*
================
e2eServer
================
*/
type e2eServer struct {
	srv       *transport.Server
	authority *store.Store
	runtime   *party.Runtime
}

// partySkillSeeder is this suite's stand-in for
// enterworld.DefaultSkillSeeder (the store's unconditional creation-seed
// invariant refuses an unseeded CreateCharacter): the same racial id
// sets, without a textdata dependency. Party tests never read skills -
// the seeder exists only to satisfy the store's creation invariant.
/*
================
partySkillSeeder
================
*/
func partySkillSeeder(raceKey string, learned []uint32) ([]uint32, error) {
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

// startPartyServer opens the authority store in dir and stands up the
// transport with the bootstrap + party lanes composed like server.go:
// one deps pointer, store collaborators assigned before the registers
// capture it, the presence facade over the hub, the exclusive world
// bind + party stale-drop in OnWorldBound, and the disconnect hook on
// OnSessionClose. The party runtime is constructed FRESH - its registry
// is process-local by design.
/*
================
startPartyServer
================
*/
func startPartyServer(t *testing.T, dir string, createCharacters bool) e2eServer {
	t.Helper()

	authority, err := store.Open(dir, store.Options{DefaultSkills: partySkillSeeder})
	if err != nil {
		t.Fatalf("store.Open(%s): %v", dir, err)
	}
	t.Cleanup(authority.Close)

	if createCharacters && len(authority.Characters().CharactersForDivision(e2eDivision)) == 0 {
		for _, name := range []string{e2eNameA, e2eNameB, e2eNameC, e2eNameD} {
			seed := &enterworld.Character{
				Name:          name,
				ModelCodename: "CHAR_CH_MAN_ADVENTURER",
				RaceIndex:     e2eInt64(enterworld.RaceChina),
				Gender:        e2eInt64(enterworld.GenderMale),
			}
			if err := authority.CreateCharacter(e2eDivision, "test-account", seed); err != nil {
				t.Fatalf("CreateCharacter(%s): %v", name, err)
			}
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
	runtime := party.NewRuntime(deps, presence)
	deps.OnWorldBound = func(s *transport.Session, divisionID string, character *enterworld.Character) {
		key := divisionID + ":" + strings.ToLower(character.Name)
		if old, replaced := srv.Hub.BindExclusive(key, s); replaced {
			old.ClearGameplayContext()
		}
		if s.Evicted() {
			return
		}
		runtime.WorldBound(divisionID, character)
	}
	srv.Hub.OnSessionClose(func(s *transport.Session, _ error) {
		runtime.SessionClosed(s)
	})

	enterworld.Register(srv.Hub, deps)
	runtime.Register(srv.Hub)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shutdownServer(t, srv) })
	return e2eServer{srv: srv, authority: authority, runtime: runtime}
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
expectExactFrame
================
*/
func expectExactFrame(t *testing.T, c *websocket.Conn, opcode uint16, want []byte, what string) {
	t.Helper()
	got := expectFrame(t, c, opcode, what)
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: 0x%04X payload = % X, want % X", what, opcode, got, want)
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

// enterWorld performs the 0x0006 bind and consumes the frozen bootstrap
// frame sequence.
/*
================
enterWorld
================
*/
func enterWorld(t *testing.T, c *websocket.Conn, charName string) {
	t.Helper()
	sendFrame(t, c, transport.OpEnterWorld, transport.EncodeEnterWorld(
		entryauth.NewAuthenticatedEntryFixture(t, e2eDivision, charName),
	))
	result, err := transport.DecodeEnterWorldResult(expectFrame(t, c, transport.OpEnterWorldResult, "enter world "+charName))
	if err != nil {
		t.Fatalf("decoding 0x0007 for %s: %v", charName, err)
	}
	if !result.OK {
		t.Fatalf("enter world refused for %s: nativeErrorCode=%#x", charName, result.NativeErrorCode)
	}
	expectFrame(t, c, enterworld.OpcodeResetClient, "bootstrap[0]")
	expectFrame(t, c, enterworld.OpcodeMyCharacterData, "bootstrap[1]")
	expectFrame(t, c, enterworld.OpcodeMyCharacterChunk, "bootstrap[2]")
	expectFrame(t, c, enterworld.OpcodeMyCharacterFlush, "bootstrap[3]")
	expectFrame(t, c, enterworld.OpcodeServerClockGidLatch, "bootstrap[4]")
	expectFrame(t, c, enterworld.OpcodeObjectListStart, "bootstrap[5]")
	expectFrame(t, c, enterworld.OpcodeObjectListFinalize, "bootstrap[6]")
	wiretest.ActivateWorld(t, c, "enter world "+charName)
	// The exclusive bind and the world-bound hooks (party WorldBound
	// included) run at the end of the game-ready handler, after the frames
	// above. Another session's action or a direct registry read can outrun
	// that tail, so entering the world waits for it.
	gameReadyBarrier(t, c, "world-bound tail "+charName)
}

// gameReadyBarrier proves NOTHING is queued using a non-mutating transport
// FIFO barrier; it must not replay world admission.
/*
================
gameReadyBarrier
================
*/
func gameReadyBarrier(t *testing.T, c *websocket.Conn, what string) {
	t.Helper()
	wiretest.AssertQueueDrained(t, c, what)
}

/*
================
e2eInt64
================
*/
func e2eInt64(v int64) *int64 { return &v }

/*
================
u32le
================
*/
func u32le(v uint32) []byte {
	return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
}

/*
================
concat
================
*/
func concat(chunks ...[]byte) []byte {
	var out []byte
	for _, chunk := range chunks {
		out = append(out, chunk...)
	}
	return out
}

// chinaMaleRow is the expected masked row for the fresh China-male
// seeds: model 1907 (the race/gender fallback - no explicit ModelRef,
// empty roster catalog), level 1, full vitals (0xAA against the 0xa/0xa
// denominators), the China start profile spawn (region 0x62A8, the
// float coordinates truncated onto the wire int16s).
/*
================
chinaMaleRow
================
*/
func chinaMaleRow(gid uint32, name string) party.MemberRow {
	return party.MemberRow{
		MemberID:      gid,
		Name:          name,
		ModelRefID:    1907,
		Level:         1,
		StatusNibbles: 0xAA,
		War:           domain.DefaultWorldInstance,
		Region:        0x62A8,
		PosX:          960,
		PosY:          20,
		PosZ:          458,
	}
}

// inviteFrame renders the sub_6fd830 0x70D5 body.
/*
================
inviteFrame
================
*/
func inviteFrame(targetGid uint32, optionBits uint8) []byte {
	return concat(u32le(targetGid), []byte{optionBits})
}

// consentFrame renders native party form acceptance {1,1} or refusal {2,12}.
/*
================
consentFrame
================
*/
func consentFrame(button uint8) []byte {
	if button == 1 {
		return []byte{1, 1}
	}
	return []byte{2, 0x0c}
}

// expectPartyPrompt asserts the byte-exact S->C 0x3393 prompt: {01,
// inviterGid} - the frame the sub_7644e0 party arm opens msgbox kind 7
// from.
/*
================
expectPartyPrompt
================
*/
func expectPartyPrompt(t *testing.T, c *websocket.Conn, inviterGid uint32, what string, kind, options uint8) {
	t.Helper()
	expectExactFrame(
		t, c, party.OpInvitationProposal,
		append(append([]byte{kind}, u32le(inviterGid)...), options),
		what+" 0x3393 prompt",
	)
}

// expectPartySeed asserts the enter-a-party pair: 0xB0D5 result=1 with
// the receiver's OWN gid, then the 0x35D6 settings+roster bulk.
/*
================
expectPartySeed
================
*/
func expectPartySeed(t *testing.T, c *websocket.Conn, ownGid, leaderGid uint32, optionBits uint8, rows []party.MemberRow, what string) {
	t.Helper()
	expectExactFrame(t, c, party.OpCreatePartyAck, party.EncodeCreatePartyAckB0D5(ownGid), what+" 0xB0D5 ack")
	expectExactFrame(t, c, party.OpPartyInfo, party.EncodePartyInfo35D6(leaderGid, optionBits, rows), what+" 0x35D6 bulk")
}

/*
================
TestPartyCapacityRefusalsOverWire
================
*/
func TestPartyCapacityRefusalsOverWire(t *testing.T) {
	for _, tc := range []struct {
		name     string
		options  uint8
		capacity int
		code     byte
	}{{"unshared", 0, 4, 0x14}, {"shared", 1, 8, 0x13}} {
		t.Run(tc.name, func(t *testing.T) {
			server := startPartyServer(t, filepath.Join(t.TempDir(), "authority"), true)
			connA := dialWS(t, server.srv)
			helloWS(t, connA)
			enterWorld(t, connA, e2eNameA)
			connC := dialWS(t, server.srv)
			helloWS(t, connC)
			enterWorld(t, connC, e2eNameC)
			registry := server.runtime.Registry()
			if _, reason := registry.Form(e2eDivision, party.Member{MemberID: gidA, Name: e2eNameA}, party.Member{MemberID: gidB, Name: e2eNameB}, tc.options); reason != "" {
				t.Fatal(reason)
			}
			for i := 2; i < tc.capacity; i++ {
				if _, reason := registry.Join(e2eDivision, e2eNameA, party.Member{MemberID: uint32(900000 + i), Name: fmt.Sprintf("capacity%d", i)}); reason != "" {
					t.Fatal(reason)
				}
			}
			if reason := server.runtime.MatchJoinPrecheck(e2eDivision, e2eNameA, e2eNameC); reason != "owner's party is full" {
				t.Fatalf("matching capacity check: %q", reason)
			}
			sendFrame(t, connA, party.OpPartyJoinInviteRequest, u32le(gidC))
			expectExactFrame(t, connA, party.OpPartyJoinInviteAck, []byte{2, tc.code}, "capacity refusal")
			gameReadyBarrier(t, connC, "full party must not send a consent prompt")
			if registry.PendingInviteCount() != 0 {
				t.Fatal("full party stored an unanswerable invitation")
			}
		})
	}
}

/*
================
TestPartyInvitationExpirationOverWire
================
*/
func TestPartyInvitationExpirationOverWire(t *testing.T) {
	for _, kind := range []party.PendingInviteKind{party.PendingInviteForm, party.PendingInviteJoin} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			server := startPartyServer(t, filepath.Join(t.TempDir(), "authority"), true)
			a := dialWS(t, server.srv)
			helloWS(t, a)
			enterWorld(t, a, e2eNameA)
			b := dialWS(t, server.srv)
			helloWS(t, b)
			enterWorld(t, b, e2eNameB)
			server.runtime.Registry().SetPendingInviteAt(e2eDivision, e2eNameB, party.PendingInvite{Kind: kind, InviterName: e2eNameA}, 1000)
			server.runtime.ExpireInvitations(31000)
			gameReadyBarrier(t, a, "not expired at equality")
			gameReadyBarrier(t, b, "prompt remains at equality")
			server.runtime.ExpireInvitations(31001)
			ack := party.OpCreatePartyAck
			if kind == party.PendingInviteJoin {
				ack = party.OpPartyJoinInviteAck
			}
			expectExactFrame(t, a, ack, []byte{2, 16}, "proposer timeout")
			expectExactFrame(t, b, party.OpPartyJoinAck, []byte{2, 16}, "invitee prompt retirement")
			sendFrame(t, b, party.OpInvitationProposal, consentFrame(1))
			gameReadyBarrier(t, b, "late acceptance cannot form a party")
			server.runtime.ExpireInvitations(32000)
			gameReadyBarrier(t, a, "no repeated timeout")
			if server.runtime.Registry().Count() != 0 || server.runtime.Registry().PendingInviteCount() != 0 {
				t.Fatal("expired invitation retained authority")
			}
		})
	}
}

/*
================
TestPartyLaneEndToEndOverWire
================
*/
func TestPartyLaneEndToEndOverWire(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")

	first := startPartyServer(t, dir, true)
	if got := first.runtime.Registry().Count(); got != 0 {
		t.Fatalf("baseline registry count = %d, want 0", got)
	}

	connA := dialWS(t, first.srv)
	helloWS(t, connA)
	enterWorld(t, connA, e2eNameA)
	connB := dialWS(t, first.srv)
	helloWS(t, connB)
	enterWorld(t, connB, e2eNameB)
	connC := dialWS(t, first.srv)
	helloWS(t, connC)
	enterWorld(t, connC, e2eNameC)

	rowA := chinaMaleRow(gidA, e2eNameA)
	rowB := chinaMaleRow(gidB, e2eNameB)
	rowC := chinaMaleRow(gidC, e2eNameC)

	// ---- refusal arms are SILENT (the blocker list: 0xB0D5 result-2
	// category-2 codes are unpinned, so nothing may ride) ----
	sendFrame(t, connA, party.OpPartyInviteRequest, inviteFrame(gidD, 0x03)) // offline target
	sendFrame(t, connA, party.OpPartyInviteRequest, inviteFrame(gidA, 0x03)) // self
	sendFrame(t, connA, party.OpPartyBanishRequest, u32le(gidB))             // not in a party
	sendFrame(t, connA, party.OpPartyLeaveRequest, nil)                      // not in a party
	// Stray consents: no outstanding invitation, an unshipped guild-type
	// consent, and a malformed body - all SILENT.
	sendFrame(t, connA, party.OpInvitationProposal, consentFrame(1))
	sendFrame(t, connA, party.OpInvitationProposal, []byte{0x05, 0x01})
	sendFrame(t, connA, party.OpInvitationProposal, []byte{0x01})
	gameReadyBarrier(t, connA, "post-refusals")
	if got := first.runtime.Registry().Count(); got != 0 {
		t.Fatalf("registry count after refusals = %d, want 0", got)
	}
	if got := first.runtime.Registry().PendingInviteCount(); got != 0 {
		t.Fatalf("pending invites after refusals = %d, want 0", got)
	}

	// ---- A invites B (0x70D5, exp+item share): B gets the byte-exact
	// prompt and REFUSES (button 2, the sub_52c800 result id) - no party
	// forms and NOTHING is emitted to anyone ----
	sendFrame(t, connA, party.OpPartyInviteRequest, inviteFrame(gidB, 0x03))
	expectPartyPrompt(t, connB, gidA, "B's proposal", 2, 3)
	sendFrame(t, connB, party.OpInvitationProposal, consentFrame(2))
	expectExactFrame(t, connA, party.OpCreatePartyAck, []byte{2, 12}, "formation refused toward proposer")
	expectExactFrame(t, connB, party.OpPartyJoinAck, []byte{2, 12}, "formation refused toward invitee")
	gameReadyBarrier(t, connA, "A after B's refusal")
	gameReadyBarrier(t, connB, "B after refusing")
	if got := first.runtime.Registry().Count(); got != 0 {
		t.Fatalf("registry count after refusal = %d, want 0", got)
	}
	if got := first.runtime.Registry().PendingInviteCount(); got != 0 {
		t.Fatalf("pending invites after refusal = %d, want 0", got)
	}

	// ---- A re-invites B: the prompt again, B ACCEPTS (button 1, the
	// sub_526020 result id) - the party forms and both sessions get the
	// 0xB0D5 + 0x35D6 seed; C gets NOTHING ----
	sendFrame(t, connA, party.OpPartyInviteRequest, inviteFrame(gidB, 0x03))
	expectPartyPrompt(t, connB, gidA, "B's re-proposal", 2, 3)
	sendFrame(t, connB, party.OpInvitationProposal, consentFrame(1))
	pairAB := []party.MemberRow{rowA, rowB}
	expectPartySeed(t, connA, gidA, gidA, 0x03, pairAB, "A's form")
	expectPartySeed(t, connB, gidB, gidA, 0x03, pairAB, "B's form")
	gameReadyBarrier(t, connC, "C isolation after form")
	if got := first.runtime.Registry().Count(); got != 1 {
		t.Fatalf("registry count after form = %d, want 1", got)
	}

	// A DUPLICATE consent (the pending invitation was consumed) is silent.
	sendFrame(t, connB, party.OpInvitationProposal, consentFrame(1))
	gameReadyBarrier(t, connB, "post-duplicate-consent")
	if got := first.runtime.Registry().Count(); got != 1 {
		t.Fatalf("registry count after duplicate consent = %d, want 1", got)
	}

	// ---- non-leader 0x751A with join-anyone OFF: silent refusal ----
	// A partyless proposer cannot invite a member of an existing party.
	sendFrame(t, connC, party.OpPartyInviteRequest, inviteFrame(gidB, 0x03))
	expectExactFrame(t, connC, party.OpCreatePartyAck, []byte{2, 0x18}, "other-party member refusal")
	gameReadyBarrier(t, connB, "no prompt for already-partied target")
	sendFrame(t, connB, party.OpPartyJoinInviteRequest, u32le(gidC))
	gameReadyBarrier(t, connB, "post-nonleader-invite")

	// ---- the leader extends with C (0x751A): C gets the prompt and
	// accepts - C gets the seed, the existing members the type-2 JOIN row ----
	sendFrame(t, connA, party.OpPartyJoinInviteRequest, u32le(gidC))
	expectPartyPrompt(t, connC, gidA, "C's refused join proposal", 3, 3)
	sendFrame(t, connC, party.OpInvitationProposal, []byte{2, 23})
	expectExactFrame(t, connA, party.OpPartyJoinInviteAck, []byte{2, 23}, "join refused toward proposer")
	expectExactFrame(t, connC, party.OpPartyJoinAck, []byte{2, 23}, "join refused toward invitee")
	sendFrame(t, connA, party.OpPartyJoinInviteRequest, u32le(gidC))
	expectPartyPrompt(t, connC, gidA, "C's join proposal", 3, 3)
	sendFrame(t, connC, party.OpInvitationProposal, consentFrame(1))
	joinC := party.EncodePartyJoin3E58(rowC)
	expectExactFrame(t, connA, party.OpPartyUpdate, joinC, "A's JOIN push")
	expectExactFrame(t, connB, party.OpPartyUpdate, joinC, "B's JOIN push")
	expectPartySeed(t, connC, gidC, gidA, 0x03, []party.MemberRow{rowA, rowB, rowC}, "C's join")

	// Existing-member refusal goes to the inviter, without another prompt.
	sendFrame(t, connA, party.OpPartyJoinInviteRequest, u32le(gidB))
	expectExactFrame(t, connA, party.OpPartyJoinInviteAck, []byte{2, 0x12}, "existing member refusal")
	gameReadyBarrier(t, connA, "post-duplicate-invite")
	gameReadyBarrier(t, connB, "B saw no prompt for the duplicate invite")

	// ---- A banishes C (0x7664): type-3 BOOTED to all three (C's is-me
	// split full-clears; the others remove the row) ----
	sendFrame(t, connA, party.OpPartyBanishRequest, u32le(gidC))
	bootedC := party.EncodePartyLeave3E58(gidC, party.PartyLeaveReasonBooted)
	expectExactFrame(t, connC, party.OpPartyUpdate, bootedC, "C's BOOTED push")
	expectExactFrame(t, connA, party.OpPartyUpdate, bootedC, "A's boot fan-out")
	expectExactFrame(t, connB, party.OpPartyUpdate, bootedC, "B's boot fan-out")

	// A non-leader banish refuses silently.
	sendFrame(t, connB, party.OpPartyBanishRequest, u32le(gidA))
	gameReadyBarrier(t, connB, "post-nonleader-banish")

	// ---- the LEADER leaves (0x704F): the native dissolve split -
	// type-1 BROKEN to every member including the initiator ----
	sendFrame(t, connA, party.OpPartyLeaveRequest, nil)
	broken := party.EncodePartyBroken3E58()
	expectExactFrame(t, connA, party.OpPartyUpdate, broken, "A's dissolve BROKEN")
	expectExactFrame(t, connB, party.OpPartyUpdate, broken, "B's dissolve BROKEN")
	if got := first.runtime.Registry().Count(); got != 0 {
		t.Fatalf("registry count after leader leave = %d, want 0", got)
	}

	// ---- reform under B with JOIN-ANYONE (0x4): the NON-leader A may
	// extend the party (the sub_5b78e0 dispatch gate's other arm) ----
	sendFrame(t, connB, party.OpPartyInviteRequest, inviteFrame(gidA, 0x04))
	expectPartyPrompt(t, connA, gidB, "A's reform proposal", 2, 4)
	sendFrame(t, connA, party.OpInvitationProposal, consentFrame(1))
	pairBA := []party.MemberRow{rowB, rowA}
	expectPartySeed(t, connB, gidB, gidB, 0x04, pairBA, "B's reform")
	expectPartySeed(t, connA, gidA, gidB, 0x04, pairBA, "A's reform")

	sendFrame(t, connA, party.OpPartyJoinInviteRequest, u32le(gidC))
	expectPartyPrompt(t, connC, gidA, "C's join-anyone proposal", 3, 4)
	sendFrame(t, connC, party.OpInvitationProposal, consentFrame(1))
	expectExactFrame(t, connB, party.OpPartyUpdate, joinC, "B's JOIN push (join-anyone)")
	expectExactFrame(t, connA, party.OpPartyUpdate, joinC, "A's JOIN push (join-anyone)")
	expectPartySeed(t, connC, gidC, gidB, 0x04, []party.MemberRow{rowB, rowA, rowC}, "C's re-join")

	// ---- a MEMBER leaves (0x704F): type-3 SECEDE to everyone, no
	// dissolve (two remain) ----
	sendFrame(t, connC, party.OpPartyLeaveRequest, nil)
	secededC := party.EncodePartyLeave3E58(gidC, party.PartyLeaveReasonSecede)
	expectExactFrame(t, connC, party.OpPartyUpdate, secededC, "C's SECEDE push")
	expectExactFrame(t, connB, party.OpPartyUpdate, secededC, "B's secede fan-out")
	expectExactFrame(t, connA, party.OpPartyUpdate, secededC, "A's secede fan-out")
	if got := first.runtime.Registry().Count(); got != 1 {
		t.Fatalf("registry count after member leave = %d, want 1", got)
	}

	// ---- A DISCONNECTS: the close hook drops the member with the
	// pinned LOGOUT reason, and the sub-two remainder BREAKS ----
	sendFrame(t, connA, transport.OpBye, []byte{transport.ByeReasonNormal})
	connA.Close()
	expectExactFrame(t, connB, party.OpPartyUpdate, party.EncodePartyLeave3E58(gidA, party.PartyLeaveReasonLogout), "B's LOGOUT fan-out")
	expectExactFrame(t, connB, party.OpPartyUpdate, broken, "B's disconnect BROKEN")
	if got := first.runtime.Registry().Count(); got != 0 {
		t.Fatalf("registry count after disconnect = %d, want 0", got)
	}

	if dropped := first.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s)", dropped)
	}
	sendFrame(t, connB, transport.OpBye, []byte{transport.ByeReasonNormal})
	sendFrame(t, connC, transport.OpBye, []byte{transport.ByeReasonNormal})
	connB.Close()
	connC.Close()

	// ---- the reboot: party state is IN-MEMORY and must be GONE ----
	shutdownServer(t, first.srv)
	first.authority.Close()

	second := startPartyServer(t, dir, false)
	if got := second.runtime.Registry().Count(); got != 0 {
		t.Fatalf("post-reboot registry count = %d, want 0 (in-memory party state dies with the process by design)", got)
	}
	if got := second.runtime.Registry().PendingInviteCount(); got != 0 {
		t.Fatalf("post-reboot pending invites = %d, want 0 (in-memory like the parties)", got)
	}
	conn := dialWS(t, second.srv)
	helloWS(t, conn)
	enterWorld(t, conn, e2eNameA)
	// The pre-reboot membership is gone: a leave refuses silently.
	sendFrame(t, conn, party.OpPartyLeaveRequest, nil)
	gameReadyBarrier(t, conn, "post-reboot leave")

	if dropped := second.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s) on the reopened server", dropped)
	}
	sendFrame(t, conn, transport.OpBye, []byte{transport.ByeReasonNormal})
}

// TestPartyRebindDropsStaleMembership proves the OnWorldBound arm: a
// character whose earlier session is REPLACED (a second EnterWorld for
// the same character) is dropped from their party like a logout - the
// fresh client holds no party state, so the registry must not carry a
// ghost membership.
/*
================
TestPartyRebindDropsStaleMembership
================
*/
func TestPartyRebindDropsStaleMembership(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")
	server := startPartyServer(t, dir, true)

	connA := dialWS(t, server.srv)
	helloWS(t, connA)
	enterWorld(t, connA, e2eNameA)
	connB := dialWS(t, server.srv)
	helloWS(t, connB)
	enterWorld(t, connB, e2eNameB)

	sendFrame(t, connA, party.OpPartyInviteRequest, inviteFrame(gidB, 0x00))
	expectPartyPrompt(t, connB, gidA, "B's proposal", 2, 0)
	sendFrame(t, connB, party.OpInvitationProposal, consentFrame(1))
	pair := []party.MemberRow{chinaMaleRow(gidA, e2eNameA), chinaMaleRow(gidB, e2eNameB)}
	expectPartySeed(t, connA, gidA, gidA, 0x00, pair, "A's form")
	expectPartySeed(t, connB, gidB, gidA, 0x00, pair, "B's form")

	// B re-enters on a SECOND connection: the rebind evicts the old
	// session and the OnWorldBound tail drops B's stale membership -
	// the sub-two remainder (leader A) gets LOGOUT + BROKEN.
	connB2 := dialWS(t, server.srv)
	helloWS(t, connB2)
	enterWorld(t, connB2, e2eNameB)
	expectExactFrame(t, connA, party.OpPartyUpdate, party.EncodePartyLeave3E58(gidB, party.PartyLeaveReasonLogout), "A's rebind LOGOUT fan-out")
	expectExactFrame(t, connA, party.OpPartyUpdate, party.EncodePartyBroken3E58(), "A's rebind BROKEN")
	if got := server.runtime.Registry().Count(); got != 0 {
		t.Fatalf("registry count after rebind = %d, want 0", got)
	}
	// The fresh B session carries no party frames: the barrier is clean.
	gameReadyBarrier(t, connB2, "fresh B after rebind")

	sendFrame(t, connA, transport.OpBye, []byte{transport.ByeReasonNormal})
	sendFrame(t, connB2, transport.OpBye, []byte{transport.ByeReasonNormal})
}

// TestPartyConsentRaceEdges proves the consent handshake's races over the
// real transport, with terminal failures delivered to surviving participants:
//
//	inviter logged off    -> the accept resolves no live inviter session
//	                         and commits nothing;
//	second proposal       -> refused while the first waits (46F420),
//	                         {2, 2} to both; after a decline the retry
//	                         prompts and commits;
//	inviter got partied   -> a form-kind accept re-validates under the
//	                         registry lock and refuses;
//	target session rebind -> the OnWorldBound tail drops the pending
//	                         invitation, so a consent from the fresh
//	                         session finds nothing outstanding;
//	party dissolved       -> a join-kind accept finds the inviter no
//	                         longer partied and commits nothing.
/*
================
TestPartyConsentRaceEdges
================
*/
func TestPartyConsentRaceEdges(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")
	server := startPartyServer(t, dir, true)

	connA := dialWS(t, server.srv)
	helloWS(t, connA)
	enterWorld(t, connA, e2eNameA)
	connB := dialWS(t, server.srv)
	helloWS(t, connB)
	enterWorld(t, connB, e2eNameB)
	connC := dialWS(t, server.srv)
	helloWS(t, connC)
	enterWorld(t, connC, e2eNameC)

	rowC := chinaMaleRow(gidC, e2eNameC)

	// ---- inviter logs off before consent: the remaining peer gets 0E ----
	sendFrame(t, connA, party.OpPartyInviteRequest, inviteFrame(gidB, 0x00))
	expectPartyPrompt(t, connB, gidA, "B's proposal from A", 2, 0)
	sendFrame(t, connA, transport.OpBye, []byte{transport.ByeReasonNormal})
	connA.Close()
	waitForPresenceDrop(t, server, e2eNameA)
	sendFrame(t, connB, party.OpInvitationProposal, consentFrame(1))
	expectExactFrame(t, connB, party.OpPartyJoinAck, []byte{2, 0x0e}, "missing inviter refusal")
	gameReadyBarrier(t, connB, "B after missing inviter refusal")
	if got := server.runtime.Registry().Count(); got != 0 {
		t.Fatalf("registry count after dead-inviter accept = %d, want 0", got)
	}

	// ---- a second proposal is REFUSED while the first waits; after C
	// declines B, A's retry prompts and commits ----
	sendFrame(t, connB, party.OpPartyInviteRequest, inviteFrame(gidC, 0x00))
	expectPartyPrompt(t, connC, gidB, "C's proposal from B", 2, 0)
	sendFrame(t, connB, party.OpPartyLeaveRequest, nil) // B backs out: still partyless (no party formed yet - silent)
	gameReadyBarrier(t, connB, "B after backing out")
	// The A session is gone; re-enter A to propose the replacement.
	connA2 := dialWS(t, server.srv)
	helloWS(t, connA2)
	enterWorld(t, connA2, e2eNameA)
	sendFrame(t, connA2, party.OpPartyInviteRequest, inviteFrame(gidC, 0x03))
	expectExactFrame(t, connA2, party.OpCreatePartyAck, []byte{2, 2}, "A's busy ack")
	expectExactFrame(t, connC, party.OpPartyJoinAck, []byte{2, 2}, "C's busy ack")
	sendFrame(t, connC, party.OpInvitationProposal, []byte{0x02, 0x0c})
	expectExactFrame(t, connC, party.OpPartyJoinAck, []byte{2, 0x0c}, "C declines B")
	expectExactFrame(t, connB, party.OpCreatePartyAck, []byte{2, 0x0c}, "B's decline ack")
	sendFrame(t, connA2, party.OpPartyInviteRequest, inviteFrame(gidC, 0x03))
	expectPartyPrompt(t, connC, gidA, "C's proposal from A", 2, 3)
	sendFrame(t, connC, party.OpInvitationProposal, consentFrame(1))
	rowA := chinaMaleRow(gidA, e2eNameA)
	pairAC := []party.MemberRow{rowA, rowC}
	expectPartySeed(t, connA2, gidA, gidA, 0x03, pairAC, "A's form")
	expectPartySeed(t, connC, gidC, gidA, 0x03, pairAC, "C's form")
	gameReadyBarrier(t, connB, "B after C formed with A")
	if got := server.runtime.Registry().Count(); got != 1 {
		t.Fatalf("registry count after the retry accept = %d, want 1", got)
	}

	// ---- inviter got partied while the prompt was up: the form-kind
	// accept re-validates and refuses ----
	// B (partyless) invites... nobody is free but B itself; use the
	// A+C party: C proposes a JOIN to B, then the party dissolves before
	// B answers - the join-kind accept finds no party.
	sendFrame(t, connA2, party.OpPartyJoinInviteRequest, u32le(gidB))
	expectPartyPrompt(t, connB, gidA, "B's join proposal from A", 3, 3)
	sendFrame(t, connA2, party.OpPartyLeaveRequest, nil) // the leader leaves: dissolve
	expectExactFrame(t, connA2, party.OpPartyUpdate, party.EncodePartyBroken3E58(), "A's dissolve BROKEN")
	expectExactFrame(t, connC, party.OpPartyUpdate, party.EncodePartyBroken3E58(), "C's dissolve BROKEN")
	sendFrame(t, connB, party.OpInvitationProposal, consentFrame(1))
	expectExactFrame(t, connB, party.OpPartyJoinAck, []byte{2, 2}, "dissolved party refusal")
	expectExactFrame(t, connA2, party.OpPartyJoinInviteAck, []byte{2, 2}, "dissolved party proposer refusal")
	gameReadyBarrier(t, connB, "B after accepting into a dissolved party")
	if got := server.runtime.Registry().Count(); got != 0 {
		t.Fatalf("registry count after dissolved-party accept = %d, want 0", got)
	}

	// ---- the inviter gets PARTIED between a form proposal and its
	// accept: the registry re-validation refuses ----
	sendFrame(t, connA2, party.OpPartyInviteRequest, inviteFrame(gidB, 0x00))
	expectPartyPrompt(t, connB, gidA, "B's form proposal from A", 2, 0)
	// A forms with C meanwhile (C proposes, A accepts).
	sendFrame(t, connC, party.OpPartyInviteRequest, inviteFrame(gidA, 0x00))
	expectPartyPrompt(t, connA2, gidC, "A's proposal from C", 2, 0)
	sendFrame(t, connA2, party.OpInvitationProposal, consentFrame(1))
	pairCA := []party.MemberRow{rowC, rowA}
	expectPartySeed(t, connC, gidC, gidC, 0x00, pairCA, "C's form")
	expectPartySeed(t, connA2, gidA, gidC, 0x00, pairCA, "A's form")
	// B's stale accept: report the failed formation to both peers.
	sendFrame(t, connB, party.OpInvitationProposal, consentFrame(1))
	expectExactFrame(t, connB, party.OpPartyJoinAck, []byte{2, 2}, "partied inviter refusal")
	expectExactFrame(t, connA2, party.OpCreatePartyAck, []byte{2, 2}, "failed formation proposer refusal")
	gameReadyBarrier(t, connB, "B after accepting a partied inviter's proposal")
	if got := server.runtime.Registry().Count(); got != 1 {
		t.Fatalf("registry count after partied-inviter accept = %d, want 1", got)
	}
	if got := server.runtime.Registry().PendingInviteCount(); got != 0 {
		t.Fatalf("pending invites after the race edges = %d, want 0", got)
	}

	// ---- target session REBIND drops the pending prompt: the fresh
	// session's consent finds nothing outstanding ----
	sendFrame(t, connC, party.OpPartyJoinInviteRequest, u32le(gidB))
	expectPartyPrompt(t, connB, gidC, "B's join proposal from C", 3, 0)
	connB2 := dialWS(t, server.srv)
	helloWS(t, connB2)
	enterWorld(t, connB2, e2eNameB)
	if got := server.runtime.Registry().PendingInviteCount(); got != 0 {
		t.Fatalf("pending invites after B's rebind = %d, want 0 (the OnWorldBound tail drops the prompt)", got)
	}
	sendFrame(t, connB2, party.OpInvitationProposal, consentFrame(1))
	gameReadyBarrier(t, connB2, "fresh B after a stale consent")
	if got := server.runtime.Registry().Count(); got != 1 {
		t.Fatalf("registry count after stale rebind consent = %d, want 1 (only C+A's party)", got)
	}

	if dropped := server.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s)", dropped)
	}
	sendFrame(t, connA2, transport.OpBye, []byte{transport.ByeReasonNormal})
	sendFrame(t, connB2, transport.OpBye, []byte{transport.ByeReasonNormal})
	sendFrame(t, connC, transport.OpBye, []byte{transport.ByeReasonNormal})
}

// waitForPresenceDrop polls until the character's session binding is gone
// (the close hook runs on the hub's session teardown, asynchronously to
// the closing frame).
/*
================
waitForPresenceDrop
================
*/
func waitForPresenceDrop(t *testing.T, server e2eServer, name string) {
	t.Helper()
	presence := presence.NewDirectory(server.srv.Hub)
	wait.Eventually(t, 5*time.Second, "the session for "+name+" to unbind", func() bool {
		_, online := presence.SessionByName(e2eDivision, name)
		return !online
	})
}
