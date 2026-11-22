/*
===========================================================================

inventorysort.go - the sort button of the bag, the warehouses and pet bags

Port-only, not native (the owner's choice, 2026-11-22: off by default,
switched on per shard). With SRO_INVENTORY_SORT on, the GameWorld
publishes the rule at world entry and the client puts a sort button
beside the close button of the inventory, the warehouse (personal and
guild) and a companion's bag. The button only plans: it sends ordinary
item moves (0x706D types 0x00, 0x01, 0x1D and 0x10) one at a time, and
the server validates each exactly as it does a drag, so this rule adds
no server path. Off, the default here, is native: no button.

===========================================================================
*/
package action

import (
	"os"
	"strings"
)

// EnvInventorySort turns the sort button on ("on", "1", "true").
const EnvInventorySort = "SRO_INVENTORY_SORT"

/*
================
InventorySortFromEnv
================
*/
func InventorySortFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvInventorySort))) {
	case "on", "1", "true":
		return true
	}
	return false
}
