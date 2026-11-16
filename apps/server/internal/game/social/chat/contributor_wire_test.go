/*
===========================================================================

contributor_wire_test.go - /contributor over the wire: consumed, one line back

Port-only, not native (#3358). Over the real transport: A's
/contributor line reaches the title switch with A's account, A gets the
ordinary success ack plus exactly one notice carrying the answer, and B
hears nothing. With no switch wired the same text is ordinary chat.

===========================================================================
*/
package chat_test

import (
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/social/chat"
)

/*
================
TestChatContributorLineIsConsumedAndPrivate
================
*/
func TestChatContributorLineIsConsumedAndPrivate(t *testing.T) {
	const division = "global-official"
	const answer = "Your Contributor title is hidden."
	server := startChatServer(t, filepath.Join(t.TempDir(), "authority"), division)
	asked := make(chan string, 2)
	server.chat.Contributor = func(accountID, message string) (string, bool) {
		if message != "/contributor off" {
			return "", false
		}
		asked <- accountID
		return answer, true
	}

	a := dialWS(t, server.srv)
	helloWS(t, a)
	enterChatWorld(t, a, division, e2eChatNameA)
	b := dialWS(t, server.srv)
	helloWS(t, b)
	enterChatWorld(t, b, division, e2eChatNameB)

	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeAll, "", "/contributor off"))
	expectExact(t, a, chat.OpChatAck, []byte{1, 1, 255}, "title receipt")
	expectExact(t, a, chat.OpChatBroadcast, chat.EncodeChatNotice(answer), "title answer")
	gameReadyBarrier(t, b, "the room hears nothing")
	if account := <-asked; account == "" {
		t.Fatal("the switch was asked without the speaker's account")
	}

	// Unwired, the same text is ordinary chat the room hears.
	server.chat.Contributor = nil
	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeAll, "", "/contributor off"))
	expectExact(t, a, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "/contributor off"), "unwired line is chat")
	expectExact(t, a, chat.OpChatAck, []byte{1, 1, 255}, "ordinary receipt")
	expectExact(t, b, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "/contributor off"), "the room hears the unwired line")
}
