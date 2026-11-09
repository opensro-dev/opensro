/*
===========================================================================

itemlinks_test.go - the chat item-link wire helpers and their trimming

Port-only, not native (SRO_CHAT_ITEM_LINKS). The byte vectors here are the
ones the client's chat-item-links tests pin, so both sides read one layout.

===========================================================================
*/
package chat

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/chatfeed"
	"opensro.online/server/internal/game/social/chatlog"
	"opensro.online/server/internal/game/social/party"
)

/*
================
TestLinkRefsRoundTrip
================
*/
func TestLinkRefsRoundTrip(t *testing.T) {
	refs := []LinkRef{{Source: LinkSourceStorage, Slot: 0x0102}}
	encoded := EncodeLinkRefs(refs)
	if want := []byte{0x01, 0x01, 0x02, 0x01}; !bytes.Equal(encoded, want) {
		t.Fatalf("EncodeLinkRefs = % X, want % X", encoded, want)
	}
	// A pet ref carries its pet's GID after the slot (pinned with the
	// client's chat-item-links tests): [02 41 00] then 0x12345678 LE.
	refs = []LinkRef{{Source: LinkSourceStorage, Slot: 0x0102}, {Source: LinkSourcePet, Slot: 0x0041, Pet: 0x12345678}}
	encoded = EncodeLinkRefs(refs)
	if want := []byte{0x02, 0x01, 0x02, 0x01, 0x02, 0x41, 0x00, 0x78, 0x56, 0x34, 0x12}; !bytes.Equal(encoded, want) {
		t.Fatalf("EncodeLinkRefs = % X, want % X", encoded, want)
	}
	reader := wire.NewReader(encoded)
	decoded, err := DecodeLinkRefs(reader)
	if err != nil {
		t.Fatalf("DecodeLinkRefs: %v", err)
	}
	if !reflect.DeepEqual(decoded, refs) {
		t.Fatalf("decoded %+v, want %+v", decoded, refs)
	}
	if reader.Done() != nil {
		t.Fatal("the reader is not done after the block")
	}
	// A pet ref without its GID bytes is a truncated entry.
	if _, err := DecodeLinkRefs(wire.NewReader([]byte{0x01, LinkSourcePet, 0x41, 0x00, 0x78})); err == nil {
		t.Fatal("a pet ref without its full GID decoded")
	}
}

/*
================
TestDecodeLinkRefsRefusesOverWireMax
================
*/
func TestDecodeLinkRefsRefusesOverWireMax(t *testing.T) {
	over := []byte{MaxLinksOnWire + 1}
	for i := 0; i < MaxLinksOnWire+1; i++ {
		over = append(over, LinkSourceInventory, 0x0D, 0x00)
	}
	if _, err := DecodeLinkRefs(wire.NewReader(over)); err == nil {
		t.Fatal("a block above MaxLinksOnWire decoded")
	}
	if _, err := DecodeLinkRefs(wire.NewReader([]byte{0x01, 0x00, 0x0D})); err == nil {
		t.Fatal("a truncated entry decoded")
	}
}

/*
================
TestAppendLinkTailVector
================
*/
func TestAppendLinkTailVector(t *testing.T) {
	got := AppendLinkTail([]byte{0xAA}, []LinkedItem{{Body: []byte{1, 2, 3}}})
	if want := []byte{0xAA, 0x01, 0x03, 0x00, 1, 2, 3}; !bytes.Equal(got, want) {
		t.Fatalf("AppendLinkTail = % X, want % X", got, want)
	}
	plain := []byte{0xAA, 0xBB}
	if got := AppendLinkTail(plain, nil); !bytes.Equal(got, plain) || len(got) != len(plain) {
		t.Fatalf("AppendLinkTail without items = % X, want % X", got, plain)
	}
}

/*
================
TestLabelText
================
*/
func TestLabelText(t *testing.T) {
	got := LabelText("look \uFFFC ok", []LinkedItem{{Label: "<Blade +7>"}})
	if want := "look <Blade +7> ok"; got != want {
		t.Fatalf("LabelText = %q, want %q", got, want)
	}
}

//============================================================================

