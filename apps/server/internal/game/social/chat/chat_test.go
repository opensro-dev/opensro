/*
===========================================================================

chat_test.go - native chat delivery, acknowledgement and privacy regressions.

===========================================================================
*/
package chat

// Transport-free pins for the 0x7367 decode and the HandleChat routing:
// every wire body asserted BYTE-EXACT against hand-rolled expectations
// (never against this package's own encoders), so the client agent's
// parser folds (sub_753290 / sub_753760) are pinned against the same
// contract.

import (
	"bytes"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/party"
)

const testDivision = "global-official"

// chatFrame hand-rolls a 0x7367 body: {u8 type, u8 second, [whisper:
// u16 len + ANSI target], u16 wcharCount + UTF-16LE text}. The text is
// ASCII/BMP only in these fixtures, so each rune is one code unit.
/*
================
chatFrame
================
*/
func chatFrame(chatType, second uint8, target, message string) []byte {
	frame := []byte{chatType, second}
	if chatType == ChatTypeWhisper {
		frame = append(frame, byte(len(target)), byte(len(target)>>8))
		frame = append(frame, []byte(target)...)
	}
	runes := []rune(message)
	frame = append(frame, byte(len(runes)), byte(len(runes)>>8))
	for _, r := range runes {
		frame = append(frame, byte(r), byte(uint16(r)>>8))
	}
	return frame
}

// utf16LE hand-rolls the sized-wide text tail {u16 count, count*2 bytes}.
/*
================
utf16LE
================
*/
func utf16LE(message string) []byte {
	runes := []rune(message)
	out := []byte{byte(len(runes)), byte(len(runes) >> 8)}
	for _, r := range runes {
		out = append(out, byte(r), byte(uint16(r)>>8))
	}
	return out
}

type stubPresence map[string]bool

/*
================
OnlineByName
================
*/
func (p stubPresence) OnlineByName(_, name string) bool {
	return p[strings.ToLower(name)]
}

type stubParties map[string]party.Snapshot

/*
================
PartyOf
================
*/
func (p stubParties) PartyOf(_, name string) (party.Snapshot, bool) {
	snapshot, ok := p[strings.ToLower(name)]
	return snapshot, ok
}

// stubGuilds is a one-guild enterworld.GuildStore.
/*
================
stubGuilds
================
*/
type stubGuilds struct {
	guildID int64
	members []enterworld.GuildMemberRecord
}

/*
================
Guild
================
*/
func (g stubGuilds) Guild(_ string, guildID int64) (enterworld.GuildRecord, []enterworld.GuildMemberRecord, bool) {
	if guildID != g.guildID {
		return enterworld.GuildRecord{}, nil, false
	}
	members := make([]enterworld.GuildMemberRecord, len(g.members))
	copy(members, g.members)
	return enterworld.GuildRecord{}, members, true
}

/*
================
GuildOfCharacter
================
*/
func (g stubGuilds) GuildOfCharacter(_ string, characterID int64) (int64, bool) {
	for _, member := range g.members {
		if member.CharID == characterID {
			return g.guildID, true
		}
	}
	return 0, false
}

/*
================
CreateGuild
================
*/
func (g stubGuilds) CreateGuild(string, enterworld.GuildRecord, enterworld.GuildMemberRecord, *enterworld.Character) (int64, error) {
	return 0, nil
}

/*
================
UpdateGuildAs
================
*/
func (g stubGuilds) UpdateGuildAs(string, int64, string, enterworld.GuildAuthorization, func(enterworld.GuildRecord, []enterworld.GuildMemberRecord) (enterworld.GuildRecord, []enterworld.GuildMemberRecord, bool)) (enterworld.GuildSnapshot, enterworld.GuildRefusal) {
	return enterworld.GuildSnapshot{}, enterworld.GuildRefusalUpdateRejected
}

/*
================
AddGuildMemberAs
================
*/
func (g stubGuilds) AddGuildMemberAs(string, int64, int64, uint32, enterworld.GuildMemberRecord) (enterworld.GuildSnapshot, enterworld.GuildRefusal) {
	return enterworld.GuildSnapshot{}, enterworld.GuildRefusalUpdateRejected
}

/*
================
KickGuildMember
================
*/
func (g stubGuilds) KickGuildMember(string, int64, string, uint32) (enterworld.GuildRemovalResult, enterworld.GuildRefusal) {
	return enterworld.GuildRemovalResult{}, enterworld.GuildRefusalUpdateRejected
}

