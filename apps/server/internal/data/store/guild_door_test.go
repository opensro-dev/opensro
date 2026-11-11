/*
===========================================================================

guild_door_test.go - the record-only and single-member guild doors (#3126)

GuildRecord and GuildMember answer from the same stored rows Guild's
full view reads: the record identical, and each member's view equal
to that member's row inside the full list, live level included.

===========================================================================
*/
package store

import (
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestGuildRecordAndMemberDoors
================
*/
func TestGuildRecordAndMemberDoors(t *testing.T) {
	t.Parallel()
	s := openTest(t, t.TempDir(), newTestClock())
	leader := guildTestCharacter("doorleader")
	member := guildTestCharacter("doormember")
	for _, character := range []*domain.Character{leader, member} {
		if err := s.CreateCharacter(testDivision, "test-account", character); err != nil {
			t.Fatal(err)
		}
	}
	guildID, _, _ := seedTestGuild(t, s, leader, member)
	door := s.Guilds()

	full, members, ok := door.Guild(testDivision, guildID)
	if !ok {
		t.Fatal("the seeded guild does not resolve through the full door")
	}
	record, ok := door.GuildRecord(testDivision, guildID)
	if !ok || record != full {
		t.Fatalf("record door: %+v ok=%v, want %+v", record, ok, full)
	}
	for _, view := range members {
		got, ok := door.GuildMember(testDivision, guildID, view.CharID)
		if !ok || got != view {
			t.Fatalf("member door for %d: %+v ok=%v, want %+v", view.CharID, got, ok, view)
		}
	}
	if _, ok := door.GuildRecord(testDivision, guildID+1000); ok {
		t.Fatal("an unknown guild resolved through the record door")
	}
	if _, ok := door.GuildMember(testDivision, guildID+1000, leader.ID); ok {
		t.Fatal("an unknown guild resolved through the member door")
	}
	if _, ok := door.GuildMember(testDivision, guildID, member.ID+1000); ok {
		t.Fatal("a non-member resolved through the member door")
	}

	// The level in the view is the character's CURRENT level: raise it
	// through the character door and both doors show the new value.
	raised := int64(41)
	if !s.UpdateCharacters([]*domain.Character{member}, "guild-door-level", func() bool {
		member.Level = &raised
		return true
	}) {
		t.Fatal("the level raise was refused")
	}
	if got, ok := door.GuildMember(testDivision, guildID, member.ID); !ok || got.Level != uint8(raised) {
		t.Fatalf("member view after the raise: %+v ok=%v, want level %d", got, ok, raised)
	}
}
