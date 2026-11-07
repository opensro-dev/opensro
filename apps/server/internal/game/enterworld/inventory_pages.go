/*
===========================================================================

inventory_pages.go - the 2.5-page inventory base (port-only, not native)

#2769 (owner, 2026-11-07): the expansion quests are "not worth it for half
a page", so a character starts with 2.5 pages (domain.InventoryPagesBase)
and the quests still take it to the v1.188 client's three-page maximum.
The switch is on by default; "off" is native. Nothing persists the
choice: a record's InventorySize is its capacity under either setting,
so switching off never takes slots (or the items in them) away - it only
stops raising new and entering characters.

===========================================================================
*/
package enterworld

import (
	"os"
	"strings"
)

// EnvInventoryPages turns the 2.5-page inventory base off ("off", "0" or
// "false"); unset or anything else keeps the owner's on.
const EnvInventoryPages = "SRO_INVENTORY_PAGES"

/*
================
InventoryPagesFromEnv

The rule as the environment states it. Read once at wiring and published
through Deps and the store's creation options, so one process answers one
way everywhere.
================
*/
func InventoryPagesFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvInventoryPages))) {
	case "off", "0", "false":
		return false
	}
	return true
}
