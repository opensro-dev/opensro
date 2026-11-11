/*
===========================================================================

guild_door_bench_test.go - what one guild lookup costs per caller shape (#3126)

The appearance path resolves a peer's guild every snapshot: the record
half (name, id, crest) plus ONE member's grant and fortress role. The
full door also builds the whole member view - a fresh slice, one index
lookup and one live-level resolution per member - for those callers.
The three benchmarks price the three caller shapes over one 50-member
guild so the door a hot path pays for is the door it uses.

===========================================================================
*/
package store

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/domain"
)

// benchGuildMembers is a full guild book: the native roster cap the
// largest guilds reach, and the size the appearance path pays per peer.
const benchGuildMembers = 50

var (
	benchGuildRecordSink domain.GuildRecord
	benchGuildViewSink   []domain.GuildMemberRecord
	benchGuildMemberSink domain.GuildMemberRecord
	benchGuildBoolSink   bool
)

/*
================
benchGuildDoorFixture

One store holding a single 50-member guild; returns the door, the
guild id and one mid-book member's char id.
================
*/
func benchGuildDoorFixture(b *testing.B) (domain.GuildStore, int64, int64) {
	b.Helper()
	s := openTest(b, b.TempDir(), newTestClock())
	characters := make([]*domain.Character, 0, benchGuildMembers)
	for i := 0; i < benchGuildMembers; i++ {
		c := guildTestCharacter(fmt.Sprintf("bmember%02d", i))
		if err := s.CreateCharacter(testDivision, fmt.Sprintf("bench-acct-%02d", i), c); err != nil {
			b.Fatal(err)
		}
		characters = append(characters, c)
	}
	leader := characters[0]
	guild := domain.GuildRecord{Name: "BenchWatch", Level: 5, CrestParam: 0x00c81234}
	guildID, err := s.Guilds().CreateGuild(testDivision, guild,
		domain.GuildMemberRecord{CharID: leader.ID, JID: uint32(100000 + leader.ID), Name: leader.Name, Grade: 0, GrantName: "Leader", RefObjID: 1907},
		leader)
	if err != nil {
		b.Fatal(err)
	}
	for _, c := range characters[1:] {
		if !addTestGuildMember(s, guildID, leader.ID, domain.GuildMemberRecord{
			CharID: c.ID, JID: uint32(100000 + c.ID), Name: c.Name, Grade: 3, GrantName: "Bench", RefObjID: 1907,
		}) {
			b.Fatalf("member %s join refused", c.Name)
		}
	}
	return s.Guilds(), guildID, characters[benchGuildMembers/2].ID
}

/*
================
BenchmarkGuildFullView

The full door: record plus every member's view, in wire order.
================
*/
func BenchmarkGuildFullView(b *testing.B) {
	door, guildID, _ := benchGuildDoorFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchGuildRecordSink, benchGuildViewSink, benchGuildBoolSink = door.Guild(testDivision, guildID)
	}
}

/*
================
BenchmarkGuildRecordOnly

The record door: the guild row alone, no member view built.
================
*/
func BenchmarkGuildRecordOnly(b *testing.B) {
	door, guildID, _ := benchGuildDoorFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchGuildRecordSink, benchGuildBoolSink = door.GuildRecord(testDivision, guildID)
	}
}

/*
================
BenchmarkGuildMemberView

The member door: one member's view out of the 50-member book.
================
*/
func BenchmarkGuildMemberView(b *testing.B) {
	door, guildID, memberID := benchGuildDoorFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchGuildMemberSink, benchGuildBoolSink = door.GuildMember(testDivision, guildID, memberID)
	}
}
