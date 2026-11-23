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
	"os"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/social/shadowmute"
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
	// The staff word list is the real one, so the hook is handed the
	// line's own text; the account mute is toggled by the test.
	words := t.TempDir()
	if err := os.WriteFile(filepath.Join(words, shadowmute.WordsFileName), []byte("rival server\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	list := shadowmute.Open(words)
	muted := true
	server.chat.ShadowMuted = func(accountID, text string) bool {
		return (muted && accountID == "test-account") || list.Muted(accountID, text)
	}

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

	// A listed word mutes the line whoever sends it, however it is spelled.
	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeAll, "", "join Rival.Server now"))
	expectExact(t, a, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "join Rival.Server now"), "own word-muted line")
	expectExact(t, a, chat.OpChatAck, []byte{1, 1, 255}, "word-muted receipt")
	gameReadyBarrier(t, b, "no word-muted line")

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
