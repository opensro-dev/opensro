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

func (rt *Runtime) hasExpiredOwner(now time.Time) bool {
	for _, divisionID := range rt.Ground.DivisionIDs() {
		for _, item := range rt.groundDivisionSnapshot(divisionID).items {
			if item.OwnerJID != 0 && !item.DroppedAt.IsZero() &&
				now.Sub(item.DroppedAt) >= grounditem.OwnerLifetime {
				return true
			}
		}
	}
	return false
}