/*
================
LeaveGuild
================
*/
func (g stubGuilds) LeaveGuild(string, int64) (enterworld.GuildRemovalResult, enterworld.GuildRefusal) {
	return enterworld.GuildRemovalResult{}, enterworld.GuildRefusalUpdateRejected
}

/*
================
DissolveGuildAs
================
*/
func (g stubGuilds) DissolveGuildAs(string, int64) (enterworld.GuildSnapshot, enterworld.GuildRefusal) {
	return enterworld.GuildSnapshot{}, enterworld.GuildRefusalUpdateRejected
}

/*
================
DonateGuildPoints
================
*/
func (g stubGuilds) DonateGuildPoints(string, int64, uint32) (enterworld.GuildDonationResult, enterworld.GuildRefusal) {
	return enterworld.GuildDonationResult{}, enterworld.GuildRefusalUpdateRejected
}

/*
================
testDeps
================
*/
func testDeps(characters ...*enterworld.Character) *enterworld.Deps {
	return &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: characters},
	}
}

// ---- 0x7367 decode ----

/*
================
TestDecodeChatRequestPerMode
================
*/
func TestDecodeChatRequestPerMode(t *testing.T) {
	// Every non-whisper client-composable mode carries NO name field.
	for _, chatType := range []uint8{ChatTypeAll, ChatTypeGM, ChatTypeParty, ChatTypeGuild, ChatTypeUnion} {
		request, err := DecodeChatRequest(chatFrame(chatType, 0xFF, "", "hi"))
		if err != nil {
			t.Fatalf("type 0x%02X: %v", chatType, err)
		}
		if request.ChatType != chatType || request.Second != 0xFF || request.TargetName != "" || request.Message != "hi" {
			t.Fatalf("type 0x%02X decoded %+v", chatType, request)
		}
	}
	// Whisper alone carries the sized ANSI target name.
	request, err := DecodeChatRequest(chatFrame(ChatTypeWhisper, 0xFF, "Berk", "hi"))
	if err != nil {
		t.Fatal(err)
	}
	if request.TargetName != "Berk" || request.Message != "hi" {
		t.Fatalf("whisper decoded %+v", request)
	}
}

/*
================
TestDecodeChatRequestUTF16
================
*/
func TestDecodeChatRequestUTF16(t *testing.T) {
	// U+6F22 U+5B57 - two BMP code units, LE on the wire.
	payload := []byte{ChatTypeAll, 0xFF, 0x02, 0x00, 0x22, 0x6F, 0x57, 0x5B}
	request, err := DecodeChatRequest(payload)
	if err != nil {
		t.Fatal(err)
	}
	if request.Message != "\u6f22\u5b57" {
		t.Fatalf("message = %q, want 漢字", request.Message)
	}
}

/*
================
TestDecodeChatRequestRefusals
================
*/
func TestDecodeChatRequestRefusals(t *testing.T) {
	good := chatFrame(ChatTypeWhisper, 0xFF, "Berk", "hi")
	cases := map[string][]byte{
		"empty":             {},
		"typeOnly":          {ChatTypeAll},
		"noCount":           {ChatTypeAll, 0xFF},
		"countOnly":         {ChatTypeAll, 0xFF, 0x02},
		"textShort":         {ChatTypeAll, 0xFF, 0x02, 0x00, 0x68, 0x00},
		"trailing":          append(chatFrame(ChatTypeAll, 0xFF, "", "hi"), 0x00),
		"whisperNoName":     {ChatTypeWhisper, 0xFF},
		"whisperNameShort":  {ChatTypeWhisper, 0xFF, 0x04, 0x00, 'B', 'e'},
		"whisperNameBound":  {ChatTypeWhisper, 0xFF, 0x80, 0x00},
		"overCap":           append([]byte{ChatTypeAll, 0xFF, 0x65, 0x00}, make([]byte, 0x65*2)...),
		"truncatedMidFrame": good[:len(good)-1],
	}
	for name, payload := range cases {
		if _, err := DecodeChatRequest(payload); err == nil {
			t.Fatalf("%s: decode accepted % X", name, payload)
		}
	}
	// The cap boundary itself (exactly 0x64 wchars) decodes.
	atCap := append([]byte{ChatTypeAll, 0xFF, 0x64, 0x00}, make([]byte, 0x64*2)...)
	if _, err := DecodeChatRequest(atCap); err != nil {
		t.Fatalf("0x64-wchar message refused: %v", err)
	}
}

