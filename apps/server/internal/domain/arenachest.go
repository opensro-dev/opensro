/*
===========================================================================
arenachest.go - port-only, durable survival rewards, separate from carried gear.

Also the account's daily survival entry count (SookArenaAccount): the
three entries a UTC day are counted per account, not per character, so
an alt adds no entries (#3322).
===========================================================================
*/
package domain

// SookArenaDailyEntries is how many survival arena entries one account
// may spend per UTC day (port-only, not native: SRO_SOOK_SURVIVAL_ARENA).
const SookArenaDailyEntries = 3

/*
================
ArenaChest - one run's rolled items; Pending survives a full bag and recovery.
================
*/
type ArenaChest struct {
	ID         string         `json:"id"`
	Stage      int            `json:"stage"`
	Kills      int            `json:"kills"`
	ElapsedMs  int64          `json:"elapsedMs"`
	Checkpoint bool           `json:"checkpoint"`
	Pending    bool           `json:"pending"`
	GoldPaid   bool           `json:"goldPaid"`
	Items      []InventoryRow `json:"items"`
}

/*
==================
SookArenaAccount

One account's survival arena entries for one UTC day (port-only, not
native: SRO_SOOK_SURVIVAL_ARENA, #3322; schema 43). Every character of
the account enters against this one record, kept in the store's
metadata plane and mutated only inside the entry's door, so two
characters of one account cannot both take the last entry. Day is the
UTC day number (sookarena.Day) Entries belongs to. An account with no
record has entered nothing; at first load the record is seeded from
the characters' schema 42 per-character counts (Character.SookArenaDay
and SookArenaEntries), which nothing reads afterwards.
==================
*/
type SookArenaAccount struct {
	Day     int32 `json:"day"`
	Entries uint8 `json:"entries"`
}

/*
================
cloneArenaChest - snapshots and rollback must not alias the pending item rows.
================
*/
func cloneArenaChest(source *ArenaChest) *ArenaChest {
	if source == nil {
		return nil
	}
	clone := *source
	clone.Items = cloneInventoryRows(source.Items)
	return &clone
}
