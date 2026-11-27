/*
===========================================================================

groupletter_test.go - the 0x7600 group letter's handler rules

A guild letter (kind 1) reaches only the sender's own guild members
and needs guild level 3; a union letter (kind 2) reaches every named
character and needs level 4. The refusal answers are the 0xB600
result-2 codes; a filtered name or body dies silently; a full
mailbox names its owner in the result-1 failed list while the rest
deliver.

===========================================================================
*/

package community

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

// fakeGuildStore answers the two guild reads the group handler
// makes; every other GuildStore door stays unimplemented.
type fakeGuildStore struct {
	domain.GuildStore
	record  domain.GuildRecord
	members []domain.GuildMemberRecord
	byChar  map[int64]int64
}

func (f *fakeGuildStore) GuildOfCharacter(_ string, characterID int64) (int64, bool) {
	guildID, ok := f.byChar[characterID]
	return guildID, ok
}

func (f *fakeGuildStore) Guild(_ string, guildID int64) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
	if guildID != f.record.ID {
		return domain.GuildRecord{}, nil, false
	}
	return f.record, f.members, true
}

func groupLetterPayload(kind uint8, body string, names ...string) []byte {
	out := []byte{kind, byte(len(names))}
	for _, name := range names {
		out = append(out, byte(len(name)), byte(len(name)>>8))
		out = append(out, []byte(name)...)
	}
	out = append(out, byte(len(body)), byte(len(body)>>8))
	return append(out, []byte(body)...)
}

func groupLetterDeps(letters *fakeLetterStore, guild *fakeGuildStore, characters ...*enterworld.Character) *enterworld.Deps {
	deps := letterTestDeps(letters, characters...)
	deps.Guilds = guild
	return deps
}

func groupLetterGuild(level uint8, members ...domain.GuildMemberRecord) *fakeGuildStore {
	byChar := map[int64]int64{}
	for _, m := range members {
		byChar[m.CharID] = 77
	}
	return &fakeGuildStore{
		record:  domain.GuildRecord{ID: 77, Name: "Wardens", Level: level},
		members: members,
		byChar:  byChar,
	}
}

func groupLetterMember(id int64, name string) domain.GuildMemberRecord {
	return domain.GuildMemberRecord{CharID: id, Name: name}
}

func groupLetterCharacter(id int64, name string) *enterworld.Character {
	return &enterworld.Character{
		ID:            id,
		Name:          name,
		ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		RaceIndex:     letterTestInt64(enterworld.RaceChina),
		Gender:        letterTestInt64(enterworld.GenderMale),
	}
}

func TestGroupLetterGuildKindFiltersToMembers(t *testing.T) {
	letters := newFakeLetterStore()
	sender := letterTestSender()
	berk := letterTestReceiver()
	cleo := groupLetterCharacter(3, "Cleo")
	stranger := groupLetterCharacter(4, "Dain")
	guild := groupLetterGuild(3,
		groupLetterMember(1, "Ash"), groupLetterMember(2, "Berk"), groupLetterMember(3, "Cleo"))
	deps := groupLetterDeps(letters, guild, sender, berk, cleo, stranger)

	outcome := HandleGroupLetterSend(deps, letterTestDivision, sender,
		groupLetterPayload(GroupLetterKindGuild, "raid at dawn", "Berk", "Dain", "cleo"), time.Now())
	if outcome.Refusal != "" {
		t.Fatalf("refusal = %q", outcome.Refusal)
	}
	if len(outcome.AckPayload) != 2 || outcome.AckPayload[0] != 1 || outcome.AckPayload[1] != 0 {
		t.Fatalf("ack = %x, want [01 00]", outcome.AckPayload)
	}
	if got := len(letters.Mailbox(letterTestDivision, 2)); got != 1 {
		t.Fatalf("member mailbox = %d letters, want 1", got)
	}
	if got := len(letters.Mailbox(letterTestDivision, 3)); got != 1 {
		t.Fatalf("case-folded member mailbox = %d letters, want 1", got)
	}
	if got := len(letters.Mailbox(letterTestDivision, 4)); got != 0 {
		t.Fatalf("non-member mailbox = %d letters, want 0", got)
	}
	if len(outcome.Deliveries) != 2 {
		t.Fatalf("deliveries = %d, want 2", len(outcome.Deliveries))
	}
}

func TestGroupLetterUnionKindKeepsEveryName(t *testing.T) {
	letters := newFakeLetterStore()
	sender := letterTestSender()
	berk := letterTestReceiver()
	guild := groupLetterGuild(4, groupLetterMember(1, "Ash"))
	deps := groupLetterDeps(letters, guild, sender, berk)

	outcome := HandleGroupLetterSend(deps, letterTestDivision, sender,
		groupLetterPayload(GroupLetterKindUnion, "union call", "Berk", "NoSuchName"), time.Now())
	if outcome.Refusal != "" {
		t.Fatalf("refusal = %q", outcome.Refusal)
	}
	if len(outcome.AckPayload) != 2 || outcome.AckPayload[0] != 1 || outcome.AckPayload[1] != 0 {
		t.Fatalf("ack = %x, want [01 00] (unknown names are skipped, not failed)", outcome.AckPayload)
	}
	if got := len(letters.Mailbox(letterTestDivision, 2)); got != 1 {
		t.Fatalf("mailbox = %d letters, want 1", got)
	}
	if len(outcome.Deliveries) != 1 || outcome.Deliveries[0].RecipientName != "Berk" {
		t.Fatalf("deliveries = %+v", outcome.Deliveries)
	}
}

