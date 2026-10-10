/*
===========================================================================

instant_expansion.go - the bag grows at the quest turn-in (port-only)

Port-only, not native. The v1.150 client learns its bag size only at world
entry (CICPlayer+0x1848, written by the entry parser 8675F0), so natively a
QSP_*_EXINVENTORY reward waits for the next login, teleport or resurrection.
Players read that as a lost reward. With SRO_INSTANT_INVENTORY_EXPANSION on,
the turn-in presents the slots at once and announces the new capacity with
the v1.188 message 0x3092 [u8 kind][u8 capacity] (4E73D0 case 0x11, after
CGStorage_SetCapacity 4B7D80), which the port client applies live. Off (the
default) keeps the native wait. The owner approved it on for the beta.

===========================================================================
*/
package quest

import (
	"os"
	"strings"

	"opensro.online/server/internal/game/item/wire"
)

// EnvInstantInventoryExpansion turns the immediate bag growth on ("on", "1", "true").
const EnvInstantInventoryExpansion = "SRO_INSTANT_INVENTORY_EXPANSION"

// OpStorageCapacity is the v1.188 capacity announce (4E73D0 case 0x11).
const OpStorageCapacity uint16 = 0x3092

// storageCapacityInventory is 0x3092's kind byte for the bag; 2 is the chest.
const storageCapacityInventory = 1

/*
================
InstantInventoryExpansionFromEnv

Reads the port-only flag. Anything but an explicit on keeps native.
================
*/
func InstantInventoryExpansionFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvInstantInventoryExpansion))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
storageCapacityFrame

The bag's capacity byte as 0x3092 carries it: sockets and bag together, the
same count the 0x32B3 entry block ships.
================
*/
func storageCapacityFrame(capacity uint8) wire.Frame {
	return wire.Frame{Opcode: OpStorageCapacity, Payload: []byte{storageCapacityInventory, capacity}}
}
