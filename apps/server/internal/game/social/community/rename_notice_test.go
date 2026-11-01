/*
===========================================================================

rename_notice_test.go - the rename notice's audience and frame (#1689)

The audience is exactly the renamed character's friends and guild
members: friends first, then guild members in roster order, each name
once, the character itself never. The frame is the plain notice on the
rename channel, and the bind spends the debt so no second bind repeats
it.

===========================================================================
*/

package community

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
renameNoticeRoster

The renamed character (id 1, was OldName), a friend (2), a guildmate
(3), a friend who is also a guildmate (4), a stranger (5) and another
guild's member (6).
================
*/
func renameNoticeRoster() []*enterworld.Character {
	guild := int64(7)
	self := &enterworld.Character{ID: 1, Name: "NewName", RenameNoticeFrom: "OldName", GuildID: &guild,
		Friends: []enterworld.FriendRecord{{ID: 2, Name: "Berk"}, {ID: 4, Name: "Duo"}}}
	otherGuild := int64(9)
	return []*enterworld.Character{
		self,
		{ID: 2, Name: "Berk"},
		{ID: 3, Name: "Cato", GuildID: &guild},
		{ID: 4, Name: "Duo", GuildID: &guild},
		{ID: 5, Name: "Elio"},
		{ID: 6, Name: "Fay", GuildID: &otherGuild},
	}
}

/*
================
TestRenameNoticeDelivery

The frame names old and new; the recipients are the friend and the two
guildmates, deduplicated, with the stranger and the other guild left
out.
================
*/
func TestRenameNoticeDelivery(t *testing.T) {
	roster := renameNoticeRoster()
	deps := &enterworld.Deps{Roster: &enterworld.Roster{},
		Characters: fakeCharacterSource{divisionID: letterTestDivision, characters: roster}}

	frame, recipients, owed := renameNoticeDelivery(deps, letterTestDivision, roster[0])
	if !owed {
		t.Fatal("a character owing a notice delivered nothing")
	}
	want := wire.RenameNotificationFrame("OldName is now known as NewName")
	if frame.Opcode != want.Opcode || !bytes.Equal(frame.Payload, want.Payload) {
		t.Fatalf("frame = %04x % X, want %04x % X", frame.Opcode, frame.Payload, want.Opcode, want.Payload)
	}
	if frame.Payload[0] != wire.RenameNoticeChannel {
		t.Fatalf("channel = %#x, want %#x", frame.Payload[0], wire.RenameNoticeChannel)
	}
	wantNames := []string{"Berk", "Duo", "Cato"}
	if len(recipients) != len(wantNames) {
		t.Fatalf("recipients = %v, want %v", recipients, wantNames)
	}
	for i, name := range wantNames {
		if recipients[i] != name {
			t.Fatalf("recipients = %v, want %v", recipients, wantNames)
		}
	}
}

/*
================
TestRenameNoticeWorldBoundSpendsTheDebt

One bind clears the debt; a second bind owes nothing.
================
*/
func TestRenameNoticeWorldBoundSpendsTheDebt(t *testing.T) {
	roster := renameNoticeRoster()
	deps := &enterworld.Deps{Roster: &enterworld.Roster{},
		Characters: fakeCharacterSource{divisionID: letterTestDivision, characters: roster}}

	RenameNoticeWorldBound(deps, nil, letterTestDivision, roster[0])
	if roster[0].RenameNoticeFrom != "" {
		t.Fatalf("debt still owed after the bind: %q", roster[0].RenameNoticeFrom)
	}
	if _, _, owed := renameNoticeDelivery(deps, letterTestDivision, roster[0]); owed {
		t.Fatal("a second bind would repeat the notice")
	}
}

/*
================
TestRenameNoticeNothingOwed
================
*/
func TestRenameNoticeNothingOwed(t *testing.T) {
	roster := renameNoticeRoster()
	roster[0].RenameNoticeFrom = ""
	deps := &enterworld.Deps{Roster: &enterworld.Roster{},
		Characters: fakeCharacterSource{divisionID: letterTestDivision, characters: roster}}
	if _, _, owed := renameNoticeDelivery(deps, letterTestDivision, roster[0]); owed {
		t.Fatal("an unrenamed character owes a notice")
	}
}