func TestGroupLetterRefusals(t *testing.T) {
	sender := letterTestSender()
	berk := letterTestReceiver()
	cases := []struct {
		name    string
		guild   *fakeGuildStore
		payload []byte
		code    uint8
	}{
		{"no guild", groupLetterGuild(3), groupLetterPayload(1, "hi", "Berk"), GroupLetterRefusalNoGuild},
		{"guild kind at level 2", groupLetterGuild(2, groupLetterMember(1, "Ash"), groupLetterMember(2, "Berk")), groupLetterPayload(1, "hi", "Berk"), GroupLetterRefusalGuildLevel},
		{"union kind at level 3", groupLetterGuild(3, groupLetterMember(1, "Ash")), groupLetterPayload(2, "hi", "Berk"), GroupLetterRefusalGuildLevel},
		{"unknown kind", groupLetterGuild(5, groupLetterMember(1, "Ash")), groupLetterPayload(9, "hi", "Berk"), GroupLetterRefusalKind},
		{"empty name", groupLetterGuild(4, groupLetterMember(1, "Ash")), groupLetterPayload(2, "hi", ""), GroupLetterRefusalEmpty},
		{"nobody kept", groupLetterGuild(3, groupLetterMember(1, "Ash")), groupLetterPayload(1, "hi", "Berk"), GroupLetterRefusalEmpty},
		{"empty body", groupLetterGuild(4, groupLetterMember(1, "Ash")), groupLetterPayload(2, "", "Berk"), GroupLetterRefusalBodyLength},
		{"body over the read cap", groupLetterGuild(4, groupLetterMember(1, "Ash")), groupLetterPayload(2, string(make([]byte, GroupLetterBodyMaxBytes+1)), "Berk"), GroupLetterRefusalBodyLength},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			letters := newFakeLetterStore()
			outcome := HandleGroupLetterSend(groupLetterDeps(letters, tc.guild, sender, berk),
				letterTestDivision, sender, tc.payload, time.Now())
			if outcome.Refusal == "" {
				t.Fatal("refusal is empty")
			}
			if len(outcome.AckPayload) != 2 || outcome.AckPayload[0] != 2 || outcome.AckPayload[1] != tc.code {
				t.Fatalf("ack = %x, want [02 %02x]", outcome.AckPayload, tc.code)
			}
			if got := len(letters.Mailbox(letterTestDivision, 2)); got != 0 {
				t.Fatalf("mailbox = %d letters after a refusal, want 0", got)
			}
		})
	}
}

func TestGroupLetterFilteredTextDiesSilently(t *testing.T) {
	sender := letterTestSender()
	berk := letterTestReceiver()
	guild := groupLetterGuild(4, groupLetterMember(1, "Ash"))
	for _, payload := range [][]byte{
		groupLetterPayload(2, "hi", "Be'rk"),
		groupLetterPayload(2, "it's me", "Berk"),
	} {
		letters := newFakeLetterStore()
		outcome := HandleGroupLetterSend(groupLetterDeps(letters, guild, sender, berk),
			letterTestDivision, sender, payload, time.Now())
		if outcome.Refusal == "" || outcome.AckPayload != nil {
			t.Fatalf("outcome = %+v, want a silent refusal", outcome)
		}
		if got := len(letters.Mailbox(letterTestDivision, 2)); got != 0 {
			t.Fatalf("mailbox = %d letters after a filtered request, want 0", got)
		}
	}
}

func TestGroupLetterFullMailboxNamesTheFailure(t *testing.T) {
	letters := newFakeLetterStore()
	sender := letterTestSender()
	berk := letterTestReceiver()
	cleo := groupLetterCharacter(3, "Cleo")
	guild := groupLetterGuild(3,
		groupLetterMember(1, "Ash"), groupLetterMember(2, "Berk"), groupLetterMember(3, "Cleo"))
	deps := groupLetterDeps(letters, guild, sender, berk, cleo)
	for i := 0; i < LetterMailboxMaxCount; i++ {
		if !letters.UpdateMailbox(letterTestDivision, 2, "fill", func(mailbox []enterworld.LetterRecord) ([]enterworld.LetterRecord, bool) {
			return append(mailbox, enterworld.LetterRecord{Sender: "Ash", Body: "old"}), true
		}) {
			t.Fatal("fill failed")
		}
	}

	outcome := HandleGroupLetterSend(deps, letterTestDivision, sender,
		groupLetterPayload(GroupLetterKindGuild, "fresh", "Berk", "Cleo"), time.Now())
	if outcome.Refusal != "" {
		t.Fatalf("refusal = %q", outcome.Refusal)
	}
	want := EncodeGroupLetterSendAckB600([]string{"Berk"})
	if string(outcome.AckPayload) != string(want) {
		t.Fatalf("ack = %x, want %x (Berk named as failed)", outcome.AckPayload, want)
	}
	if got := len(letters.Mailbox(letterTestDivision, 3)); got != 1 {
		t.Fatalf("reachable member mailbox = %d letters, want 1", got)
	}
}

func TestDecodeGroupLetterSendRequest(t *testing.T) {
	request, err := DecodeGroupLetterSendRequest(groupLetterPayload(1, "body text", "Berk", "Cleo"))
	if err != nil {
		t.Fatal(err)
	}
	if request.Kind != 1 || len(request.Names) != 2 || request.Names[0] != "Berk" || request.Names[1] != "Cleo" || request.Body != "body text" {
		t.Fatalf("request = %+v", request)
	}
	if _, err := DecodeGroupLetterSendRequest([]byte{1, 2, 4, 0, 'B'}); err == nil {
		t.Fatal("a truncated frame decoded without error")
	}
	if _, err := DecodeGroupLetterSendRequest(append(groupLetterPayload(1, "b", "Berk"), 0)); err == nil {
		t.Fatal("a trailing byte decoded without error")
	}
}
