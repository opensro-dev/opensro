package community_test

// End-to-end exercise of the FRIEND lane over the REAL transport with
// TWO live sessions (the multi-session presence proof the state-design
// recon requires) plus the reboot persistence leg:
//
//	enter A, enter B -> both roster seeds EMPTY (0x3769 [00]);
//	A adds B (0x7164) -> 0x3F9A case 2 to A {jid B, name, model} AND
//	                     case 2 to B {jid A, name, model}; the store
//	                     carries the MUTUAL edge pair;
//	duplicate add     -> SILENT refusal (game-ready barrier);
//	B disconnects     -> A gets 0x3F9A case 4 {jid B, state 1 offline};
//	reboot            -> the edges are PERSISTED state: A's fresh roster
//	                     seed carries B OFFLINE (derived at encode time,
//	                     never persisted);
//	B re-enters       -> B's roster seed carries A ONLINE, and A gets
//	                     case 4 {jid B, state 0 online} live;
//	A deletes B (0x75DB) -> case 3 {jid B} to A, case 3 {jid A} to B,
//	                     both persisted lists empty.
//
// The server composition mirrors server.go: ONE *enterworld.Deps,
// the store door / presence facade / division-aware seed seam assigned
// BEFORE the pointer-based Register shares, BindExclusive claimed in
// OnWorldBound with the presence bind key, the friend fanout hooks on
// the OnWorldBound tail and OnSessionClose.

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
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
	e2eFriendNameA = "friendAlfa"
	e2eFriendNameB = "friendBravo"
)

