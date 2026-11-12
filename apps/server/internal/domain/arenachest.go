/*
===========================================================================
arenachest.go - port-only, durable survival rewards, separate from carried gear.
===========================================================================
*/
package domain

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
