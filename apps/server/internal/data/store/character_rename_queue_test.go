/*
===========================================================================

character_rename_queue_test.go - the scroll's queue/apply doors (#1689)

===========================================================================
*/
package store

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
)

/*
================
TestQueuedRenameAppliesAtLoginAndReservesOldName

The scroll flow at the store boundary: queue refuses bad targets,
apply renames with every social copy, the old name is held for 90
days against creation and other renames, and the 30-day cooldown
gates the next queue. Everything survives a reopen.
================
*/
func TestQueuedRenameAppliesAtLoginAndReservesOldName(t *testing.T) {
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	hero := &domain.Character{Name: "Hero"}
	peer := &domain.Character{Name: "Peer"}
	for _, c := range []*domain.Character{hero, peer} {
		if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
			t.Fatal(err)
		}
	}
	s.Mutate("rename-queue-fixture", func() {
		hero.Friends = []domain.FriendRecord{{ID: peer.ID, Name: peer.Name}}
		peer.Friends = []domain.FriendRecord{{ID: hero.ID, Name: hero.Name}}
		peer.BlockedWhisperers = []string{"HERO"}
	})
	now := clock.Now().UnixMilli()

	if err := s.QueueCharacterRename(testDivision, hero, "bad name!", now); !errors.Is(err, ErrCharacterNameInvalid) {
		t.Fatalf("invalid target: %v", err)
	}
	if err := s.QueueCharacterRename(testDivision, hero, "Peer", now); !errors.Is(err, ErrCharacterNameConflict) {
		t.Fatalf("taken target: %v", err)
	}
	if err := s.QueueCharacterRename(testDivision, hero, "HeroTwo", now); err != nil {
		t.Fatalf("queue: %v", err)
	}
	if err := s.QueueCharacterRename(testDivision, hero, "HeroThree", now); !errors.Is(err, ErrRenamePending) {
		t.Fatalf("second queue: %v", err)
	}
	if hero.Name != "Hero" {
		t.Fatal("the queue renamed mid-session")
	}

	outcomes := s.ApplyPendingRenames(testDivision, "test-account", now)
	if len(outcomes) != 1 || !outcomes[0].Applied || outcomes[0].OldName != "Hero" || outcomes[0].NewName != "HeroTwo" {
		t.Fatalf("outcomes: %+v", outcomes)
	}
	if hero.Name != "HeroTwo" || hero.PendingRenameName != "" || hero.LastRenameMs != now {
		t.Fatalf("hero after apply: %+v", hero)
	}
	if peer.Friends[0].Name != "HeroTwo" || peer.BlockedWhisperers[0] != "HeroTwo" {
		t.Fatal("stale social references after apply")
	}

	// The old name is held: creation and a peer's rename both refuse it.
	if err := s.CreateCharacter(testDivision, "other-account", &domain.Character{Name: "hero"}); !errors.Is(err, ErrNameReserved) {
		t.Fatalf("create over reservation: %v", err)
	}
	if err := s.QueueCharacterRename(testDivision, peer, "Hero", now); !errors.Is(err, ErrNameReserved) {
		t.Fatalf("rename into reservation: %v", err)
	}

	// The cooldown gates the next queue; after 30 days it opens.
	if err := s.QueueCharacterRename(testDivision, hero, "HeroThree", now+1000); !errors.Is(err, ErrRenameCooldown) {
		t.Fatalf("cooldown queue: %v", err)
	}
	clock.Advance(31 * 24 * time.Hour)
	later := clock.Now().UnixMilli()
	if err := s.QueueCharacterRename(testDivision, hero, "HeroThree", later); err != nil {
		t.Fatalf("queue after cooldown: %v", err)
	}
	outcomes = s.ApplyPendingRenames(testDivision, "test-account", later)
	if len(outcomes) != 1 || !outcomes[0].Applied || hero.Name != "HeroThree" {
		t.Fatalf("second apply: %+v (hero %q)", outcomes, hero.Name)
	}

	s.Close()
	reopened := openTest(t, dir, clock)
	var renamed *domain.Character
	for _, c := range reopened.Characters().CharactersForDivision(testDivision) {
		if c.ID == hero.ID {
			renamed = c
		}
	}
	if renamed == nil || renamed.Name != "HeroThree" || renamed.LastRenameMs != later {
		t.Fatalf("reopened hero: %+v", renamed)
	}
	if err := reopened.CreateCharacter(testDivision, "other-account", &domain.Character{Name: "HeroTwo"}); !errors.Is(err, ErrNameReserved) {
		t.Fatalf("create over persisted reservation: %v", err)
	}
	// After the 90-day hold lapses the name is free again.
	clock.Advance(90 * 24 * time.Hour)
	if err := reopened.CreateCharacter(testDivision, "other-account", &domain.Character{Name: "HeroTwo"}); err != nil {
		t.Fatalf("create after reservation lapse: %v", err)
	}
}

/*
================
TestQueuedRenameCancelsWhenTargetIsTaken

The queue is not a reservation: a target another character takes
before the login cancels the rename (the scroll stays spent) and
reports the reason; the old name is untouched.
================
*/
func TestQueuedRenameCancelsWhenTargetIsTaken(t *testing.T) {
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	hero := &domain.Character{Name: "Stayer"}
	if err := s.CreateCharacter(testDivision, "test-account", hero); err != nil {
		t.Fatal(err)
	}
	now := clock.Now().UnixMilli()
	if err := s.QueueCharacterRename(testDivision, hero, "FreshName", now); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCharacter(testDivision, "other-account", &domain.Character{Name: "FreshName"}); err != nil {
		t.Fatal(err)
	}
	outcomes := s.ApplyPendingRenames(testDivision, "test-account", now)
	if len(outcomes) != 1 || outcomes[0].Applied || outcomes[0].Reason != RenameRefusedTaken {
		t.Fatalf("outcomes: %+v", outcomes)
	}
	if hero.Name != "Stayer" || hero.PendingRenameName != "" || hero.LastRenameMs != 0 {
		t.Fatalf("hero after cancel: %+v", hero)
	}
}

/*
================
TestQueuedRenameCancelsDuringGuildWar

A war that starts between the queue and the login cancels the rename:
the decision refuses a rename during guild war at both checkpoints.
================
*/
func TestQueuedRenameCancelsDuringGuildWar(t *testing.T) {
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	hero := &domain.Character{Name: "Warrior"}
	if err := s.CreateCharacter(testDivision, "test-account", hero); err != nil {
		t.Fatal(err)
	}
	now := clock.Now().UnixMilli()
	if err := s.QueueCharacterRename(testDivision, hero, "Peaceful", now); err != nil {
		t.Fatal(err)
	}
	guildID := int64(77)
	s.Mutate("rename-war-fixture", func() { hero.GuildID = &guildID })
	raw, err := json.Marshal(domain.GuildWarRecord{ID: 1, Guilds: [2]int64{guildID, 78}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO guild_wars (division, id, guild_a, guild_b, record) VALUES (?, 1, ?, 78, ?)",
		testDivision, guildID, string(raw)); err != nil {
		t.Fatal(err)
	}
	outcomes := s.ApplyPendingRenames(testDivision, "test-account", now)
	if len(outcomes) != 1 || outcomes[0].Applied || outcomes[0].Reason != RenameRefusedGuildWar {
		t.Fatalf("outcomes: %+v", outcomes)
	}
	if hero.Name != "Warrior" || hero.PendingRenameName != "" {
		t.Fatalf("hero after war cancel: %+v", hero)
	}
}