// Two readable items: a +7 blade in bag slot 13, a potion in storage slot 2.
var (
	refBlade  = LinkRef{Source: LinkSourceInventory, Slot: 13}
	refPotion = LinkRef{Source: LinkSourceStorage, Slot: 2}
	itemBlade = LinkedItem{Body: []byte{0x0B, 0x0B}, Label: "<Blade +7>"}
	itemPot   = LinkedItem{Body: []byte{0x0C}, Label: "<Potion>"}
)

// stubLinkSource answers the refs it holds.
type stubLinkSource map[LinkRef]LinkedItem

func (s stubLinkSource) LinkedItem(_ string, _ *enterworld.Character, ref LinkRef) (LinkedItem, bool) {
	if ref.Source == LinkSourceStorage {
		return LinkedItem{}, false
	}
	item, ok := s[ref]
	return item, ok
}

func (s stubLinkSource) StorageLinks(_ string, _ *enterworld.Character, refs []LinkRef) map[LinkRef]LinkedItem {
	read := map[LinkRef]LinkedItem{}
	for _, ref := range refs {
		if item, ok := s[ref]; ok && ref.Source == LinkSourceStorage {
			read[ref] = item
		}
	}
	return read
}

// resolve runs ResolveLinks the way HandleChat does: warehouse refs first.
func resolve(links *ItemLinks, message string, refs []LinkRef) (string, []LinkedItem) {
	sender := &enterworld.Character{Name: "Alfa"}
	storage := links.Source.StorageLinks(testDivision, sender, refs)
	return ResolveLinks(links, testDivision, sender, storage, message, refs)
}

func testLinks(limit int) *ItemLinks {
	return &ItemLinks{Source: stubLinkSource{refBlade: itemBlade, refPotion: itemPot}, Limit: limit}
}

// linkFrame appends a request link block to a chat frame.
func linkFrame(frame []byte, refs ...LinkRef) []byte {
	return append(append([]byte(nil), frame...), EncodeLinkRefs(refs)...)
}

/*
================
TestResolveLinksKeepsOne
================
*/
func TestResolveLinksKeepsOne(t *testing.T) {
	text, items := resolve(testLinks(1), "\uFFFC and \uFFFC", []LinkRef{refBlade, refPotion})
	if text != "\uFFFC and " || !reflect.DeepEqual(items, []LinkedItem{itemBlade}) {
		t.Fatalf("ResolveLinks = %q %+v", text, items)
	}
}

/*
================
TestResolveLinksDropsUnreadable
================
*/
func TestResolveLinksDropsUnreadable(t *testing.T) {
	missing := LinkRef{Source: LinkSourcePet, Slot: 4}
	text, items := resolve(testLinks(1), "x \uFFFC", []LinkRef{missing})
	if text != "x " || len(items) != 0 {
		t.Fatalf("ResolveLinks = %q %+v", text, items)
	}
}

/*
================
TestResolveLinksDropsStrayMarkers
================
*/
func TestResolveLinksDropsStrayMarkers(t *testing.T) {
	text, items := resolve(testLinks(1), "a\uFFFCb", nil)
	if text != "ab" || len(items) != 0 {
		t.Fatalf("ResolveLinks = %q %+v", text, items)
	}
}

/*
================
TestHandleChatLinkSwitchOffIsByteExact

The switch off is native: an unlinked line encodes as ever and a frame
with a link block is refused like any trailing frame, without an ack.
================
*/
func TestHandleChatLinkSwitchOffIsByteExact(t *testing.T) {
	sender := &enterworld.Character{ID: 7, Name: "Alfa"}
	views := Views{Presence: stubPresence{}, Parties: stubParties{}}
	outcome := HandleChat(testDeps(sender), views, testDivision, sender, chatFrame(ChatTypeAll, 0xFF, "", "hi"))
	want := append([]byte{ChatTypeGlobal, 4, 0, 'A', 'l', 'f', 'a'}, utf16LE("hi")...)
	if !bytes.Equal(outcome.Broadcast, want) {
		t.Fatalf("0x3667 = % X, want % X", outcome.Broadcast, want)
	}
	refused := HandleChat(testDeps(sender), views, testDivision, sender, linkFrame(chatFrame(ChatTypeAll, 0xFF, "", "hi"), refBlade))
	if refused.Refusal == "" || refused.Ack != nil || refused.Broadcast != nil {
		t.Fatalf("a link block with the switch off = %+v, want a silent refusal", refused)
	}
}

