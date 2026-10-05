/*
===========================================================================

guildlevel_test.go - the guild level-up door and the per-level member cap

===========================================================================
*/

package store

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestGuildLevelUpChargesTheMasterAndTheGuild

Level 2 to 3 costs 50,400 GP and 9,000,000 gold (client 0xBE4C58 /
0xBE4C6C, v1.188 0xADE910 / 0xADE8EC); the gold shortfall answers before
the GP (5C6240), a member is not the master, and the result survives a
reopen.
================
*/
func TestGuildLevelUpChargesTheMasterAndTheGuild(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	leader := guildTestCharacter("levelmaster")
	member := guildTestCharacter("levelhelper")
	for _, character := range []*enterworld.Character{leader, member} {
		if err := s.CreateCharacter(testDivision, "test-account", character); err != nil {
			t.Fatal(err)
		}
	}
	guildID, _, _ := seedTestGuild(t, s, leader, member)
	if _, refusal := s.Guilds().LevelUpGuildAs(testDivision, member.ID); refusal != enterworld.GuildRefusalLeaderRequired {
		t.Fatalf("a member levelled the guild: %v", refusal)
	}
	if _, refusal := s.Guilds().LevelUpGuildAs(testDivision, leader.ID); refusal != enterworld.GuildRefusalGoldDeficit {
		t.Fatalf("a master without gold levelled the guild: %v", refusal)
	}
	gold := int64(9000100)
	if !s.UpdateCharacters([]*enterworld.Character{leader}, "fixture-gold", func() bool { leader.Gold = &gold; return true }) {
		t.Fatal("fixture gold refused")
	}
	if _, refusal := s.Guilds().LevelUpGuildAs(testDivision, leader.ID); refusal != enterworld.GuildRefusalGPDeficit {
		t.Fatalf("1500 GP levelled the guild: %v", refusal)
	}
	if !updateTestGuild(s, leader.ID, "fixture-gp", func(g enterworld.GuildRecord, m []enterworld.GuildMemberRecord) (enterworld.GuildRecord, []enterworld.GuildMemberRecord) {
		g.GP = 60000
		return g, m
	}) {
		t.Fatal("fixture GP refused")
	}
	snapshot, refusal := s.Guilds().LevelUpGuildAs(testDivision, leader.ID)
	if refusal.Refused() || snapshot.Guild.Level != 3 || snapshot.Guild.GP != 60000-50400 || *leader.Gold != 100 {
		t.Fatalf("level up %+v/%v gold %d", snapshot.Guild, refusal, *leader.Gold)
	}
	s.Close()
	reopened := openTest(t, dir, clock)
	stored, _, ok := reopened.Guilds().Guild(testDivision, guildID)
	if !ok || stored.Level != 3 || stored.GP != 9600 {
		t.Fatalf("reopened guild %+v", stored)
	}
}

/*
================
TestGuildRosterStopsAtTheLevelCapacity

A level 1 guild holds 15 members (client 0xBE4490).
================
*/
func TestGuildRosterStopsAtTheLevelCapacity(t *testing.T) {
	t.Parallel()
	s := openTest(t, t.TempDir(), newTestClock())
	leader := guildTestCharacter("capmaster")
	if err := s.CreateCharacter(testDivision, "test-account", leader); err != nil {
		t.Fatal(err)
	}
	guildID, err := s.Guilds().CreateGuild(testDivision, enterworld.GuildRecord{Name: "Capped", Level: 1},
		enterworld.GuildMemberRecord{CharID: leader.ID, JID: 1, Name: leader.Name, Grade: 0, PermMask: 0xffffffff}, leader)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 15; index++ {
		joiner := guildTestCharacter("capjoiner" + string(rune('a'+index)))
		if err := s.CreateCharacter(testDivision, "cap-account-"+string(rune('a'+index)), joiner); err != nil {
			t.Fatal(err)
		}
		_, refusal := s.Guilds().AddGuildMemberAs(testDivision, guildID, leader.ID, 0,
			enterworld.GuildMemberRecord{CharID: joiner.ID, JID: uint32(2 + index), Name: joiner.Name, Grade: 3})
		if want := index >= 14; refusal.Refused() != want {
			t.Fatalf("joiner %d refusal %v", index+2, refusal)
		}
	}
}
