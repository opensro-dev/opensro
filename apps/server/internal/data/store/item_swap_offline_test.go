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
	t.Parallel()

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

/*
================
TestSwapItemsOfflineShardCoversEveryone

The shard run names nobody and still reaches every container: a character
pending deletion (it can be restored) and its account's warehouse, another
character's bag and its guild's storage. A refused row anywhere writes
nothing.
================
*/
func TestSwapItemsOfflineShardCoversEveryone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	sun := domain.InventoryRow{Slot: 13, RefObjID: 1, Codename: "SUN", StackCount: 3}
	stay := &domain.Character{Name: "Stay", MissionInventory: []domain.InventoryRow{sun}}
	leaving := &domain.Character{Name: "Leaving", MissionInventory: []domain.InventoryRow{sun}}
	for _, c := range []*domain.Character{stay, leaving} {
		if err := s.CreateCharacter(testDivision, "acct-"+c.Name, c); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.TransactStorage(leaving, func(_ *domain.Character, storage *domain.AccountStorage) error {
		row := sun
		row.Slot = 0
		storage.Rows = append(storage.Rows, row)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	guildRow := sun
	guildRow.Slot = 0
	if _, err := s.Guilds().CreateGuild(testDivision, domain.GuildRecord{Name: "Elsewhere", Level: 2,
		Storage: &domain.AccountStorage{Capacity: 30, Rows: []domain.InventoryRow{guildRow}}},
		domain.GuildMemberRecord{CharID: stay.ID, JID: uint32(stay.ID), Name: stay.Name}, stay); err != nil {
		t.Fatal(err)
	}

	s.Mutate("shard-swap-fixture", func() { leaving.DeletePending = true })
	s.Mutate("shard-swap-refusal", func() {
		stay.MissionInventory = append(stay.MissionInventory, domain.InventoryRow{Slot: 14, RefObjID: 3, Codename: "BAD", StackCount: 1})
	})
	if _, err := s.SwapItemsOfflineShard(testDivision, sunToMoon, true); err == nil {
		t.Fatal("an unmapped row was accepted")
	}
	if leaving.MissionInventory[0].Codename != "SUN" {
		t.Fatal("a refused run changed memory")
	}
	s.Mutate("shard-swap-refusal-undo", func() { stay.MissionInventory = stay.MissionInventory[:1] })

	preview, err := s.SwapItemsOfflineShard(testDivision, sunToMoon, false)
	if err != nil || len(preview) != 4 {
		t.Fatalf("preview %v %+v", err, preview)
	}
	applied, err := s.SwapItemsOfflineShard(testDivision, sunToMoon, true)
	if err != nil || len(applied) != 4 {
		t.Fatalf("apply %v %+v", err, applied)
	}
	s.Close()

	reopened := openTest(t, dir, newTestClock())
	for _, c := range reopened.Characters().CharactersForDivision(testDivision) {
		if got := c.MissionInventory[0]; got.Codename != "MOON" || got.StackCount != 3 {
			t.Fatalf("%s bag row %+v", c.Name, got)
		}
	}
	storage, err := readAccountStorage(reopened.syncDBLocked(), "acct-Leaving")
	if err != nil || len(storage.Rows) != 1 || storage.Rows[0].Codename != "MOON" {
		t.Fatalf("storage %v %+v", err, storage.Rows)
	}
	for _, guild := range reopened.guilds[testDivision] {
		if guild.Storage == nil || guild.Storage.Rows[0].Codename != "MOON" {
			t.Fatalf("guild storage %+v", guild.Storage)
		}
	}
}

/*
================
seedGround

Writes plane straight into ground_items, as a shard that stopped with
those drops on the ground left it; the next open loads them.
================
*/
func seedGround(t *testing.T, dir string, plane map[string][]domain.GroundItemRecord) {
	t.Helper()
	s := openTest(t, dir, newTestClock())
	tx, err := s.syncDBLocked().Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := replaceGroundTx(tx, &s.encodeScratch, plane, s.residues.ground); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	s.Close()
}

/*
================
TestSwapItemsOfflineShardCoversTheGround

A plain row on the ground, and one in a dropped pet's container, come back
at boot (ground_items keeps them with their time to live), so the shard run
swaps both; a named run leaves the ground alone, a refused ground row
writes nothing, and another division's drops are never touched.
================
*/
func TestSwapItemsOfflineShardCoversTheGround(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first := domain.GroundItemGIDBase + 1
	dropped := domain.GroundItemRecord{Gid: first, RefObjID: 1, Codename: "SUN", TypeFlags: 1, StackCount: 4, Plus: 3,
		VarianceBits: "77", Durability: 40, RegionID: 25000, DroppedBy: "Owner", DroppedAtMs: 1000}
	pet := domain.GroundItemRecord{Gid: first + 1, RefObjID: 70, Codename: "SCROLL", TypeFlags: 1, StackCount: 1, RegionID: 25000,
		Summon: &domain.CharacterCOS{RefObjID: 7, Container: &domain.COSContainer{Rows: []domain.InventoryRow{
			{Slot: 0, RefObjID: 1, Codename: "SUN", StackCount: 2},
		}}}}
	elsewhere := dropped
	seedGround(t, dir, map[string][]domain.GroundItemRecord{testDivision: {dropped, pet}, "other": {elsewhere}})

	s := openTest(t, dir, newTestClock())
	owner := &domain.Character{Name: "Owner"}
	if err := s.CreateCharacter(testDivision, "acct-Owner", owner); err != nil {
		t.Fatal(err)
	}
	if named, err := s.SwapItemsOffline(testDivision, []string{"Owner"}, sunToMoon, false); err != nil || len(named) != 0 {
		t.Fatalf("a named run reached the ground: %v %+v", err, named)
	}
	preview, err := s.SwapItemsOfflineShard(testDivision, sunToMoon, false)
	if err != nil || len(preview) != 2 {
		t.Fatalf("preview %v %+v", err, preview)
	}
	if s.ground.loadedRecords[testDivision][1].Summon.Container.Rows[0].Codename != "SUN" {
		t.Fatal("a preview changed the store's ground plane")
	}
	applied, err := s.SwapItemsOfflineShard(testDivision, sunToMoon, true)
	if err != nil || len(applied) != 2 {
		t.Fatalf("apply %v %+v", err, applied)
	}
	s.Close()

	reopened := openTest(t, dir, newTestClock())
	ground := reopened.ground.loadedRecords[testDivision]
	if len(ground) != 2 {
		t.Fatalf("ground %+v", ground)
	}
	got := ground[0]
	if got.Codename != "MOON" || got.RefObjID != 2 || got.StackCount != 4 || got.Plus != 3 || got.VarianceBits != "77" ||
		got.Durability != 40 || got.DroppedBy != "Owner" || got.DroppedAtMs != 1000 || got.Gid != first {
		t.Fatalf("ground row %+v", got)
	}
	if rows := ground[1].Summon.Container.Rows; ground[1].Codename != "SCROLL" || rows[0].Codename != "MOON" || rows[0].StackCount != 2 {
		t.Fatalf("dropped pet %+v", ground[1])
	}
	if other := reopened.ground.loadedRecords["other"]; len(other) != 1 || other[0].Codename != "SUN" {
		t.Fatalf("another division's ground was swapped: %+v", other)
	}
	reopened.Close()

	refused := domain.GroundItemRecord{Gid: first + 2, RefObjID: 3, Codename: "BAD", TypeFlags: 1, StackCount: 1, RegionID: 25000}
	seedGround(t, dir, map[string][]domain.GroundItemRecord{testDivision: {dropped, refused}})
	again := openTest(t, dir, newTestClock())
	if _, err := again.SwapItemsOfflineShard(testDivision, sunToMoon, true); err == nil {
		t.Fatal("an unmapped ground row was accepted")
	}
	if again.ground.loadedRecords[testDivision][0].Codename != "SUN" {
		t.Fatal("a refused run changed the ground plane")
	}
}

/*
================
TestSwapItemsOfflineCoversDormantCompanions

An unsummoned pet's record lives in its summoner row; its container is
swapped too. An active pet whose record is also seated in its summoner row
(a restore adoption) is one record: its row is swapped and counted once.
================
*/
func TestSwapItemsOfflineCoversDormantCompanions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	sun := domain.InventoryRow{Slot: 0, RefObjID: 1, Codename: "SUN", StackCount: 1}
	owner := &domain.Character{Name: "Owner"}
	if err := s.CreateCharacter(testDivision, "acct-Owner", owner); err != nil {
		t.Fatal(err)
	}
	s.Mutate("swap-dormant-fixture", func() {
		dormant := &domain.CharacterCOS{RefObjID: 7, Container: &domain.COSContainer{Rows: []domain.InventoryRow{sun}}}
		active := &domain.CharacterCOS{RefObjID: 8, GID: 900, Container: &domain.COSContainer{Rows: []domain.InventoryRow{sun}}}
		owner.ActiveCOS = active
		owner.MissionInventory = []domain.InventoryRow{
			{Slot: 13, RefObjID: 70, Codename: "SCROLL_DORMANT", StackCount: 1, Summon: dormant},
			{Slot: 14, RefObjID: 80, Codename: "SCROLL_ACTIVE", StackCount: 1, Summon: active},
		}
	})
	preview, err := s.SwapItemsOffline(testDivision, []string{"Owner"}, sunToMoon, false)
	if err != nil || len(preview) != 2 {
		t.Fatalf("preview %v %+v", err, preview)
	}
	if _, err := s.SwapItemsOffline(testDivision, []string{"Owner"}, sunToMoon, true); err != nil {
		t.Fatal(err)
	}
	s.Close()

	reopened := openTest(t, dir, newTestClock())
	for _, c := range reopened.Characters().CharactersForDivision(testDivision) {
		if c.Name != "Owner" {
			continue
		}
		for _, pet := range c.Companions() {
			if pet.Container == nil || pet.Container.Rows[0].Codename != "MOON" {
				t.Fatalf("companion %d not swapped: %+v", pet.RefObjID, pet.Container)
			}
		}
		return
	}
	t.Fatal("owner not reloaded")
}

/*
================
TestSwapItemsOfflineCoversThePendingSookChest

A survival arena chest still pending on the character (rolled before a
full bag or a disconnect) holds its rows until they are claimed: the
preview counts its row, and -apply rewrites it and survives a reopen.
================
*/
func TestSwapItemsOfflineCoversThePendingSookChest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	owner := &domain.Character{Name: "Chested"}
	if err := s.CreateCharacter(testDivision, "acct-chested", owner); err != nil {
		t.Fatal(err)
	}
	s.Mutate("swap-chest-fixture", func() {
		owner.SookChest = &domain.ArenaChest{ID: "run-1", Pending: true,
			Items: []domain.InventoryRow{{Slot: 0, RefObjID: 1, Codename: "SUN", StackCount: 3}}}
	})
	preview, err := s.SwapItemsOffline(testDivision, []string{"Chested"}, sunToMoon, false)
	if err != nil || len(preview) != 1 || preview[0].Container != "sook chest" {
		t.Fatalf("preview %v %+v", err, preview)
	}
	if owner.SookChest.Items[0].Codename != "SUN" {
		t.Fatal("a preview changed memory")
	}
	if applied, err := s.SwapItemsOffline(testDivision, []string{"Chested"}, sunToMoon, true); err != nil || len(applied) != 1 {
		t.Fatalf("apply %v %+v", err, applied)
	}
	s.Close()

	reopened := openTest(t, dir, newTestClock())
	for _, c := range reopened.Characters().CharactersForDivision(testDivision) {
		if c.Name != "Chested" {
			continue
		}
		if c.SookChest == nil || c.SookChest.Items[0].Codename != "MOON" || c.SookChest.Items[0].StackCount != 3 {
			t.Fatalf("chest %+v", c.SookChest)
		}
		return
	}
	t.Fatal("character not reloaded")
}
