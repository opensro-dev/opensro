/*
===========================================================================

shadowmute_wire_test.go - a shadow-muted author's chat reaches only itself

Port-only, not native (shadowmute). Over the wire: the muted author keeps
its own echo and acknowledgement, a peer receives neither the public line
nor a whisper, and the line never enters the history a later player is
replayed. Lifting the mute restores ordinary delivery.

===========================================================================
*/
package chat_test

import (
	"encoding/binary"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/game/social/chat"
)

/*
================
TestShadowMutedChatReachesOnlyItsAuthor
================
*/
func TestShadowMutedChatReachesOnlyItsAuthor(t *testing.T) {
	const division = "global-official"
	server := startChatServer(t, filepath.Join(t.TempDir(), "authority"), division)
	at := time.UnixMilli(1_790_000_000_000)
	server.chat.SetClock(func() time.Time { return at })
	muted := true
	server.chat.ShadowMuted = func(accountID string) bool { return muted && accountID == "test-account" }

	a := dialWS(t, server.srv)
	helloWS(t, a)
	enterChatWorld(t, a, division, e2eChatNameA)
	b := dialWS(t, server.srv)
	helloWS(t, b)
	enterChatWorld(t, b, division, e2eChatNameB)
	gameReadyBarrier(t, b, "empty history")

	// Muted: the author sees its line and receipt exactly as ever...
	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeAll, "", "hidden"))
	expectExact(t, a, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "hidden"), "own muted line")
	expectExact(t, a, chat.OpChatAck, []byte{1, 1, 255}, "muted receipt")
	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeWhisper, e2eChatNameB, "psst"))
	expectExact(t, a, chat.OpChatAck, []byte{1, 2, 255}, "muted whisper receipt")
	// ...and the peer receives neither.
	gameReadyBarrier(t, b, "no muted public line or whisper")

	muted = false
	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeAll, "", "visible"))
	expectExact(t, a, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "visible"), "own line")
	expectExact(t, a, chat.OpChatAck, []byte{1, 1, 255}, "receipt")
	expectExact(t, b, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "visible"), "unmuted line")

	// A later player is replayed only the unmuted line.
	c := dialWS(t, server.srv)
	helloWS(t, c)
	enterChatWorld(t, c, division, e2eChatNameC)
	line := namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "visible")
	history := binary.LittleEndian.AppendUint64([]byte{2, 1}, uint64(at.UnixMilli()))
	history = append(history, byte(len(line)), byte(len(line)>>8))
	history = append(history, line...)
	expectExact(t, c, chat.OpChatHistory, history, "history without the muted line")
}