/*
================
TestHandleChatAllCarriesLinkTail
================
*/
func TestHandleChatAllCarriesLinkTail(t *testing.T) {
	sender := &enterworld.Character{ID: 7, Name: "Alfa"}
	views := Views{Presence: stubPresence{}, Parties: stubParties{}, Links: testLinks(1)}
	outcome := HandleChat(testDeps(sender), views, testDivision, sender, linkFrame(chatFrame(ChatTypeAll, 0xFF, "", "look \uFFFC"), refBlade))
	if !bytes.Equal(outcome.Ack, []byte{0x01, 0x01, 0xFF}) {
		t.Fatalf("ack = % X", outcome.Ack)
	}
	want := append(append([]byte{ChatTypeGlobal, 4, 0, 'A', 'l', 'f', 'a'}, utf16LE("look \uFFFC")...), 0x01, 0x02, 0x00, 0x0B, 0x0B)
	if !bytes.Equal(outcome.Broadcast, want) {
		t.Fatalf("0x3667 = % X, want % X", outcome.Broadcast, want)
	}
	if outcome.PlainText != "look <Blade +7>" {
		t.Fatalf("PlainText = %q", outcome.PlainText)
	}
}

/*
================
TestHandleChatWhisperPartyGuildUnionCarryLinkTail

Each member channel keeps its own audience and every delivery carries
the tail.
================
*/
func TestHandleChatWhisperPartyGuildUnionCarryLinkTail(t *testing.T) {
	sender := &enterworld.Character{ID: 7, Name: "Alfa"}
	berk := &enterworld.Character{ID: 8, Name: "Berk"}
	deps := testDeps(sender, berk)
	deps.Guilds = stubGuilds{guildID: 3, members: []enterworld.GuildMemberRecord{{CharID: 7, Name: "Alfa"}, {CharID: 8, Name: "Berk"}}}
	parties := stubParties{"alfa": {Members: []party.Member{{MemberID: 1, Name: "Alfa"}, {MemberID: 2, Name: "Berk"}}}}
	views := Views{Presence: stubPresence{"berk": true}, Parties: parties, Unions: stubUnions{names: []string{"Cale"}}, Links: testLinks(1)}
	tail := []byte{0x01, 0x02, 0x00, 0x0B, 0x0B}
	for _, c := range []struct {
		chatType uint8
		target   string
		to       string
	}{{ChatTypeWhisper, "Berk", "Berk"}, {ChatTypeParty, "", "Berk"}, {ChatTypeGuild, "", "Berk"}, {ChatTypeUnion, "", "Cale"}} {
		outcome := HandleChat(deps, views, testDivision, sender, linkFrame(chatFrame(c.chatType, 0xFF, c.target, "\uFFFC"), refBlade))
		if len(outcome.Deliveries) != 1 || outcome.Deliveries[0].TargetName != c.to {
			t.Fatalf("type 0x%02X deliveries %+v, want one to %s", c.chatType, outcome.Deliveries, c.to)
		}
		want := append(append([]byte{c.chatType, 4, 0, 'A', 'l', 'f', 'a'}, utf16LE("\uFFFC")...), tail...)
		if !bytes.Equal(outcome.Deliveries[0].Payload, want) {
			t.Fatalf("type 0x%02X 0x3667 = % X, want % X", c.chatType, outcome.Deliveries[0].Payload, want)
		}
	}
}

/*
================
TestHandleChatStallIgnoresLinks
================
*/
func TestHandleChatStallIgnoresLinks(t *testing.T) {
	sender := &enterworld.Character{ID: 7, Name: "Alfa"}
	views := Views{Stalls: stubStalls{names: []string{"Bravo"}}, Links: testLinks(1)}
	outcome := HandleChat(testDeps(sender), views, testDivision, sender, linkFrame(chatFrame(ChatTypeStall, 0xFF, "", "hi\uFFFC"), refBlade))
	if !bytes.Equal(outcome.Ack, []byte{0x01, ChatTypeStall, 0xFF}) || len(outcome.Deliveries) != 1 {
		t.Fatalf("stall outcome %+v", outcome)
	}
	want := append([]byte{ChatTypeStall, 4, 0, 'A', 'l', 'f', 'a'}, utf16LE("hi")...)
	if !bytes.Equal(outcome.Deliveries[0].Payload, want) {
		t.Fatalf("stall 0x3667 = % X, want % X", outcome.Deliveries[0].Payload, want)
	}
}

