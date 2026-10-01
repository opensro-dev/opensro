/*
===========================================================================

storage_test.go - the account warehouse: sharing, atomicity, durability

===========================================================================
*/
package store

import (
	"errors"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestStorageTransferIsAtomicAndShared
================
*/
func TestStorageTransferIsAtomicAndShared(t *testing.T) {
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	first := seededCharacter()
	second := seededCharacter()
	second.Name = "StoreSib"
	for _, character := range []*domain.Character{first, second} {
		if err := s.CreateCharacter(testDivision, "storage-account", character); err != nil {
			t.Fatal(err)
		}
	}
	empty, err := s.AccountStorage(first)
	if err != nil || empty.Capacity != domain.StorageDefaultCapacity || len(empty.Rows) != 0 || empty.Gold != 0 {
		t.Fatalf("new account warehouse = %+v, %v", empty, err)
	}
	gold := int64(500)
	first.Gold = &gold
	deposit := func(next *domain.Character, storage *domain.AccountStorage) error {
		row := next.MissionInventory[0]
		row.Slot = 4
		storage.Rows = append(storage.Rows, row)
		next.MissionInventory = next.MissionInventory[1:]
		remaining := *next.Gold - 100
		next.Gold = &remaining
		storage.Gold += 100
		return nil
	}
	before := len(first.MissionInventory)

	if _, err := s.TransactStorage(first, func(*domain.Character, *domain.AccountStorage) error {
		return errors.New("refused")
	}); err == nil {
		t.Fatal("accepted a refused transfer")
	}
	s.FailCommits(errors.New("disk failure"))
	if _, err := s.TransactStorage(first, deposit); err == nil {
		t.Fatal("accepted a failed durable commit")
	}
	s.FailCommits(nil)
	if len(first.MissionInventory) != before || *first.Gold != 500 {
		t.Fatalf("a failed transfer changed the character: %d rows, %d gold", len(first.MissionInventory), *first.Gold)
	}

	stored, err := s.TransactStorage(first, deposit)
	if err != nil || len(stored.Rows) != 1 || stored.Gold != 100 || *first.Gold != 400 || len(first.MissionInventory) != before-1 {
		t.Fatalf("deposit = %+v, %v; gold %d", stored, err, *first.Gold)
	}
	shared, err := s.AccountStorage(second)
	if err != nil || len(shared.Rows) != 1 || shared.Gold != 100 {
		t.Fatalf("sibling does not share the warehouse: %+v %v", shared, err)
	}
	if _, err := s.TransactStorage(first, func(next *domain.Character, storage *domain.AccountStorage) error {
		storage.Rows = append(storage.Rows, storage.Rows[0])
		return nil
	}); err == nil {
		t.Fatal("accepted two rows in one slot")
	}

	s.Close()
	reopened := openTest(t, dir, clock)
	for _, character := range reopened.Characters().CharactersForDivision(testDivision) {
		storage, err := reopened.AccountStorage(character)
		if err != nil || len(storage.Rows) != 1 || storage.Rows[0].Slot != 4 || storage.Gold != 100 {
			t.Fatalf("warehouse did not survive reopen: %+v %v", storage, err)
		}
	}
}
