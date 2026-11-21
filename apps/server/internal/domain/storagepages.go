/*
===========================================================================

storagepages.go - the warehouse page rule for account and guild storage

Port-only, not native (owner decision 2026-11-21, after players asked for
more storage pages). Natively an account warehouse holds five pages of 30
(StorageDefaultCapacity) and a guild warehouse grows with the guild level
from one page at level 2 to four at level 5 (guildStorageCapacity).

StoragePages raises either one. Personal is the account warehouse's page
count; Guild is the level-5 guild warehouse's, and the lower levels keep
the native ladder's proportion of it, rounded up (eight pages gives 2, 4,
6, 8 at levels 2 to 5). Zero is native for each. Level 1 still has no
guild warehouse: the level test (GuildStorageMinLevel) is unchanged.

The client pages a warehouse of any capacity, but the list carries the
capacity, the row count and every slot in one byte each (0x3363, 0x321A
and the move requests), so eight pages (240 slots) is the most either
can hold. A raised account warehouse is persisted and never lowered, as
the raised pet bags are, so a lower setting never strands a row.

===========================================================================
*/
package domain

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// StoragePageSlots is one warehouse page (CIFStorageRoom, 0x1E a page).
	StoragePageSlots int64 = 30
	// MaxStoragePages keeps the capacity, row count and slots inside one
	// byte: 8 x 30 = 240 <= 255.
	MaxStoragePages uint8 = 8
	// nativeGuildStorageTopPages is the level-5 guild warehouse natively.
	nativeGuildStorageTopPages = 4
)

/*
================
StoragePages

The pages the account and guild warehouses are raised to; zero is native.
================
*/
type StoragePages struct {
	Personal uint8
	Guild    uint8
}

/*
================
ParseStoragePages

One setting's value: empty, "off" or "0" is native (0), a number from the
native count up to MaxStoragePages is that many pages. Anything else is an
error naming the setting, so a typo fails the boot instead of silently
running native.
================
*/
func ParseStoragePages(env, raw string, native uint8) (uint8, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "", "off", "false", "0":
		return 0, nil
	}
	pages, err := strconv.Atoi(value)
	if err != nil || pages < int(native) || pages > int(MaxStoragePages) {
		return 0, fmt.Errorf("%s=%q: want off or a page count from %d to %d", env, raw, native, MaxStoragePages)
	}
	return uint8(pages), nil
}

/*
================
NativeStoragePages

The account warehouse's native page count.
================
*/
func NativeStoragePages() uint8 {
	return uint8(StorageDefaultCapacity / StoragePageSlots)
}

/*
================
NativeGuildStoragePages

The level-5 guild warehouse's native page count.
================
*/
func NativeGuildStoragePages() uint8 {
	return nativeGuildStorageTopPages
}

/*
================
PersonalCapacity

The account warehouse's capacity: the stored one, raised to the setting's
pages and never lowered.
================
*/
func (p StoragePages) PersonalCapacity(stored int64) int64 {
	return max(stored, int64(min(p.Personal, MaxStoragePages))*StoragePageSlots)
}

/*
================
GuildCapacity

A guild level's warehouse: the native table, or with Guild set the native
pages scaled so level 5 has Guild pages, rounded up and never below the
native size.
================
*/
func (p StoragePages) GuildCapacity(level uint8) int64 {
	native := GuildStorageCapacity(level)
	if p.Guild == 0 || native == 0 {
		return native
	}
	nativePages := native / StoragePageSlots
	top := int64(min(p.Guild, MaxStoragePages))
	pages := (nativePages*top + nativeGuildStorageTopPages - 1) / nativeGuildStorageTopPages
	return max(native, pages*StoragePageSlots)
}
