package store

// Commit revision tests: the presentation caches of the world tick key
// their kept inputs on CommitRevision (character records) and
// GuildRevision (guild rows), so every store door that commits a write
// must move the matching count, a refused conditional door must not,
// and a dissolved guild must stop reporting one (#3100).

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

// TestCommitRevisionMovesWithEveryCharacterDoor: the four character
// commit doors (immediate and deferred) each move the record's count
// exactly once per committed write, and a refused UpdateCharacter
// moves nothing.
func TestCommitRevisionMovesWithEveryCharacterDoor(t *testing.T) {
	t.Parallel()
	s := openTest(t, t.TempDir(), newTestClock())

	c := &enterworld.Character{Name: "revhero"}
	if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
		t.Fatal(err)
	}
	start := c.CommitRevision

	s.MutateCharacter(c, "rev-mutate", func() { c.Gold = int64Ptr(5) })
	if c.CommitRevision != start+1 {
		t.Fatalf("after MutateCharacter revision = %d, want %d", c.CommitRevision, start+1)
	}
	if !s.UpdateCharacter(c, "rev-update", func() bool { return true }) {
		t.Fatal("UpdateCharacter refused a true update")
	}
	if c.CommitRevision != start+2 {
		t.Fatalf("after UpdateCharacter revision = %d, want %d", c.CommitRevision, start+2)
	}
	if s.UpdateCharacter(c, "rev-refused", func() bool { return false }) {
		t.Fatal("UpdateCharacter accepted a false update")
	}
	if c.CommitRevision != start+2 {
		t.Fatalf("a refused update moved the revision to %d, want %d", c.CommitRevision, start+2)
	}
	s.MutateCharacterDeferred(c, "rev-mutate-deferred", func() { c.Gold = int64Ptr(6) })
	if c.CommitRevision != start+3 {
		t.Fatalf("after MutateCharacterDeferred revision = %d, want %d", c.CommitRevision, start+3)
	}
	if !s.UpdateCharacterDeferred(c, "rev-update-deferred", func() bool { return true }) {
		t.Fatal("UpdateCharacterDeferred refused a true update")
	}
	if c.CommitRevision != start+4 {
		t.Fatalf("after UpdateCharacterDeferred revision = %d, want %d", c.CommitRevision, start+4)
	}
}

// TestGuildRevisionMovesWithGuildDoors: a guild reports a revision
// from its creation, every committed guild write moves it, and the
// dissolved guild reports none.
func TestGuildRevisionMovesWithGuildDoors(t *testing.T) {
	t.Parallel()
	s := openTest(t, t.TempDir(), newTestClock())
	door := storeGuildDoor{s: s}

	if _, ok := door.GuildRevision(testDivision, 99); ok {
		t.Fatal("an unstored guild reports a revision")
	}

	leader := &enterworld.Character{Name: "revleader"}
	if err := s.CreateCharacter(testDivision, "test-account", leader); err != nil {
		t.Fatal(err)
	}
	guildID, err := s.Guilds().CreateGuild(testDivision, domain.GuildRecord{Name: "RevWatch", Level: 1},
		domain.GuildMemberRecord{CharID: leader.ID, JID: uint32(leader.ID), Name: leader.Name, Grade: 0, PermMask: 0xffffffff}, leader)
	if err != nil {
		t.Fatal(err)
	}
	before, ok := door.GuildRevision(testDivision, guildID)
	if !ok || before == 0 {
		t.Fatalf("a created guild reports revision %d, ok=%v", before, ok)
	}

	_, refusal := s.Guilds().UpdateGuildAs(testDivision, leader.ID, "rev-note",
		domain.GuildAuthorization{},
		func(record domain.GuildRecord, members []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
			record.NoticeSubject = "revised"
			return record, members, true
		})
	if refusal.Refused() {
		t.Fatalf("the guild update was refused: %v", refusal)
	}
	after, ok := door.GuildRevision(testDivision, guildID)
	if !ok || after != before+1 {
		t.Fatalf("after one guild write revision = %d, want %d", after, before+1)
	}

	if _, refusal := s.Guilds().DissolveGuildAs(testDivision, leader.ID); refusal.Refused() {
		t.Fatalf("the dissolve was refused: %v", refusal)
	}
	if _, ok := door.GuildRevision(testDivision, guildID); ok {
		t.Fatal("a dissolved guild still reports a revision")
	}
}

/*
================
TestCommitRevisionMovesWithAnAppliedRename

The applied rename persists through its own transaction, never the
dirty set; the counts it moves are the whole contract with the tick's
kept presentations. A rename that changed the live records while the
counts stood still would leave the old name on screen forever.
================
*/
func TestCommitRevisionMovesWithAnAppliedRename(t *testing.T) {
	t.Parallel()
	s := openTest(t, t.TempDir(), newTestClock())
	door := storeGuildDoor{s: s}

	hero := &domain.Character{Name: "revhero"}
	if err := s.CreateCharacter(testDivision, "test-account", hero); err != nil {
		t.Fatal(err)
	}
	guildID, err := s.Guilds().CreateGuild(testDivision, domain.GuildRecord{Name: "RevNames", Level: 1},
		domain.GuildMemberRecord{CharID: hero.ID, JID: uint32(hero.ID), Name: hero.Name, Grade: 0, PermMask: 0xffffffff}, hero)
	if err != nil {
		t.Fatal(err)
	}

	now := int64(1_700_000_000_000)
	if err := s.QueueCharacterRename(testDivision, hero, "revrenamed", now); err != nil {
		t.Fatal(err)
	}
	characterBefore := hero.CommitRevision
	guildBefore, ok := door.GuildRevision(testDivision, guildID)
	if !ok {
		t.Fatal("a created guild reports no revision")
	}

	outcomes := s.ApplyPendingRenames(testDivision, "test-account", now, nil)
	if len(outcomes) != 1 || !outcomes[0].Applied || hero.Name != "revrenamed" {
		t.Fatalf("outcomes: %+v hero: %+v", outcomes, hero)
	}
	if hero.CommitRevision == characterBefore {
		t.Fatal("the applied rename left the character's commit revision standing")
	}
	if revision, _ := door.GuildRevision(testDivision, guildID); revision == guildBefore {
		t.Fatal("the applied rename left the guild's commit revision standing")
	}
}
