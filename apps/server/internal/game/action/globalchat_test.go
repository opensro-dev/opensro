/*
===========================================================================

globalchat_test.go - the Global Chatting item sends one line to the shard

===========================================================================
*/

package action

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/social/chatfeed"
	"opensro.online/server/internal/game/social/chatlog"
)

/*
================
globalChatFixture

A character holding three Global Chatting items (ref 3851, TID 3/3/3/5)
in slot 21, and the request that sends message with them.
================
*/
func globalChatFixture(t *testing.T) (*Runtime, *enterworld.Character, *enterworld.ItemRef) {
	t.Helper()
	c := testCharacter()
	items := testItems()
	ref := &enterworld.ItemRef{
		RefObjID: 3851, Codename: "ITEM_MALL_GLOBAL_CHATTING", TypeIDs: [4]int64{3, 3, 3, 5},
		ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 50, "canUse": 1}),
	}
	items[ref.Codename] = ref
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: 21, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 3,
	})
	rt, _ := newTestRuntime(c, items)
	return rt, c, ref
}

/*
================
globalChatRequest
================
*/
func globalChatRequest(ref *enterworld.ItemRef, message string) []byte {
	units := utf16.Encode([]rune(message))
	w := wire.NewWriter(5 + len(units)*2).U8(21).U16(ref.TypeFlags()).U16(uint16(len(units)))
	for _, u := range units {
		w.U16(u)
	}
	return w.Payload()
}

/*
================
TestGlobalChattingSendsTheLineToTheWholeShard

The native item use case 4: the item is spent, the author sees its own success, the item
visual, then the type 6 line, and every other player in the shard receives the same line.
================
*/
func TestGlobalChattingSendsTheLineToTheWholeShard(t *testing.T) {
	rt, c, ref := globalChatFixture(t)
	var shard []wire.Frame
	var except string
	rt.PushShardPeerFrames = func(_, name string, frames []wire.Frame) { except, shard = name, frames }
	rt.PublicChat = chatfeed.New()
	result := rt.HandleItemUse(testDivision, c, globalChatRequest(ref, "Hello, Silk Road"))
	// The success and the item visual, then the shard's relayed line.
	assertOpcodes(t, result.Frames, wire.OpItemUseResponse, opCommerceItemReferences, wire.OpItemUseVisual, chat.OpChatBroadcast)
	want := chat.EncodeChatBroadcastNamed(chat.ChatTypeGlobal, c.Name, "Hello, Silk Road")
	if !bytes.Equal(result.Frames[3].Payload, want) {
		t.Fatalf("author line %x, want %x", result.Frames[3].Payload, want)
	}
	if except != c.Name || len(shard) != 1 || shard[0].Opcode != chat.OpChatBroadcast || !bytes.Equal(shard[0].Payload, want) {
		t.Fatalf("shard push to all but %q: %+v", except, shard)
	}
	if c.MissionInventory[len(c.MissionInventory)-1].StackCount != 2 {
		t.Fatal("the item was not spent")
	}
	// The website feed carries the paid line as its own kind (port-only).
	if lines := rt.PublicChat.Lines(testDivision, nil); len(lines) != 1 || lines[0].Kind != chatfeed.KindGlobal ||
		lines[0].Character != c.Name || lines[0].Author != c.Name || lines[0].Text != "Hello, Silk Road" {
		t.Fatalf("feed %+v", lines)
	}
	rt.PlayerBannedGM(testDivision, c.Name)
	if lines := rt.PublicChat.Lines(testDivision, nil); len(lines) != 0 {
		t.Fatalf("feed after ban %+v", lines)
	}
}

/*
================
TestGlobalChattingSuitedSenderUsesJobAlias

A suited author's Global line, its shard copies and the website
transcript all carry the job alias, as on every chat line; the shard
push still excludes the author by character name, and a /BAN purges the
retained line by that same character identity.
================
*/
func TestGlobalChattingSuitedSenderUsesJobAlias(t *testing.T) {
	rt, c, ref := globalChatFixture(t)
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: enterworld.JobSuitSlot, TypeFlags: 0x3ac | 1<<11,
	})
	c.Job.Alias = "Merchant"
	var shard []wire.Frame
	var except string
	rt.PushShardPeerFrames = func(_, name string, frames []wire.Frame) { except, shard = name, frames }
	rt.PublicChat = chatfeed.New()
	result := rt.HandleItemUse(testDivision, c, globalChatRequest(ref, "Hello, Silk Road"))
	assertOpcodes(t, result.Frames, wire.OpItemUseResponse, opCommerceItemReferences, wire.OpItemUseVisual, chat.OpChatBroadcast)
	want := chat.EncodeChatBroadcastNamed(chat.ChatTypeGlobal, "Merchant", "Hello, Silk Road")
	if !bytes.Equal(result.Frames[3].Payload, want) {
		t.Fatalf("author line %x, want %x", result.Frames[3].Payload, want)
	}
	if except != c.Name || len(shard) != 1 || !bytes.Equal(shard[0].Payload, want) {
		t.Fatalf("shard push to all but %q: %+v", except, shard)
	}
	if lines := rt.PublicChat.Lines(testDivision, nil); len(lines) != 1 || lines[0].Character != "Merchant" ||
		lines[0].Author != c.Name {
		t.Fatalf("feed %+v", lines)
	}
	rt.PlayerBannedGM(testDivision, c.Name)
	if lines := rt.PublicChat.Lines(testDivision, nil); len(lines) != 0 {
		t.Fatalf("feed after ban %+v", lines)
	}
}

