/*
===========================================================================

globalchat_test.go - the Global Chatting item sends one line to the shard

===========================================================================
*/

package action

import (
	"bytes"
	"testing"
	"unicode/utf16"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/social/chatfeed"
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
