/*
===========================================================================
vitals_mp.go - MP-only refreshes without changing the HP reconciliation baseline
===========================================================================
*/
package simulation

import "opensro.online/server/internal/game/item/wire"

/*
================
MPRefreshPayload

The native 33A6 mask 2 carries only MP. Combat redirection publishes the
remaining MP independently of the damage record that owns the HP change.
================
*/
func MPRefreshPayload(objectID uint32, sourceFlags VitalsSourceFlags, currentMP uint32) []byte {
	const mpUpdateMask = 2
	return wire.NewWriter(11).U32(objectID).U16(uint16(sourceFlags)).U8(mpUpdateMask).U32(currentMP).Payload()
}