/*
================
TestHandleChatMalformedFrameSilent
================
*/
func TestHandleChatMalformedFrameSilent(t *testing.T) {
	sender := &enterworld.Character{ID: 7, Name: "Alfa"}
	outcome := HandleChat(testDeps(sender), Views{Presence: stubPresence{}, Parties: stubParties{}}, testDivision, sender, []byte{0x01})
	if outcome.Ack != nil || outcome.Broadcast != nil || len(outcome.Deliveries) != 0 || outcome.Refusal == "" {
		t.Fatalf("malformed frame outcome %+v, want silent refusal", outcome)
	}
}

// ---- All / GM ----

/*
================
TestHandleChatAllBroadcastByteExact
================
*/
func TestHandleChatAllBroadcastByteExact(t *testing.T) {
	sender := &enterworld.Character{ID: 7, Name: "Alfa"}
	outcome := HandleChat(testDeps(sender), Views{Presence: stubPresence{}, Parties: stubParties{}}, testDivision, sender, chatFrame(ChatTypeAll, 0xFF, "", "hi"))
	if !bytes.Equal(outcome.Ack, []byte{0x01, 0x01, 0xFF}) {
		t.Fatalf("ack = % X, want 01 01 FF", outcome.Ack)
	}
	// The beta broadcast is native channel 6 with the authoritative name.
	want := append([]byte{ChatTypeGlobal, 4, 0, 'A', 'l', 'f', 'a'}, utf16LE("hi")...)
	if !bytes.Equal(outcome.Broadcast, want) {
		t.Fatalf("0x3667 = % X, want % X", outcome.Broadcast, want)
	}
	if len(outcome.Deliveries) != 0 {
		t.Fatalf("all-chat produced %d targeted deliveries", len(outcome.Deliveries))
	}
}

/*
================
TestHandleChatGMForcesTypeThree
================
*/
func TestHandleChatGMForcesTypeThree(t *testing.T) {
	sender := &enterworld.Character{ID: 7, Name: "Alfa", GMPrivilege: true}
	// A GM's no-prefix compose is type 3; the broadcast type byte is 3.
	outcome := handleAllChat(Request{ChatType: ChatTypeGM, Second: 0xFF, Message: "hi"}, sender, false)
	want := append([]byte{0x03, 0xA7, 0x86, 0x01, 0x00}, utf16LE("hi")...)
	if !bytes.Equal(outcome.Broadcast, want) {
		t.Fatalf("GM 0x3667 = % X, want % X", outcome.Broadcast, want)
	}
	// The ack echoes the REQUESTED type (the pending-record pop key).
	if !bytes.Equal(outcome.Ack, []byte{0x01, 0x03, 0xFF}) {
		t.Fatalf("GM ack = % X, want 01 03 FF", outcome.Ack)
	}
	// A privileged speaker requesting type 1 is forced to 3 as well.
	outcome = handleAllChat(Request{ChatType: ChatTypeAll, Second: 0xFF, Message: "hi"}, sender, false)
	if outcome.Broadcast[0] != 0x03 {
		t.Fatalf("GM type-1 request broadcast type = 0x%02X, want 0x03", outcome.Broadcast[0])
	}
	// A NON-GM requesting type 3 falls into the All arm as type 1.
	plain := &enterworld.Character{ID: 8, Name: "Bravo"}
	outcome = handleAllChat(Request{ChatType: ChatTypeGM, Second: 0xFF, Message: "hi"}, plain, false)
	if outcome.Broadcast[0] != 0x01 {
		t.Fatalf("non-GM type-3 request broadcast type = 0x%02X, want 0x01", outcome.Broadcast[0])
	}
	if !bytes.Equal(outcome.Ack, []byte{0x01, 0x03, 0xFF}) {
		t.Fatalf("non-GM type-3 ack = % X, want 01 03 FF", outcome.Ack)
	}
}

// ---- Whisper ----

