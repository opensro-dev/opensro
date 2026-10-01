/*
===========================================================================

e2e_wire_test.go - the match lane end to end over the wire

===========================================================================
*/

package match_test

// End-to-end exercise of the match lane over the REAL transport (the
// community e2e_wire_test.go precedent): two WebSocket clients speak the
// production frame protocol against a loopback transport.Server composed
// like server.go's gameplay wiring (ONE *enterworld.Deps, the store
// collaborators assigned BEFORE the pointer-based Registers share),
// asserting:
//
//	party register   -> 0xB6FF success ack, byte-exact;
//	page             -> 0xB588 with the requester's OWN row 0 (the
//	                    client's row-0 split contract) plus the OTHER
//	                    session's row, byte-exact;
//	modify           -> 0xB3DC ack + the listing updates in place;
//	delete           -> 0xB535 {id} + the row disappears;
//	mentor cycle     -> 0xB55D / 0xB701 / 0xB70B twins, byte-exact;
//	duplicate reg    -> SILENT refusal (flag-2 codes unpinned; the
//	                    game-ready barrier proves nothing rode);
//	unwired joins    -> 0x75BF/0x7592 answer the pinned 0xB5BF/0xB592
//	                    {01 00} refused ack when the party/mentor seams
//	                    are not assigned (this suite wires neither -
//	                    join_e2e_test.go drives the full handshake);
//	reboot           -> the board is EMPTY: in-memory match state dying
//	                    with the process is the design, so the fresh
//	                    server answers the empty page [01 01 01 00].
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
	"opensro.online/server/internal/game/enterworld"
	wiretest "opensro.online/server/internal/game/internal"
	presence "opensro.online/server/internal/game/social"
	"opensro.online/server/internal/game/social/match"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

const (
	e2eDivision  = "global-official"
	e2eHeroName  = "e2eMatchHero"
	e2eAliceName = "e2eMatchAli"
)

type e2eServer struct {
	srv       *transport.Server
	authority *store.Store
	deps      *enterworld.Deps
	runtime   *match.Runtime
}

