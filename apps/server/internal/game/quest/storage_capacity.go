/*
===========================================================================

storage_capacity.go - the bag size announce for a live expansion

Native applies a paid inventory expansion to the running character when the
expansion completes (no world entry involved) and sends the client the new
size with 0x3092 [u8 kind][u8 capacity] (research/investigations/items/
native-audit-rows-20261031.md, row 26). The port client applies the announce
live the same way.

===========================================================================
*/
package quest

import (
	"opensro.online/server/internal/game/item/wire"
)

// OpStorageCapacity is the capacity announce.
const OpStorageCapacity uint16 = 0x3092

// storageCapacityInventory is 0x3092's kind byte for the bag; 2 is the
// companion-side storage, which no quest reward grows here.
const storageCapacityInventory = 1

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
