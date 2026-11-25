/*
===========================================================================

caravan_wire_test.go - caravan commands over the wire: consumed, the agent answers

Port-only, not native (#3704 slice 4). Over the real transport: A's
!accept line reaches the caravan command intake with A's character, A
gets the ordinary success ack plus the agent's whispered answer, and B
hears nothing. A silent claim (the rate limit) is only the ack; an
answer with no agent behind it is a notice. With no intake wired the
same text is ordinary chat.

===========================================================================
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
TestChatCaravanCommandIsConsumedAndAnswered
================
*/
func TestChatCaravanCommandIsConsumedAndAnswered(t *testing.T) {
	const division = "global-official"
	const whisper = "Walk with me to Donwhang, IronGuard."
	server := startChatServer(t, filepath.Join(t.TempDir(), "authority"), division)
	server.chat.CaravanCommands = func(_ string, sender *enterworld.Character, message string) (string, string, bool) {
		if sender == nil || sender.Name != e2eChatNameA {
			return "", "", false
		}
		switch message {
		case "!accept [AI]Trader_Jin":
			return "[AI]Trader_Jin", whisper, true
		case "!memory [AI]Trader_Jin":
			return "", "", true
		case "!accept Nobody":
			return "", "No AI Trader named Nobody is recruiting here.", true
		}
		return "", "", false
	}

	a := dialWS(t, server.srv)
	helloWS(t, a)
	enterChatWorld(t, a, division, e2eChatNameA)
	b := dialWS(t, server.srv)
	helloWS(t, b)
	enterChatWorld(t, b, division, e2eChatNameB)

	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeAll, "", "!accept [AI]Trader_Jin"))
	expectExact(t, a, chat.OpChatAck, []byte{1, 1, 255}, "command receipt")
	expectExact(t, a, chat.OpChatBroadcast, chat.EncodeChatBroadcastNamed(chat.ChatTypeWhisper, "[AI]Trader_Jin", whisper), "the agent answers")
	gameReadyBarrier(t, b, "the room hears nothing")

	// A silent claim is the ack and nothing else.
	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeAll, "", "!memory [AI]Trader_Jin"))
	expectExact(t, a, chat.OpChatAck, []byte{1, 1, 255}, "silent receipt")
	gameReadyBarrier(t, b, "the room still hears nothing")

	// An answer with no agent behind it arrives as a notice.
	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeAll, "", "!accept Nobody"))
	expectExact(t, a, chat.OpChatAck, []byte{1, 1, 255}, "notice receipt")
	expectExact(t, a, chat.OpChatBroadcast, chat.EncodeChatNotice("No AI Trader named Nobody is recruiting here."), "the notice answer")
	gameReadyBarrier(t, b, "the room hears no notice")

	// Unwired, the same text is ordinary chat the room hears.
	server.chat.CaravanCommands = nil
	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeAll, "", "!accept [AI]Trader_Jin"))
	expectExact(t, a, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "!accept [AI]Trader_Jin"), "unwired line is chat")
	expectExact(t, a, chat.OpChatAck, []byte{1, 1, 255}, "ordinary receipt")
	expectExact(t, b, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "!accept [AI]Trader_Jin"), "the room hears the unwired line")
}