/*
================
TestHandleChatWhisperDelivered
================
*/
func TestHandleChatWhisperDelivered(t *testing.T) {
	sender := &enterworld.Character{ID: 7, Name: "Alfa"}
	target := &enterworld.Character{ID: 8, Name: "Berk"}
	outcome := HandleChat(testDeps(sender, target), Views{Presence: stubPresence{"berk": true}, Parties: stubParties{}}, testDivision, sender, chatFrame(ChatTypeWhisper, 0xFF, "Berk", "yo"))
	if !bytes.Equal(outcome.Ack, []byte{0x01, 0x02, 0xFF}) {
		t.Fatalf("ack = % X, want 01 02 FF", outcome.Ack)
	}
	if len(outcome.Deliveries) != 1 || outcome.Deliveries[0].TargetName != "Berk" {
		t.Fatalf("deliveries = %+v, want one to Berk", outcome.Deliveries)
	}
	// {u8 2, u16 4 + "Alfa" ANSI, sized wide "yo"} - the SENDER's name.
	want := append([]byte{0x02, 0x04, 0x00, 'A', 'l', 'f', 'a'}, utf16LE("yo")...)
	if !bytes.Equal(outcome.Deliveries[0].Payload, want) {
		t.Fatalf("whisper 0x3667 = % X, want % X", outcome.Deliveries[0].Payload, want)
	}
	if outcome.Broadcast != nil {
		t.Fatal("whisper produced a cohort broadcast")
	}
}

/*
================
TestHandleChatWhisperBlockedAcksSuccessDeliversNothing
================
*/
func TestHandleChatWhisperBlockedAcksSuccessDeliversNothing(t *testing.T) {
	// Block "Berk" (stored casing), whisper arrives from "berk": the
	// EqualFold match - consistent with the register path's fold - must
	// suppress it, and the SENDER MUST SEE SUCCESS (the privacy rule:
	// blocked is indistinguishable from delivered; no GM bypass).
	sender := &enterworld.Character{ID: 7, Name: "berk"}
	target := &enterworld.Character{ID: 8, Name: "Cale", BlockedWhisperers: []string{"Berk"}}
	deps := testDeps(sender, target)
	outcome := HandleChat(deps, Views{Presence: stubPresence{"cale": true}, Parties: stubParties{}}, testDivision, sender, chatFrame(ChatTypeWhisper, 0xFF, "Cale", "yo"))
	if !bytes.Equal(outcome.Ack, []byte{0x01, 0x02, 0xFF}) {
		t.Fatalf("blocked-whisper ack = % X, want the SUCCESS 01 02 FF", outcome.Ack)
	}
	if len(outcome.Deliveries) != 0 || outcome.Broadcast != nil {
		t.Fatalf("blocked whisper delivered: %+v", outcome)
	}
	// The GM flag changes nothing (no bypass).
	sender.GMPrivilege = true
	outcome = HandleChat(deps, Views{Presence: stubPresence{"cale": true}, Parties: stubParties{}}, testDivision, sender, chatFrame(ChatTypeWhisper, 0xFF, "Cale", "yo"))
	if len(outcome.Deliveries) != 0 {
		t.Fatal("GM sender bypassed the whisper block")
	}
}

/*
================
TestHandleChatWhisperTargetMissing
================
*/
func TestHandleChatWhisperTargetMissing(t *testing.T) {
	sender := &enterworld.Character{ID: 7, Name: "Alfa"}
	offline := &enterworld.Character{ID: 8, Name: "Berk"}
	deps := testDeps(sender, offline)
	// Nonexistent name -> error 3, echoing {type, second}.
	outcome := HandleChat(deps, Views{Presence: stubPresence{}, Parties: stubParties{}}, testDivision, sender, chatFrame(ChatTypeWhisper, 0xFF, "Nobody", "yo"))
	if !bytes.Equal(outcome.Ack, []byte{0x02, 0x03, 0x02, 0xFF}) {
		t.Fatalf("nonexistent-target ack = % X, want 02 03 02 FF", outcome.Ack)
	}
	// Existing but OFFLINE -> the same error 3 (no cross-shard forward).
	outcome = HandleChat(deps, Views{Presence: stubPresence{}, Parties: stubParties{}}, testDivision, sender, chatFrame(ChatTypeWhisper, 0xFF, "Berk", "yo"))
	if !bytes.Equal(outcome.Ack, []byte{0x02, 0x03, 0x02, 0xFF}) {
		t.Fatalf("offline-target ack = % X, want 02 03 02 FF", outcome.Ack)
	}
	// Empty name -> error 3 too (the dump's empty-name arm).
	outcome = HandleChat(deps, Views{Presence: stubPresence{}, Parties: stubParties{}}, testDivision, sender, chatFrame(ChatTypeWhisper, 0xFF, "", "yo"))
	if !bytes.Equal(outcome.Ack, []byte{0x02, 0x03, 0x02, 0xFF}) {
		t.Fatalf("empty-target ack = % X, want 02 03 02 FF", outcome.Ack)
	}
}