/*
================
TestHandleChatOverLimitTrimmedAndAcked
================
*/
func TestHandleChatOverLimitTrimmedAndAcked(t *testing.T) {
	sender := &enterworld.Character{ID: 7, Name: "Alfa"}
	views := Views{Presence: stubPresence{}, Parties: stubParties{}, Links: testLinks(1)}
	outcome := HandleChat(testDeps(sender), views, testDivision, sender, linkFrame(chatFrame(ChatTypeAll, 0xFF, "", "\uFFFC\uFFFC"), refBlade, refPotion))
	if !bytes.Equal(outcome.Ack, []byte{0x01, 0x01, 0xFF}) {
		t.Fatalf("ack = % X, want success", outcome.Ack)
	}
	want := append(append([]byte{ChatTypeGlobal, 4, 0, 'A', 'l', 'f', 'a'}, utf16LE("\uFFFC")...), 0x01, 0x02, 0x00, 0x0B, 0x0B)
	if !bytes.Equal(outcome.Broadcast, want) {
		t.Fatalf("0x3667 = % X, want % X", outcome.Broadcast, want)
	}
}

/*
================
TestFeedAndLogCarryLinkLabels

The website feed and the moderation log store the label, never the
marker; the in-game history keeps the payload with its tail.
================
*/
func TestFeedAndLogCarryLinkLabels(t *testing.T) {
	rt := &Runtime{history: map[string][]publicLine{}, members: map[uint64]publicMember{}, now: time.Now}
	rt.Feed = chatfeed.New()
	payload := AppendLinkTail(EncodeChatBroadcastNamed(ChatTypeGlobal, "Kekw", "look \uFFFC"), []LinkedItem{itemBlade})
	rt.publish("shard", "Kekw", "look <Blade +7>", publicLine{payload: payload})
	lines := rt.Feed.Lines("shard", nil)
	if len(lines) != 1 || lines[0].Text != "look <Blade +7>" {
		t.Fatalf("feed %+v", lines)
	}
	if !bytes.Equal(rt.history["shard"][0].payload, payload) {
		t.Fatal("the history lost the link tail")
	}

	dir := t.TempDir()
	rt.ModerationLog = chatlog.Open(dir, 30)
	sender := &enterworld.Character{ID: 7, Name: "Alfa", AccountID: "acct"}
	frame := linkFrame(chatFrame(ChatTypeAll, 0xFF, "", "look \uFFFC"), refBlade)
	outcome := HandleChat(testDeps(sender), Views{Links: testLinks(1)}, testDivision, sender, frame)
	if outcome.Refusal != "" || outcome.Broadcast == nil {
		t.Fatalf("outcome %+v", outcome)
	}
	rt.recordModerationLine(outcome, false, sender, frame)
	rt.ModerationLog.Close()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("log dir %v %v", entries, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var logged struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(data), &logged); err != nil || logged.Text != "look <Blade +7>" {
		t.Fatalf("logged %s (%v)", data, err)
	}
}

/*
================
TestResolveLinksDropsTheUnreadableLinksOwnMarker

Link i belongs to marker i: when the first of two links cannot be read,
its own marker goes and the second item keeps its place.
================
*/
func TestResolveLinksDropsTheUnreadableLinksOwnMarker(t *testing.T) {
	missing := LinkRef{Source: LinkSourcePet, Slot: 4}
	text, items := resolve(testLinks(2), "x \uFFFC and \uFFFC", []LinkRef{missing, refBlade})
	if text != "x  and \uFFFC" || !reflect.DeepEqual(items, []LinkedItem{itemBlade}) {
		t.Fatalf("ResolveLinks = %q %+v", text, items)
	}
}