/*
==================
matchSkillSeeder

matchSkillSeeder is this suite's stand-in for
enterworld.DefaultSkillSeeder (the store's unconditional creation-seed
invariant refuses an unseeded CreateCharacter): the same racial id
sets, without a textdata dependency. Match tests never read skills -
the seeder exists only to satisfy the store's creation invariant.
==================
*/
func matchSkillSeeder(raceKey string, learned []uint32) ([]uint32, error) {
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

/*
==================
startMatchServer

startMatchServer opens the authority store in dir and stands up the
transport with the bootstrap + match lanes composed like server.go:
one deps pointer, store collaborators assigned before the registers
capture it, the match runtime constructed fresh (its board is
process-local by design), the presence facade over the hub's
exclusive bindings, BindExclusive claimed in OnWorldBound with the
match stale-row purge on its tail, and the disconnect purge on
OnSessionClose.
==================
*/
func startMatchServer(t *testing.T, dir string, seeds []*enterworld.Character) e2eServer {
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
	runtime := match.NewRuntime(deps, presence.NewDirectory(srv.Hub))
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
	return e2eServer{srv: srv, authority: authority, deps: deps, runtime: runtime}
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

func expectExactFrame(t *testing.T, c *websocket.Conn, opcode uint16, want []byte, what string) {
	t.Helper()
	got := expectFrame(t, c, opcode, what)
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: 0x%04X payload = % X, want % X", what, opcode, got, want)
	}
}

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
func enterWorld(t *testing.T, c *websocket.Conn, charName string) {
	t.Helper()
	sendFrame(t, c, transport.OpEnterWorld, transport.EncodeEnterWorld(
		entryauth.NewAuthenticatedEntryFixture(t, e2eDivision, charName),
	))
	result, err := transport.DecodeEnterWorldResult(expectFrame(t, c, transport.OpEnterWorldResult, "enter world"))
	if err != nil {
		t.Fatalf("decoding 0x0007: %v", err)
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
}

// gameReadyBarrier proves NOTHING is queued using a non-mutating transport
// FIFO barrier; it must not replay world admission.
func gameReadyBarrier(t *testing.T, c *websocket.Conn, what string) {
	t.Helper()
	wiretest.AssertQueueDrained(t, c, what)
}

func e2eInt64(v int64) *int64 { return &v }

// ---- wire fixture builders (the re-harness byte shapes) ----

func u16le(v uint16) []byte { return []byte{byte(v), byte(v >> 8)} }

func u32le(v uint32) []byte {
	return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
}

func narrowStr(s string) []byte {
	return append(u16le(uint16(len(s))), []byte(s)...)
}

func wideStr(s string) []byte {
	runes := []rune(s)
	out := u16le(uint16(len(runes)))
	for _, r := range runes {
		out = append(out, byte(r), byte(uint16(r)>>8))
	}
	return out
}

func concat(chunks ...[]byte) []byte {
	var out []byte
	for _, chunk := range chunks {
		out = append(out, chunk...)
	}
	return out
}

// partyRequest renders the sub_703850 C->S body.
func partyRequest(entryID, partyNumber uint32, typeBits, purpose, minLv, maxLv uint8, title string) []byte {
	return concat(u32le(entryID), u32le(partyNumber), []byte{typeBits, purpose, minLv, maxLv}, wideStr(title))
}

// mentorRequest renders the sub_7038f0 C->S body.
func mentorRequest(entryID, dword04 uint32, kind uint8, detail string) []byte {
	return concat(u32le(entryID), u32le(dword04), []byte{kind}, wideStr(detail))
}

// partyRow renders one 0xB588 listing row (flag0b BEFORE flag0a).
func partyRow(id, partyNo uint32, master string, race, count, typeBits, purpose, minLv, maxLv uint8, title string) []byte {
	return concat(u32le(id), u32le(partyNo), narrowStr(master), []byte{race, count, typeBits, purpose, minLv, maxLv}, wideStr(title))
}

// mentorRow renders one 0xB701 listing row (the dropped u32 is 0, the
// level pair rides dword08/byte09, the camp scalars are 0).
func mentorRow(id uint32, kind uint8, detail string, dword04 uint32, level uint8, refObj uint32, requester string) []byte {
	return concat(
		u32le(id), u32le(0), []byte{kind}, wideStr(detail),
		u32le(dword04), []byte{level, level}, u32le(refObj), narrowStr(requester),
		u32le(0), []byte{0}, u32le(0), u32le(0),
	)
}

const emptyListingPage = "\x01\x01\x01\x00"

func TestMatchLaneEndToEndOverWire(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")

	seeds := []*enterworld.Character{
		{
			Name:          e2eHeroName,
			ModelCodename: "CHAR_CH_MAN_ADVENTURER",
			RaceIndex:     e2eInt64(enterworld.RaceChina),
			Gender:        e2eInt64(enterworld.GenderMale),
			Level:         e2eInt64(10),
		},
		{
			Name:          e2eAliceName,
			ModelCodename: "CHAR_CH_WOMAN_ADVENTURER",
			RaceIndex:     e2eInt64(enterworld.RaceChina),
			Gender:        e2eInt64(enterworld.GenderFemale),
			Level:         e2eInt64(10),
		},
	}

	first := startMatchServer(t, dir, seeds)

	// The China seeds carry native country byte 0; the mentor rows'
	// refObjIds resolve through the same helper the handlers use.
	var heroChar, aliceChar *enterworld.Character
	for _, c := range first.deps.Characters.CharactersForDivision(e2eDivision) {
		switch c.Name {
		case e2eHeroName:
			heroChar = c
		case e2eAliceName:
			aliceChar = c
		}
	}
	if heroChar == nil || aliceChar == nil {
		t.Fatal("seed characters missing from the store")
	}
	heroModel := enterworld.CharacterModelRef(heroChar, first.deps.Roster)
	aliceModel := enterworld.CharacterModelRef(aliceChar, first.deps.Roster)

	hero := dialWS(t, first.srv)
	helloWS(t, hero)
	enterWorld(t, hero, e2eHeroName)

	alice := dialWS(t, first.srv)
	helloWS(t, alice)
	enterWorld(t, alice, e2eAliceName)

	// ---- empty board: the page answers one empty page ----
	sendFrame(t, hero, match.OpPartyPageRequest, []byte{1})
	expectExactFrame(t, hero, match.OpPartyListingPage, []byte(emptyListingPage), "empty party page")

	// ---- party register: 0xB6FF ack byte-exact ----
	sendFrame(t, hero, match.OpPartyRegisterRequest, partyRequest(0, 1001, 2, 1, 15, 80, "hero party"))
	heroPartyAck := concat([]byte{1}, u32le(1), u32le(1001), []byte{2, 1, 15, 80}, wideStr("hero party"))
	expectExactFrame(t, hero, match.OpPartyRegisterAck, heroPartyAck, "party register ack")

	sendFrame(t, alice, match.OpPartyRegisterRequest, partyRequest(0, 2002, 0, 1, 1, 40, "alice party"))
	alicePartyAck := concat([]byte{1}, u32le(2), u32le(2002), []byte{0, 1, 1, 40}, wideStr("alice party"))
	expectExactFrame(t, alice, match.OpPartyRegisterAck, alicePartyAck, "alice party register ack")

	// ---- the listing: hero's OWN row 0, then alice's row ----
	sendFrame(t, hero, match.OpPartyPageRequest, []byte{1})
	heroListing := concat(
		[]byte{1, 1, 1, 2},
		partyRow(1, 1001, e2eHeroName, 0, 1, 2, 1, 15, 80, "hero party"),
		partyRow(2, 2002, e2eAliceName, 0, 1, 0, 1, 1, 40, "alice party"),
	)
	expectExactFrame(t, hero, match.OpPartyListingPage, heroListing, "hero party listing")

	// Alice's view swaps the split: her own row rides row 0.
	sendFrame(t, alice, match.OpPartyPageRequest, []byte{1})
	aliceListing := concat(
		[]byte{1, 1, 1, 2},
		partyRow(2, 2002, e2eAliceName, 0, 1, 0, 1, 1, 40, "alice party"),
		partyRow(1, 1001, e2eHeroName, 0, 1, 2, 1, 15, 80, "hero party"),
	)
	expectExactFrame(t, alice, match.OpPartyListingPage, aliceListing, "alice party listing")

	// ---- duplicate register: native category-2 unknown error ----
	sendFrame(t, hero, match.OpPartyRegisterRequest, partyRequest(0, 9999, 0, 0, 0, 0, "dup"))
	expectExactFrame(t, hero, match.OpPartyRegisterAck, []byte{2, 2}, "duplicate register refusal")
	gameReadyBarrier(t, hero, "post-duplicate-register")

	// ---- modify: 0xB3DC ack + the listing updates in place ----
	sendFrame(t, hero, match.OpPartyModifyRequest, partyRequest(1, 1001, 1, 0, 40, 90, "hero party v2"))
	heroModifyAck := concat([]byte{1}, u32le(1), u32le(1001), []byte{1, 0, 40, 90}, wideStr("hero party v2"))
	expectExactFrame(t, hero, match.OpPartyModifyAck, heroModifyAck, "party modify ack")

	sendFrame(t, hero, match.OpPartyPageRequest, []byte{1})
	modifiedListing := concat(
		[]byte{1, 1, 1, 2},
		partyRow(1, 1001, e2eHeroName, 0, 1, 1, 0, 40, 90, "hero party v2"),
		partyRow(2, 2002, e2eAliceName, 0, 1, 0, 1, 1, 40, "alice party"),
	)
	expectExactFrame(t, hero, match.OpPartyListingPage, modifiedListing, "modified party listing")

	// ---- foreign delete: native refusal (alice's entry id) ----
	sendFrame(t, hero, match.OpPartyDeleteRequest, u32le(2))
	expectExactFrame(t, hero, match.OpPartyDeleteAck, []byte{2, 2}, "foreign delete refusal")
	gameReadyBarrier(t, hero, "post-foreign-delete")

	// ---- own delete: 0xB535 {id} + the row disappears ----
	sendFrame(t, hero, match.OpPartyDeleteRequest, u32le(1))
	expectExactFrame(t, hero, match.OpPartyDeleteAck, concat([]byte{1}, u32le(1)), "party delete ack")

	sendFrame(t, hero, match.OpPartyPageRequest, []byte{1})
	afterDelete := concat([]byte{1, 1, 1, 1}, partyRow(2, 2002, e2eAliceName, 0, 1, 0, 1, 1, 40, "alice party"))
	expectExactFrame(t, hero, match.OpPartyListingPage, afterDelete, "party listing after delete")

	// ---- mentor cycle on the CNetProcessSecond opcode twins ----
	sendFrame(t, hero, match.OpMentorPageRequest, []byte{1})
	expectExactFrame(t, hero, match.OpMentorListingPage, []byte(emptyListingPage), "empty mentor page")

	sendFrame(t, hero, match.OpMentorRegisterRequest, mentorRequest(0, 7, 1, "hero camp"))
	heroMentorAck := concat([]byte{1}, u32le(3), u32le(7), []byte{1}, wideStr("hero camp"), u32le(0))
	expectExactFrame(t, hero, match.OpMentorRegisterAck, heroMentorAck, "mentor register ack")

	sendFrame(t, alice, match.OpMentorRegisterRequest, mentorRequest(0, 8, 2, "alice camp"))
	aliceMentorAck := concat([]byte{1}, u32le(4), u32le(8), []byte{2}, wideStr("alice camp"), u32le(0))
	expectExactFrame(t, alice, match.OpMentorRegisterAck, aliceMentorAck, "alice mentor register ack")

	sendFrame(t, hero, match.OpMentorPageRequest, []byte{1})
	mentorListing := concat(
		[]byte{1, 1, 1, 2},
		mentorRow(3, 1, "hero camp", 7, 10, heroModel, e2eHeroName),
		mentorRow(4, 2, "alice camp", 8, 10, aliceModel, e2eAliceName),
	)
	expectExactFrame(t, hero, match.OpMentorListingPage, mentorListing, "mentor listing")

	sendFrame(t, hero, match.OpMentorModifyRequest, mentorRequest(3, 7, 2, "hero camp v2"))
	heroMentorModify := concat([]byte{1}, u32le(3), u32le(7), []byte{2}, wideStr("hero camp v2"))
	expectExactFrame(t, hero, match.OpMentorModifyAck, heroMentorModify, "mentor modify ack")

	sendFrame(t, hero, match.OpMentorDeleteRequest, u32le(3))
	expectExactFrame(t, hero, match.OpMentorDeleteAck, concat([]byte{1}, u32le(3)), "mentor delete ack")

	sendFrame(t, hero, match.OpMentorPageRequest, []byte{1})
	afterMentorDelete := concat([]byte{1, 1, 1, 1}, mentorRow(4, 2, "alice camp", 8, 10, aliceModel, e2eAliceName))
	expectExactFrame(t, hero, match.OpMentorListingPage, afterMentorDelete, "mentor listing after delete")

	// ---- the join handlers WITHOUT their wiring.go seams: a live
	// entry refuses on the unwired party seam, a stale id refuses on
	// the miss - both with the PINNED {01 00} refused ack (join.go;
	// the full handshake rides in join_e2e_test.go) ----
	sendFrame(t, hero, match.OpPartyJoinRequest, u32le(2))
	expectExactFrame(t, hero, match.OpPartyJoinAck, []byte{1, 0}, "unwired party join refusal")
	sendFrame(t, hero, match.OpMentorJoinRequest, u32le(0))
	expectExactFrame(t, hero, match.OpMentorJoinAck, []byte{1, 0}, "stale mentor join refusal")
	gameReadyBarrier(t, hero, "post-join-refusals")

	if dropped := first.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s)", dropped)
	}
	sendFrame(t, hero, transport.OpBye, []byte{transport.ByeReasonNormal})
	sendFrame(t, alice, transport.OpBye, []byte{transport.ByeReasonNormal})
	hero.Close()
	alice.Close()

	// ---- the reboot: match state is IN-MEMORY and must be GONE ----
	shutdownServer(t, first.srv)
	first.authority.Close()

	second := startMatchServer(t, dir, nil)
	conn := dialWS(t, second.srv)
	helloWS(t, conn)
	enterWorld(t, conn, e2eHeroName)

	sendFrame(t, conn, match.OpPartyPageRequest, []byte{1})
	expectExactFrame(t, conn, match.OpPartyListingPage, []byte(emptyListingPage), "post-reboot party page (board empty by design)")
	sendFrame(t, conn, match.OpMentorPageRequest, []byte{1})
	expectExactFrame(t, conn, match.OpMentorListingPage, []byte(emptyListingPage), "post-reboot mentor page (board empty by design)")

	sendFrame(t, conn, transport.OpBye, []byte{transport.ByeReasonNormal})
}