/*
================
TestHandleChatWhisperSelf
================
*/
func TestHandleChatWhisperSelf(t *testing.T) {
	sender := &enterworld.Character{ID: 7, Name: "Alfa"}
	// Success, NO delivery - even with the sender online and the casing
	// differing (names are CI-unique).
	outcome := HandleChat(testDeps(sender), Views{Presence: stubPresence{"alfa": true}, Parties: stubParties{}}, testDivision, sender, chatFrame(ChatTypeWhisper, 0xFF, "ALFA", "yo"))
	if !bytes.Equal(outcome.Ack, []byte{0x01, 0x02, 0xFF}) {
		t.Fatalf("self-whisper ack = % X, want 01 02 FF", outcome.Ack)
	}
	if len(outcome.Deliveries) != 0 || outcome.Broadcast != nil {
		t.Fatalf("self-whisper delivered: %+v", outcome)
	}
}

// ---- Party / Guild / Union ----

/*
================
TestHandleChatPartyMembershipGate
================
*/
func TestHandleChatPartyMembershipGate(t *testing.T) {
	sender := &enterworld.Character{ID: 7, Name: "Alfa"}
	deps := testDeps(sender)
	// No party -> error 0x0A.
	outcome := HandleChat(deps, Views{Presence: stubPresence{}, Parties: stubParties{}}, testDivision, sender, chatFrame(ChatTypeParty, 0xFF, "", "hi"))
	if !bytes.Equal(outcome.Ack, []byte{0x02, 0x0A, 0x04, 0xFF}) {
		t.Fatalf("partyless ack = % X, want 02 0A 04 FF", outcome.Ack)
	}
	// In a party -> type-4 line to every member EXCEPT the sender.
	parties := stubParties{"alfa": {Members: []party.Member{
		{MemberID: 1, Name: "Alfa"},
		{MemberID: 2, Name: "Berk"},
		{MemberID: 3, Name: "Cale"},
	}}}
	outcome = HandleChat(deps, Views{Presence: stubPresence{}, Parties: parties}, testDivision, sender, chatFrame(ChatTypeParty, 0xFF, "", "hi"))
	if !bytes.Equal(outcome.Ack, []byte{0x01, 0x04, 0xFF}) {
		t.Fatalf("party ack = % X, want 01 04 FF", outcome.Ack)
	}
	if len(outcome.Deliveries) != 2 || outcome.Deliveries[0].TargetName != "Berk" || outcome.Deliveries[1].TargetName != "Cale" {
		t.Fatalf("party deliveries = %+v, want Berk + Cale", outcome.Deliveries)
	}
	want := append([]byte{0x04, 0x04, 0x00, 'A', 'l', 'f', 'a'}, utf16LE("hi")...)
	if !bytes.Equal(outcome.Deliveries[0].Payload, want) {
		t.Fatalf("party 0x3667 = % X, want % X", outcome.Deliveries[0].Payload, want)
	}
}

