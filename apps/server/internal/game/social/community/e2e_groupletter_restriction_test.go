/*
===========================================================================

e2e_groupletter_restriction_test.go - a chat-blocked group letter dies

Over the real transport: while the sender's session carries the chat
restriction, a group letter (0x7600) gets the restriction notice and
nothing else - it dies at the door, before any guild or mailbox
work. Once the restriction clears, the same request reaches the
handler (and draws its no-guild refusal answer).

===========================================================================
*/

package community_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/community"
	"opensro.online/server/internal/transport"
)

func groupLetterRequest(kind uint8, body string, names ...string) []byte {
	out := []byte{kind, byte(len(names))}
	for _, name := range names {
		out = append(out, byte(len(name)), byte(len(name)>>8))
		out = append(out, []byte(name)...)
	}
	out = append(out, byte(len(body)), byte(len(body)>>8))
	return append(out, []byte(body)...)
}

/*
================
TestChatBlockedSenderGroupLetterIsDropped
================
*/
func TestChatBlockedSenderGroupLetterIsDropped(t *testing.T) {
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
	server := startLetterServer(t, dir, seeds)
	connAsh := dialWS(t, server.srv)
	helloWS(t, connAsh)
	letterEnterWorld(t, connAsh, letterE2ESender)

	sessions := server.srv.Hub.CharacterSessions(e2eDivision, letterE2ESender)
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want the sender's one", len(sessions))
	}
	stamp := [8]uint16{2027, 3, 1, 9, 8, 30, 0, 0}
	if !sessions[0].SetCommandRestriction(transport.CommandRestrictionChat, stamp) {
		t.Fatal("the chat restriction did not install")
	}
	beeID := letterE2ECharID(t, server.authority, letterE2EReceiver)

	sendFrame(t, connAsh, community.OpGroupLetterSendRequest, groupLetterRequest(1, letterE2EBody, letterE2EReceiver))
	if got := expectFrame(t, connAsh, wire.OpGMRestrictionNotice, "restriction notice"); !bytes.Equal(got, wire.EncodeGMRestrictionNotice(0, stamp)) {
		t.Fatalf("notice payload = % X, want the chat channel with the stamp", got)
	}
	gameReadyBarrier(t, connAsh, "the blocked group letter sent nothing more")
	if mailbox := server.authority.Letters().Mailbox(e2eDivision, beeID); len(mailbox) != 0 {
		t.Fatalf("a chat-blocked group letter landed: %+v", mailbox)
	}

	if !sessions[0].ClearCommandRestriction(transport.CommandRestrictionChat) {
		t.Fatal("the chat restriction did not clear")
	}
	sendFrame(t, connAsh, community.OpGroupLetterSendRequest, groupLetterRequest(1, letterE2EBody, letterE2EReceiver))
	if got := expectFrame(t, connAsh, community.OpGroupLetterSendAnswer, "no-guild refusal"); !bytes.Equal(got, []byte{0x02, community.GroupLetterRefusalNoGuild}) {
		t.Fatalf("0xB600 payload = % X, want [02 16] (the sender stands in no guild)", got)
	}
	sendFrame(t, connAsh, transport.OpBye, []byte{transport.ByeReasonNormal})
	connAsh.Close()
}
