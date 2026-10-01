/*
===========================================================================

companion_persistence_test.go - durable item-owned pets and warehouse rollback

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
TestCompanionWarehouseCommitRollbackAndDatabaseReopen
================
*/
func TestCompanionWarehouseCommitRollbackAndDatabaseReopen(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	c := seededCharacter()
	c.MissionInventory = []domain.InventoryRow{{Slot: 13, RefObjID: 900, Codename: "SUMMONER", TypeFlags: 0x8cc, StackCount: 1,
		Summon: &domain.CharacterCOS{RefObjID: 950, Codename: "PET", Name: "Saved", Level: 5, Experience: 123456, CurrentHP: 42, StateFlags: 1, Satiety: 7890,
			Rentals:   []domain.COSRental{{Kind: 5, ID: 77, ExpiresAtUnix: 123456789, Tag: 9}},
			Container: &domain.COSContainer{Capacity: 28, Rows: []domain.InventoryRow{{Slot: 0, RefObjID: 17, StackCount: 3}}}}}}
	if err := s.CreateCharacter(testDivision, "pet-account", c); err != nil {
		t.Fatal(err)
	}
	deposit := func(next *domain.Character, storage *domain.AccountStorage) error {
		row := next.MissionInventory[0]
		row.Slot = 0
		row.Summon.Name = "Stored"
		storage.Rows = append(storage.Rows, row)
		next.MissionInventory = nil
		return nil
	}
	s.FailCommits(errors.New("disk failure"))
	if _, err := s.TransactStorage(c, deposit); err == nil {
		t.Fatal("failed commit accepted")
	}
	s.FailCommits(nil)
	if c.MissionInventory[0].Summon.Name != "Saved" {
		t.Fatal("failed transaction mutated live pet")
	}
	if _, err := s.TransactStorage(c, deposit); err != nil {
		t.Fatal(err)
	}
	s.Close()
	reopened := openTest(t, dir, clock)
	characters := reopened.Characters().CharactersForDivision(testDivision)
	if len(characters) != 1 {
		t.Fatal("character missing after reopen")
	}
	c = characters[0]
	stored, err := reopened.AccountStorage(c)
	if err != nil || len(stored.Rows) != 1 || len(c.MissionInventory) != 0 {
		t.Fatal("item ownership duplicated or lost", err)
	}
	pet := stored.Rows[0].Summon
	if pet == nil || pet.Name != "Stored" || pet.Experience != 123456 || pet.CurrentHP != 42 || pet.Satiety != 7890 || pet.Container.Rows[0].StackCount != 3 || pet.Rentals[0].ExpiresAtUnix != 123456789 {
		t.Fatal("database lost retained companion fields", pet)
	}
	if _, err := reopened.TransactStorage(c, func(next *domain.Character, storage *domain.AccountStorage) error {
		row := storage.Rows[0]
		row.Slot = 14
		next.MissionInventory = append(next.MissionInventory, row)
		storage.Rows = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if c.MissionInventory[0].Summon.Experience != 123456 {
		t.Fatal("withdraw lost companion")
	}
}
