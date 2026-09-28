/*
===========================================================================

history_wire_test.go - public history bounds and ordered live delivery

Real transport admission must replay only the last ten public messages.
Whispers never enter the transcript, and repeated game-ready cannot replay it.

===========================================================================
*/
package chat_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/chat"
)

/*
================
TestPublicHistoryAdmissionAndLiveDelivery
================
*/
func TestPublicHistoryAdmissionAndLiveDelivery(t *testing.T) {
	const division = "global-official"
	server := startChatServer(t, filepath.Join(t.TempDir(), "authority"), division)
	a := dialWS(t, server.srv)
	helloWS(t, a)
	enterChatWorld(t, a, division, e2eChatNameA)
	for i := 0; i < 12; i++ {
		message := fmt.Sprintf("public %d", i)
		sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeAll, "", message))
		expectExact(t, a, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, message), "own global line")
		expectExact(t, a, chat.OpChatAck, []byte{1, 1, 255}, "keyed receipt")
	}
	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeWhisper, e2eChatNameA, "private"))
	expectExact(t, a, chat.OpChatAck, []byte{1, 2, 255}, "private receipt")
	b := dialWS(t, server.srv)
	helloWS(t, b)
	enterChatWorld(t, b, division, e2eChatNameB)
	for i := 2; i < 12; i++ {
		expectExact(t, b, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, fmt.Sprintf("public %d", i)), "ordered history")
	}
	gameReadyBarrier(t, b, "bounded public-only replay")
	sendFrame(t, b, enterworld.OpcodeGameReady, nil)
	gameReadyBarrier(t, b, "no second replay")
	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeAll, "", "live"))
	expectExact(t, a, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "live"), "live own line")
	expectExact(t, a, chat.OpChatAck, []byte{1, 1, 255}, "live receipt")
	expectExact(t, b, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "live"), "live after replay")
	gameReadyBarrier(t, b, "no duplicate live delivery")
}
