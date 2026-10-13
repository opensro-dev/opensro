/*
===========================================================================

feed_test.go - published public lines reach the website feed

===========================================================================
*/
package chat

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/social/chatfeed"
)

/*
================
TestPublishRecordsPublicLinesOnly

Only the named Global broadcast enters the in-game history, and only it
reaches the website feed; a private payload never does.
================
*/
func TestPublishRecordsPublicLinesOnly(t *testing.T) {
	rt := &Runtime{history: map[string][]publicLine{}, members: map[uint64]publicMember{}, now: time.Now}
	rt.Feed = chatfeed.New()
	rt.publish("shard", EncodeChatBroadcastNamed(ChatTypeGlobal, "Kekw", "hello ☆ world"))
	rt.publish("shard", EncodeChatBroadcastNamed(ChatTypeParty, "Kekw", "party secret"))
	rt.publish("shard", EncodeChatBroadcastNamed(ChatTypeWhisper, "Kekw", "whisper secret"))
	lines := rt.Feed.Lines("shard", nil)
	if len(lines) != 1 || lines[0].Kind != chatfeed.KindChat || lines[0].Character != "Kekw" || lines[0].Text != "hello ☆ world" {
		t.Fatalf("feed %+v", lines)
	}
}

/*
================
TestDecodeChatBroadcastNamedRoundTrip
================
*/
func TestDecodeChatBroadcastNamedRoundTrip(t *testing.T) {
	payload := EncodeChatBroadcastNamed(ChatTypeGlobal, "Lune", "안녕 hi")
	chatType, name, text, ok := DecodeChatBroadcastNamed(payload)
	if !ok || chatType != ChatTypeGlobal || name != "Lune" || text != "안녕 hi" {
		t.Fatalf("decoded %d %q %q %v", chatType, name, text, ok)
	}
	for cut := 0; cut < len(payload); cut++ {
		if _, _, _, ok := DecodeChatBroadcastNamed(payload[:cut]); ok {
			t.Fatalf("a %d-byte prefix decoded", cut)
		}
	}
	if _, _, _, ok := DecodeChatBroadcastNamed(append(payload, 0)); ok {
		t.Fatal("a trailing byte decoded")
	}
}
