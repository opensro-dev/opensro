/*
===========================================================================

ownership.go - ground-drop ownership release on the tick

Owns the one tick step that turns owner/party-reserved drops public at
the native 30-second boundary. The per-tick probe is the registry's own
read-only peek; the barrier and the commit door engage only when a
release is actually due.

===========================================================================
*/
package action

import (
	"time"

	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// ReleaseExpiredOwnership publishes the native 30-second transition from an
// owner/party-reserved drop to a public drop. It mutates every affected row in
// the same character-less authority commit and emits one 0x31E2 {gid} per row.
func (rt *Runtime) ReleaseExpiredOwnership(nowMs int64) []simulation.DivisionFrames {
	now := time.UnixMilli(nowMs)
	if !rt.hasExpiredOwner(now) {
		return nil
	}

	rt.maintenance.Lock()
	defer rt.maintenance.Unlock()

	var out []simulation.DivisionFrames
	rt.deps.MutateDeferred(nil, "ground-owner-release", func() {
		for _, divisionID := range rt.Ground.DivisionIDs() {
			released := rt.Ground.ReleaseExpiredOwners(divisionID, now, grounditem.OwnerLifetime)
			if len(released) == 0 {
				continue
			}

			for _, item := range released {
				out = append(out, simulation.DivisionFrames{DivisionID: divisionID, SourceGID: item.Gid, Frames: []simulation.Frame{{
					Opcode:  wire.OpGroundOwnershipExpired,
					Payload: wire.NewWriter(4).U32(item.Gid).Payload(),
				}}})
			}
		}
	})
	return out
}

/*
================
hasExpiredOwner

The tick's read-only probe: whether any reserved drop sits at or past
its owner lifetime. The registry answers from its stored stamps
under its own lock, so an idle tick pays no division list and no
snapshot walk (#2033); the release pass above re-checks every row
under the maintenance barrier before it publishes.
================
*/
func (rt *Runtime) hasExpiredOwner(now time.Time) bool {
	return rt.Ground.HasExpiredOwner(now, grounditem.OwnerLifetime)
}