// startFriendServer opens the authority store in dir and stands up the
// transport with the bootstrap + community + friend lanes composed like
// server.go, including the exclusive world bind and both presence hooks.
func startFriendServer(t *testing.T, dir string, createCharacters bool) e2eServer {
	t.Helper()

	authority, err := store.Open(dir, store.Options{DefaultSkills: communitySkillSeeder})
	if err != nil {
		t.Fatalf("store.Open(%s): %v", dir, err)
	}
	t.Cleanup(authority.Close)

	if createCharacters && len(authority.Characters().CharactersForDivision(e2eDivision)) == 0 {
		for _, name := range []string{e2eFriendNameA, e2eFriendNameB} {
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
	deps.MutateCharacters = func(cs []*enterworld.Character, label string, fn func()) {
		authority.MutateCharacters(cs, label, fn)
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

	// The presence facade and both friend hooks, wired like server.go:
	// seed seam and OnWorldBound assigned BEFORE Register captures deps.
	presence := presence.NewDirectory(srv.Hub)
	deps.CommunitySeedFramesFor = community.SeedFramesFunc(presence, nil)
	deps.OnWorldBound = func(s *transport.Session, divisionID string, character *enterworld.Character) {
		key := divisionID + ":" + strings.ToLower(character.Name)
		if old, replaced := srv.Hub.BindExclusive(key, s); replaced {
			old.ClearGameplayContext()
		}
		if s.Evicted() {
			return
		}
		community.FriendWorldBound(deps, presence, divisionID, character)
	}
	srv.Hub.OnSessionClose(func(s *transport.Session, _ error) {
		community.FriendSessionClosed(deps, presence, s)
	})

	enterworld.Register(srv.Hub, deps)
	community.Register(srv.Hub, deps)
	community.RegisterFriend(srv.Hub, deps, presence)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shutdownServer(t, srv) })
	return e2eServer{srv: srv, authority: authority}
}

// enterFriendWorld performs the 0x0006 bind for one named character,
// consumes the frozen bootstrap sequence, and returns the raw 0x3769
// roster seed payload (asserting the 0xB3CD letter seed stays empty).
func enterFriendWorld(t *testing.T, c *websocket.Conn, name string) []byte {
	t.Helper()
	sendFrame(t, c, transport.OpEnterWorld, transport.EncodeEnterWorld(
		entryauth.NewAuthenticatedEntryFixture(t, e2eDivision, name),
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
	roster := expectFrame(t, c, community.OpFriendRosterPush, "friend roster seed for "+name)
	if got := expectFrame(t, c, community.OpLetterListAnswer, "letter list seed for "+name); !bytes.Equal(got, []byte{0x01, 0x00}) {
		t.Fatalf("0xB3CD seed payload = % X, want the empty list [01 00]", got)
	}
	wiretest.ActivateWorld(t, c, "enter world "+name)
	gameReadyBarrier(t, c, "world-bound tail "+name)
	return roster
}

func friendAddFrame(name string) []byte {
	writer := wire.NewWriter(2 + len(name))
	writer.U16(uint16(len(name)))
	writer.Bytes([]byte(name))
	return writer.Payload()
}

func friendDeleteFrame(jid uint32) []byte {
	writer := wire.NewWriter(4)
	writer.U32(jid)
	return writer.Payload()
}

// expectFriendEvent asserts the next frame is 0x3F9A with the exact body.
func expectFriendEvent(t *testing.T, c *websocket.Conn, want []byte, what string) {
	t.Helper()
	got := expectFrame(t, c, community.OpFriendLetterEventPush, what)
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: 0x3F9A payload = % X, want % X", what, got, want)
	}
}

func readFriends(t *testing.T, authority *store.Store, name string) []enterworld.FriendRecord {
	t.Helper()
	var friends []enterworld.FriendRecord
	found := false
	authority.ReadCharacters(e2eDivision, func(characters []*enterworld.Character) {
		for _, c := range characters {
			if c != nil && c.Name == name {
				found = true
				friends = append([]enterworld.FriendRecord{}, c.Friends...)
			}
		}
	})
	if !found {
		t.Fatalf("character %s not in the store", name)
	}
	return friends
}

// TestFriendPairUsesOneStoreTransaction proves the community seam, not
// merely the store primitive: an injected failure counts commit attempts.
// A mutual add must produce exactly one failed attempt, retain both dirty
// records, then heal both edges together on the next successful commit.
func TestFriendPairUsesOneStoreTransaction(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")
	authority, err := store.Open(dir, store.Options{DefaultSkills: communitySkillSeeder})
	if err != nil {
		t.Fatalf("store.Open(%s): %v", dir, err)
	}
	t.Cleanup(authority.Close)

	for _, name := range []string{e2eFriendNameA, e2eFriendNameB} {
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

	characters := authority.Characters().CharactersForDivision(e2eDivision)
	var actor, target *enterworld.Character
	for _, character := range characters {
		switch character.Name {
		case e2eFriendNameA:
			actor = character
		case e2eFriendNameB:
			target = character
		}
	}
	if actor == nil || target == nil {
		t.Fatalf("created pair unresolved: actor=%p target=%p", actor, target)
	}

	deps := &enterworld.Deps{
		Roster:     &enterworld.Roster{},
		Characters: authority.Characters(),
		MutateCharacter: func(c *enterworld.Character, label string, fn func()) {
			authority.MutateCharacter(c, label, fn)
		},
		MutateCharacters: func(cs []*enterworld.Character, label string, fn func()) {
			authority.MutateCharacters(cs, label, fn)
		},
	}
	authority.FailCommits(errors.New("injected friend-pair commit failure"))
	outcome := community.HandleFriendAdd(deps, e2eDivision, actor, friendAddFrame(e2eFriendNameB), func(string) bool { return true })
	if outcome.Refusal != "" {
		t.Fatalf("friend add refused under fail-open store posture: %s", outcome.Refusal)
	}
	if failed := authority.Health().FailedWrites; failed != 1 {
		t.Fatalf("mutual friend add attempted %d commits, want exactly 1", failed)
	}
	if got := readFriends(t, authority, e2eFriendNameA); len(got) != 1 || got[0].ID != target.ID {
		t.Fatalf("actor in-memory edge after failed commit = %+v", got)
	}
	if got := readFriends(t, authority, e2eFriendNameB); len(got) != 1 || got[0].ID != actor.ID {
		t.Fatalf("target in-memory edge after failed commit = %+v", got)
	}

	authority.FailCommits(nil)
	authority.MutateCharacters(nil, "friend-pair-heal", nil)
	if failed := authority.Health().FailedWrites; failed != 0 {
		t.Fatalf("healing commit left store degraded: FailedWrites=%d", failed)
	}
	authority.Close()

	reopened, err := store.Open(dir, store.Options{DefaultSkills: communitySkillSeeder})
	if err != nil {
		t.Fatalf("reopen after healed friend pair: %v", err)
	}
	t.Cleanup(reopened.Close)
	if got := readFriends(t, reopened, e2eFriendNameA); len(got) != 1 || got[0].ID != target.ID {
		t.Fatalf("actor restored edge = %+v", got)
	}
	if got := readFriends(t, reopened, e2eFriendNameB); len(got) != 1 || got[0].ID != actor.ID {
		t.Fatalf("target restored edge = %+v", got)
	}

	var restoredActor, restoredTarget *enterworld.Character
	for _, character := range reopened.Characters().CharactersForDivision(e2eDivision) {
		switch character.Name {
		case e2eFriendNameA:
			restoredActor = character
		case e2eFriendNameB:
			restoredTarget = character
		}
	}
	if restoredActor == nil || restoredTarget == nil {
		t.Fatalf("restored pair unresolved: actor=%p target=%p", restoredActor, restoredTarget)
	}
	restoredDeps := &enterworld.Deps{
		Roster:     &enterworld.Roster{},
		Characters: reopened.Characters(),
		MutateCharacter: func(c *enterworld.Character, label string, fn func()) {
			reopened.MutateCharacter(c, label, fn)
		},
		MutateCharacters: func(cs []*enterworld.Character, label string, fn func()) {
			reopened.MutateCharacters(cs, label, fn)
		},
	}
	reopened.FailCommits(errors.New("injected friend-pair delete failure"))
	deleted := community.HandleFriendDelete(restoredDeps, e2eDivision, restoredActor, friendDeleteFrame(uint32(restoredTarget.ID)))
	if deleted.Refusal != "" {
		t.Fatalf("friend delete refused under fail-open store posture: %s", deleted.Refusal)
	}
	if failed := reopened.Health().FailedWrites; failed != 1 {
		t.Fatalf("mutual friend delete attempted %d commits, want exactly 1", failed)
	}
	if got := readFriends(t, reopened, e2eFriendNameA); len(got) != 0 {
		t.Fatalf("actor in-memory edges after failed delete commit = %+v", got)
	}
	if got := readFriends(t, reopened, e2eFriendNameB); len(got) != 0 {
		t.Fatalf("target in-memory edges after failed delete commit = %+v", got)
	}

	reopened.FailCommits(nil)
	reopened.MutateCharacters(nil, "friend-pair-delete-heal", nil)
	reopened.Close()
	finalStore, err := store.Open(dir, store.Options{DefaultSkills: communitySkillSeeder})
	if err != nil {
		t.Fatalf("reopen after healed friend delete: %v", err)
	}
	t.Cleanup(finalStore.Close)
	if got := readFriends(t, finalStore, e2eFriendNameA); len(got) != 0 {
		t.Fatalf("actor restored edges after delete = %+v", got)
	}
	if got := readFriends(t, finalStore, e2eFriendNameB); len(got) != 0 {
		t.Fatalf("target restored edges after delete = %+v", got)
	}
}

func TestFriendLaneEndToEndOverWire(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")

	// ---- session 1: two live players, empty rosters ----
	first := startFriendServer(t, dir, true)
	connA := dialWS(t, first.srv)
	helloWS(t, connA)
	if roster := enterFriendWorld(t, connA, e2eFriendNameA); !bytes.Equal(roster, []byte{0x00}) {
		t.Fatalf("A's baseline roster seed = % X, want the empty roster [00]", roster)
	}
	connB := dialWS(t, first.srv)
	helloWS(t, connB)
	if roster := enterFriendWorld(t, connB, e2eFriendNameB); !bytes.Equal(roster, []byte{0x00}) {
		t.Fatalf("B's baseline roster seed = % X, want the empty roster [00]", roster)
	}

	// ---- A adds B: case 2 to BOTH live sides, mutual persisted edges ----
	// (creation order pins the jids: friendAlfa=1, friendBravo=2; the
	// model resolves through the china-male fallback 1907 - no explicit
	// ModelRef, empty roster catalog.)
	sendFrame(t, connA, community.OpFriendAddRequest, friendAddFrame(e2eFriendNameB))
	expectFriendEvent(t, connA, community.EncodeFriendEventAdded3F9A(2, e2eFriendNameB, 1907), "A's case-2 add event")
	expectFriendEvent(t, connB, community.EncodeFriendEventAdded3F9A(1, e2eFriendNameA, 1907), "B's mutual case-2 add event")
	wantEdgeB := enterworld.FriendRecord{ID: 2, Name: e2eFriendNameB, ModelRefID: 1907}
	wantEdgeA := enterworld.FriendRecord{ID: 1, Name: e2eFriendNameA, ModelRefID: 1907}
	if got := readFriends(t, first.authority, e2eFriendNameA); len(got) != 1 || got[0] != wantEdgeB {
		t.Fatalf("A's stored edges = %+v, want [%+v]", got, wantEdgeB)
	}
	if got := readFriends(t, first.authority, e2eFriendNameB); len(got) != 1 || got[0] != wantEdgeA {
		t.Fatalf("B's stored edges = %+v, want [%+v]", got, wantEdgeA)
	}

	// Duplicate add: SILENT refusal (the barrier proves no frame rode).
	sendFrame(t, connA, community.OpFriendAddRequest, friendAddFrame(e2eFriendNameB))
	gameReadyBarrier(t, connA, "post-duplicate-add")
	if got := readFriends(t, first.authority, e2eFriendNameA); len(got) != 1 {
		t.Fatalf("A's stored edges after duplicate add = %d, want 1", len(got))
	}

	// ---- B disconnects: A gets the case-4 OFFLINE flip ----
	sendFrame(t, connB, transport.OpBye, []byte{transport.ByeReasonNormal})
	connB.Close()
	expectFriendEvent(t, connA, community.EncodeFriendEventState3F9A(2, community.FriendStateOffline), "A's case-4 offline event")

	if dropped := first.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s)", dropped)
	}
	sendFrame(t, connA, transport.OpBye, []byte{transport.ByeReasonNormal})
	connA.Close()

	// ---- the reboot: the edges are PERSISTED, the state byte is not ----
	shutdownServer(t, first.srv)
	first.authority.Close()

	second := startFriendServer(t, dir, false)
	if got := readFriends(t, second.authority, e2eFriendNameA); len(got) != 1 || got[0] != wantEdgeB {
		t.Fatalf("A's restored edges = %+v, want [%+v]", got, wantEdgeB)
	}
	if got := readFriends(t, second.authority, e2eFriendNameB); len(got) != 1 || got[0] != wantEdgeA {
		t.Fatalf("B's restored edges = %+v, want [%+v]", got, wantEdgeA)
	}

	// ---- session 2: the reseed derives presence at encode time ----
	connA2 := dialWS(t, second.srv)
	helloWS(t, connA2)
	wantOffline := community.EncodeFriendRoster3769([]community.FriendRosterEntry{
		{JID: 2, Name: e2eFriendNameB, ModelRefID: 1907, State: community.FriendStateOffline},
	})
	if roster := enterFriendWorld(t, connA2, e2eFriendNameA); !bytes.Equal(roster, wantOffline) {
		t.Fatalf("A's post-reboot roster seed = % X, want % X (B offline)", roster, wantOffline)
	}

	// B re-enters: B's own seed shows A ONLINE, and A gets the live flip.
	connB2 := dialWS(t, second.srv)
	helloWS(t, connB2)
	wantOnline := community.EncodeFriendRoster3769([]community.FriendRosterEntry{
		{JID: 1, Name: e2eFriendNameA, ModelRefID: 1907, State: community.FriendStateOnline},
	})
	if roster := enterFriendWorld(t, connB2, e2eFriendNameB); !bytes.Equal(roster, wantOnline) {
		t.Fatalf("B's post-reboot roster seed = % X, want % X (A online)", roster, wantOnline)
	}
	expectFriendEvent(t, connA2, community.EncodeFriendEventState3F9A(2, community.FriendStateOnline), "A's case-4 online event")

	// ---- A deletes B: case 3 to both sides, both lists empty ----
	sendFrame(t, connA2, community.OpFriendDeleteRequest, friendDeleteFrame(2))
	expectFriendEvent(t, connA2, community.EncodeFriendEventDeleted3F9A(2), "A's case-3 delete event")
	expectFriendEvent(t, connB2, community.EncodeFriendEventDeleted3F9A(1), "B's mutual case-3 delete event")
	if got := readFriends(t, second.authority, e2eFriendNameA); len(got) != 0 {
		t.Fatalf("A's stored edges after delete = %+v, want empty", got)
	}
	if got := readFriends(t, second.authority, e2eFriendNameB); len(got) != 0 {
		t.Fatalf("B's stored edges after delete = %+v, want empty", got)
	}

	// A stray delete of the now-unlisted jid: SILENT refusal.
	sendFrame(t, connA2, community.OpFriendDeleteRequest, friendDeleteFrame(2))
	gameReadyBarrier(t, connA2, "post-stray-delete")

	if dropped := second.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s) on the reopened server", dropped)
	}
	sendFrame(t, connA2, transport.OpBye, []byte{transport.ByeReasonNormal})
	sendFrame(t, connB2, transport.OpBye, []byte{transport.ByeReasonNormal})
}
