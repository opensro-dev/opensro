/*
===========================================================================

item_swap_offline_test.go - tests for item_swap_offline.go

A store on disk holds a "SUN" row in each container the door covers: the
bag, a companion, the account's storage and the guild's storage. A preview
writes nothing; -apply rewrites every one and survives a reopen; a refused
row writes nothing at all; another character is never touched.

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
sunToMoon

The test's swap: SUN rows become MOON, keeping everything else; a BAD row
refuses the run.
================
*/
func sunToMoon(row domain.InventoryRow) (domain.InventoryRow, bool, error) {
	switch row.Codename {
	case "SUN":
		row.Codename, row.RefObjID = "MOON", 2
		return row, true, nil
	case "BAD":
		return row, false, errors.New("unmapped")
	}
	return row, false, nil
}

/*
================
TestSwapItemsOfflineCoversEveryContainer
================
*/
func TestSwapItemsOfflineCoversEveryContainer(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	sun := domain.InventoryRow{Slot: 13, RefObjID: 1, Codename: "SUN", Plus: 7, VarianceBits: "12345", Durability: 99, StackCount: 1, MagicOptions: []uint64{4<<32 | 50}}
	owner := &domain.Character{Name: "Owner", MissionInventory: []domain.InventoryRow{sun}}
	other := &domain.Character{Name: "Other", MissionInventory: []domain.InventoryRow{sun}}
	for _, c := range []*domain.Character{owner, other} {
		if err := s.CreateCharacter(testDivision, "acct-"+c.Name, c); err != nil {
			t.Fatal(err)
		}
	}
	s.Mutate("swap-fixture", func() {
		bagRow := sun
		bagRow.Slot = 0
		owner.ActiveCOS = &domain.CharacterCOS{RefObjID: 7, Container: &domain.COSContainer{Rows: []domain.InventoryRow{bagRow}}}
	})
	if _, err := s.TransactStorage(owner, func(_ *domain.Character, storage *domain.AccountStorage) error {
		row := sun
		row.Slot = 0
		storage.Rows = append(storage.Rows, row)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	guildRow := sun
	guildRow.Slot = 0
	if _, err := s.Guilds().CreateGuild(testDivision, domain.GuildRecord{Name: "Swappers", Level: 2,
		Storage: &domain.AccountStorage{Capacity: 30, Rows: []domain.InventoryRow{guildRow}}},
		domain.GuildMemberRecord{CharID: owner.ID, JID: uint32(owner.ID), Name: owner.Name}, owner); err != nil {
		t.Fatal(err)
	}

	preview, err := s.SwapItemsOffline(testDivision, []string{"Owner"}, sunToMoon, false)
	if err != nil || len(preview) != 4 {
		t.Fatalf("preview %v %+v", err, preview)
	}
	if owner.MissionInventory[0].Codename != "SUN" {
		t.Fatal("a preview changed memory")
	}
	s.Mutate("swap-refusal", func() {
		owner.MissionInventory = append(owner.MissionInventory, domain.InventoryRow{Slot: 14, RefObjID: 3, Codename: "BAD", StackCount: 1})
	})
	if _, err := s.SwapItemsOffline(testDivision, []string{"Owner"}, sunToMoon, true); err == nil {
		t.Fatal("an unmapped row was accepted")
	}
	s.Mutate("swap-refusal-undo", func() { owner.MissionInventory = owner.MissionInventory[:1] })
	if _, err := s.SwapItemsOffline(testDivision, []string{"Nobody"}, sunToMoon, true); err == nil {
		t.Fatal("an unknown character was accepted")
	}
	applied, err := s.SwapItemsOffline(testDivision, []string{"Owner"}, sunToMoon, true)
	if err != nil || len(applied) != 4 {
		t.Fatalf("apply %v %+v", err, applied)
	}
	s.Close()

	reopened := openTest(t, dir, newTestClock())
	var loadedOwner, loadedOther *domain.Character
	for _, c := range reopened.Characters().CharactersForDivision(testDivision) {
		switch c.Name {
		case "Owner":
			loadedOwner = c
		case "Other":
			loadedOther = c
		}
	}
	got := loadedOwner.MissionInventory[0]
	if got.Codename != "MOON" || got.RefObjID != 2 || got.Plus != 7 || got.VarianceBits != "12345" || got.Durability != 99 ||
		len(got.MagicOptions) != 1 || got.MagicOptions[0] != 4<<32|50 || got.Slot != 13 {
		t.Fatalf("bag row %+v", got)
	}
	if loadedOwner.ActiveCOS == nil || loadedOwner.ActiveCOS.Container.Rows[0].Codename != "MOON" {
		t.Fatal("companion row not swapped")
	}
	storage, err := reopened.AccountStorage(loadedOwner)
	if err != nil || storage.Rows[0].Codename != "MOON" {
		t.Fatalf("storage %v %+v", err, storage.Rows)
	}
	guild := reopened.guilds[testDivision][*loadedOwner.GuildID]
	if guild.Storage == nil || guild.Storage.Rows[0].Codename != "MOON" {
		t.Fatalf("guild storage %+v", guild.Storage)
	}
	if loadedOther.MissionInventory[0].Codename != "SUN" {
		t.Fatal("another character was swapped")
	}
}
