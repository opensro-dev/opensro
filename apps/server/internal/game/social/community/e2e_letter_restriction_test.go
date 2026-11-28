/*
===========================================================================

e2e_letter_restriction_test.go - a chat-blocked sender's letter dies

Over the real transport: while the sender's session carries the chat
restriction, a letter send gets the restriction notice and nothing
else - the recipient's mailbox stays empty. Once the restriction
clears, the same letter lands.

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

/*
================
TestChatBlockedSenderLetterIsDropped
================
*/
func TestChatBlockedSenderLetterIsDropped(t *testing.T) {
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

	sendFrame(t, connAsh, community.OpLetterSendRequest, letterSendRequest(letterE2EReceiver, letterE2EBody))
	if got := expectFrame(t, connAsh, wire.OpGMRestrictionNotice, "restriction notice"); !bytes.Equal(got, wire.EncodeGMRestrictionNotice(0, stamp)) {
		t.Fatalf("notice payload = % X, want the chat channel with the stamp", got)
	}
	gameReadyBarrier(t, connAsh, "the blocked letter sent nothing more")
	if mailbox := server.authority.Letters().Mailbox(e2eDivision, beeID); len(mailbox) != 0 {
		t.Fatalf("a chat-blocked letter landed: %+v", mailbox)
	}

	if !sessions[0].ClearCommandRestriction(transport.CommandRestrictionChat) {
		t.Fatal("the chat restriction did not clear")
	}
	sendFrame(t, connAsh, community.OpLetterSendRequest, letterSendRequest(letterE2EReceiver, letterE2EBody))
	if got := expectFrame(t, connAsh, community.OpLetterSendAckAnswer, "send ack"); !bytes.Equal(got, []byte{0x01}) {
		t.Fatalf("0xB261 ack payload = % X, want [01]", got)
	}
	if mailbox := server.authority.Letters().Mailbox(e2eDivision, beeID); len(mailbox) != 1 {
		t.Fatalf("mailbox after the clear = %d letter(s), want 1", len(mailbox))
	}
	sendFrame(t, connAsh, transport.OpBye, []byte{transport.ByeReasonNormal})
	connAsh.Close()
}
