/*
===========================================================================

e2e_notice_wire_test.go - /NOTICE over the real transport

A GM and a player enter the world through the production frame protocol
(the chat lane's e2e precedent). The GM's 0x75B6 /NOTICE reaches both
sessions as the byte-exact 0x3667 type 7 line, after the GM's own reply;
with SRO_GM_NOTICE off, the GM reads the refusal and nobody receives a
line.

===========================================================================
*/
package gmcommand_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/gmcommand"
	wiretest "opensro.online/server/internal/game/internal"
	presence "opensro.online/server/internal/game/social"
	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/staff"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

const (
	noticeDivision = "global-official"
	noticeGMName   = "noticeGm"
	noticePlayer   = "noticeAlfa"
)

/*
================
noticeLaneOwner

The lane owner the GameWorld wiring composes, cut to what /NOTICE reads.
================
*/
type noticeLaneOwner struct{ enabled bool }

func (noticeLaneOwner) ToggleGMBodyStatus(string, string, uint8) bool { return false }

func (o noticeLaneOwner) GMNoticeEnabled() bool { return o.enabled }

/*
================
startNoticeServer

The authority store with a GM and a player, and the transport with the
world-entry and GM lanes registered as wiring_register.go does.
================
*/
func startNoticeServer(t *testing.T, enabled bool) *transport.Server {
	t.Helper()
	authority, err := store.Open(t.TempDir(), store.Options{DefaultSkills: func(string, []uint32) ([]uint32, error) {
		return nil, nil
	}})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(authority.Close)
	race, gender := int64(enterworld.RaceChina), int64(enterworld.GenderMale)
	for _, name := range []string{noticeGMName, noticePlayer} {
		seed := &enterworld.Character{Name: name, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: &race, Gender: &gender}
		if err := authority.CreateCharacter(noticeDivision, "account-"+name, seed); err != nil {
			t.Fatalf("CreateCharacter(%s): %v", name, err)
		}
	}
	for _, character := range authority.Characters().CharactersForDivision(noticeDivision) {
		if character.Name == noticeGMName {
			authority.MutateCharacter(character, "test-gm", func() {
				character.GMPrivilege = true
				character.GMCapabilities = uint32(staff.All)
			})
		}
	}

	deps := &enterworld.Deps{Roster: &enterworld.Roster{}, Characters: authority.Characters()}
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
	srv.Hub.SetHelloAuth(entryauth.UniqueAccountHelloAuth())
	entryauth.NewAuthenticatedSessionFixture(t, srv.Hub)
	deps.OnWorldBound = func(s *transport.Session, divisionID string, character *enterworld.Character) {
		if old, replaced := srv.Hub.BindExclusive(divisionID+":"+strings.ToLower(character.Name), s); replaced {
			old.ClearGameplayContext()
		}
	}
	enterworld.Register(srv.Hub, deps)
	gmcommand.Register(srv.Hub, deps, presence.NewDirectory(srv.Hub), noticeLaneOwner{enabled: enabled})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	})
	return srv
}

/*
================
sendNoticeFrame
================
*/
func sendNoticeFrame(t *testing.T, c *websocket.Conn, opcode uint16, payload []byte) {
	t.Helper()
	if err := c.WriteMessage(websocket.BinaryMessage, transport.Frame{Opcode: opcode, Payload: payload}.Encode()); err != nil {
		t.Fatalf("writing frame 0x%04X: %v", opcode, err)
	}
}

/*
================
expectNoticeFrame

The next application frame, which must carry opcode.
================
*/
func expectNoticeFrame(t *testing.T, c *websocket.Conn, opcode uint16, what string) []byte {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, data, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("%s: reading ws message: %v", what, err)
		}
		f, err := transport.DecodeFrame(data)
		if err != nil {
			t.Fatalf("%s: decoding frame: %v", what, err)
		}
		if f.Opcode == transport.OpPing || f.Opcode == transport.OpPong {
			continue
		}
		wiretest.AckRead(t, c, f)
		if f.Opcode != opcode {
			t.Fatalf("%s: next frame 0x%04X % X, want 0x%04X", what, f.Opcode, f.Payload, opcode)
		}
		return f.Payload
	}
}

