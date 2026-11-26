/*
===========================================================================

cubestorage.go - top-level Magic Cube storage packing for the store

InventoryRow.LinkedStorage is json:"-" so adding Magic Cube storage adds
no nested key inside existing item arrays (#3090). This file packs and
unpacks each record's top-level cube storage slice on write and load:
Character.CubeStorages, accountStorageRecord.CubeStorages, and
GuildRecord.StorageCubeStorages. GroundItemRecord.LinkedStorage is
already top-level on its own record.

===========================================================================
*/
package store

import "opensro.online/server/internal/domain"

/*
================
accountStorageRecord

The persisted account_storage row: domain.AccountStorage plus its
top-level cube storages. AccountStorage itself stays unchanged so
GuildRecord.Storage gains no nested key.
================
*/
type accountStorageRecord struct {
	domain.AccountStorage
	CubeStorages []domain.SlotCubeStorage `json:"cubeStorages,omitempty"`
}

/*
================
packCharacterForStore

Returns a character ready to marshal with top-level CubeStorages. When
the character already carries CubeStorages directly or owns no cube with
linked storage, returns c untouched with zero allocations.
================
*/
func packCharacterForStore(c *domain.Character) *domain.Character {
	if c == nil || len(c.CubeStorages) > 0 {
		return c
	}
	entries := packCharacterCubeStorages(c)
	if len(entries) == 0 {
		return c
	}
	packed := *c
	packed.CubeStorages = entries
	return &packed
}

/*
================
packCharacterCubeStorages

Collects every non-nil LinkedStorage from the character's bag, active
companion bag, and dormant companion bags.
================
*/
func packCharacterCubeStorages(c *domain.Character) []domain.CharacterCubeStorage {
	var out []domain.CharacterCubeStorage
	for _, row := range c.MissionInventory {
		if row.LinkedStorage != nil {
			out = append(out, domain.CharacterCubeStorage{
				Container: domain.CubeContainerBag,
				Slot:      row.Slot,
				Storage:   row.LinkedStorage,
			})
		}
		if row.Summon != nil && row.Summon.Container != nil {
			for _, petRow := range row.Summon.Container.Rows {
				if petRow.LinkedStorage != nil {
					out = append(out, domain.CharacterCubeStorage{
						Container: domain.CubeContainerCOS,
						OwnerSlot: row.Slot,
						Slot:      petRow.Slot,
						Storage:   petRow.LinkedStorage,
					})
				}
			}
		}
	}
	if c.ActiveCOS != nil && c.ActiveCOS.Container != nil {
		for _, petRow := range c.ActiveCOS.Container.Rows {
			if petRow.LinkedStorage != nil {
				out = append(out, domain.CharacterCubeStorage{
					Container: domain.CubeContainerActiveCOS,
					Slot:      petRow.Slot,
					Storage:   petRow.LinkedStorage,
				})
			}
		}
	}
	return out
}

/*
================
unpackCharacterCubeStorages

Restores top-level CubeStorages back onto their owning InventoryRows and
clears the transport slice so InventoryRow.LinkedStorage stays the sole
live owner.
================
*/
func unpackCharacterCubeStorages(c *domain.Character) {
	if c == nil || len(c.CubeStorages) == 0 {
		return
	}
	for _, entry := range c.CubeStorages {
		if entry.Storage == nil {
			continue
		}
		switch entry.Container {
		case domain.CubeContainerBag:
			attachLinkedStorage(c.MissionInventory, entry.Slot, entry.Storage)
		case domain.CubeContainerActiveCOS:
			if c.ActiveCOS != nil && c.ActiveCOS.Container != nil {
				attachLinkedStorage(c.ActiveCOS.Container.Rows, entry.Slot, entry.Storage)
			}
		case domain.CubeContainerCOS:
			for i := range c.MissionInventory {
				if c.MissionInventory[i].Slot == entry.OwnerSlot &&
					c.MissionInventory[i].Summon != nil &&
					c.MissionInventory[i].Summon.Container != nil {
					attachLinkedStorage(c.MissionInventory[i].Summon.Container.Rows, entry.Slot, entry.Storage)
					break
				}
			}
		}
	}
	c.CubeStorages = nil
}

/*
================
packSlotCubeStorages
================
*/
func packSlotCubeStorages(rows []domain.InventoryRow) []domain.SlotCubeStorage {
	var out []domain.SlotCubeStorage
	for _, row := range rows {
		if row.LinkedStorage != nil {
			out = append(out, domain.SlotCubeStorage{
				Slot:    row.Slot,
				Storage: row.LinkedStorage,
			})
		}
	}
	return out
}

/*
================
unpackSlotCubeStorages
================
*/
func unpackSlotCubeStorages(rows []domain.InventoryRow, entries []domain.SlotCubeStorage) {
	for _, entry := range entries {
		if entry.Storage != nil {
			attachLinkedStorage(rows, entry.Slot, entry.Storage)
		}
	}
}

/*
================
attachLinkedStorage
================
*/
func attachLinkedStorage(rows []domain.InventoryRow, slot int64, storage *domain.COSContainer) {
	for i := range rows {
		if rows[i].Slot == slot {
			rows[i].LinkedStorage = storage
			return
		}
	}
}

/*
================
packAccountStorage
================
*/
func packAccountStorage(storage domain.AccountStorage) accountStorageRecord {
	return accountStorageRecord{
		AccountStorage: storage,
		CubeStorages:   packSlotCubeStorages(storage.Rows),
	}
}

/*
================
unpackAccountStorage
================
*/
func unpackAccountStorage(record accountStorageRecord) domain.AccountStorage {
	storage := record.AccountStorage
	unpackSlotCubeStorages(storage.Rows, record.CubeStorages)
	return storage
}

/*
================
guildRecordStored

The persisted guilds row: domain.GuildRecord plus its top-level cube
storages. GuildRecord itself stays comparable and gains no slice field.
================
*/
type guildRecordStored struct {
	domain.GuildRecord
	StorageCubeStorages []domain.SlotCubeStorage `json:"storageCubeStorages,omitempty"`
}

/*
================
packGuildForStore
================
*/
func packGuildForStore(guild domain.GuildRecord) guildRecordStored {
	var entries []domain.SlotCubeStorage
	if guild.Storage != nil {
		entries = packSlotCubeStorages(guild.Storage.Rows)
	}
	return guildRecordStored{
		GuildRecord:         guild,
		StorageCubeStorages: entries,
	}
}

/*
================
unpackGuildRecord
================
*/
func unpackGuildRecord(stored guildRecordStored) domain.GuildRecord {
	guild := stored.GuildRecord
	if guild.Storage != nil && len(stored.StorageCubeStorages) > 0 {
		unpackSlotCubeStorages(guild.Storage.Rows, stored.StorageCubeStorages)
	}
	return guild
}
