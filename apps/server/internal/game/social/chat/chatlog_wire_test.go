/*
===========================================================================

chatlog_wire_test.go - accepted lines land in the staff moderation log

Port-only, not native (chatlog). Over the wire: a shadow-muted author's
public line and whisper are logged and marked muted, an unmuted line is
logged unmarked, and a refused line (a whisper to nobody) is not logged
at all. The log carries the UTC time, account, character, channel,
target and text of each line.

===========================================================================
*/
package chat_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/social/chatlog"
)

// loggedLine mirrors chatlog's on-disk JSON shape.
type loggedLine struct {
	TS        string `json:"ts"`
	Account   string `json:"account"`
	Character string `json:"character"`
	Channel   string `json:"channel"`
	Target    string `json:"target,omitempty"`
	Muted     bool   `json:"muted"`
	Text      string `json:"text"`
}

/*
================
TestModerationLogKeepsAcceptedLinesMutedMarked
================
*/
func TestModerationLogKeepsAcceptedLinesMutedMarked(t *testing.T) {
	const division = "global-official"
	server := startChatServer(t, filepath.Join(t.TempDir(), "authority"), division)
	at := time.UnixMilli(1_790_000_000_000)
	server.chat.SetClock(func() time.Time { return at })
	muted := true
	server.chat.ShadowMuted = func(accountID string) bool { return muted && accountID == "test-account" }
	dir := filepath.Join(t.TempDir(), "modlog")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	modLog := chatlog.Open(dir, 30)
	defer modLog.Close()
	server.chat.ModerationLog = modLog

	a := dialWS(t, server.srv)
	helloWS(t, a)
	enterChatWorld(t, a, division, e2eChatNameA)
	b := dialWS(t, server.srv)
	helloWS(t, b)
	enterChatWorld(t, b, division, e2eChatNameB)
	gameReadyBarrier(t, b, "empty history")

	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeAll, "", "hidden"))
	expectExact(t, a, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "hidden"), "muted author echo")
	expectExact(t, a, chat.OpChatAck, []byte{1, 1, 255}, "muted receipt")
	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeWhisper, e2eChatNameB, "psst"))
	expectExact(t, a, chat.OpChatAck, []byte{1, 2, 255}, "muted whisper receipt")

	muted = false
	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeAll, "", "visible"))
	expectExact(t, a, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "visible"), "own line")
	expectExact(t, a, chat.OpChatAck, []byte{1, 1, 255}, "receipt")
	expectExact(t, b, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "visible"), "unmuted line")

	// A whisper to a player who is not there is refused, not said:
	// nothing may be logged for it.
	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeWhisper, "chatNobody", "anyone?"))
	gameReadyBarrier(t, b, "refused whisper leaves no line")

	modLog.Close()
	data, err := os.ReadFile(filepath.Join(dir, "chat-"+at.UTC().Format("2006-01-02")+".log"))
	if err != nil {
		t.Fatal(err)
	}
	var lines []loggedLine
	for _, raw := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var entry loggedLine
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			t.Fatalf("logged line %q is not JSON: %v", raw, err)
		}
		lines = append(lines, entry)
	}
	want := []loggedLine{
		{TS: at.UTC().Format(time.RFC3339), Account: "test-account", Character: e2eChatNameA, Channel: "all", Muted: true, Text: "hidden"},
		{TS: at.UTC().Format(time.RFC3339), Account: "test-account", Character: e2eChatNameA, Channel: "whisper", Target: e2eChatNameB, Muted: true, Text: "psst"},
		{TS: at.UTC().Format(time.RFC3339), Account: "test-account", Character: e2eChatNameA, Channel: "all", Text: "visible"},
	}
	if len(lines) != len(want) {
		t.Fatalf("logged %d lines, want %d: %+v", len(lines), len(want), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %+v, want %+v", i, lines[i], want[i])
		}
	}
	if got := modLog.Dropped(); got != 0 {
		t.Fatalf("dropped %d lines on a healthy disk", got)
	}
}