/*
================
enterNoticeWorld

Dial, handshake, enter the world and mark it ready.
================
*/
func enterNoticeWorld(t *testing.T, srv *transport.Server, name string) *websocket.Conn {
	t.Helper()
	url := fmt.Sprintf("ws://%s%s", srv.WSAddr(), transport.PathWS)
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	t.Cleanup(func() { c.Close() })
	sendNoticeFrame(t, c, transport.OpHello, transport.EncodeHello(transport.Hello{AdmissionToken: []byte("test-admission")}))
	expectNoticeFrame(t, c, transport.OpWelcome, "handshake "+name)
	sendNoticeFrame(t, c, transport.OpEnterWorld, transport.EncodeEnterWorld(
		entryauth.NewAuthenticatedEntryFixture(t, noticeDivision, name)))
	result, err := transport.DecodeEnterWorldResult(expectNoticeFrame(t, c, transport.OpEnterWorldResult, "enter "+name))
	if err != nil || !result.OK {
		t.Fatalf("enter world %s: %+v %v", name, result, err)
	}
	for _, opcode := range []uint16{enterworld.OpcodeResetClient, enterworld.OpcodeMyCharacterData,
		enterworld.OpcodeMyCharacterChunk, enterworld.OpcodeMyCharacterFlush, enterworld.OpcodeServerClockGidLatch,
		enterworld.OpcodeObjectListStart, enterworld.OpcodeObjectListFinalize} {
		expectNoticeFrame(t, c, opcode, "bootstrap "+name)
	}
	wiretest.ActivateWorld(t, c, "enter "+name)
	return c
}

/*
================
noticeRequest

The 0x75B6 body the port's client composes for /NOTICE text.
================
*/
func noticeRequest(text string) []byte {
	frame := binary.LittleEndian.AppendUint16([]byte{gmcommand.SubNotice}, uint16(len(text)))
	return append(frame, text...)
}

/*
================
TestNoticeReachesEveryPlayerOverWire
================
*/
func TestNoticeReachesEveryPlayerOverWire(t *testing.T) {
	srv := startNoticeServer(t, true)
	gm := enterNoticeWorld(t, srv, noticeGMName)
	player := enterNoticeWorld(t, srv, noticePlayer)

	sendNoticeFrame(t, gm, gmcommand.OpGmCommand, noticeRequest("Server restart in 5 minutes"))
	reply := expectNoticeFrame(t, gm, gmcommand.OpGmCommandAck, "GM reply")
	if want := gmcommand.EncodeModerationReply(gmcommand.AckResultOK, gmcommand.SubNotice, "notice sent"); !bytes.Equal(reply, want) {
		t.Fatalf("GM reply % X, want % X", reply, want)
	}
	want := chat.EncodeChatNotice("Server restart in 5 minutes")
	for label, c := range map[string]*websocket.Conn{"GM": gm, "player": player} {
		if got := expectNoticeFrame(t, c, chat.OpChatBroadcast, label+" notice"); !bytes.Equal(got, want) {
			t.Fatalf("%s notice % X, want % X", label, got, want)
		}
	}
	wiretest.AssertQueueDrained(t, player, "player after the notice")
}

/*
================
TestNoticeOffSendsNothingOverWire
================
*/
func TestNoticeOffSendsNothingOverWire(t *testing.T) {
	srv := startNoticeServer(t, false)
	gm := enterNoticeWorld(t, srv, noticeGMName)
	player := enterNoticeWorld(t, srv, noticePlayer)

	sendNoticeFrame(t, gm, gmcommand.OpGmCommand, noticeRequest("hello"))
	reply := expectNoticeFrame(t, gm, gmcommand.OpGmCommandAck, "GM refusal")
	if reply[0] != gmcommand.AckResultFail || !strings.Contains(string(reply), gmcommand.EnvGMNotice) {
		t.Fatalf("GM refusal % q does not name %s", reply, gmcommand.EnvGMNotice)
	}
	wiretest.AssertQueueDrained(t, gm, "GM after the refusal")
	wiretest.AssertQueueDrained(t, player, "player after the refusal")
}