/*
================
moderationLine

One moderation-log line as written on disk (the chatlog package's JSON
contract), read back by the Global Chatting tests.
================
*/
type moderationLine struct {
	TS        string `json:"ts"`
	Account   string `json:"account"`
	Character string `json:"character"`
	Channel   string `json:"channel"`
	Muted     bool   `json:"muted"`
	Text      string `json:"text"`
}

/*
================
moderationLines

Every line the moderation log under dir holds (no file yet reads as none).
================
*/
func moderationLines(t *testing.T, dir string) []moderationLine {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []moderationLine
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, raw := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if raw == "" {
				continue
			}
			var line moderationLine
			if err := json.Unmarshal([]byte(raw), &line); err != nil {
				t.Fatalf("moderation line %q is not the log's JSON: %v", raw, err)
			}
			out = append(out, line)
		}
	}
	return out
}

/*
================
TestGlobalChattingRecordsTheLineOnlyOnceTheUseCommits

The moderation record is a publication like the shard push and the
website line beside it: it belongs to the use's post-commit step, so a
door that rolls the use back leaves no line for a message nobody saw
and an item nobody spent. A shadow-muted author's line is still kept,
marked, once the use commits, and is still pushed nowhere.
================
*/
func TestGlobalChattingRecordsTheLineOnlyOnceTheUseCommits(t *testing.T) {
	for _, trial := range []struct {
		muted  bool
		commit bool
	}{
		{muted: false, commit: false},
		{muted: false, commit: true},
		{muted: true, commit: false},
		{muted: true, commit: true},
	} {
		rt, c, ref := globalChatFixture(t)
		rt.PublicChat = chatfeed.New()
		dir := t.TempDir()
		rt.ModerationLog = chatlog.Open(dir, 30)
		pushed := false
		rt.PushShardPeerFrames = func(string, string, []wire.Frame) { pushed = true }
		if trial.muted {
			rt.ChatShadowMuted = func(string) bool { return true }
		}
		row := -1
		for i, r := range c.MissionInventory {
			if r.Slot == 21 {
				row = i
			}
		}
		if row < 0 {
			t.Fatal("the fixture lost its Global Chatting row")
		}
		units := utf16.Encode([]rune("Hello, Silk Road"))
		tail := wire.NewWriter(2 + len(units)*2).U16(uint16(len(units)))
		for _, u := range units {
			tail.U16(u)
		}
		var result OpResult
		var after func()
		use := globalChatUse{division: testDivision, character: c, row: row,
			request: wire.ItemUseRequest{Slot: 21, TypeWord: ref.TypeFlags()}}
		if !rt.useGlobalChatting(use, tail.Payload(), &result, &after) {
			t.Fatalf("muted %v: the use was refused", trial.muted)
		}
		if trial.commit && after != nil {
			after()
		}
		rt.ModerationLog.Close()
		lines := moderationLines(t, dir)
		want := 0
		if trial.commit {
			want = 1
		}
		if len(lines) != want {
			t.Fatalf("muted %v commit %v: %d moderation lines, want %d: %+v",
				trial.muted, trial.commit, len(lines), want, lines)
		}
		if want == 1 {
			line := lines[0]
			if line.Channel != "global" || line.Character != c.Name || line.Text != "Hello, Silk Road" ||
				line.Muted != trial.muted {
				t.Fatalf("muted %v: recorded line %+v", trial.muted, line)
			}
		}
		if pushed != (trial.commit && !trial.muted) {
			t.Fatalf("muted %v commit %v: shard pushed %v", trial.muted, trial.commit, pushed)
		}
	}
}

/*
================
TestGlobalChattingRefusesARestrictedOrMalformedLine

A chat-restricted player is answered 2 and keeps the item; an empty, an
over-long or a trailing tail is refused without spending it.
================
*/
func TestGlobalChattingRefusesARestrictedOrMalformedLine(t *testing.T) {
	rt, c, ref := globalChatFixture(t)
	pushed := false
	rt.PushShardPeerFrames = func(string, string, []wire.Frame) { pushed = true }
	rt.PublicChat = chatfeed.New()
	rt.ChatRestricted = func(string, string) bool { return true }
	result := rt.HandleItemUse(testDivision, c, globalChatRequest(ref, "muted"))
	if len(result.Frames) != 1 || !bytes.Equal(result.Frames[0].Payload, wire.EncodeItemUseError(2)) {
		t.Fatalf("restricted answer %+v, want B5BD {2, 2}", result.Frames)
	}
	rt.ChatRestricted = nil
	long := make([]rune, chat.ChatMessageMaxWideChars+1)
	for i := range long {
		long[i] = 'a'
	}
	for _, payload := range [][]byte{
		globalChatRequest(ref, ""),
		globalChatRequest(ref, string(long)),
		append(globalChatRequest(ref, "tail"), 0),
	} {
		rt.HandleItemUse(testDivision, c, payload)
	}
	if pushed || c.MissionInventory[len(c.MissionInventory)-1].StackCount != 3 || len(rt.PublicChat.Lines(testDivision, nil)) != 0 {
		t.Fatal("a refused line was sent, spent the item or reached the website feed")
	}
}