/*
================
TestHandleChatGuildAndUnionGates
================
*/
func TestHandleChatGuildAndUnionGates(t *testing.T) {
	sender := &enterworld.Character{ID: 7, Name: "Alfa"}
	deps := testDeps(sender)
	// No guild door at all -> 0x0B for guild chat, 0x0B for union chat.
	outcome := HandleChat(deps, Views{Presence: stubPresence{}, Parties: stubParties{}}, testDivision, sender, chatFrame(ChatTypeGuild, 0xFF, "", "hi"))
	if !bytes.Equal(outcome.Ack, []byte{0x02, 0x0B, 0x05, 0xFF}) {
		t.Fatalf("guildless ack = % X, want 02 0B 05 FF", outcome.Ack)
	}
	outcome = HandleChat(deps, Views{Presence: stubPresence{}, Parties: stubParties{}}, testDivision, sender, chatFrame(ChatTypeUnion, 0xFF, "", "hi"))
	if !bytes.Equal(outcome.Ack, []byte{0x02, 0x0B, 0x0B, 0xFF}) {
		t.Fatalf("guildless union ack = % X, want 02 0B 0B FF", outcome.Ack)
	}

	// In a guild: guild chat fans to members except the sender (by
	// CharID); union chat with no union lane refuses 0x0C (no union).
	deps.Guilds = stubGuilds{guildID: 3, members: []enterworld.GuildMemberRecord{
		{CharID: 7, Name: "Alfa"},
		{CharID: 8, Name: "Berk"},
	}}
	outcome = HandleChat(deps, Views{Presence: stubPresence{}, Parties: stubParties{}}, testDivision, sender, chatFrame(ChatTypeGuild, 0xFF, "", "hi"))
	if !bytes.Equal(outcome.Ack, []byte{0x01, 0x05, 0xFF}) {
		t.Fatalf("guild ack = % X, want 01 05 FF", outcome.Ack)
	}
	if len(outcome.Deliveries) != 1 || outcome.Deliveries[0].TargetName != "Berk" {
		t.Fatalf("guild deliveries = %+v, want one to Berk", outcome.Deliveries)
	}
	want := append([]byte{0x05, 0x04, 0x00, 'A', 'l', 'f', 'a'}, utf16LE("hi")...)
	if !bytes.Equal(outcome.Deliveries[0].Payload, want) {
		t.Fatalf("guild 0x3667 = % X, want % X", outcome.Deliveries[0].Payload, want)
	}
	outcome = HandleChat(deps, Views{Presence: stubPresence{}, Parties: stubParties{}}, testDivision, sender, chatFrame(ChatTypeUnion, 0xFF, "", "hi"))
	if !bytes.Equal(outcome.Ack, []byte{0x02, 0x0C, 0x0B, 0xFF}) {
		t.Fatalf("in-guild union ack = % X, want 02 0C 0B FF", outcome.Ack)
	}

	// A union lane answers the audience and the right.
	views := Views{Unions: stubUnions{names: []string{"Cale"}}}
	outcome = HandleChat(deps, views, testDivision, sender, chatFrame(ChatTypeUnion, 0xFF, "", "hi"))
	if !bytes.Equal(outcome.Ack, []byte{0x01, 0x0B, 0xFF}) || len(outcome.Deliveries) != 1 || outcome.Deliveries[0].TargetName != "Cale" {
		t.Fatalf("union line ack % X deliveries %+v", outcome.Ack, outcome.Deliveries)
	}
	if want := append([]byte{0x0B, 0x04, 0x00, 'A', 'l', 'f', 'a'}, utf16LE("hi")...); !bytes.Equal(outcome.Deliveries[0].Payload, want) {
		t.Fatalf("union 0x3667 = % X, want % X", outcome.Deliveries[0].Payload, want)
	}
	views = Views{Unions: stubUnions{code: 0x0E}}
	outcome = HandleChat(deps, views, testDivision, sender, chatFrame(ChatTypeUnion, 0xFF, "", "hi"))
	if !bytes.Equal(outcome.Ack, []byte{0x02, 0x0E, 0x0B, 0xFF}) {
		t.Fatalf("no-right union ack = % X, want 02 0E 0B FF", outcome.Ack)
	}
}

/*
================
stubUnions
================
*/
type stubUnions struct {
	names []string
	code  uint8
}

func (s stubUnions) UnionChatAudience(string, *enterworld.Character) ([]string, uint8) {
	return s.names, s.code
}

/*
================
TestHandleChatUncomposableTypeAcksInvalidCommand
================
*/
func TestHandleChatUncomposableTypeAcksInvalidCommand(t *testing.T) {
	sender := &enterworld.Character{ID: 7, Name: "Alfa"}
	deps := testDeps(sender)
	// A forged notice (7) or stall (9) must NEVER broadcast - error 8.
	for _, chatType := range []uint8{6, 7, 9, 0x0D, 0x10, 0xFE} {
		outcome := HandleChat(deps, Views{Presence: stubPresence{}, Parties: stubParties{}}, testDivision, sender, chatFrame(chatType, 0xFF, "", "hi"))
		if !bytes.Equal(outcome.Ack, []byte{0x02, 0x08, chatType, 0xFF}) {
			t.Fatalf("type 0x%02X ack = % X, want 02 08 %02X FF", chatType, outcome.Ack, chatType)
		}
		if outcome.Broadcast != nil || len(outcome.Deliveries) != 0 {
			t.Fatalf("type 0x%02X produced output: %+v", chatType, outcome)
		}
	}
}

// The ack echoes the second byte VERBATIM (the client's pending-record
// pop key) - not just the retail 0xFF.
/*
================
TestHandleChatSecondByteEchoed
================
*/
func TestHandleChatSecondByteEchoed(t *testing.T) {
	sender := &enterworld.Character{ID: 7, Name: "Alfa"}
	outcome := HandleChat(testDeps(sender), Views{Presence: stubPresence{}, Parties: stubParties{}}, testDivision, sender, chatFrame(ChatTypeAll, 0x2A, "", "hi"))
	if !bytes.Equal(outcome.Ack, []byte{0x01, 0x01, 0x2A}) {
		t.Fatalf("ack = % X, want 01 01 2A", outcome.Ack)
	}
}
