package chat

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf16"
)

/*
================
TestAgentChatFitsTheClientLimit

An AI agent's line longer than the client's 100-unit chat limit is cut to
fit because the client drops the session over a longer one. A short line
is kept whole and a surrogate pair is never split.
================
*/
func TestAgentChatFitsTheClientLimit(t *testing.T) {
	long := "Hold on [AI]Hunter_Wei! [AI]Hunter_Ming is rushing to region 23187 to help defend your caravan against AIThief_Kael!"
	cut := ClampChatUnits(long, MaxChatNoticeUnits)
	if n := len(utf16.Encode([]rune(cut))); n > MaxChatNoticeUnits || !strings.HasSuffix(cut, "...") {
		t.Fatalf("cut line is %d units: %q", n, cut)
	}
	if ClampChatUnits("short", MaxChatNoticeUnits) != "short" {
		t.Fatal("a short line was changed")
	}
	emoji := strings.Repeat("\U0001F600", 60) // 120 units, pairs only
	cut = ClampChatUnits(emoji, MaxChatNoticeUnits)
	if strings.ContainsRune(cut, '�') || len(utf16.Encode([]rune(cut))) > MaxChatNoticeUnits {
		t.Fatalf("a surrogate pair was split: %q", cut)
	}

	if got, want := AgentChatPayload(99, long), EncodeChatBroadcastGid(ChatTypeAll, 99, ClampChatUnits(long, MaxChatNoticeUnits)); !bytes.Equal(got, want) {
		t.Fatal("agent line did not use normal, GID-scoped chat")
	}
}
