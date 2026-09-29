package community_test

// End-to-end exercise of the LETTER lane over the REAL transport against
// the REAL authority store (the community e2e_wire_test.go precedent,
// extended to TWO live sessions for the presence-targeted push):
//
//	A sends 0x7261  -> A gets the pinned 0xB261 [01] ack; B (ONLINE,
//	                   bound via the same BindExclusive glue server.go
//	                   uses) gets the pinned 0x3F9A case-8 push; the
//	                   letter lands in B's PERSISTED mailbox (the memos
//	                   table) unread;
//	refusal arms    -> oversized body / unknown receiver stay SILENT
//	                   (game-ready barrier) and the mailbox is untouched;
//	reboot          -> store + server torn down and reopened: the memos
//	                   TABLE survives, and B's fresh enter-world 0xB3CD
//	                   seed carries the letter;
//	B sends 0x73F2  -> the pinned 0xB3F2 [01 idx body] answer and the
//	                   persisted read flag flips;
//	B sends 0x70CC  -> the pinned 0xB0CC [01 idx] ack and the mailbox
//	                   empties; an out-of-range index stays silent.

import (
	"bytes"
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
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

const (
	// Both 12 chars: the top of the native 2..12 creation window, and
	// exactly the 0xc receiver edit cap.
	letterE2ESender   = "e2eLetterAsh"
	letterE2EReceiver = "e2eLetterBee"
	letterE2EBody     = "Meet me at the Jangan south gate."
)

// startLetterServer opens the authority store in dir and stands up the
// transport with the bootstrap + community + letter lanes composed like
// server.go: one deps pointer; store collaborators, the letter door and
// the division-aware seed seam assigned before the registers capture it;
// OnWorldBound claiming the same presence bind key the case-8 push
// resolves recipients through.
func startLetterServer(t *testing.T, dir string, seeds []*enterworld.Character) e2eServer {
	t.Helper()

	authority, err := store.Open(dir, store.Options{DefaultSkills: communitySkillSeeder})
	if err != nil {
		t.Fatalf("store.Open(%s): %v", dir, err)
	}
	t.Cleanup(authority.Close)

	existing := map[string]bool{}
	for _, c := range authority.Characters().CharactersForDivision(e2eDivision) {
		existing[c.Name] = true
	}
	for _, seed := range seeds {
		if seed == nil || existing[seed.Name] {
			continue
		}
		if err := authority.CreateCharacter(e2eDivision, "test-account", seed); err != nil {
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
	deps.CommunitySeedFramesFor = community.SeedFramesFunc(directory, deps.Letters)
	// The presence bind: the same key server.go's OnWorldBound claims -
	// without it the case-8 push cannot resolve the online recipient.
	deps.OnWorldBound = func(s *transport.Session, divisionID string, character *enterworld.Character) {
		srv.Hub.BindExclusive(presence.BindKey(divisionID, character.Name), s)
	}
	enterworld.Register(srv.Hub, deps)
	community.Register(srv.Hub, deps)
	community.RegisterFriend(srv.Hub, deps, directory)
	community.RegisterLetter(srv.Hub, deps, directory)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shutdownServer(t, srv) })
	return e2eServer{srv: srv, authority: authority}
}

// letterEnterWorld performs the 0x0006 bind as name, consumes the frozen
// bootstrap sequence and the friend seed, and returns the letter seed
// (0xB3CD) payload for the caller's assertion.
func letterEnterWorld(t *testing.T, c *websocket.Conn, name string) []byte {
	t.Helper()
	sendFrame(t, c, transport.OpEnterWorld, transport.EncodeEnterWorld(
		entryauth.NewAuthenticatedEntryFixture(t, e2eDivision, name),
	))
	result, err := transport.DecodeEnterWorldResult(expectFrame(t, c, transport.OpEnterWorldResult, "enter world "+name))
	if err != nil {
		t.Fatalf("decoding 0x0007: %v", err)
	}
	if !result.OK {
		t.Fatalf("enter world %s refused: nativeErrorCode=%#x", name, result.NativeErrorCode)
	}
	expectFrame(t, c, enterworld.OpcodeResetClient, "bootstrap[0]")
	expectFrame(t, c, enterworld.OpcodeMyCharacterData, "bootstrap[1]")
	expectFrame(t, c, enterworld.OpcodeMyCharacterChunk, "bootstrap[2]")
	expectFrame(t, c, enterworld.OpcodeMyCharacterFlush, "bootstrap[3]")
	expectFrame(t, c, enterworld.OpcodeServerClockGidLatch, "bootstrap[4]")
	expectFrame(t, c, enterworld.OpcodeObjectListStart, "bootstrap[5]")
	expectFrame(t, c, enterworld.OpcodeObjectListFinalize, "bootstrap[6]")
	if got := expectFrame(t, c, community.OpFriendRosterPush, "friend roster seed"); !bytes.Equal(got, []byte{0x00}) {
		t.Fatalf("0x3769 seed payload = % X, want the empty roster [00]", got)
	}
	letters := expectFrame(t, c, community.OpLetterListAnswer, "letter list seed")
	wiretest.ActivateWorld(t, c, "enter world "+name)
	gameReadyBarrier(t, c, "world-bound tail "+name)
	return letters
}

func letterE2ECharID(t *testing.T, authority *store.Store, name string) int64 {
	t.Helper()
	var id int64
	found := false
	authority.ReadCharacters(e2eDivision, func(characters []*enterworld.Character) {
		for _, c := range characters {
			if c != nil && c.Name == name {
				id, found = c.ID, true
				return
			}
		}
	})
	if !found {
		t.Fatalf("character %s not in the store", name)
	}
	return id
}

func letterSendRequest(receiver, body string) []byte {
	out := []byte{byte(len(receiver)), byte(len(receiver) >> 8)}
	out = append(out, []byte(receiver)...)
	out = append(out, byte(len(body)), byte(len(body)>>8))
	return append(out, []byte(body)...)
}

func TestLetterLaneEndToEndOverWire(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")

	seeds := []*enterworld.Character{
		{
			Name:          letterE2ESender,
			ModelCodename: "CHAR_CH_MAN_ADVENTURER",
			RaceIndex:     e2eInt64(enterworld.RaceChina),
			Gender:        e2eInt64(enterworld.GenderMale),
		},
		{
			Name:          letterE2EReceiver,
			ModelCodename: "CHAR_CH_MAN_ADVENTURER",
			RaceIndex:     e2eInt64(enterworld.RaceChina),
			Gender:        e2eInt64(enterworld.GenderMale),
		},
	}

	// ---- session pair: empty seeds, then the ONLINE delivery ----
	first := startLetterServer(t, dir, seeds)
	connAsh := dialWS(t, first.srv)
	helloWS(t, connAsh)
	if got := letterEnterWorld(t, connAsh, letterE2ESender); !bytes.Equal(got, []byte{0x01, 0x00}) {
		t.Fatalf("sender letter seed = % X, want the empty list [01 00]", got)
	}
	connBee := dialWS(t, first.srv)
	helloWS(t, connBee)
	if got := letterEnterWorld(t, connBee, letterE2EReceiver); !bytes.Equal(got, []byte{0x01, 0x00}) {
		t.Fatalf("receiver letter seed = % X, want the empty list [01 00]", got)
	}

	before := time.Now()
	sendFrame(t, connAsh, community.OpLetterSendRequest, letterSendRequest(letterE2EReceiver, letterE2EBody))
	if got := expectFrame(t, connAsh, community.OpLetterSendAckAnswer, "send ack"); !bytes.Equal(got, []byte{0x01}) {
		t.Fatalf("0xB261 ack payload = % X, want [01]", got)
	}
	push := expectFrame(t, connBee, community.OpFriendLetterEventPush, "letter-received push")
	after := time.Now()
	// The sender resolves to the race/gender fallback model (empty test
	// roster); the packed time is minute-granular, so either boundary
	// capture is the legal composition.
	senderModel := enterworld.CharacterModelRef(seeds[0], &enterworld.Roster{})
	wantBefore := community.EncodeLetterReceivedEvent3F9A(letterE2ESender, senderModel, community.PackLetterReceiveTime(before))
	wantAfter := community.EncodeLetterReceivedEvent3F9A(letterE2ESender, senderModel, community.PackLetterReceiveTime(after))
	if !bytes.Equal(push, wantBefore) && !bytes.Equal(push, wantAfter) {
		t.Fatalf("0x3F9A push payload = % X, want % X", push, wantBefore)
	}

	beeID := letterE2ECharID(t, first.authority, letterE2EReceiver)
	mailbox := first.authority.Letters().Mailbox(e2eDivision, beeID)
	if len(mailbox) != 1 || mailbox[0].Sender != letterE2ESender || mailbox[0].Body != letterE2EBody || mailbox[0].ReadFlag != 0 {
		t.Fatalf("persisted mailbox = %+v, want the one unread letter", mailbox)
	}
	packedTime := mailbox[0].PackedReceiveTime

	// Refusal arms stay silent and the mailbox untouched: a body past
	// the 0x100 edit cap, then an unknown receiver.
	sendFrame(t, connAsh, community.OpLetterSendRequest, letterSendRequest(letterE2EReceiver, strings.Repeat("x", community.LetterBodyMaxBytes+1)))
	sendFrame(t, connAsh, community.OpLetterSendRequest, letterSendRequest("Nobody", "hello?"))
	gameReadyBarrier(t, connAsh, "post-refusals")
	if got := first.authority.Letters().Mailbox(e2eDivision, beeID); len(got) != 1 {
		t.Fatalf("mailbox after refusals = %d letter(s), want 1", len(got))
	}

	if dropped := first.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s)", dropped)
	}
	sendFrame(t, connAsh, transport.OpBye, []byte{transport.ByeReasonNormal})
	sendFrame(t, connBee, transport.OpBye, []byte{transport.ByeReasonNormal})
	connAsh.Close()
	connBee.Close()

	// ---- the reboot: the memos TABLE is persisted state ----
	shutdownServer(t, first.srv)
	first.authority.Close()

	second := startLetterServer(t, dir, nil)
	if got := second.authority.Letters().Mailbox(e2eDivision, beeID); len(got) != 1 || got[0].Body != letterE2EBody {
		t.Fatalf("restored mailbox = %+v, want the persisted letter", got)
	}

	// ---- session 2: the seed carries the letter; read + delete ----
	connBee2 := dialWS(t, second.srv)
	helloWS(t, connBee2)
	seedPayload := letterEnterWorld(t, connBee2, letterE2EReceiver)
	wantSeed := community.EncodeLetterListB3CD([]community.LetterListEntry{{
		Sender:            letterE2ESender,
		SenderModelRefID:  senderModel,
		PackedReceiveTime: packedTime,
		ReadFlag:          0,
	}})
	if !bytes.Equal(seedPayload, wantSeed) {
		t.Fatalf("post-reboot 0xB3CD seed = % X, want % X", seedPayload, wantSeed)
	}

	// Read-fetch index 0: the pinned body answer, and the persisted flag
	// flips.
	sendFrame(t, connBee2, community.OpLetterReadFetchRequest, []byte{0x00})
	if got, want := expectFrame(t, connBee2, community.OpLetterReadBodyAnswer, "read body"), community.EncodeLetterReadBodyB3F2(0, letterE2EBody); !bytes.Equal(got, want) {
		t.Fatalf("0xB3F2 payload = % X, want % X", got, want)
	}
	if got := second.authority.Letters().Mailbox(e2eDivision, beeID); len(got) != 1 || got[0].ReadFlag != 1 {
		t.Fatalf("mailbox after read = %+v, want the read flag flipped", got)
	}

	// Delete index 0: the pinned ack, and the mailbox empties.
	sendFrame(t, connBee2, community.OpLetterDeleteRequest, []byte{0x00})
	if got := expectFrame(t, connBee2, community.OpLetterDeleteAckAnswer, "delete ack"); !bytes.Equal(got, []byte{0x01, 0x00}) {
		t.Fatalf("0xB0CC payload = % X, want [01 00]", got)
	}
	if got := second.authority.Letters().Mailbox(e2eDivision, beeID); len(got) != 0 {
		t.Fatalf("mailbox after delete = %+v, want empty", got)
	}

	// Out-of-range delete: silent (the barrier proves no frame rode).
	sendFrame(t, connBee2, community.OpLetterDeleteRequest, []byte{0x05})
	gameReadyBarrier(t, connBee2, "post-out-of-range")

	if dropped := second.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s) on the reopened server", dropped)
	}
	sendFrame(t, connBee2, transport.OpBye, []byte{transport.ByeReasonNormal})
}
