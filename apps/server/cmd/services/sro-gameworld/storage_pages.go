/*
===========================================================================

storage_pages.go - the warehouse page settings, read once at boot

Port-only, not native (owner decision 2026-11-21): SRO_STORAGE_PAGES sizes
the account warehouse and SRO_GUILD_STORAGE_PAGES the level-5 guild
warehouse, the lower guild levels scaled from it (domain/storagepages.go
owns the rule). Unset or "off" is native. The store applies them
(store.Options StoragePages); the public rules page reads the same values.

===========================================================================
*/
package main

import (
	"fmt"
	"os"

	"opensro.online/server/internal/domain"
)

const (
	// EnvStoragePages is the account warehouse's page count (5 native, up to 8).
	EnvStoragePages = "SRO_STORAGE_PAGES"
	// EnvGuildStoragePages is the level-5 guild warehouse's page count (4 native, up to 8).
	EnvGuildStoragePages = "SRO_GUILD_STORAGE_PAGES"
)

/*
================
storagePagesFromEnv

Both settings; an unreadable value is a boot error naming it.
================
*/
func storagePagesFromEnv() (domain.StoragePages, error) {
	personal, err := domain.ParseStoragePages(EnvStoragePages, os.Getenv(EnvStoragePages), domain.NativeStoragePages())
	if err != nil {
		return domain.StoragePages{}, err
	}
	guild, err := domain.ParseStoragePages(EnvGuildStoragePages, os.Getenv(EnvGuildStoragePages), domain.NativeGuildStoragePages())
	if err != nil {
		return domain.StoragePages{}, err
	}
	return domain.StoragePages{Personal: personal, Guild: guild}, nil
}

/*
================
storagePagesNow

The public rules page's line for a raised warehouse.
================
*/
func storagePagesNow(what string, pages uint8) string {
	return fmt.Sprintf("%s holds %d pages (%d slots).", what, pages, int64(pages)*domain.StoragePageSlots)
}

/*
================
guildStoragePagesNow

The public rules page's line for raised guild storage: its pages at
each level that has a warehouse.
================
*/
func guildStoragePagesNow(pages domain.StoragePages) string {
	at := func(level uint8) int64 { return pages.GuildCapacity(level) / domain.StoragePageSlots }
	return fmt.Sprintf("Guild storage opens at guild level 2 and grows to %d pages at level 5 (%d, %d, %d and %d pages at levels 2 to 5).",
		at(5), at(2), at(3), at(4), at(5))
}
