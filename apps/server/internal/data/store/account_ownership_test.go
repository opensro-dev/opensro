package store

import (
	"errors"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

func TestTransferCharacterOwnershipCommitsEveryMatchingCharacter(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	clock := newTestClock()
	authority := openTest(t, dir, clock)

	for _, character := range []*enterworld.Character{
		{Name: "DevOne"},
		{Name: "DevTwo"},
	} {
		if err := authority.CreateCharacter(testDivision, domain.ReservedAccountID, character); err != nil {
			t.Fatalf("CreateCharacter(%s): %v", character.Name, err)
		}
	}
	if err := authority.CreateCharacter(testDivision, "other-account", &enterworld.Character{Name: "OtherOne"}); err != nil {
		t.Fatalf("CreateCharacter(OtherOne): %v", err)
	}

	transferred, err := authority.TransferCharacterOwnership(domain.ReservedAccountID, "real-account")
	if err != nil {
		t.Fatalf("TransferCharacterOwnership: %v", err)
	}
	if len(transferred) != 2 ||
		transferred[0].CharacterName != "DevOne" ||
		transferred[1].CharacterName != "DevTwo" {
		t.Fatalf("transferred = %#v", transferred)
	}
	authority.Close()

	reopened := openTest(t, dir, clock)
	owners := map[string]string{}
	for _, ownership := range reopened.CharacterOwnerships() {
		owners[ownership.CharacterName] = ownership.AccountID
	}
	if owners["DevOne"] != "real-account" || owners["DevTwo"] != "real-account" {
		t.Fatalf("development owners were not persisted: %#v", owners)
	}
	if owners["OtherOne"] != "other-account" {
		t.Fatalf("unrelated owner changed: %#v", owners)
	}
}

func TestTransferCharacterOwnershipRollsBackMemoryAndDiskOnCommitFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	clock := newTestClock()
	authority := openTest(t, dir, clock)
	if err := authority.CreateCharacter(
		testDivision,
		domain.ReservedAccountID,
		&enterworld.Character{Name: "DevOne"},
	); err != nil {
		t.Fatalf("CreateCharacter: %v", err)
	}

	authority.FailCommits(errors.New("injected outage"))
	if _, err := authority.TransferCharacterOwnership(
		domain.ReservedAccountID,
		"real-account",
	); err == nil || !strings.Contains(err.Error(), "injected outage") {
		t.Fatalf("transfer error = %v, want injected outage", err)
	}
	if got := authority.CharacterOwnerships()[0].AccountID; got != domain.ReservedAccountID {
		t.Fatalf("in-memory owner after refusal = %q", got)
	}
	authority.FailCommits(nil)
	authority.Close()

	reopened := openTest(t, dir, clock)
	if got := reopened.CharacterOwnerships()[0].AccountID; got != domain.ReservedAccountID {
		t.Fatalf("persisted owner after refusal = %q", got)
	}
}

func TestTransferCharacterOwnershipRefusesAmbiguousOrInvalidRequests(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	authority := openTest(t, dir, newTestClock())
	if err := authority.CreateCharacter(
		testDivision,
		domain.ReservedAccountID,
		&enterworld.Character{Name: "DevOne"},
	); err != nil {
		t.Fatalf("CreateCharacter: %v", err)
	}

	tests := []struct {
		name string
		from string
		to   string
	}{
		{name: "invalid source", from: "", to: "real-account"},
		{name: "invalid target", from: domain.ReservedAccountID, to: " padded "},
		{name: "same owner", from: "real-account", to: "real-account"},
		{name: "reserved target", from: "real-account", to: domain.ReservedAccountID},
		{name: "source absent", from: "missing-account", to: "real-account"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := authority.TransferCharacterOwnership(test.from, test.to); err == nil {
				t.Fatal("transfer unexpectedly succeeded")
			}
		})
	}
}

func TestTransferCharacterOwnershipRefusesRetainedGameplayChanges(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	authority := openTest(t, dir, newTestClock())
	gold := int64(0)
	character := &enterworld.Character{Name: "DevOne", Gold: &gold}
	if err := authority.CreateCharacter(testDivision, domain.ReservedAccountID, character); err != nil {
		t.Fatalf("CreateCharacter: %v", err)
	}

	authority.FailCommits(errors.New("injected outage"))
	authority.MutateCharacter(character, "dirty-before-ownership-transfer", func() {
		*character.Gold = *character.Gold + 1
	})
	authority.FailCommits(nil)

	if _, err := authority.TransferCharacterOwnership(
		domain.ReservedAccountID,
		"real-account",
	); err == nil || !strings.Contains(err.Error(), "pending uncommitted gameplay changes") {
		t.Fatalf("transfer error = %v", err)
	}
}
