/*
===========================================================================

plusfail_wire_test.go - /plusfail over the wire: consumed, one line back

Port-only, not native (#3682). Over the real transport: A's /plusfail
line reaches the switch with A's own character, A gets the ordinary
success ack plus exactly one notice carrying the answer, and B hears
nothing. With no switch wired the same text is ordinary chat.
================
*/
package chat_test

import (
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/chat"
)

/*
================
TestChatPlusFailLineIsConsumedAndPrivate
================
*/
func TestChatPlusFailLineIsConsumedAndPrivate(t *testing.T) {
	const division = "global-official"
	const answer = "Failed reinforcement announcements are off for this character: your failed reinforcement stays private."
	server := startChatServer(t, filepath.Join(t.TempDir(), "authority"), division)
	asked := make(chan bool, 2)
	server.chat.PlusFailNotice = func(character *enterworld.Character, on bool) bool {
		if character == nil || character.Name != e2eChatNameA {
			return false
		}
		character.PlusFailOptOut = !on
		asked <- on
		return true
	}

	a := dialWS(t, server.srv)
	helloWS(t, a)
	enterChatWorld(t, a, division, e2eChatNameA)
	b := dialWS(t, server.srv)
	helloWS(t, b)
	enterChatWorld(t, b, division, e2eChatNameB)

	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeAll, "", "/plusfail off"))
	expectExact(t, a, chat.OpChatAck, []byte{1, 1, 255}, "switch receipt")
	expectExact(t, a, chat.OpChatBroadcast, chat.EncodeChatNotice(answer), "switch answer")
	gameReadyBarrier(t, b, "the room hears nothing")
	if on := <-asked; on {
		t.Fatal("the switch was asked to turn the announcements on for '/plusfail off'")
	}

	// Unwired, the same text is ordinary chat the room hears.
	server.chat.PlusFailNotice = nil
	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeAll, "", "/plusfail off"))
	expectExact(t, a, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "/plusfail off"), "unwired line is chat")
	expectExact(t, a, chat.OpChatAck, []byte{1, 1, 255}, "ordinary receipt")
	expectExact(t, b, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "/plusfail off"), "the room hears the unwired line")
}
